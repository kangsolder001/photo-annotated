// Command healthcheck dipakai HEALTHCHECK container (image distroless tidak punya curl/wget):
// keluar 0 bila GET /healthz pada API lokal menjawab 200.
//
// Env HEALTHCHECK_URL menimpa alamat default http://127.0.0.1:8080/healthz.
package main

import (
	"net/http"
	"os"
	"time"
)

func main() {
	url := os.Getenv("HEALTHCHECK_URL")
	if url == "" {
		url = "http://127.0.0.1:8080/healthz"
	}
	c := &http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get(url)
	if err != nil {
		os.Exit(1)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		os.Exit(1)
	}
}
