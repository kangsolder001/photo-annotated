package survey

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"photo-annotated/internal/domain"
)

// wib: Asia/Jakarta (UTC+7, tanpa DST). Zona tetap agar tidak bergantung pada tzdata di image.
var wib = time.FixedZone("WIB", 7*60*60)

// Today mengembalikan tanggal now di Asia/Jakarta sebagai "YYYY-MM-DD".
func Today(now time.Time) string { return now.In(wib).Format(time.DateOnly) }

func errAnswer(id, reason string) error {
	return domain.Invalid("INVALID_ANSWER", "answers.%s: %s", id, reason)
}

// Answers adalah jawaban yang sudah dinormalisasi: string (teks di-trim, id opsi, tanggal), int64, atau
// []string (multi). Jawaban kosong tidak ada di map.
type Answers map[string]any

// Validate memeriksa jawaban terhadap definisi versi itu (§4.1, §4.3) tanpa I/O.
func Validate(version int, answers map[string]json.RawMessage, now time.Time) error {
	_, err := Normalize(version, answers, now)
	return err
}

// Normalize memvalidasi jawaban lalu mengembalikan bentuk yang disimpan: teks di-trim (baris baru
// textarea dinormalkan), jawaban kosong (tidak ada, null, "", []) dibuang. Error pertama dikembalikan:
// id tak dikenal lebih dulu (urut abjad), lalu urut definisi. Versi tak dikenal → UNSUPPORTED_FORM_VERSION.
func Normalize(version int, answers map[string]json.RawMessage, now time.Time) (Answers, error) {
	f, ok := ByVersion(version)
	if !ok {
		return nil, domain.Invalid("UNSUPPORTED_FORM_VERSION", "form_version %d is not supported", version)
	}
	// id yang dikenal: pertanyaan + kunci teks "Lainnya" milik multi
	known := map[string]bool{}
	for _, q := range f.Questions() {
		known[q.ID] = true
		if _, ok := q.OtherOption(); ok && q.Type == TypeMulti {
			known[OtherKey(q.ID)] = true
		}
	}
	var unknown []string
	for id := range answers {
		if !known[id] {
			unknown = append(unknown, id)
		}
	}
	if len(unknown) > 0 {
		slices.Sort(unknown)
		return nil, errAnswer(unknown[0], "pertanyaan tidak dikenal")
	}

	today := Today(now)
	out := Answers{}
	consented := false
	for _, s := range f.Sections {
		applies := s.ID == SectionIdentity || consented
		for _, q := range s.Questions {
			v, err := normalizeOne(&q, answers[q.ID], today)
			if err != nil {
				return nil, err
			}
			var other string
			if q.Type == TypeMulti {
				if other, err = normalizeOther(&q, v, answers); err != nil {
					return nil, err
				}
			}
			if !applies {
				if v != nil || other != "" {
					return nil, errAnswer(q.ID, "harus kosong bila responden tidak bersedia")
				}
				continue
			}
			if v == nil {
				if q.Required {
					return nil, errAnswer(q.ID, "wajib diisi")
				}
				continue
			}
			out[q.ID] = v
			if other != "" {
				out[OtherKey(q.ID)] = other
			}
		}
		if s.ID == SectionIdentity {
			consented = out[ConsentID] == ConsentYes
		}
	}
	return out, nil
}

// Consented: jawaban (mentah atau ternormalisasi) menyatakan responden bersedia.
func Consented(answers map[string]json.RawMessage) bool {
	var s string
	return json.Unmarshal(answers[ConsentID], &s) == nil && s == ConsentYes
}

func isNull(raw json.RawMessage) bool {
	t := bytes.TrimSpace(raw)
	return len(t) == 0 || bytes.Equal(t, []byte("null"))
}

// normalizeOne mengembalikan nilai ternormalisasi, atau nil bila kosong.
func normalizeOne(q *Question, raw json.RawMessage, today string) (any, error) {
	if isNull(raw) {
		return nil, nil
	}
	switch q.Type {
	case TypeDate:
		s, err := str(q.ID, raw)
		if err != nil || s == "" {
			return nilIfEmpty(s), err
		}
		d, perr := time.Parse(time.DateOnly, s)
		if perr != nil || d.Format(time.DateOnly) != s {
			return nil, errAnswer(q.ID, "tanggal tidak valid (format YYYY-MM-DD)")
		}
		if s > today {
			return nil, errAnswer(q.ID, "tanggal tidak boleh di masa depan")
		}
		return s, nil
	case TypeText, TypeTextarea:
		s, err := str(q.ID, raw)
		if err != nil {
			return nil, err
		}
		max := TextareaMax
		if q.Type == TypeText && q.Max != nil {
			max = *q.Max
		}
		s, err = cleanText(q.ID, s, max, q.Type == TypeTextarea)
		return nilIfEmpty(s), err
	case TypeInt:
		var n json.Number
		// json.Number juga menerima string berisi angka ("3"); hanya angka JSON yang sah
		if t := bytes.TrimSpace(raw); t[0] == '"' || json.Unmarshal(raw, &n) != nil {
			return nil, errAnswer(q.ID, "harus bilangan bulat")
		}
		i, err := n.Int64()
		if err != nil {
			return nil, errAnswer(q.ID, "harus bilangan bulat")
		}
		if (q.Min != nil && i < int64(*q.Min)) || (q.Max != nil && i > int64(*q.Max)) {
			return nil, errAnswer(q.ID, fmt.Sprintf("harus antara %d dan %d", *q.Min, *q.Max))
		}
		return i, nil
	case TypeChoice:
		s, err := str(q.ID, raw)
		if err != nil || s == "" {
			return nilIfEmpty(s), err
		}
		if !hasOption(q, s) {
			return nil, errAnswer(q.ID, "pilihan tidak dikenal")
		}
		return s, nil
	case TypeMulti:
		var ids []string
		if err := json.Unmarshal(raw, &ids); err != nil {
			return nil, errAnswer(q.ID, "harus berupa daftar id pilihan")
		}
		if len(ids) == 0 {
			return nil, nil
		}
		seen := map[string]bool{}
		for _, id := range ids {
			if !hasOption(q, id) {
				return nil, errAnswer(q.ID, "pilihan tidak dikenal")
			}
			if seen[id] {
				return nil, errAnswer(q.ID, "pilihan tidak boleh berulang")
			}
			seen[id] = true
		}
		return ids, nil
	}
	return nil, errAnswer(q.ID, "tipe pertanyaan tidak dikenal")
}

// normalizeOther memeriksa teks "Lainnya" milik multi: wajib 1–100 karakter bila opsi Lainnya dipilih,
// harus kosong bila tidak.
func normalizeOther(q *Question, v any, answers map[string]json.RawMessage) (string, error) {
	key := OtherKey(q.ID)
	o, ok := q.OtherOption()
	if !ok {
		return "", nil
	}
	text := ""
	if raw := answers[key]; !isNull(raw) {
		s, err := str(key, raw)
		if err != nil {
			return "", err
		}
		if text, err = cleanText(key, s, OtherTextMax, false); err != nil {
			return "", err
		}
	}
	ids, _ := v.([]string)
	selected := slices.Contains(ids, o.ID)
	switch {
	case selected && text == "":
		return "", errAnswer(key, "wajib diisi bila memilih "+o.Label)
	case !selected && text != "":
		return "", errAnswer(key, "hanya boleh diisi bila memilih "+o.Label)
	}
	return text, nil
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func hasOption(q *Question, id string) bool {
	return slices.ContainsFunc(q.Options, func(o Option) bool { return o.ID == id })
}

func str(id string, raw json.RawMessage) (string, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", errAnswer(id, "harus berupa teks")
	}
	return s, nil
}

// cleanText: UTF-8 valid, trim, maksimal max karakter, tanpa karakter kontrol/bidi. multiline: baris
// baru (\r\n, \r → \n) boleh dan tab menjadi spasi.
func cleanText(id, s string, max int, multiline bool) (string, error) {
	if !utf8.ValidString(s) {
		return "", errAnswer(id, "mengandung karakter yang tidak diizinkan")
	}
	if multiline {
		s = strings.NewReplacer("\r\n", "\n", "\r", "\n", "\t", " ").Replace(s)
	}
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > max {
		return "", errAnswer(id, fmt.Sprintf("maksimal %d karakter", max))
	}
	for _, r := range s {
		if (unicode.IsControl(r) && !(multiline && r == '\n')) || (r >= 0x202A && r <= 0x202E) || (r >= 0x2066 && r <= 0x2069) {
			return "", errAnswer(id, "mengandung karakter yang tidak diizinkan")
		}
	}
	return s, nil
}
