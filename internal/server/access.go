package server

import (
	"crypto/subtle"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

func isLoopbackHost(host string) bool {
	if name, _, err := net.SplitHostPort(host); err == nil {
		host = name
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func validateListenAddress(addr, token string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid listen address: %w", err)
	}
	if !isLoopbackHost(host) && strings.TrimSpace(token) == "" {
		return fmt.Errorf("non-loopback listening requires ICLOUD_PRIME_API_TOKEN or -api-token")
	}
	return nil
}

func (s *Server) authorizeAPI(c *gin.Context) {
	if s.apiToken != "" {
		header := c.GetHeader("Authorization")
		token, bearer := strings.CutPrefix(header, "Bearer ")
		if !bearer || subtle.ConstantTimeCompare([]byte(token), []byte(s.apiToken)) != 1 {
			c.Header("WWW-Authenticate", "Bearer")
			fail(c, http.StatusUnauthorized, "需要有效的 API 访问令牌")
			c.Abort()
			return
		}
	} else {
		peer, _, err := net.SplitHostPort(c.Request.RemoteAddr)
		if err != nil || !isLoopbackHost(peer) || !isLoopbackHost(c.Request.Host) {
			fail(c, http.StatusForbidden, "未配置访问令牌时只允许本机访问")
			c.Abort()
			return
		}
		// Validate browser origins as well as Host to reject cross-site local API calls.
		if origin := c.GetHeader("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || !isLoopbackHost(u.Host) {
				fail(c, http.StatusForbidden, "请求来源不被允许")
				c.Abort()
				return
			}
		}
	}
	c.Next()
}
