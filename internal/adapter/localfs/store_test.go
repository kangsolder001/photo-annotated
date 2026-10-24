package localfs_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"photo-annotated/internal/adapter/localfs"
	"photo-annotated/internal/domain"
	"photo-annotated/internal/usecase"
)

var bg = context.Background()

func newStore(t *testing.T, mut func(*localfs.Options)) (*localfs.Store, string) {
	t.Helper()
	dir := t.TempDir()
	opts := localfs.Options{Root: dir}
	if mut != nil {
		mut(&opts)
	}
	s, err := localfs.New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, dir
}

func newKey(t *testing.T) string {
	t.Helper()
	k, err := domain.NewStorageKey(uuid.New(), time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC), domain.ContentTypeWebP)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func payload(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

func listTmp(t *testing.T, root string) []string {
	t.Helper()
	ents, err := os.ReadDir(filepath.Join(root, "tmp"))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range ents {
		out = append(out, e.Name())
	}
	return out
}

func codeOf(err error) string {
	if de, ok := domain.AsError(err); ok {
		return de.Code
	}
	return ""
}

// ---------------------------------------------------------------- New / marker

func TestNewValidatesRoot(t *testing.T) {
	if _, err := localfs.New(localfs.Options{Root: "relatif/path"}); err == nil {
		t.Fatal("root relatif harus ditolak")
	}
	if _, err := localfs.New(localfs.Options{Root: "/tidak/ada/sama/sekali"}); err == nil {
		t.Fatal("root yang tidak ada harus ditolak")
	}
	f := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(f, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := localfs.New(localfs.Options{Root: f}); err == nil {
		t.Fatal("root berupa file harus ditolak")
	}
}

func TestNewCreatesTmpAndSetsModes(t *testing.T) {
	_, dir := newStore(t, nil)
	fi, err := os.Stat(filepath.Join(dir, "tmp"))
	if err != nil || !fi.IsDir() {
		t.Fatalf("tmp/ harus dibuat: %v", err)
	}
	if fi.Mode().Perm() != 0o750 {
		t.Fatalf("mode tmp = %o, want 750", fi.Mode().Perm())
	}
}

func TestRequireMarker(t *testing.T) {
	dir := t.TempDir()
	// tanpa penanda → gagal start dengan STORAGE_UNAVAILABLE (SSD belum ter-mount)
	_, err := localfs.New(localfs.Options{Root: dir, RequireMarker: true})
	if err == nil || codeOf(err) != "STORAGE_UNAVAILABLE" || !strings.Contains(err.Error(), localfs.MarkerName) {
		t.Fatalf("tanpa penanda harus gagal dengan petunjuk: %v", err)
	}
	// penanda berupa direktori → ditolak
	if err := os.Mkdir(filepath.Join(dir, localfs.MarkerName), 0o750); err != nil {
		t.Fatal(err)
	}
	if _, err := localfs.New(localfs.Options{Root: dir, RequireMarker: true}); err == nil {
		t.Fatal("penanda direktori harus ditolak")
	}
	_ = os.Remove(filepath.Join(dir, localfs.MarkerName))
	// penanda berupa symlink → ditolak
	if err := os.Symlink("/etc/hostname", filepath.Join(dir, localfs.MarkerName)); err != nil {
		t.Fatal(err)
	}
	if _, err := localfs.New(localfs.Options{Root: dir, RequireMarker: true}); err == nil {
		t.Fatal("penanda symlink harus ditolak")
	}
	_ = os.Remove(filepath.Join(dir, localfs.MarkerName))
	// penanda file biasa → lolos
	if err := os.WriteFile(filepath.Join(dir, localfs.MarkerName), nil, 0o640); err != nil {
		t.Fatal(err)
	}
	s, err := localfs.New(localfs.Options{Root: dir, RequireMarker: true})
	if err != nil {
		t.Fatalf("dengan penanda harus lolos: %v", err)
	}
	_ = s.Close()
}

func TestReadyDetectsMarkerDisappearingAtRuntimeAndCaches(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, localfs.MarkerName)
	if err := os.WriteFile(marker, nil, 0o640); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	s, err := localfs.New(localfs.Options{Root: dir, RequireMarker: true, ReadyCacheTTL: 5 * time.Second, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if err := s.Ready(bg); err != nil {
		t.Fatal(err)
	}
	// SSD "dicabut": penanda hilang. Dalam TTL cache hasil lama masih dipakai ...
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	if err := s.Ready(bg); err != nil {
		t.Fatalf("dalam TTL harus memakai cache: %v", err)
	}
	// ... lewat TTL harus terdeteksi
	now = now.Add(4 * time.Second)
	if err := s.Ready(bg); codeOf(err) != "STORAGE_UNAVAILABLE" {
		t.Fatalf("setelah TTL harus STORAGE_UNAVAILABLE, dapat %v", err)
	}
	// SSD kembali → pulih setelah TTL
	if err := os.WriteFile(marker, nil, 0o640); err != nil {
		t.Fatal(err)
	}
	now = now.Add(6 * time.Second)
	if err := s.Ready(bg); err != nil {
		t.Fatalf("harus pulih: %v", err)
	}
}

func TestReadyReportsFullDisk(t *testing.T) {
	s, _ := newStore(t, func(o *localfs.Options) { o.MinFreeBytes = 1 << 62 })
	err := s.Ready(bg)
	if codeOf(err) != "STORAGE_FULL" {
		t.Fatalf("harus STORAGE_FULL, dapat %v", err)
	}
	if de, _ := domain.AsError(err); de.Kind != domain.KindUnavailable {
		t.Fatalf("Kind = %v", de.Kind)
	}
	if !strings.Contains(err.Error(), "byte") {
		t.Fatalf("detail penyebab harus ada di pesan log: %v", err)
	}
	s2, _ := newStore(t, nil)
	if err := s2.Ready(bg); err != nil {
		t.Fatalf("disk normal harus siap: %v", err)
	}
}

// ---------------------------------------------------------------- Stage / Commit

func TestStageCommitRoundTrip(t *testing.T) {
	s, dir := newStore(t, nil)
	key, data := newKey(t), payload(t, 400*1024)

	st, err := s.Stage(bg, key, bytes.NewReader(data), 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Discard()
	if st.Size() != int64(len(data)) {
		t.Fatalf("Size = %d", st.Size())
	}
	want := sha256.Sum256(data)
	if !bytes.Equal(st.SHA256(), want[:]) {
		t.Fatal("SHA256 salah")
	}
	// belum terbit sebelum Commit
	if _, err := os.Stat(filepath.Join(dir, key)); !os.IsNotExist(err) {
		t.Fatalf("file tidak boleh ada di lokasi akhir sebelum Commit: %v", err)
	}
	// bisa dibaca dari tmp untuk validasi
	rc, err := st.Open()
	if err != nil {
		t.Fatal(err)
	}
	head := make([]byte, 16)
	if _, err := io.ReadFull(rc, head); err != nil || !bytes.Equal(head, data[:16]) {
		t.Fatalf("Open harus membaca dari awal: %v", err)
	}
	rc.Close()

	if err := st.Commit(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, key))
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("isi final salah: %v", err)
	}
	if tmp := listTmp(t, dir); len(tmp) != 0 {
		t.Fatalf("tmp harus kosong setelah Commit: %v", tmp)
	}
	fi, _ := os.Stat(filepath.Join(dir, key))
	if fi.Mode().Perm() != 0o640 {
		t.Fatalf("mode file = %o, want 640", fi.Mode().Perm())
	}
	di, _ := os.Stat(filepath.Dir(filepath.Join(dir, key)))
	if di.Mode().Perm() != 0o750 {
		t.Fatalf("mode direktori = %o, want 750", di.Mode().Perm())
	}
	// Discard setelah Commit aman (pola defer) dan tidak menghapus file final
	if err := st.Discard(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, key)); err != nil {
		t.Fatal("Discard setelah Commit tidak boleh menghapus file final")
	}
	// Commit kedua ditolak
	if err := st.Commit(); err == nil {
		t.Fatal("Commit dua kali harus gagal")
	}
}

func TestStageDiscardLeavesNothing(t *testing.T) {
	s, dir := newStore(t, nil)
	key := newKey(t)
	st, err := s.Stage(bg, key, bytes.NewReader(payload(t, 1000)), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Discard(); err != nil {
		t.Fatal(err)
	}
	if err := st.Discard(); err != nil {
		t.Fatalf("Discard harus idempotent: %v", err)
	}
	if tmp := listTmp(t, dir); len(tmp) != 0 {
		t.Fatalf("tmp tidak kosong: %v", tmp)
	}
	if _, err := os.Stat(filepath.Join(dir, key)); !os.IsNotExist(err) {
		t.Fatal("tidak boleh ada file final")
	}
	if _, err := st.Open(); err == nil {
		t.Fatal("Open setelah Discard harus gagal")
	}
}

func TestStageZeroBytesAllowed(t *testing.T) {
	s, _ := newStore(t, nil)
	st, err := s.Stage(bg, newKey(t), bytes.NewReader(nil), 100)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Discard()
	if st.Size() != 0 {
		t.Fatalf("Size = %d", st.Size())
	}
}

func TestStageRejectsOversize(t *testing.T) {
	s, dir := newStore(t, nil)
	// tepat batas: lolos
	st, err := s.Stage(bg, newKey(t), bytes.NewReader(payload(t, 1000)), 1000)
	if err != nil {
		t.Fatalf("tepat batas harus lolos: %v", err)
	}
	st.Discard()
	// satu byte lebih: ditolak, tmp bersih
	_, err = s.Stage(bg, newKey(t), bytes.NewReader(payload(t, 1001)), 1000)
	if codeOf(err) != "SIZE_MISMATCH" {
		t.Fatalf("harus SIZE_MISMATCH, dapat %v", err)
	}
	if tmp := listTmp(t, dir); len(tmp) != 0 {
		t.Fatalf("tmp tidak bersih setelah ditolak: %v", tmp)
	}
	// pembaca tak berujung tidak boleh menulis tanpa batas
	_, err = s.Stage(bg, newKey(t), zeroReader{}, 4096)
	if codeOf(err) != "SIZE_MISMATCH" {
		t.Fatalf("pembaca tak berujung harus dipotong: %v", err)
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }

type failingReader struct {
	data []byte
	err  error
}

func (f *failingReader) Read(p []byte) (int, error) {
	if len(f.data) == 0 {
		return 0, f.err
	}
	n := copy(p, f.data)
	f.data = f.data[n:]
	return n, nil
}

func TestInterruptedWriteLeavesNoFinalFileAndKeepsOldContent(t *testing.T) {
	s, dir := newStore(t, nil)
	key := newKey(t)
	old := payload(t, 5000)

	st, err := s.Stage(bg, key, bytes.NewReader(old), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Commit(); err != nil {
		t.Fatal(err)
	}

	// PUT ulang yang terputus di tengah jalan
	boom := errors.New("koneksi putus")
	_, err = s.Stage(bg, key, &failingReader{data: payload(t, 3000), err: boom}, 1<<20)
	if !errors.Is(err, boom) {
		t.Fatalf("error pembaca harus diteruskan: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, key))
	if err != nil || !bytes.Equal(got, old) {
		t.Fatal("isi lama harus utuh saat penulisan baru terputus (atomik)")
	}
	if tmp := listTmp(t, dir); len(tmp) != 0 {
		t.Fatalf("tmp harus bersih: %v", tmp)
	}

	// kunci baru yang terputus: tidak ada file final sama sekali
	key2 := newKey(t)
	_, err = s.Stage(bg, key2, &failingReader{data: payload(t, 100), err: io.ErrUnexpectedEOF}, 1<<20)
	if err == nil {
		t.Fatal("seharusnya gagal")
	}
	if _, err := os.Stat(filepath.Join(dir, key2)); !os.IsNotExist(err) {
		t.Fatal("tidak boleh ada file final dari upload yang terputus")
	}
}

func TestStageHonorsContextCancellation(t *testing.T) {
	s, dir := newStore(t, nil)
	ctx, cancel := context.WithCancel(bg)
	pr, pw := io.Pipe()
	go func() {
		_, _ = pw.Write(payload(t, 100))
		cancel() // dibatalkan di tengah aliran
		_, _ = pw.Write(payload(t, 100))
		_ = pw.Close()
	}()
	_, err := s.Stage(ctx, newKey(t), pr, 1<<20)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("harus context.Canceled, dapat %v", err)
	}
	if tmp := listTmp(t, dir); len(tmp) != 0 {
		t.Fatalf("tmp harus bersih: %v", tmp)
	}
}

func TestCommitOverwritesAtomically(t *testing.T) {
	s, dir := newStore(t, nil)
	key := newKey(t)
	for i, size := range []int{1000, 300, 5000} {
		data := payload(t, size)
		st, err := s.Stage(bg, key, bytes.NewReader(data), 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		if err := st.Commit(); err != nil {
			t.Fatal(err)
		}
		got, _ := os.ReadFile(filepath.Join(dir, key))
		if !bytes.Equal(got, data) {
			t.Fatalf("putaran %d: isi tidak sama dengan tulisan terakhir", i)
		}
	}
	if tmp := listTmp(t, dir); len(tmp) != 0 {
		t.Fatalf("tmp: %v", tmp)
	}
}

func TestConcurrentStageCommit(t *testing.T) {
	s, dir := newStore(t, nil)
	var wg sync.WaitGroup
	keys := make([]string, 40)
	datas := make([][]byte, 40)
	for i := range keys {
		keys[i], datas[i] = newKey(t), payload(t, 50_000+i)
	}
	for i := range keys {
		wg.Add(1)
		go func() {
			defer wg.Done()
			st, err := s.Stage(bg, keys[i], bytes.NewReader(datas[i]), 1<<20)
			if err != nil {
				t.Errorf("stage %d: %v", i, err)
				return
			}
			if err := st.Commit(); err != nil {
				t.Errorf("commit %d: %v", i, err)
			}
		}()
	}
	wg.Wait()
	for i := range keys {
		got, err := os.ReadFile(filepath.Join(dir, keys[i]))
		if err != nil || !bytes.Equal(got, datas[i]) {
			t.Errorf("file %d rusak/hilang: %v", i, err)
		}
	}
	if tmp := listTmp(t, dir); len(tmp) != 0 {
		t.Fatalf("tmp: %v", tmp)
	}
}

func TestConcurrentWritersSameKeyNeverMixContent(t *testing.T) {
	s, dir := newStore(t, nil)
	key := newKey(t)
	const n = 20
	datas := make([][]byte, n)
	var wg sync.WaitGroup
	for i := range datas {
		datas[i] = bytes.Repeat([]byte{byte('A' + i)}, 200_000)
		wg.Add(1)
		go func() {
			defer wg.Done()
			st, err := s.Stage(bg, key, bytes.NewReader(datas[i]), 1<<20)
			if err != nil {
				t.Error(err)
				return
			}
			if err := st.Commit(); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	got, err := os.ReadFile(filepath.Join(dir, key))
	if err != nil {
		t.Fatal(err)
	}
	match := false
	for _, d := range datas {
		if bytes.Equal(got, d) {
			match = true
		}
	}
	if !match {
		t.Fatal("isi akhir bukan salah satu tulisan utuh (tercampur)")
	}
}

// ---------------------------------------------------------------- kunci tak sah & os.Root

func TestInvalidKeysRejectedEverywhere(t *testing.T) {
	s, dir := newStore(t, nil)
	outside := filepath.Join(filepath.Dir(dir), "rahasia.txt")
	if err := os.WriteFile(outside, []byte("rahasia"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "../rahasia.txt", "/etc/passwd", "2026/10/24/../../../rahasia.txt", "tmp/x.part", "2026/10/24/foto.webp", "a\x00b"} {
		if _, err := s.Stage(bg, bad, strings.NewReader("x"), 10); !errors.Is(err, localfs.ErrInvalidKey) {
			t.Errorf("Stage(%q): %v", bad, err)
		}
		if _, err := s.Open(bg, bad); !errors.Is(err, localfs.ErrInvalidKey) {
			t.Errorf("Open(%q): %v", bad, err)
		}
		if _, err := s.Stat(bg, bad); !errors.Is(err, localfs.ErrInvalidKey) {
			t.Errorf("Stat(%q): %v", bad, err)
		}
		if err := s.Delete(bg, bad); !errors.Is(err, localfs.ErrInvalidKey) {
			t.Errorf("Delete(%q): %v", bad, err)
		}
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatal("file di luar root tidak boleh tersentuh")
	}
}

func TestSymlinkEscapingRootIsRefused(t *testing.T) {
	s, dir := newStore(t, nil)
	outside := filepath.Join(filepath.Dir(dir), "target-luar.txt")
	if err := os.WriteFile(outside, []byte("di luar root"), 0o600); err != nil {
		t.Fatal(err)
	}
	key := newKey(t)
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, key)), 0o750); err != nil {
		t.Fatal(err)
	}
	// seseorang menaruh symlink yang menunjuk keluar root (kunci tetap kanonik)
	if err := os.Symlink(outside, filepath.Join(dir, key)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Open(bg, key); err == nil {
		t.Fatal("Open lewat symlink keluar root harus ditolak (os.Root)")
	}
	if _, err := s.Stat(bg, key); err == nil {
		t.Fatal("Stat lewat symlink keluar root harus ditolak")
	}
}

// ---------------------------------------------------------------- Open / Stat / Delete

func TestOpenStatDelete(t *testing.T) {
	s, _ := newStore(t, nil)
	key, data := newKey(t), payload(t, 100_000)
	st, err := s.Stage(bg, key, bytes.NewReader(data), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Commit(); err != nil {
		t.Fatal(err)
	}

	fi, err := s.Stat(bg, key)
	if err != nil || fi.Size != int64(len(data)) || time.Since(fi.ModTime) > time.Minute {
		t.Fatalf("Stat: %+v %v", fi, err)
	}
	f, err := s.Open(bg, key)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if f.Info().Size != int64(len(data)) {
		t.Fatalf("Info.Size = %d", f.Info().Size)
	}
	// Range: seek ke tengah lalu baca
	if _, err := f.Seek(1000, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 50)
	if _, err := io.ReadFull(f, buf); err != nil || !bytes.Equal(buf, data[1000:1050]) {
		t.Fatalf("seek+read salah: %v", err)
	}

	if err := s.Delete(bg, key); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(bg, key); err != nil {
		t.Fatalf("Delete harus idempotent: %v", err)
	}
	if _, err := s.Stat(bg, key); !errors.Is(err, usecase.ErrFileNotFound) {
		t.Fatalf("Stat setelah Delete: %v", err)
	}
	if _, err := s.Open(bg, key); !errors.Is(err, usecase.ErrFileNotFound) {
		t.Fatalf("Open setelah Delete: %v", err)
	}
	if _, err := s.Open(bg, newKey(t)); !errors.Is(err, usecase.ErrFileNotFound) {
		t.Fatalf("Open file yang tak pernah ada: %v", err)
	}
}

func TestOpenRejectsDirectory(t *testing.T) {
	s, dir := newStore(t, nil)
	key := newKey(t)
	if err := os.MkdirAll(filepath.Join(dir, key), 0o750); err != nil { // direktori bernama seperti kunci
		t.Fatal(err)
	}
	if _, err := s.Open(bg, key); err == nil {
		t.Fatal("direktori tidak boleh dibuka sebagai foto")
	}
	if _, err := s.Stat(bg, key); err == nil {
		t.Fatal("direktori tidak boleh lolos Stat")
	}
}

// ---------------------------------------------------------------- pemetaan error I/O

func TestStageMapsDiskErrors(t *testing.T) {
	s, _ := newStore(t, nil)
	// /dev/full: setiap tulisan menghasilkan ENOSPC — dipakai lewat pembaca yang gagal ENOSPC
	_, err := s.Stage(bg, newKey(t), &failingReader{data: []byte("x"), err: fmt.Errorf("tulis: %w", errNoSpace)}, 100)
	if codeOf(err) != "STORAGE_FULL" {
		t.Fatalf("ENOSPC harus dipetakan ke STORAGE_FULL: %v", err)
	}
	_, err = s.Stage(bg, newKey(t), &failingReader{data: []byte("x"), err: errIO}, 100)
	if codeOf(err) != "STORAGE_UNAVAILABLE" {
		t.Fatalf("EIO harus dipetakan ke STORAGE_UNAVAILABLE: %v", err)
	}
	_, err = s.Stage(bg, newKey(t), &failingReader{data: []byte("x"), err: errors.New("lain-lain")}, 100)
	if err == nil || codeOf(err) != "" {
		t.Fatalf("error lain diteruskan apa adanya: %v", err)
	}
}

func mustNew(t *testing.T, root string, marker bool) *localfs.Store {
	t.Helper()
	s, err := newWithMarkerOpt(root, marker)
	if err != nil {
		t.Fatalf("New(%s): %v", root, err)
	}
	return s
}

func newWithMarker(root string) (*localfs.Store, error) { return newWithMarkerOpt(root, true) }

func newWithMarkerOpt(root string, marker bool) (*localfs.Store, error) {
	return localfs.New(localfs.Options{Root: root, RequireMarker: marker})
}

// ---------------------------------------------------------------- SweepTmp / WalkOlderThan

func age(t *testing.T, path string, d time.Duration) {
	t.Helper()
	old := time.Now().Add(-d)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
}

func TestSweepTmp(t *testing.T) {
	s, dir := newStore(t, nil)
	tmp := filepath.Join(dir, "tmp")
	mk := func(name string, a time.Duration) string {
		p := filepath.Join(tmp, name)
		if err := os.WriteFile(p, []byte("x"), 0o640); err != nil {
			t.Fatal(err)
		}
		age(t, p, a)
		return p
	}
	oldPart := mk("a.webp.1111.part", 2*time.Hour)
	youngPart := mk("b.webp.2222.part", 5*time.Minute)
	oldProbe := mk(".probe-abc", 3*time.Hour)
	other := mk("catatan.txt", 5*time.Hour) // bukan milik kita: jangan disentuh
	if err := os.Mkdir(filepath.Join(tmp, "subdir.part"), 0o750); err != nil {
		t.Fatal(err)
	}

	n, err := s.SweepTmp(bg, time.Now().Add(-time.Hour))
	if err != nil || n != 2 {
		t.Fatalf("n=%d err=%v, want 2", n, err)
	}
	for p, want := range map[string]bool{oldPart: false, oldProbe: false, youngPart: true, other: true} {
		_, err := os.Stat(p)
		if (err == nil) != want {
			t.Errorf("%s ada=%v, want %v", filepath.Base(p), err == nil, want)
		}
	}
	if _, err := os.Stat(filepath.Join(tmp, "subdir.part")); err != nil {
		t.Fatal("direktori tidak boleh dihapus")
	}
	// idempotent
	if n, err := s.SweepTmp(bg, time.Now().Add(-time.Hour)); err != nil || n != 0 {
		t.Fatalf("putaran kedua: n=%d err=%v", n, err)
	}
}

func TestSweepTmpHonorsContext(t *testing.T) {
	s, dir := newStore(t, nil)
	for i := 0; i < 3; i++ {
		p := filepath.Join(dir, "tmp", fmt.Sprintf("f%d.part", i))
		_ = os.WriteFile(p, []byte("x"), 0o640)
		age(t, p, 2*time.Hour)
	}
	ctx, cancel := context.WithCancel(bg)
	cancel()
	if _, err := s.SweepTmp(ctx, time.Now()); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestWalkOlderThanFiltersAndOrders(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, localfs.MarkerName), nil, 0o640); err != nil {
		t.Fatal(err)
	}
	s, err := localfs.New(localfs.Options{Root: dir, RequireMarker: true})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	put := func(day int, ext string, a time.Duration) string {
		id := uuid.New()
		key := fmt.Sprintf("2026/10/%02d/%s.%s", day, id, ext)
		p := filepath.Join(dir, key)
		_ = os.MkdirAll(filepath.Dir(p), 0o750)
		if err := os.WriteFile(p, []byte("x"), 0o640); err != nil {
			t.Fatal(err)
		}
		age(t, p, a)
		return key
	}
	k22 := put(22, "webp", 48*time.Hour)
	k20 := put(20, "jpg", 72*time.Hour)
	put(23, "webp", time.Hour) // terlalu muda
	// berkas asing tidak boleh muncul
	_ = os.WriteFile(filepath.Join(dir, "2026", "10", "20", "catatan.txt"), []byte("x"), 0o640)
	age(t, filepath.Join(dir, "2026", "10", "20", "catatan.txt"), 100*time.Hour)
	_ = os.WriteFile(filepath.Join(dir, "tmp", "x.webp.1.part"), []byte("x"), 0o640)
	age(t, filepath.Join(dir, "tmp", "x.webp.1.part"), 100*time.Hour)
	age(t, filepath.Join(dir, localfs.MarkerName), 100*time.Hour)

	var got []string
	err = s.WalkOlderThan(bg, time.Now().Add(-24*time.Hour), func(key string) error {
		got = append(got, key)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != k20 || got[1] != k22 {
		t.Fatalf("got %v, want [%s %s] (berurutan menurut kunci, tanpa tmp/penanda/berkas asing/file muda)", got, k20, k22)
	}

	// fn mengembalikan error → berhenti dan error diteruskan
	boom := errors.New("berhenti")
	calls := 0
	err = s.WalkOlderThan(bg, time.Now().Add(-24*time.Hour), func(string) error { calls++; return boom })
	if !errors.Is(err, boom) || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
	// konteks batal
	ctx, cancel := context.WithCancel(bg)
	cancel()
	if err := s.WalkOlderThan(ctx, time.Now(), func(string) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}
