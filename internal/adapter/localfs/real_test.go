package localfs_test

import (
	"bytes"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"
)

// TestRealRoot menguji root nyata (mis. SSD di Pi). Dilewati kecuali LOCALFS_REAL_ROOT diisi.
// LOCALFS_SD_ROOT (opsional) adalah direktori TANPA penanda (mis. di SD card) yang harus ditolak.
func TestRealRoot(t *testing.T) {
	root := os.Getenv("LOCALFS_REAL_ROOT")
	if root == "" {
		t.Skip("LOCALFS_REAL_ROOT kosong")
	}
	s := mustNew(t, root, true)
	defer s.Close()
	if err := s.Ready(bg); err != nil {
		t.Fatalf("Ready pada root nyata: %v", err)
	}

	// throughput file 400 KB dengan fsync+rename+fsync dir (profil upload sebenarnya)
	const n, size = 100, 400 * 1024
	data := bytes.Repeat([]byte{0xAB}, size)
	keys := make([]string, n)
	start := time.Now()
	for i := range keys {
		keys[i] = newKey(t)
		st, err := s.Stage(bg, keys[i], bytes.NewReader(data), 2<<20)
		if err != nil {
			t.Fatal(err)
		}
		if err := st.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	el := time.Since(start)
	t.Logf("%d file x %d KB: %.2fs = %.0f file/s (%.1f MB/s)", n, size/1024, el.Seconds(), n/el.Seconds(), float64(n*size)/1e6/el.Seconds())
	for _, k := range keys {
		fi, err := s.Stat(bg, k)
		if err != nil || fi.Size != size {
			t.Fatalf("Stat %s: %+v %v", k, fi, err)
		}
		if err := s.Delete(bg, k); err != nil {
			t.Fatal(err)
		}
	}

	// paralel: profil server nyata (banyak upload bersamaan; fsync antar-upload bisa saling menggabung)
	for _, workers := range []int{4, 16} {
		const per = 25
		var wg sync.WaitGroup
		pkeys := make([][]string, workers)
		start := time.Now()
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < per; i++ {
					k := newKey(t)
					pkeys[w] = append(pkeys[w], k)
					st, err := s.Stage(bg, k, bytes.NewReader(data), 2<<20)
					if err != nil {
						t.Error(err)
						return
					}
					if err := st.Commit(); err != nil {
						t.Error(err)
						return
					}
				}
			}()
		}
		wg.Wait()
		el := time.Since(start)
		total := workers * per
		t.Logf("paralel x%d: %d file x %d KB: %.2fs = %.0f file/s (%.1f MB/s)", workers, total, size/1024, el.Seconds(), float64(total)/el.Seconds(), float64(total*size)/1e6/el.Seconds())
		for _, ks := range pkeys {
			for _, k := range ks {
				_ = s.Delete(bg, k)
			}
		}
	}

	if sd := os.Getenv("LOCALFS_SD_ROOT"); sd != "" {
		if _, err := newWithMarker(sd); err == nil {
			t.Fatalf("root tanpa penanda (%s) harus ditolak", sd)
		} else {
			t.Logf("root tanpa penanda ditolak seperti seharusnya: %v", err)
		}
	}
	fmt.Println("REAL-ROOT-OK")
}
