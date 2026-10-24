package httpx_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"photo-annotated/internal/delivery/http/httpx"
	"photo-annotated/internal/domain"
)

func init() { gin.SetMode(gin.TestMode) }

func TestWriteErrorMapsEveryKind(t *testing.T) {
	cases := []struct {
		err    *domain.Error
		status int
	}{
		{domain.Invalid("A", "m"), http.StatusUnprocessableEntity},
		{domain.NotFound("B", "m"), http.StatusNotFound},
		{domain.Unauthorized("C", "m"), http.StatusUnauthorized},
		{domain.Forbidden("D", "m"), http.StatusForbidden},
		{domain.Conflict("E", "m"), http.StatusConflict},
		{domain.Gone("F", "m"), http.StatusGone},
		{domain.Unavailable("G", "m"), http.StatusServiceUnavailable},
		{domain.RateLimited("H", "m"), http.StatusTooManyRequests},
		{domain.LengthRequired("I", "m"), http.StatusLengthRequired},
		{domain.Timeout("J", "m"), http.StatusRequestTimeout},
	}
	for _, tc := range cases {
		t.Run(tc.err.Code, func(t *testing.T) {
			w := write(tc.err)
			if w.Code != tc.status {
				t.Fatalf("status = %d, want %d", w.Code, tc.status)
			}
			var b struct {
				Error struct{ Code, Message string }
			}
			if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil || b.Error.Code != tc.err.Code || b.Error.Message != "m" {
				t.Fatalf("body: %s (%v)", w.Body.String(), err)
			}
		})
	}
}

func TestWriteErrorSetsRetryAfter(t *testing.T) {
	for _, tc := range []struct {
		d    time.Duration
		want string
	}{{2 * time.Second, "2"}, {1500 * time.Millisecond, "2"}, {200 * time.Millisecond, "1"}, {0, ""}} {
		w := write(domain.Unavailable("UPLOAD_BUSY", "busy").WithRetryAfter(tc.d))
		if got := w.Header().Get("Retry-After"); got != tc.want {
			t.Errorf("RetryAfter %v → header %q, want %q", tc.d, got, tc.want)
		}
	}
	// WithRetryAfter tidak boleh mengubah sentinel/error asli
	base := domain.Unavailable("X", "m")
	_ = base.WithRetryAfter(time.Minute)
	if base.RetryAfter != 0 {
		t.Fatal("WithRetryAfter harus mengembalikan salinan")
	}
}

func TestWriteErrorUnwrapsAndHidesCause(t *testing.T) {
	cause := errors.New("detail internal: disk /dev/sda1 I/O error")
	err := fmt.Errorf("%w: %v", domain.Unavailable("STORAGE_UNAVAILABLE", "photo storage is unavailable"), cause)
	w := write(err)
	if w.Code != 503 {
		t.Fatalf("status = %d", w.Code)
	}
	if body := w.Body.String(); contains(body, "sda1") || contains(body, "detail internal") {
		t.Fatalf("penyebab internal bocor ke klien: %s", body)
	}
}

func TestWriteErrorUnknownBecomes500(t *testing.T) {
	w := write(errors.New("rahasia: password=hunter2"))
	if w.Code != 500 || contains(w.Body.String(), "hunter2") || !contains(w.Body.String(), `"INTERNAL"`) {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestWriteErrorKeepsCauseInGinErrors(t *testing.T) {
	var recorded error
	r := gin.New()
	r.GET("/", func(c *gin.Context) {
		httpx.WriteError(c, errors.New("penyebab asli"))
		if len(c.Errors) > 0 {
			recorded = c.Errors.Last().Err
		}
	})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	if recorded == nil || recorded.Error() != "penyebab asli" {
		t.Fatalf("penyebab harus dicatat di c.Errors untuk log: %v", recorded)
	}
}

func write(err error) *httptest.ResponseRecorder {
	r := gin.New()
	r.GET("/", func(c *gin.Context) { httpx.WriteError(c, err) })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	return w
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
