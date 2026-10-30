package usecase

import (
	"context"
	"errors"

	"photo-annotated/internal/domain"
)

// Groups mengelola daftar kelompok mahasiswa (spec-pendataan §5.2, §5.5). Pola sama dengan label,
// tanpa warna, urutan manual, dan batas jumlah.
type Groups struct {
	repo  GroupRepo
	clock Clock
}

func NewGroups(repo GroupRepo, clock Clock) *Groups {
	return &Groups{repo: repo, clock: clock}
}

var errGroupNotFound = domain.NotFound("NOT_FOUND", "group not found")

// Active: kelompok aktif urut nama (pilihan registrasi).
func (u *Groups) Active(ctx context.Context) ([]domain.Group, error) {
	return u.repo.ListActive(ctx)
}

// All: semua kelompok termasuk arsip, dengan jumlah mahasiswa (dashboard).
func (u *Groups) All(ctx context.Context) ([]GroupUsage, error) {
	return u.repo.ListAll(ctx)
}

// Create menambah kelompok aktif. Nama kembar (tanpa beda huruf besar-kecil) → GROUP_NAME_TAKEN.
func (u *Groups) Create(ctx context.Context, name string) (*GroupUsage, error) {
	name, err := domain.NormalizeGroupName(name)
	if err != nil {
		return nil, err
	}
	now := u.clock.Now()
	g := &domain.Group{Name: name, CreatedAt: now, UpdatedAt: now}
	if err := u.repo.Create(ctx, g); err != nil {
		return nil, err
	}
	return &GroupUsage{Group: *g}, nil
}

// Update mengganti nama dan status arsip kelompok.
func (u *Groups) Update(ctx context.Context, id int, name string, archived bool) (*GroupUsage, error) {
	name, err := domain.NormalizeGroupName(name)
	if err != nil {
		return nil, err
	}
	g, err := u.repo.Update(ctx, id, name, archived, u.clock.Now())
	if errors.Is(err, domain.ErrNotFound) {
		return nil, errGroupNotFound
	}
	return g, err
}

// Delete menghapus kelompok yang belum dipakai akun mana pun (dipakai → GROUP_IN_USE; arsipkan saja).
func (u *Groups) Delete(ctx context.Context, id int) (*domain.Group, error) {
	g, err := u.repo.Delete(ctx, id)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, errGroupNotFound
	}
	return g, err
}
