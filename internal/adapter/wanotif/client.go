// Package wanotif adalah adapter OTPSender ke layanan wa-notif (gateway WhatsApp di Raspi).
//
// wa-notif menerima nomor tujuan mentah, termasuk JID grup ("…@g.us"), dan tidak memeriksa apa pun
// selain format. Karena itu adapter ini menolak nomor yang bukan bentuk kanonik 628… sebelum
// menghubungi jaringan: tidak ada jalur kode yang bisa membuat pesan OTP terkirim ke tujuan selain
// nomor seluler Indonesia yang sudah dinormalisasi.
package wanotif

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"photo-annotated/internal/domain"
	"photo-annotated/internal/usecase"
)

const maxResponseBytes = 64 << 10

var (
	_ usecase.OTPSender    = (*Client)(nil)
	_ usecase.ChannelProbe = (*Client)(nil)
)

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// New membuat client. timeout membatasi satu panggilan penuh (koneksi + respons).
func New(baseURL, token string, timeout time.Duration) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("wanotif: base_url tidak valid: %q", baseURL)
	}
	if timeout <= 0 {
		return nil, errors.New("wanotif: timeout harus > 0")
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http: &http.Client{
			Timeout: timeout,
			// wa-notif tidak pernah mengalihkan; jangan ikuti redirect (mencegah token terkirim ke tempat lain)
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

type apiResponse struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   *struct {
		Code string `json:"code"`
	} `json:"error"`
}

func sendErr(reason string, retryable, alert bool, format string, a ...any) *usecase.SendError {
	return &usecase.SendError{Reason: reason, Retryable: retryable, Alert: alert, Err: fmt.Errorf(format, a...)}
}

// Send mengirim pesan teks. phone harus nomor kanonik.
func (c *Client) Send(ctx context.Context, phone, message string) (string, error) {
	if !domain.IsCanonicalPhone(phone) {
		// pagar terakhir: jangan pernah meneruskan nilai selain nomor kanonik ke wa-notif
		return "", sendErr(domain.OTPFailSendFailed, false, true, "refusing to send: destination is not a canonical phone number")
	}
	body, err := json.Marshal(map[string]string{"to": phone, "message": message})
	if err != nil {
		return "", sendErr(domain.OTPFailSendFailed, false, true, "encode request: %v", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/messages/send", bytes.NewReader(body))
	if err != nil {
		return "", sendErr(domain.OTPFailSendFailed, false, true, "build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err() // shutdown/pembatalan: bukan kegagalan gateway
		}
		// pesan error net/http memuat URL tetapi tidak body/token
		return "", sendErr(domain.OTPFailWAUnavailable, true, false, "request failed: %s", redactURLError(err))
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	var out apiResponse
	_ = json.Unmarshal(raw, &out)
	code := ""
	if out.Error != nil {
		code = out.Error.Code
	}

	switch {
	case resp.StatusCode == http.StatusOK:
		var data struct {
			MessageID string `json:"message_id"`
		}
		if out.Success && json.Unmarshal(out.Data, &data) == nil && data.MessageID != "" {
			return data.MessageID, nil
		}
		// 200 tanpa bentuk yang kita kenal: kemungkinan proxy/portal di tengah jalan
		return "", sendErr(domain.OTPFailWAUnavailable, true, false, "unexpected 200 response from gateway")
	case resp.StatusCode == http.StatusUnauthorized:
		return "", sendErr(domain.OTPFailWAUnavailable, false, true, "gateway rejected the token (HTTP 401)")
	case resp.StatusCode == http.StatusServiceUnavailable:
		return "", sendErr(domain.OTPFailWAUnavailable, true, false, "gateway not connected (HTTP 503 %s)", code)
	case resp.StatusCode == http.StatusInternalServerError:
		return "", sendErr(domain.OTPFailSendFailed, true, false, "gateway failed to send (HTTP 500 %s)", code)
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return "", sendErr(domain.OTPFailWAUnavailable, true, false, "gateway unavailable (HTTP %d)", resp.StatusCode)
	default: // 4xx lain, termasuk 400: request kita ditolak (bug/konfigurasi), mengulang tidak berguna
		return "", sendErr(domain.OTPFailSendFailed, false, true, "gateway rejected the request (HTTP %d %s)", resp.StatusCode, code)
	}
}

// Check memeriksa sesi WhatsApp lewat GET /api/v1/session/status (publik di wa-notif, jadi tanpa token).
func (c *Client) Check(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/session/status", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("status request failed: %s", redactURLError(err))
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status endpoint returned HTTP %d", resp.StatusCode)
	}
	var out struct {
		Success bool `json:"success"`
		Data    struct {
			Status    string `json:"status"`
			Connected bool   `json:"connected"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || !out.Success {
		return errors.New("status endpoint returned an unexpected body")
	}
	if !out.Data.Connected {
		return fmt.Errorf("whatsapp session is not connected (%s)", out.Data.Status)
	}
	return nil
}

func redactURLError(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err.Error() // tanpa URL lengkap
	}
	return err.Error()
}
