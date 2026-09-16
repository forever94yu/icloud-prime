package hme

import (
	"fmt"
	stdhttp "net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	http "github.com/bogdanfinn/fhttp"
)

func TestSessionRequestsSendOnlyCurrentCookies(t *testing.T) {
	received := make(chan string, 2)
	upstream := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		received <- r.Header.Get("Cookie")
		if r.URL.Path == "/rotate" {
			w.Header().Set("Set-Cookie", "session=rotated; Path=/")
		}
		fmt.Fprint(w, `{}`)
	}))
	defer upstream.Close()
	c, err := NewClient(map[string]string{"session": "initial"}, "icloud.com", "", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.httpc.CloseIdleConnections)
	u, _ := url.Parse(upstream.URL)
	c.httpc.SetCookies(u, []*http.Cookie{{Name: "session", Value: "stale", Path: "/"}})
	loginJar := c.httpc.GetCookieJar()
	for _, path := range []string{"/rotate", "/next"} {
		if _, err := c.request("GET", upstream.URL+path, nil, 0, 1); err != nil {
			t.Fatal(err)
		}
	}
	for i, want := range []string{`session="initial"`, `session="rotated"`} {
		if got := <-received; got != want {
			t.Errorf("request %d sent %q, want %q", i+1, got, want)
		}
	}
	if c.httpc.GetCookieJar() != loginJar {
		t.Fatal("session requests must preserve the login cookie jar")
	}
}

func TestSessionCookiesHonorDeletion(t *testing.T) {
	for _, deletion := range []string{
		"session=; Max-Age=0; Path=/",
		"session=obsolete; Expires=Thu, 01 Jan 1970 00:00:00 GMT; Path=/",
	} {
		t.Run(deletion, func(t *testing.T) {
			nextCookie := make(chan string, 1)
			upstream := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
				if r.URL.Path == "/expire" {
					w.Header().Set("Set-Cookie", deletion)
					w.WriteHeader(stdhttp.StatusUnauthorized)
				} else {
					nextCookie <- r.Header.Get("Cookie")
				}
				fmt.Fprint(w, `{}`)
			}))
			defer upstream.Close()
			c, err := NewClient(map[string]string{"session": "initial"}, "icloud.com", "", false)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(c.httpc.CloseIdleConnections)
			if _, err := c.request("GET", upstream.URL+"/expire", nil, 0, 1); err == nil {
				t.Fatal("expected authentication error")
			}
			if _, exists := c.Cookies["session"]; exists {
				t.Error("deleted session cookie retained for persistence")
			}
			if _, err := c.request("GET", upstream.URL+"/next", nil, 0, 1); err != nil {
				t.Fatal(err)
			}
			if got := <-nextCookie; got != "" {
				t.Errorf("deleted cookie sent again: %s", got)
			}
		})
	}
}

func TestCreateAliasDoesNotRetryExpiredSession(t *testing.T) {
	for _, status := range []int{401, 403} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			c := &Client{Cookies: map[string]string{}, httpc: mockHTTP{do: func(req *http.Request) (*http.Response, error) {
				calls++
				return mockResponse(status, `{}`), nil
			}}}
			if _, err := c.CreateAlias("test", 3); err == nil {
				t.Fatal("expected authentication error")
			}
			if calls != 1 {
				t.Errorf("expired session caused %d requests, want 1", calls)
			}
		})
	}
}
