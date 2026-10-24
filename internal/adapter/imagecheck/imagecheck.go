// Package imagecheck memeriksa isi file gambar yang di-upload: magic bytes harus cocok dengan
// content-type, dan header gambar harus bisa dibaca (menghasilkan dimensi). Piksel tidak
// di-decode, sehingga murah di CPU Pi dan tidak rentan terhadap decompression bomb.
package imagecheck

import (
	"bytes"
	"image/jpeg"
	"io"

	"golang.org/x/image/webp"

	"photo-annotated/internal/domain"
	"photo-annotated/internal/usecase"
)

var _ usecase.ImageInspector = Inspector{}

type Inspector struct{}

func invalid(format string, a ...any) error { return domain.Invalid("INVALID_IMAGE", format, a...) }

// Inspect memverifikasi bahwa r berisi gambar bertipe contentType dan mengembalikan dimensinya.
func (Inspector) Inspect(r io.Reader, contentType string) (usecase.ImageInfo, error) {
	head := make([]byte, 12)
	n, err := io.ReadFull(r, head)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return usecase.ImageInfo{}, err // kegagalan baca, bukan format
	}
	head = head[:n]
	full := io.MultiReader(bytes.NewReader(head), r)

	switch contentType {
	case domain.ContentTypeJPEG:
		if len(head) < 3 || head[0] != 0xFF || head[1] != 0xD8 || head[2] != 0xFF {
			return usecase.ImageInfo{}, invalid("file is not a JPEG image")
		}
		cfg, err := jpeg.DecodeConfig(full)
		if err != nil {
			return usecase.ImageInfo{}, invalid("JPEG header could not be read")
		}
		return usecase.ImageInfo{Width: cfg.Width, Height: cfg.Height}, nil
	case domain.ContentTypeWebP:
		if len(head) < 12 || string(head[0:4]) != "RIFF" || string(head[8:12]) != "WEBP" {
			return usecase.ImageInfo{}, invalid("file is not a WebP image")
		}
		cfg, err := webp.DecodeConfig(full)
		if err != nil {
			return usecase.ImageInfo{}, invalid("WebP header could not be read")
		}
		return usecase.ImageInfo{Width: cfg.Width, Height: cfg.Height}, nil
	}
	return usecase.ImageInfo{}, invalid("unsupported content type")
}
