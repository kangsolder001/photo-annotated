// Package migrations menyematkan file SQL migrasi ke dalam binary (dijalankan oleh cmd/migrate).
package migrations

import "embed"

// FS berisi seluruh file *.sql di direktori ini, diberi nomor urut (0001_..., 0002_...).
//
//go:embed *.sql
var FS embed.FS
