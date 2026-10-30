package survey_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"photo-annotated/internal/domain"
	"photo-annotated/internal/domain/survey"
)

func TestV1Definition(t *testing.T) {
	f, ok := survey.ByVersion(1)
	if !ok || f.Version != 1 || f.ConsentText == "" {
		t.Fatal("versi 1 harus ada")
	}
	if _, ok := survey.ByVersion(2); ok {
		t.Fatal("versi 2 belum ada")
	}
	var ids []string
	seen := map[string]bool{}
	for _, q := range f.Questions() {
		if seen[q.ID] {
			t.Fatalf("id kembar %s", q.ID)
		}
		seen[q.ID] = true
		ids = append(ids, q.ID)
		if (q.Type == survey.TypeChoice || q.Type == survey.TypeMulti) && len(q.Options) < 2 {
			t.Fatalf("%s: opsi < 2", q.ID)
		}
		if q.Label == "" {
			t.Fatalf("%s: tanpa label", q.ID)
		}
	}
	want := []string{"a_date", "a_rw", "a_rt", "a_household_code", "a_initials", "a_respondent", "a_consent"}
	for i := 1; i <= 30; i++ {
		want = append(want, fmt.Sprintf("b%d", i))
	}
	for i := 1; i <= 5; i++ {
		want = append(want, fmt.Sprintf("b_note_%d", i))
	}
	for i := 1; i <= 10; i++ {
		want = append(want, fmt.Sprintf("c%d", i))
	}
	want = append(want, "c_note", "r2_occupants", "r2_head_age", "r2_head_edu",
		"r3_sorting", "r3_visible", "r3_containers", "r3_container_types", "r3_location", "r3_lighting")
	if len(ids) != len(want) {
		t.Fatalf("jumlah pertanyaan %d, ingin %d", len(ids), len(want))
	}
	for _, id := range want {
		if !seen[id] {
			t.Fatalf("pertanyaan %s tidak ada", id)
		}
	}
	var secs []string
	for _, s := range f.Sections {
		secs = append(secs, s.ID)
	}
	if strings.Join(secs, ",") != "A,B1,B2,B3,B4,B5,C,R2,R3" {
		t.Fatalf("urutan bagian: %v", secs)
	}
	q, _ := f.Question("b7")
	if q.Label != "Apakah keluarga memisahkan sampah residu?" || q.OptionLabel("yes") != "Ya" {
		t.Fatalf("teks b7: %+v", q)
	}
}

var now = time.Date(2026, 11, 2, 20, 0, 0, 0, time.UTC) // 3 Nov 03:00 WIB

// declined: hanya bagian A, menolak.
func declined() map[string]any {
	return map[string]any{"a_date": "2026-11-02", "a_rw": "05", "a_rt": "03", "a_household_code": "RT03/RW05/12",
		"a_initials": "Ny. S", "a_respondent": "head", "a_consent": "no"}
}

// consented: semua wajib terisi.
func consented() map[string]any {
	m := declined()
	m["a_consent"] = "yes"
	for i := 1; i <= 30; i++ {
		m[fmt.Sprintf("b%d", i)] = "yes"
	}
	for i := 1; i <= 10; i++ {
		m[fmt.Sprintf("c%d", i)] = "na"
	}
	m["r3_sorting"], m["r3_visible"] = "partial", "clear"
	return m
}

func raw(t *testing.T, m map[string]any) map[string]json.RawMessage {
	t.Helper()
	b, _ := json.Marshal(m)
	var out map[string]json.RawMessage
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func with(base map[string]any, kv ...any) map[string]any {
	m := map[string]any{}
	for k, v := range base {
		m[k] = v
	}
	for i := 0; i < len(kv); i += 2 {
		if kv[i+1] == "<delete>" {
			delete(m, kv[i].(string))
		} else {
			m[kv[i].(string)] = kv[i+1]
		}
	}
	return m
}

func TestValidate(t *testing.T) {
	long := strings.Repeat("x", 1001)
	tests := []struct {
		name    string
		answers map[string]any
		want    string // "" = sah; selain itu awal pesan error
	}{
		{"menolak cukup A", declined(), ""},
		{"menolak + jawaban B", with(declined(), "b1", "yes"), "answers.b1: harus kosong"},
		{"menolak + B null/kosong tetap sah", with(declined(), "b1", nil, "b_note_1", "  "), ""},
		{"bersedia lengkap", consented(), ""},
		{"bersedia, b7 kosong", with(consented(), "b7", "<delete>"), "answers.b7: wajib diisi"},
		{"A belum lengkap", with(declined(), "a_rt", "  "), "answers.a_rt: wajib diisi"},
		{"tanpa persetujuan", with(declined(), "a_consent", "<delete>"), "answers.a_consent: wajib diisi"},
		{"opsi tak dikenal", with(consented(), "c1", "mungkin"), "answers.c1: pilihan tidak dikenal"},
		{"id tak dikenal", with(declined(), "zz", "x"), "answers.zz: pertanyaan tidak dikenal"},
		{"tanggal besok (WIB)", with(declined(), "a_date", "2026-11-04"), "answers.a_date: tanggal tidak boleh di masa depan"},
		{"tanggal hari ini (WIB)", with(declined(), "a_date", "2026-11-03"), ""},
		{"tanggal salah", with(declined(), "a_date", "2026-02-30"), "answers.a_date: tanggal tidak valid"},
		{"teks > max", with(declined(), "a_rw", "12345678901"), "answers.a_rw: maksimal 10 karakter"},
		{"teks baris baru", with(declined(), "a_initials", "Ny.\nS"), "answers.a_initials: mengandung karakter"},
		{"teks bukan string", with(declined(), "a_rw", 5), "answers.a_rw: harus berupa teks"},
		{"textarea baris baru boleh", with(consented(), "c_note", "baris 1\r\nbaris 2"), ""},
		{"textarea > 1000", with(consented(), "c_note", long), "answers.c_note: maksimal 1000 karakter"},
		{"int di luar batas", with(consented(), "r2_occupants", 51), "answers.r2_occupants: harus antara 1 dan 50"},
		{"int pecahan", with(consented(), "r2_occupants", 2.5), "answers.r2_occupants: harus bilangan bulat"},
		{"int string", with(consented(), "r3_containers", "3"), "answers.r3_containers: harus bilangan bulat"},
		{"int nol boleh", with(consented(), "r3_containers", 0), ""},
		{"multi kembar", with(consented(), "r3_container_types", []string{"bucket", "bucket"}), "answers.r3_container_types: pilihan tidak boleh berulang"},
		{"multi opsi asing", with(consented(), "r3_container_types", []string{"galon"}), "answers.r3_container_types: pilihan tidak dikenal"},
		{"multi bukan array", with(consented(), "r3_container_types", "bucket"), "answers.r3_container_types: harus berupa daftar"},
		{"other tanpa teks", with(consented(), "r3_container_types", []string{"bucket", "other"}), "answers.r3_container_types_other: wajib diisi"},
		{"other dengan teks", with(consented(), "r3_container_types", []string{"other"}, "r3_container_types_other", "Galon"), ""},
		{"teks other tanpa other", with(consented(), "r3_container_types", []string{"bucket"}, "r3_container_types_other", "Galon"), "answers.r3_container_types_other: hanya boleh"},
		{"teks other > 100", with(consented(), "r3_container_types", []string{"other"}, "r3_container_types_other", strings.Repeat("g", 101)), "answers.r3_container_types_other: maksimal 100"},
		{"menolak + teks other", with(declined(), "r3_container_types_other", "Galon"), "answers.r3_container_types_other: hanya boleh"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := survey.Validate(1, raw(t, tc.answers), now)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("ingin sah: %v", err)
				}
				return
			}
			de, ok := domain.AsError(err)
			if !ok || de.Code != "INVALID_ANSWER" || !strings.HasPrefix(de.Message, tc.want) {
				t.Fatalf("err = %v, ingin %q", err, tc.want)
			}
		})
	}
	if de, _ := domain.AsError(survey.Validate(2, raw(t, declined()), now)); de == nil || de.Code != "UNSUPPORTED_FORM_VERSION" {
		t.Fatalf("versi 2: %v", de)
	}
}

func TestNormalizeTrimsAndDropsEmpty(t *testing.T) {
	a, err := survey.Normalize(1, raw(t, with(consented(), "a_rw", " 05 ", "b_note_1", "", "c_note", " a\r\nb\t"+"c ", "r2_occupants", 4)), now)
	if err != nil {
		t.Fatal(err)
	}
	if a["a_rw"] != "05" || a["c_note"] != "a\nb c" || a["r2_occupants"] != int64(4) {
		t.Fatalf("normalisasi: %v", a)
	}
	if _, ok := a["b_note_1"]; ok {
		t.Fatal("jawaban kosong harus dibuang")
	}
	if !survey.Consented(raw(t, consented())) || survey.Consented(raw(t, declined())) {
		t.Fatal("Consented")
	}
}
