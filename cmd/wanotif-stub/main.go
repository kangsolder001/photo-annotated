// Command wanotif-stub menjalankan stub wa-notif untuk load test dan pengembangan lokal, agar
// beban uji TIDAK PERNAH mengirim WhatsApp sungguhan dan tidak membahayakan nomor pengirim.
//
//	wanotif-stub -addr :13030 -token rahasia -interval 1500ms
//
// Endpoint tambahan untuk inspeksi: GET /_stub/messages dan GET /_stub/stats.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"time"

	"photo-annotated/internal/testutil/wastub"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:13030", "alamat listen")
	token := flag.String("token", "", "bearer token (kosong = tanpa autentikasi)")
	interval := flag.Duration("interval", 1500*time.Millisecond, "jeda minimum antar pengiriman (wa-notif asli: 1,5 detik)")
	latency := flag.Duration("latency", 0, "penundaan tambahan tiap pengiriman")
	flag.Parse()

	_, h := wastub.NewHandler(wastub.Options{Token: *token, SendInterval: *interval, Latency: *latency})
	fmt.Printf("wanotif-stub mendengarkan di %s (interval %v)\n", *addr, *interval)
	srv := &http.Server{Addr: *addr, Handler: h, ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(srv.ListenAndServe())
}
