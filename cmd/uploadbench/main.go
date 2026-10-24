// Command uploadbench mengukur throughput presign + PUT foto terhadap API yang sedang berjalan.
//
//	uploadbench -url http://192.168.1.222:8080 -n 200 -c 16 -size 409600
//
// Setiap iterasi: POST /api/v1/uploads/presign lalu PUT ke URL yang dikembalikan. Isi file
// adalah WebP sintetis (header valid) berukuran -size, jadi lolos validasi isi di server.
// PERHATIAN: alat ini hanya menyentuh endpoint upload; tidak pernah memanggil wa-notif.
package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	base := flag.String("url", "http://127.0.0.1:8080", "alamat dasar API")
	n := flag.Int("n", 100, "jumlah foto")
	conc := flag.Int("c", 8, "jumlah pekerja paralel")
	size := flag.Int("size", 400*1024, "ukuran file (byte, genap)")
	w := flag.Int("w", 1920, "lebar yang dideklarasikan")
	h := flag.Int("h", 1440, "tinggi yang dideklarasikan")
	flag.Parse()
	if *size%2 != 0 || *size < 26 {
		fmt.Fprintln(os.Stderr, "size harus genap dan >= 26")
		os.Exit(2)
	}

	body := webp(*w, *h, *size)
	client := &http.Client{Timeout: 2 * time.Minute, Transport: &http.Transport{MaxIdleConnsPerHost: *conc}}
	jobs := make(chan int)
	var (
		wg        sync.WaitGroup
		ok, fail  atomic.Int64
		mu        sync.Mutex
		putLat    []time.Duration
		failCodes = map[string]int{}
	)
	record := func(code string) {
		mu.Lock()
		failCodes[code]++
		mu.Unlock()
	}

	start := time.Now()
	for i := 0; i < *conc; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range jobs {
				url, err := presign(client, *base, len(body), *w, *h)
				if err != nil {
					fail.Add(1)
					record("presign: " + err.Error())
					continue
				}
				t0 := time.Now()
				req, _ := http.NewRequest(http.MethodPut, url, bytes.NewReader(body))
				req.Header.Set("Content-Type", "image/webp")
				resp, err := client.Do(req)
				if err != nil {
					fail.Add(1)
					record("put: " + err.Error())
					continue
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				if resp.StatusCode != http.StatusNoContent {
					fail.Add(1)
					record(fmt.Sprintf("put: HTTP %d", resp.StatusCode))
					continue
				}
				ok.Add(1)
				mu.Lock()
				putLat = append(putLat, time.Since(t0))
				mu.Unlock()
			}
		}()
	}
	for i := 0; i < *n; i++ {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	el := time.Since(start)

	sort.Slice(putLat, func(i, j int) bool { return putLat[i] < putLat[j] })
	pct := func(p float64) time.Duration {
		if len(putLat) == 0 {
			return 0
		}
		return putLat[min(len(putLat)-1, int(float64(len(putLat))*p))]
	}
	fmt.Printf("berhasil=%d gagal=%d  durasi=%.2fs  %.1f foto/s  %.1f MB/s (%.0f Mbps)\n",
		ok.Load(), fail.Load(), el.Seconds(), float64(ok.Load())/el.Seconds(),
		float64(ok.Load())*float64(len(body))/1e6/el.Seconds(), float64(ok.Load())*float64(len(body))*8/1e6/el.Seconds())
	fmt.Printf("latensi PUT: p50=%v p95=%v p99=%v max=%v\n", pct(0.50).Round(time.Millisecond), pct(0.95).Round(time.Millisecond),
		pct(0.99).Round(time.Millisecond), pct(1.0).Round(time.Millisecond))
	for k, v := range failCodes {
		fmt.Printf("  gagal %dx: %s\n", v, k)
	}
	if fail.Load() > 0 {
		os.Exit(1)
	}
}

func presign(c *http.Client, base string, size, w, h int) (string, error) {
	payload := fmt.Sprintf(`{"content_type":"image/webp","size_bytes":%d,"width":%d,"height":%d}`, size, w, h)
	resp, err := c.Post(strings.TrimRight(base, "/")+"/api/v1/uploads/presign", "application/json", strings.NewReader(payload))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out struct {
		Upload struct {
			URL string `json:"url"`
		} `json:"upload"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.Upload.URL == "" {
		return "", fmt.Errorf("respons presign tidak valid: %s", raw)
	}
	return out.Upload.URL, nil
}

// webp membuat WebP (VP8L) sintetis dengan header valid; lihat internal/testutil/imgfix.
func webp(w, h, total int) []byte {
	payloadLen := total - 20
	payload := bytes.Repeat([]byte{0x5A}, payloadLen)
	payload[0] = 0x2f
	binary.LittleEndian.PutUint32(payload[1:5], uint32(w-1)|uint32(h-1)<<14)
	var b bytes.Buffer
	b.WriteString("RIFF")
	_ = binary.Write(&b, binary.LittleEndian, uint32(total-8))
	b.WriteString("WEBPVP8L")
	_ = binary.Write(&b, binary.LittleEndian, uint32(payloadLen))
	b.Write(payload)
	return b.Bytes()
}
