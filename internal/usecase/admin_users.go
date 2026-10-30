package usecase

import (
	"context"
	"time"

	"github.com/google/uuid"

	"photo-annotated/internal/domain"
)

// AdminUsers mengelola annotator (spec-labels-admin §5.8; hanya auth.mode = otp): daftar, blokir,
// buka blokir, dan hapus data.
type AdminUsers struct {
	repo     AdminUserRepo
	sessions SessionRepo
	deletion DeletionRepo
	tx       TxManager
	clock    Clock
	// afterDataDeleted dipanggil setelah hapus data annotator (menjalankan cleanup segera); opsional.
	afterDataDeleted func()
}

func NewAdminUsers(repo AdminUserRepo, sessions SessionRepo, deletion DeletionRepo, tx TxManager, clock Clock, afterDataDeleted func()) *AdminUsers {
	return &AdminUsers{repo: repo, sessions: sessions, deletion: deletion, tx: tx, clock: clock, afterDataDeleted: afterDataDeleted}
}

// AdminUserList: Next nil bila tidak ada halaman berikutnya.
type AdminUserList struct {
	Items []AdminUserRow
	Next  *AdminCursor
}

// List mengembalikan satu halaman annotator.
func (u *AdminUsers) List(ctx context.Context, q AdminUserQuery) (*AdminUserList, error) {
	if q.Limit == 0 {
		q.Limit = AdminListDefaultLimit
	}
	if q.Limit < 1 || q.Limit > AdminListMaxLimit {
		return nil, domain.Invalid("INVALID_QUERY", "limit must be between 1 and %d", AdminListMaxLimit)
	}
	want := q.Limit
	q.Limit++
	rows, err := u.repo.ListUsers(ctx, q)
	if err != nil {
		return nil, err
	}
	res := &AdminUserList{Items: rows}
	if len(rows) > want {
		res.Items = rows[:want]
		last := res.Items[want-1].User
		res.Next = &AdminCursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	return res, nil
}

// Get mengembalikan satu annotator aktif; selain itu NOT_FOUND.
func (u *AdminUsers) Get(ctx context.Context, id uuid.UUID) (*AdminUserRow, error) {
	return u.repo.GetUser(ctx, id)
}

// BlockResult: Changed false bila user memang sudah dalam keadaan itu (tetap 200).
type BlockResult struct {
	User    *AdminUserRow
	Changed bool
}

// Block memblokir annotator dan mencabut semua sesinya dalam satu transaksi. Data user tidak berubah.
func (u *AdminUsers) Block(ctx context.Context, id uuid.UUID, reason string) (*BlockResult, error) {
	r, err := domain.NormalizeReason(reason, "INVALID_BLOCK_REASON")
	if err != nil {
		return nil, err
	}
	now := u.clock.Now().UTC().Truncate(time.Microsecond)
	var changed bool
	err = u.tx.WithTx(ctx, func(ctx context.Context) error {
		var err error
		if changed, err = u.repo.Block(ctx, id, r, now); err != nil {
			return err
		}
		// token lama langsung 401: lookup sesi juga menolak pengguna yang diblokir
		return u.sessions.RevokeAllForUser(ctx, id, now)
	})
	if err != nil {
		return nil, err
	}
	row, err := u.repo.GetUser(ctx, id)
	if err != nil {
		return nil, err
	}
	return &BlockResult{User: row, Changed: changed}, nil
}

// SetGroup memindahkan annotator ke kelompok aktif lain (spec-pendataan §5.5).
func (u *AdminUsers) SetGroup(ctx context.Context, id uuid.UUID, groupID int) (*AdminUserRow, error) {
	if groupID < 1 {
		return nil, ErrInvalidGroup
	}
	if err := u.repo.SetGroup(ctx, id, groupID); err != nil {
		return nil, err
	}
	return u.repo.GetUser(ctx, id)
}

// Unblock membuka blokir; user login lagi dengan password lamanya (sesi lama tetap dicabut).
func (u *AdminUsers) Unblock(ctx context.Context, id uuid.UUID) (*BlockResult, error) {
	changed, err := u.repo.Unblock(ctx, id)
	if err != nil {
		return nil, err
	}
	row, err := u.repo.GetUser(ctx, id)
	if err != nil {
		return nil, err
	}
	return &BlockResult{User: row, Changed: changed}, nil
}

// DeleteData menyembunyikan dan menandai hapus semua submission user (langkah 1 hapus permanen) dalam
// satu transaksi; file dan baris dibersihkan cleanup yang dijalankan segera. Submission accepted
// dipertahankan kecuali includeAccepted. Akun user tidak dihapus.
func (u *AdminUsers) DeleteData(ctx context.Context, id uuid.UUID, includeAccepted bool) (*UserDataMark, error) {
	if _, err := u.repo.GetUser(ctx, id); err != nil {
		return nil, err
	}
	var m UserDataMark
	err := u.tx.WithTx(ctx, func(ctx context.Context) error {
		var err error
		m, err = u.deletion.MarkUserSubmissions(ctx, id, includeAccepted, u.clock.Now())
		return err
	})
	if err != nil {
		return nil, err
	}
	if m.Submissions > 0 && u.afterDataDeleted != nil {
		u.afterDataDeleted()
	}
	return &m, nil
}

// DeleteAccount menghapus akun annotator beserta SEMUA datanya (termasuk submission accepted dan foto
// yang belum dikirim) dalam satu transaksi: langkah 1 hapus permanen, lalu baris pengguna dihapus
// (sesi dan challenge OTP ikut). File dibersihkan cleanup yang dijalankan segera. Setelahnya nomor
// itu bisa didaftarkan lagi. Baris pengguna dikunci lebih dulu: submit yang berbarengan menunggu
// lalu gagal (pengguna sudah tidak ada), atau sudah commit dan ikut ditandai hapus.
func (u *AdminUsers) DeleteAccount(ctx context.Context, id uuid.UUID) (*UserDataMark, error) {
	var m UserDataMark
	err := u.tx.WithTx(ctx, func(ctx context.Context) error {
		if err := u.repo.LockForDelete(ctx, id); err != nil {
			return err
		}
		var err error
		if m, err = u.deletion.MarkUserSubmissions(ctx, id, true, u.clock.Now()); err != nil {
			return err
		}
		loose, err := u.deletion.MarkUserLoosePhotos(ctx, id)
		if err != nil {
			return err
		}
		m.Photos += loose
		return u.repo.DeleteUser(ctx, id)
	})
	if err != nil {
		return nil, err
	}
	if m.Photos > 0 && u.afterDataDeleted != nil {
		u.afterDataDeleted()
	}
	return &m, nil
}
