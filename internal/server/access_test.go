package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func localRequest(method, path string, body io.Reader) *http.Request {
	req := httptest.NewRequest(method, "http://127.0.0.1"+path, body)
	req.RemoteAddr = "127.0.0.1:12345"
	return req
}

func TestAPIAccessControl(t *testing.T) {
	for _, tc := range []struct {
		name, token, authorization, peer, host, origin, forwarded string
		status                                                    int
	}{
		{name: "local", peer: "127.0.0.1:12345", host: "127.0.0.1", status: 200},
		{name: "local dev origin", peer: "127.0.0.1:12345", host: "localhost:8081", origin: "http://127.0.0.1:5173", status: 200},
		{name: "remote", peer: "192.0.2.1:12345", host: "127.0.0.1", status: 403},
		{name: "forwarded IP cannot bypass", peer: "192.0.2.1:12345", host: "localhost", forwarded: "127.0.0.1", status: 403},
		{name: "rebound host", peer: "127.0.0.1:12345", host: "untrusted.example", status: 403},
		{name: "cross site", peer: "127.0.0.1:12345", host: "localhost", origin: "https://untrusted.example", status: 403},
		{name: "missing token", token: "test-only-token", peer: "127.0.0.1:12345", host: "localhost", status: 401},
		{name: "incorrect token", token: "test-only-token", authorization: "Bearer wrong", status: 401},
		{name: "valid remote token", token: "test-only-token", authorization: "Bearer test-only-token", peer: "192.0.2.1:12345", host: "console.example", status: 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := newCreateJobTestServer(t)
			srv.SetAPIToken(tc.token)
			req := localRequest(http.MethodGet, "/api/accounts", nil)
			req.RemoteAddr, req.Host = tc.peer, tc.host
			req.Header.Set("Authorization", tc.authorization)
			req.Header.Set("Origin", tc.origin)
			req.Header.Set("X-Forwarded-For", tc.forwarded)
			w := httptest.NewRecorder()
			srv.Handler().ServeHTTP(w, req)
			if w.Code != tc.status {
				t.Fatalf("status=%d want=%d body=%s", w.Code, tc.status, w.Body.String())
			}
		})
	}
}

func TestRemoteListenerRequiresToken(t *testing.T) {
	for _, addr := range []string{":8081", "0.0.0.0:8081", "[::]:8081", "192.0.2.1:8081"} {
		if validateListenAddress(addr, "") == nil {
			t.Errorf("unprotected remote listener accepted: %s", addr)
		}
		if err := validateListenAddress(addr, "test-only-token"); err != nil {
			t.Error(err)
		}
	}
	for _, addr := range []string{"127.0.0.1:8081", "localhost:8081", "[::1]:8081"} {
		if err := validateListenAddress(addr, ""); err != nil {
			t.Error(err)
		}
	}
}
