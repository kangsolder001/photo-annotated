// Command wanotif-check memeriksa apakah sesi WhatsApp di wa-notif tersambung, lewat endpoint status
// yang sama dengan probe kanal OTP. Alat ini HANYA membaca status; ia tidak bisa mengirim pesan.
//
//	wanotif-check -url http://192.168.1.222:3030
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"photo-annotated/internal/adapter/wanotif"
)

func main() {
	url := flag.String("url", "http://127.0.0.1:3030", "alamat dasar wa-notif")
	timeout := flag.Duration("timeout", 5*time.Second, "batas waktu pemeriksaan")
	flag.Parse()

	c, err := wanotif.New(*url, "", *timeout)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(2)
	}
	start := time.Now()
	if err := c.Check(context.Background()); err != nil {
		fmt.Printf("TIDAK TERSEDIA (%v): %v\n", time.Since(start).Round(time.Millisecond), err)
		os.Exit(1)
	}
	fmt.Printf("TERSAMBUNG (%v)\n", time.Since(start).Round(time.Millisecond))
}
