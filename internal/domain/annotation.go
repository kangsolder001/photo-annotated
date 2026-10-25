package domain

import "math"

const (
	// MinBBoxSize adalah lebar/tinggi minimum kotak (ternormalisasi) agar kotak tak sengaja ditolak.
	MinBBoxSize = 0.005

	// bboxEpsilon menoleransi pembulatan floating point (mis. piksel→0..1 di browser) pada
	// batas x+w ≤ 1 dan y+h ≤ 1. Konstanta yang sama dipakai di CHECK constraint database.
	bboxEpsilon = 1e-6
)

// BBoxEpsilon diekspos agar migrasi dan test merujuk nilai yang sama dengan domain.
const BBoxEpsilon = bboxEpsilon

// Annotation adalah satu kotak berlabel pada sebuah foto. Koordinat dinormalisasi 0–1 relatif
// terhadap foto hasil proses, origin kiri atas (spec §6.3).
//
// Anotasi baru selalu memakai LabelID (spec-labels-admin §4.2). Baris lama dari masa teks bebas
// punya LabelID 0 dan Text berisi catatannya; baris seperti itu hanya dibaca, tidak pernah ditulis.
type Annotation struct {
	LabelID int     // 0 = baris lama tanpa label
	Label   *Label  // diisi saat dibaca (nama/warna terkini); nil untuk baris lama
	Text    string  // hanya baris lama
	X, Y    float64 // pojok kiri atas
	W, H    float64 // lebar dan tinggi
}

// ValidateBBox memeriksa geometri kotak.
func (a Annotation) ValidateBBox() error {
	for _, v := range [...]float64{a.X, a.Y, a.W, a.H} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return Invalid("INVALID_BBOX", "bounding box coordinates must be finite numbers")
		}
	}
	switch {
	case a.X < 0 || a.X >= 1 || a.Y < 0 || a.Y >= 1:
		return Invalid("INVALID_BBOX", "x and y must satisfy 0 <= value < 1")
	case a.W <= 0 || a.W > 1 || a.H <= 0 || a.H > 1:
		return Invalid("INVALID_BBOX", "w and h must satisfy 0 < value <= 1")
	case a.W < MinBBoxSize || a.H < MinBBoxSize:
		return Invalid("INVALID_BBOX", "w and h must be at least %v", MinBBoxSize)
	case a.X+a.W > 1+bboxEpsilon || a.Y+a.H > 1+bboxEpsilon:
		return Invalid("INVALID_BBOX", "the box must stay inside the photo (x+w <= 1 and y+h <= 1)")
	}
	return nil
}

func isBidiControl(r rune) bool {
	return (r >= 0x202A && r <= 0x202E) || (r >= 0x2066 && r <= 0x2069)
}

// Validate memeriksa anotasi yang dikirim klien: geometri kotak dan label_id wajib. Keberadaan dan
// status label diperiksa usecase di dalam transaksi.
func (a Annotation) Validate() (Annotation, error) {
	if err := a.ValidateBBox(); err != nil {
		return Annotation{}, err
	}
	if a.LabelID < 1 {
		return Annotation{}, Invalid("INVALID_LABEL", "label_id is required")
	}
	return Annotation{LabelID: a.LabelID, X: a.X, Y: a.Y, W: a.W, H: a.H}, nil
}
