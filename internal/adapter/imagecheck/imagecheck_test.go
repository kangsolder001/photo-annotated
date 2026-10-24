package imagecheck_test

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"testing"

	"photo-annotated/internal/adapter/imagecheck"
	"photo-annotated/internal/domain"
	"photo-annotated/internal/testutil/imgfix"
)

var insp imagecheck.Inspector

const (
	webp = domain.ContentTypeWebP
	jpg  = domain.ContentTypeJPEG
)

// WebP lossy 1x1 sungguhan (encoder libwebp), untuk memastikan jalur VP8 juga terbaca.
const realWebP1x1 = "UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEADsD+JaQAA3AAAAAA"

func inspect(t *testing.T, data []byte, ct string) (w, h int, err error) {
	t.Helper()
	info, err := insp.Inspect(bytes.NewReader(data), ct)
	return info.Width, info.Height, err
}

func wantInvalid(t *testing.T, err error) {
	t.Helper()
	de, ok := domain.AsError(err)
	if !ok || de.Code != "INVALID_IMAGE" || de.Kind != domain.KindInvalid {
		t.Fatalf("want INVALID_IMAGE, got %v", err)
	}
}

func TestInspectValidImages(t *testing.T) {
	real, err := base64.StdEncoding.DecodeString(realWebP1x1)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		data []byte
		ct   string
		w, h int
	}{
		{"jpeg 64x48", imgfix.JPEG(64, 48), jpg, 64, 48},
		{"jpeg 1x1", imgfix.JPEG(1, 1), jpg, 1, 1},
		{"jpeg 300x200", imgfix.JPEG(300, 200), jpg, 300, 200},
		{"webp lossy asli 1x1", real, webp, 1, 1},
		{"webp sintetis 1920x1440", imgfix.WebP(1920, 1440, 4096, 0x11), webp, 1920, 1440},
		{"webp sintetis 2048x2048", imgfix.WebP(2048, 2048, 100, 0), webp, 2048, 2048},
		{"webp sintetis 1x1", imgfix.WebP(1, 1, 26, 0), webp, 1, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w, h, err := inspect(t, tc.data, tc.ct)
			if err != nil || w != tc.w || h != tc.h {
				t.Fatalf("got %dx%d err=%v, want %dx%d", w, h, err, tc.w, tc.h)
			}
		})
	}
}

func TestInspectRejectsWrongOrBrokenImages(t *testing.T) {
	jpegData := imgfix.JPEG(64, 48)
	webpData := imgfix.WebP(100, 100, 2000, 0)
	tests := []struct {
		name string
		data []byte
		ct   string
	}{
		{"kosong (jpeg)", nil, jpg},
		{"kosong (webp)", nil, webp},
		{"byte acak sebagai jpeg", imgfix.Junk(5000), jpg},
		{"byte acak sebagai webp", imgfix.Junk(5000), webp},
		{"jpeg dideklarasikan webp", jpegData, webp},
		{"webp dideklarasikan jpeg", webpData, jpg},
		{"teks biasa", []byte("<html><script>alert(1)</script></html>"), jpg},
		{"png dideklarasikan jpeg", []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"), jpg},
		{"jpeg terpotong setelah magic", jpegData[:3], jpg},
		{"jpeg terpotong di header", jpegData[:30], jpg},
		{"RIFF tapi bukan WEBP", append([]byte("RIFF\x10\x00\x00\x00WAVEfmt "), make([]byte, 20)...), webp},
		{"webp terpotong setelah RIFF", webpData[:12], webp},
		{"webp dengan chunk tak dikenal", append(append([]byte("RIFF\x1a\x00\x00\x00WEBP"), []byte("XXXX\x0a\x00\x00\x00")...), make([]byte, 10)...), webp},
		{"content-type tak didukung", jpegData, "image/png"},
		{"content-type kosong", jpegData, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := inspect(t, tc.data, tc.ct)
			wantInvalid(t, err)
		})
	}
}

type errReader struct{ err error }

func (e errReader) Read([]byte) (int, error) { return 0, e.err }

func TestInspectPassesThroughRealIOErrors(t *testing.T) {
	boom := errors.New("disk error")
	_, err := insp.Inspect(errReader{boom}, jpg)
	if !errors.Is(err, boom) {
		t.Fatalf("kegagalan baca (bukan format) harus diteruskan, dapat %v", err)
	}
	if _, ok := domain.AsError(err); ok {
		t.Fatal("kegagalan I/O tidak boleh disamarkan sebagai INVALID_IMAGE")
	}
}

func TestInspectDoesNotReadWholeFile(t *testing.T) {
	// header cukup; membaca satu aliran besar tidak boleh menghabiskan seluruhnya
	big := io.MultiReader(bytes.NewReader(imgfix.JPEG(64, 48)), io.LimitReader(zeros{}, 1<<40))
	if _, err := insp.Inspect(big, jpg); err != nil {
		t.Fatal(err)
	}
}

type zeros struct{}

func (zeros) Read(p []byte) (int, error) { clear(p); return len(p), nil }
