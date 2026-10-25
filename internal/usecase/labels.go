package usecase

import (
	"context"
	"errors"
	"slices"

	"photo-annotated/internal/domain"
)

// LabelsConfig: batas jumlah label aktif (limits.max_labels).
type LabelsConfig struct {
	MaxActive int
}

// Labels mengelola daftar label (spec-labels-admin §4, §5.3). Pengguna hanya membaca label aktif;
// admin menambah, mengubah, mengurutkan, mengarsipkan, dan menghapus label yang belum dipakai.
type Labels struct {
	repo  LabelRepo
	tx    TxManager
	clock Clock
	cfg   LabelsConfig
}

func NewLabels(repo LabelRepo, tx TxManager, clock Clock, cfg LabelsConfig) *Labels {
	if cfg.MaxActive < 1 {
		panic("usecase: LabelsConfig.MaxActive harus >= 1")
	}
	return &Labels{repo: repo, tx: tx, clock: clock, cfg: cfg}
}

var (
	errLabelNotFound   = domain.NotFound("NOT_FOUND", "label not found")
	errLabelOrderStale = domain.Conflict("LABEL_ORDER_STALE", "the label list has changed; reload and try again")
)

func (u *Labels) errLimit() error {
	return domain.Invalid("LABEL_LIMIT", "at most %d active labels are allowed; archive one first", u.cfg.MaxActive)
}

// Active mengembalikan label aktif untuk pemilih label pengguna.
func (u *Labels) Active(ctx context.Context) ([]domain.Label, error) {
	return u.repo.ListActive(ctx)
}

// All mengembalikan semua label beserta jumlah pemakaiannya (dashboard).
func (u *Labels) All(ctx context.Context) ([]LabelUsage, error) {
	return u.repo.ListAll(ctx)
}

// LabelInput adalah isi POST/PUT /admin/labels. Color kosong pada Create = pilih dari palet.
type LabelInput struct {
	Name, Description, Color string
	Archived                 bool // hanya Update
}

func normalizeLabel(in LabelInput, colorOptional bool) (LabelInput, error) {
	name, err := domain.NormalizeLabelName(in.Name)
	if err != nil {
		return in, err
	}
	desc, err := domain.NormalizeLabelDescription(in.Description)
	if err != nil {
		return in, err
	}
	color := ""
	if !colorOptional || in.Color != "" {
		if color, err = domain.NormalizeLabelColor(in.Color); err != nil {
			return in, err
		}
	}
	return LabelInput{Name: name, Description: desc, Color: color, Archived: in.Archived}, nil
}

// Create menambah label aktif di urutan terakhir.
func (u *Labels) Create(ctx context.Context, in LabelInput) (*LabelUsage, error) {
	in, err := normalizeLabel(in, true)
	if err != nil {
		return nil, err
	}
	l := &domain.Label{Name: in.Name, Description: in.Description, Color: in.Color}
	err = u.tx.WithTx(ctx, func(ctx context.Context) error {
		if err := u.repo.LockForWrite(ctx); err != nil {
			return err
		}
		active, err := u.repo.CountActive(ctx)
		if err != nil {
			return err
		}
		if active >= u.cfg.MaxActive {
			return u.errLimit()
		}
		if l.Color == "" {
			all, err := u.repo.CountAll(ctx)
			if err != nil {
				return err
			}
			l.Color = domain.LabelPalette[all%len(domain.LabelPalette)]
		}
		now := u.clock.Now()
		l.CreatedAt, l.UpdatedAt = now, now
		return u.repo.Create(ctx, l)
	})
	if err != nil {
		return nil, err
	}
	return &LabelUsage{Label: *l}, nil
}

// LabelChange menjelaskan apa yang berubah oleh Update (untuk log admin action).
type LabelChange struct {
	Label      LabelUsage
	Archived   bool // baru diarsipkan
	Unarchived bool // baru dibuka arsipnya
}

// Update mengganti label (replace). Membuka arsip tunduk pada batas label aktif; label yang dibuka
// arsipnya diletakkan di urutan terakhir.
func (u *Labels) Update(ctx context.Context, id int, in LabelInput) (*LabelChange, error) {
	in, err := normalizeLabel(in, false)
	if err != nil {
		return nil, err
	}
	var ch LabelChange
	err = u.tx.WithTx(ctx, func(ctx context.Context) error {
		if err := u.repo.LockForWrite(ctx); err != nil {
			return err
		}
		l, err := u.repo.Get(ctx, id)
		if errors.Is(err, domain.ErrNotFound) {
			return errLabelNotFound
		}
		if err != nil {
			return err
		}
		now := u.clock.Now()
		switch {
		case in.Archived && !l.Archived():
			l.ArchivedAt, ch.Archived = &now, true
		case !in.Archived && l.Archived():
			active, err := u.repo.CountActive(ctx)
			if err != nil {
				return err
			}
			if active >= u.cfg.MaxActive {
				return u.errLimit()
			}
			last, err := u.repo.MaxActiveOrder(ctx)
			if err != nil {
				return err
			}
			l.ArchivedAt, l.SortOrder, ch.Unarchived = nil, last+1, true
		}
		l.Name, l.Description, l.Color, l.UpdatedAt = in.Name, in.Description, in.Color, now
		if err := u.repo.Update(ctx, l); err != nil {
			return err
		}
		n, err := u.repo.UsageAttached(ctx, id)
		ch.Label = LabelUsage{Label: *l, UsageCount: n}
		return err
	})
	if err != nil {
		return nil, err
	}
	return &ch, nil
}

// Reorder menerapkan urutan baru untuk semua label aktif. ids harus tepat sama dengan himpunan label
// aktif saat ini (tanpa duplikat); selain itu LABEL_ORDER_STALE agar FE memuat ulang.
func (u *Labels) Reorder(ctx context.Context, ids []int) ([]LabelUsage, error) {
	var out []LabelUsage
	err := u.tx.WithTx(ctx, func(ctx context.Context) error {
		if err := u.repo.LockForWrite(ctx); err != nil {
			return err
		}
		active, err := u.repo.ListActive(ctx)
		if err != nil {
			return err
		}
		if len(active) != len(ids) {
			return errLabelOrderStale
		}
		want := make([]int, len(active))
		for i, l := range active {
			want[i] = l.ID
		}
		got := slices.Clone(ids)
		slices.Sort(want)
		slices.Sort(got)
		if !slices.Equal(want, got) {
			return errLabelOrderStale // termasuk duplikat (himpunan berbeda karena panjang sama)
		}
		if err := u.repo.SetOrder(ctx, ids, u.clock.Now()); err != nil {
			return err
		}
		out, err = u.repo.ListAll(ctx)
		return err
	})
	return out, err
}

// Delete menghapus label yang belum pernah dipakai anotasi mana pun (termasuk pada foto deleted yang
// belum dibersihkan). Dipakai → LABEL_IN_USE dengan details.usage_count; FE menawarkan arsip.
func (u *Labels) Delete(ctx context.Context, id int) (*domain.Label, error) {
	var deleted *domain.Label
	err := u.tx.WithTx(ctx, func(ctx context.Context) error {
		if err := u.repo.LockForWrite(ctx); err != nil {
			return err
		}
		l, err := u.repo.Get(ctx, id)
		if errors.Is(err, domain.ErrNotFound) {
			return errLabelNotFound
		}
		if err != nil {
			return err
		}
		n, err := u.repo.UsageAll(ctx, id)
		if err != nil {
			return err
		}
		if n > 0 {
			return labelInUse(n)
		}
		deleted = l
		return u.repo.Delete(ctx, id)
	})
	return deleted, err
}

func labelInUse(n int) error {
	return domain.Conflict("LABEL_IN_USE", "the label is used by annotations; archive it instead").
		WithDetails(map[string]any{"usage_count": n})
}
