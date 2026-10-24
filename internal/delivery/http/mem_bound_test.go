//go:build !race

package httpapi_test

import "testing"

// Pagar memori: 200 upload serentak @ 400 KB (total 80 MB) tidak boleh menaikkan heap secara
// berarti. Bila backend membuffer seluruh body di memori, kenaikannya >= 80 MB. Dilewati di
// mode -race karena detektor race menggandakan pemakaian memori.
func TestE2EConcurrentUploadsMemoryIsBounded(t *testing.T) {
	st := newStack(t, stackOpts{maxConc: 200})
	growth := runConcurrentUploads(t, st, 200, 400*1024)
	const limit = 40 << 20
	t.Logf("kenaikan heap puncak: %.1f MB (total payload 80 MB; batas %d MB)", float64(growth)/(1<<20), limit>>20)
	if growth > limit {
		t.Fatalf("heap naik %.1f MB — upload tampaknya dibuffer di memori", float64(growth)/(1<<20))
	}
}
