// Package imgfix membuat file gambar uji. JPEG dibuat dengan encoder stdlib; WebP dibuat
// sintetis (header VP8L valid dengan dimensi dan ukuran total yang bisa diatur) karena
// Go tidak punya encoder WebP. Header-nya cukup bagi DecodeConfig, yang memang satu-satunya
// pemeriksaan isi yang dilakukan server.
package imgfix

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
)

// JPEG membuat gambar JPEG sungguhan berukuran w×h.
func JPEG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 7), G: uint8(y * 5), B: uint8(x + y), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 70}); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// WebP membuat berkas WebP (VP8L) sintetis: header RIFF/VP8L valid untuk dimensi w×h,
// total berukuran totalSize byte (genap, ≥ 26), sisanya diisi fill.
func WebP(w, h, totalSize int, fill byte) []byte {
	if totalSize < 26 || totalSize%2 != 0 {
		panic("imgfix.WebP: totalSize harus genap dan >= 26")
	}
	payloadLen := totalSize - 20
	payload := bytes.Repeat([]byte{fill}, payloadLen)
	payload[0] = 0x2f                     // tanda tangan VP8L
	bits := uint32(w-1) | uint32(h-1)<<14 // 14 bit lebar-1, 14 bit tinggi-1, alpha=0, versi=0
	binary.LittleEndian.PutUint32(payload[1:5], bits)

	var buf bytes.Buffer
	buf.WriteString("RIFF")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(totalSize-8))
	buf.WriteString("WEBP")
	buf.WriteString("VP8L")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(payloadLen))
	buf.Write(payload)
	return buf.Bytes()
}

// Junk membuat n byte yang bukan gambar apa pun.
func Junk(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*31 + 7)
	}
	return b
}
