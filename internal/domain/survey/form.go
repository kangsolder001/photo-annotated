// Package survey adalah definisi instrumen pendataan baseline (spec-pendataan §4): data murni Go yang
// dipakai backend untuk validasi dan disajikan ke FE lewat GET /survey/form. Satu-satunya sumber
// pertanyaan; perubahan pertanyaan = deploy (naikkan Version bila arti jawaban berubah).
package survey

// Type adalah tipe jawaban (§4.1).
type Type string

const (
	TypeDate     Type = "date"     // "YYYY-MM-DD", tidak di masa depan (Asia/Jakarta)
	TypeText     Type = "text"     // satu baris, 1–Max karakter
	TypeTextarea Type = "textarea" // boleh baris baru, 0–TextareaMax karakter
	TypeInt      Type = "int"      // bilangan bulat Min–Max
	TypeChoice   Type = "choice"   // satu id opsi
	TypeMulti    Type = "multi"    // array id opsi unik; opsi Other wajib disertai teks di OtherKey(id)
)

const (
	TextareaMax  = 1000
	OtherTextMax = 100
)

// Option adalah satu pilihan. Other: opsi "Lainnya" pada multi yang wajib disertai teks.
type Option struct {
	ID, Label string
	Other     bool
}

// Question adalah satu pertanyaan. Min/Max hanya berlaku untuk int (Min–Max) dan text (Max).
type Question struct {
	ID, Label, Help string
	Type            Type
	Required        bool
	Min, Max        *int
	Options         []Option
}

// Section adalah satu layar wizard.
type Section struct {
	ID, Title, Help string
	Questions       []Question
}

// Form adalah satu versi instrumen.
type Form struct {
	Version     int
	ConsentText string
	Sections    []Section
}

// Identitas & persetujuan (§4.2–4.3).
const (
	SectionIdentity = "A"
	ConsentID       = "a_consent"
	ConsentYes      = "yes"
	ConsentNo       = "no"
)

// OtherKey adalah kunci jawaban untuk teks opsi "Lainnya" pada multi, mis. r3_container_types_other.
func OtherKey(questionID string) string { return questionID + "_other" }

// ByVersion mengembalikan definisi versi itu; false bila tidak didukung.
func ByVersion(v int) (*Form, bool) {
	if v == V1.Version {
		return V1, true
	}
	return nil, false
}

// Questions mengembalikan semua pertanyaan berurutan (urut definisi).
func (f *Form) Questions() []Question {
	var out []Question
	for _, s := range f.Sections {
		out = append(out, s.Questions...)
	}
	return out
}

// Question mencari pertanyaan berdasarkan id.
func (f *Form) Question(id string) (*Question, bool) {
	for i := range f.Sections {
		for j := range f.Sections[i].Questions {
			if f.Sections[i].Questions[j].ID == id {
				return &f.Sections[i].Questions[j], true
			}
		}
	}
	return nil, false
}

// Section mencari bagian berdasarkan id.
func (f *Form) Section(id string) (*Section, bool) {
	for i := range f.Sections {
		if f.Sections[i].ID == id {
			return &f.Sections[i], true
		}
	}
	return nil, false
}

// OptionLabel mengembalikan teks opsi; id tak dikenal dikembalikan apa adanya.
func (q *Question) OptionLabel(id string) string {
	for _, o := range q.Options {
		if o.ID == id {
			return o.Label
		}
	}
	return id
}

// OtherOption: opsi "Lainnya" pada pertanyaan ini, bila ada.
func (q *Question) OtherOption() (*Option, bool) {
	for i := range q.Options {
		if q.Options[i].Other {
			return &q.Options[i], true
		}
	}
	return nil, false
}
