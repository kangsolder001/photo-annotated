// Package turnstile memverifikasi token Cloudflare Turnstile lewat endpoint siteverify.
package turnstile

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"photo-annotated/internal/domain"
	"photo-annotated/internal/usecase"
)

// DefaultEndpoint adalah endpoint verifikasi resmi Cloudflare.
const DefaultEndpoint = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

const maxResponseBytes = 16 << 10

var _ usecase.TurnstileVerifier = (*Verifier)(nil)

type Verifier struct {
	secret   string
	endpoint string
	http     *http.Client
}

// New membuat verifier. endpoint kosong memakai DefaultEndpoint (override dipakai test).
func New(secret, endpoint string, timeout time.Duration) (*Verifier, error) {
	if strings.TrimSpace(secret) == "" {
		return nil, errors.New("turnstile: secret wajib diisi")
	}
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	if timeout <= 0 {
		return nil, errors.New("turnstile: timeout harus > 0")
	}
	return &Verifier{
		secret: secret, endpoint: endpoint,
		http: &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}, nil
}

func failed() error {
	return domain.Forbidden("TURNSTILE_FAILED", "human verification failed; reload the page and try again")
}

func unavailable(cause error) error {
	return errors.Join(domain.Unavailable("TURNSTILE_UNAVAILABLE", "human verification is temporarily unavailable; try again shortly"), cause)
}

// Verify memeriksa token. Token kosong ditolak tanpa menghubungi Cloudflare. Gagal tertutup:
// bila Cloudflare tidak terjangkau atau menjawab tak terduga, hasilnya Unavailable (bukan lolos).
func (v *Verifier) Verify(ctx context.Context, token, remoteIP string) error {
	token = strings.TrimSpace(token)
	if token == "" || len(token) > 2048 {
		return failed()
	}
	form := url.Values{"secret": {v.secret}, "response": {token}}
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return unavailable(errors.New("build request"))
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := v.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err // tanpa URL lengkap
		}
		return unavailable(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return unavailable(errors.New("siteverify returned a non-200 status"))
	}
	var out struct {
		Success bool `json:"success"`
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err := json.Unmarshal(raw, &out); err != nil {
		return unavailable(errors.New("siteverify returned an unexpected body"))
	}
	if !out.Success {
		return failed()
	}
	return nil
}
