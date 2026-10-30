package domain

import "time"

const MaxGroupNameLen = 100

// Group adalah kelompok mahasiswa (spec-pendataan §5.5): teks bebas yang dikelola admin dan dipilih
// saat registrasi. Kelompok yang diarsipkan tidak muncul di pilihan registrasi.
type Group struct {
	ID         int
	Name       string
	ArchivedAt *time.Time // nil = aktif
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func (g *Group) Archived() bool { return g.ArchivedAt != nil }

// NormalizeGroupName: trim, 1–100 karakter, tanpa karakter kontrol/bidi (aturan sama dengan nama label).
func NormalizeGroupName(raw string) (string, error) {
	s, ok := cleanLine(raw, 1, MaxGroupNameLen)
	if !ok {
		return "", Invalid("INVALID_GROUP_NAME", "group name must be 1-%d characters without control characters", MaxGroupNameLen)
	}
	return s, nil
}
