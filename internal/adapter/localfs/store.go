// Package localfs adalah adapter PhotoStore di atas filesystem lokal (SSD ext4).
//
// Semua akses file lewat os.Root sehingga path tidak bisa keluar dari root foto (lewat ".."
// maupun symlink), dan setiap kunci divalidasi ketat lebih dulu. Penulisan memakai pola
// tmp → fsync → rename → fsync direktori: mati listrik tidak pernah meninggalkan file
// separuh di lokasi akhir.
package localfs

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"photo-annotated/internal/domain"
	"photo-annotated/internal/usecase"
)

const (
	// MarkerName adalah file penanda yang dibuat operator sekali di SSD (lihat spec §8.2).
	MarkerName = ".photo-annotated-store"

	tmpDir      = "tmp"
	dirMode     = 0o750
	fileMode    = 0o640
	copyBufSize = 32 << 10
	defaultTTL  = 5 * time.Second
)

// ErrInvalidKey berarti kunci penyimpanan tidak berbentuk kanonik (bug pemanggil atau serangan).
var ErrInvalidKey = errors.New("localfs: invalid storage key")

type Options struct {
	Root          string // path absolut root foto
	RequireMarker bool   // wajib ada file penanda MarkerName di Root
	MinFreeBytes  int64  // di bawah ini Ready mengembalikan STORAGE_FULL
	ReadyCacheTTL time.Duration
	Now           func() time.Time // untuk test
}

type Store struct {
	opts Options
	root *os.Root

	mu        sync.Mutex
	lastCheck time.Time
	lastErr   error
}

var _ usecase.PhotoStore = (*Store)(nil)

// New membuka root foto dan memeriksa kondisinya. Masalah struktural (bukan direktori,
// penanda hilang, tidak bisa ditulis) membuat New gagal; ruang disk menipis tidak, karena
// lebih berguna menyala dan menjawab 503 STORAGE_FULL daripada crash-loop.
func New(opts Options) (*Store, error) {
	if !filepath.IsAbs(opts.Root) {
		return nil, fmt.Errorf("localfs: root harus path absolut: %q", opts.Root)
	}
	if opts.ReadyCacheTTL <= 0 {
		opts.ReadyCacheTTL = defaultTTL
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	fi, err := os.Stat(opts.Root)
	if err != nil {
		return nil, fmt.Errorf("localfs: root tidak bisa diakses: %w", err)
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("localfs: root bukan direktori: %q", opts.Root)
	}
	r, err := os.OpenRoot(opts.Root)
	if err != nil {
		return nil, fmt.Errorf("localfs: membuka root: %w", err)
	}
	s := &Store{opts: opts, root: r}
	if err := s.checkStructure(); err != nil {
		_ = r.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.root.Close() }

// ---------------------------------------------------------------- kesiapan

// Ready memeriksa penyimpanan siap; hasil di-cache selama ReadyCacheTTL.
func (s *Store) Ready(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.lastCheck.IsZero() && s.opts.Now().Sub(s.lastCheck) < s.opts.ReadyCacheTTL {
		return s.lastErr
	}
	err := s.checkStructure()
	if err == nil {
		err = s.checkFreeSpace()
	}
	s.lastCheck, s.lastErr = s.opts.Now(), err
	return err
}

// checkStructure: penanda ada, tmp/ ada, dan root bisa ditulis.
func (s *Store) checkStructure() error {
	if s.opts.RequireMarker {
		fi, err := s.root.Lstat(MarkerName)
		if err != nil {
			return unavailable(fmt.Errorf("file penanda %s tidak ditemukan di %s (SSD belum ter-mount?): %w", MarkerName, s.opts.Root, err))
		}
		if !fi.Mode().IsRegular() {
			return unavailable(fmt.Errorf("%s harus berupa file biasa", MarkerName))
		}
	}
	if err := s.root.MkdirAll(tmpDir, dirMode); err != nil {
		return mapIOError(fmt.Errorf("membuat %s: %w", tmpDir, err))
	}
	probe := path.Join(tmpDir, ".probe-"+randHex(6))
	f, err := s.root.OpenFile(probe, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return mapIOError(fmt.Errorf("probe tulis: %w", err))
	}
	_, werr := f.Write([]byte{0})
	cerr := f.Close()
	rerr := s.root.Remove(probe)
	if err := errors.Join(werr, cerr, rerr); err != nil {
		return mapIOError(fmt.Errorf("probe tulis: %w", err))
	}
	return nil
}

func (s *Store) checkFreeSpace() error {
	var st syscall.Statfs_t
	if err := syscall.Statfs(s.opts.Root, &st); err != nil {
		return unavailable(fmt.Errorf("statfs: %w", err))
	}
	free := int64(st.Bavail) * int64(st.Bsize) //nolint:gosec // ukuran disk jauh di bawah int64
	if free < s.opts.MinFreeBytes {
		return fmt.Errorf("%w: sisa %d byte < batas %d byte", errFull, free, s.opts.MinFreeBytes)
	}
	return nil
}

// ---------------------------------------------------------------- tulis

type staged struct {
	s    *Store
	key  string
	tmp  string
	size int64
	sum  []byte

	mu   sync.Mutex
	done bool
}

// Stage menulis r ke tmp/ dan menghitung SHA-256. Tidak ada yang terbit sampai Commit.
func (s *Store) Stage(ctx context.Context, key string, r io.Reader, maxBytes int64) (usecase.StagedFile, error) {
	if !domain.IsValidStorageKey(key) {
		return nil, ErrInvalidKey
	}
	if maxBytes < 1 {
		return nil, errors.New("localfs: maxBytes harus >= 1")
	}
	tmp := path.Join(tmpDir, fmt.Sprintf("%s.%s.part", path.Base(key), randHex(8)))
	f, err := s.root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, fileMode)
	if err != nil {
		return nil, mapIOError(fmt.Errorf("membuat file sementara: %w", err))
	}
	cleanup := func() { _ = s.root.Remove(tmp) }

	h := sha256.New()
	buf := make([]byte, copyBufSize)
	// +1 agar kelebihan satu byte pun terdeteksi
	n, err := io.CopyBuffer(io.MultiWriter(f, h), &ctxReader{ctx: ctx, r: io.LimitReader(r, maxBytes+1)}, buf)
	cerr := f.Close()
	if err == nil {
		err = cerr
	}
	if err != nil {
		cleanup()
		return nil, mapIOError(err)
	}
	if n > maxBytes {
		cleanup()
		return nil, domain.Invalid("SIZE_MISMATCH", "body is larger than the allowed %d bytes", maxBytes)
	}
	return &staged{s: s, key: key, tmp: tmp, size: n, sum: h.Sum(nil)}, nil
}

func (st *staged) Size() int64    { return st.size }
func (st *staged) SHA256() []byte { return append([]byte(nil), st.sum...) }

func (st *staged) Open() (io.ReadCloser, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.done {
		return nil, errors.New("localfs: staged file sudah selesai")
	}
	return st.s.root.Open(st.tmp)
}

func (st *staged) Commit() error {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.done {
		return errors.New("localfs: staged file sudah selesai")
	}
	st.done = true
	if err := st.commit(); err != nil {
		_ = st.s.root.Remove(st.tmp)
		return mapIOError(err)
	}
	return nil
}

func (st *staged) commit() error {
	s := st.s
	// 1. data ke disk sebelum terbit
	f, err := s.root.Open(st.tmp)
	if err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	// 2. direktori tujuan
	dir := path.Dir(st.key)
	created, err := s.ensureDir(dir)
	if err != nil {
		return err
	}
	// 3. terbit secara atomik (menimpa file lama bila PUT diulang)
	if err := s.root.Rename(st.tmp, st.key); err != nil {
		return err
	}
	// 4. catatan direktori ke disk (entri rename, dan direktori baru di induknya)
	if err := s.syncDir(dir); err != nil {
		return err
	}
	for _, d := range created {
		if err := s.syncDir(path.Dir(d)); err != nil {
			return err
		}
	}
	return nil
}

func (st *staged) Discard() error {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.done {
		return nil
	}
	st.done = true
	if err := st.s.root.Remove(st.tmp); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return mapIOError(err)
	}
	return nil
}

// ensureDir membuat dir (dan induknya) dan mengembalikan direktori yang baru dibuat.
func (s *Store) ensureDir(dir string) (created []string, err error) {
	var chain []string // dari terdalam ke teratas
	for d := dir; d != "." && d != "/" && d != ""; d = path.Dir(d) {
		chain = append(chain, d)
	}
	for i := len(chain) - 1; i >= 0; i-- { // dari teratas
		d := chain[i]
		if _, serr := s.root.Stat(d); serr == nil {
			continue
		} else if !errors.Is(serr, fs.ErrNotExist) {
			return nil, serr
		}
		if err := s.root.Mkdir(d, dirMode); err != nil && !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
		created = append(created, d)
	}
	return created, nil
}

func (s *Store) syncDir(dir string) error {
	d, err := s.root.Open(dir)
	if err != nil {
		return err
	}
	if err := d.Sync(); err != nil {
		_ = d.Close()
		return err
	}
	return d.Close()
}

// ---------------------------------------------------------------- baca & hapus

type file struct {
	*os.File
	info usecase.FileInfo
}

func (f *file) Info() usecase.FileInfo { return f.info }

func (s *Store) Open(_ context.Context, key string) (usecase.File, error) {
	if !domain.IsValidStorageKey(key) {
		return nil, ErrInvalidKey
	}
	f, err := s.root.Open(key)
	if err != nil {
		return nil, mapOpenError(err)
	}
	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, mapIOError(err)
	}
	if !fi.Mode().IsRegular() {
		_ = f.Close()
		return nil, fmt.Errorf("localfs: %s bukan file biasa", key)
	}
	return &file{File: f, info: usecase.FileInfo{Size: fi.Size(), ModTime: fi.ModTime()}}, nil
}

func (s *Store) Stat(_ context.Context, key string) (usecase.FileInfo, error) {
	if !domain.IsValidStorageKey(key) {
		return usecase.FileInfo{}, ErrInvalidKey
	}
	fi, err := s.root.Stat(key)
	if err != nil {
		return usecase.FileInfo{}, mapOpenError(err)
	}
	if !fi.Mode().IsRegular() {
		return usecase.FileInfo{}, fmt.Errorf("localfs: %s bukan file biasa", key)
	}
	return usecase.FileInfo{Size: fi.Size(), ModTime: fi.ModTime()}, nil
}

func (s *Store) Delete(_ context.Context, key string) error {
	if !domain.IsValidStorageKey(key) {
		return ErrInvalidKey
	}
	if err := s.root.Remove(key); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return mapIOError(err)
	}
	return nil
}

// ---------------------------------------------------------------- util

type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand: " + err.Error())
	}
	return hex.EncodeToString(b)
}

var errFull = domain.Unavailable("STORAGE_FULL", "photo storage is full")

func unavailable(cause error) error {
	return fmt.Errorf("%w: %v", domain.Unavailable("STORAGE_UNAVAILABLE", "photo storage is unavailable"), cause)
}

// mapIOError menerjemahkan error I/O tingkat OS menjadi error domain yang bermakna;
// error lain dikembalikan apa adanya.
func mapIOError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, syscall.ENOSPC), errors.Is(err, syscall.EDQUOT):
		return fmt.Errorf("%w: %v", errFull, err)
	case errors.Is(err, syscall.EIO), errors.Is(err, syscall.EROFS), errors.Is(err, syscall.ENODEV),
		errors.Is(err, syscall.ENXIO), errors.Is(err, syscall.ESTALE), errors.Is(err, syscall.EUCLEAN):
		return unavailable(err)
	}
	return err
}

func mapOpenError(err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: %v", usecase.ErrFileNotFound, err)
	}
	return mapIOError(err)
}
