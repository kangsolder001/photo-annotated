package middleware

import (
	"net"
	"net/netip"
	"strings"

	"github.com/gin-gonic/gin"

	"photo-annotated/internal/delivery/http/httpx"
)

const headerCFConnectingIP = "CF-Connecting-IP"

// ClientIP menentukan IP klien. Header CF-Connecting-IP hanya dipercaya jika koneksi
// berasal dari proxy tepercaya (cloudflared); selain itu dipakai alamat koneksi langsung,
// sehingga klien yang terhubung langsung tidak bisa memalsukan IP-nya.
func ClientIP(trusted []netip.Prefix) gin.HandlerFunc {
	return func(c *gin.Context) {
		httpx.SetClientIP(c, resolveClientIP(c.Request.RemoteAddr, c.GetHeader(headerCFConnectingIP), trusted))
		c.Next()
	}
}

func resolveClientIP(remoteAddr, cfHeader string, trusted []netip.Prefix) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	remote, err := netip.ParseAddr(strings.TrimSpace(host))
	if err != nil {
		return ""
	}
	remote = remote.Unmap()
	if !isTrusted(remote, trusted) {
		return remote.String()
	}
	if h := strings.TrimSpace(cfHeader); h != "" {
		if a, err := netip.ParseAddr(h); err == nil {
			return a.Unmap().String()
		}
	}
	return remote.String()
}

func isTrusted(a netip.Addr, trusted []netip.Prefix) bool {
	for _, p := range trusted {
		if p.Contains(a) {
			return true
		}
	}
	return false
}
