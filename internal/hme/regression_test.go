package hme

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	http "github.com/bogdanfinn/fhttp"
	tls_client "github.com/bogdanfinn/tls-client"
)

type mockHTTP struct {
	tls_client.HttpClient
	do func(*http.Request) (*http.Response, error)
}

func (m mockHTTP) Do(req *http.Request) (*http.Response, error) { return m.do(req) }

func mockResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestClientsOwnCookieMaps(t *testing.T) {
	shared := map[string]string{"session": "initial"}
	var clients []*Client
	for i := 0; i < 2; i++ {
		c, err := NewClient(shared, "icloud.com", "", false)
		if err != nil {
			t.Fatal(err)
		}
		c.httpc = mockHTTP{do: func(req *http.Request) (*http.Response, error) {
			resp := mockResponse(200, `{}`)
			resp.Header.Set("Set-Cookie", "session=refreshed; Path=/")
			return resp, nil
		}}
		clients = append(clients, c)
	}
	var wg sync.WaitGroup
	for _, c := range clients {
		wg.Add(1)
		go func(c *Client) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				if _, err := c.request("GET", "https://setup.icloud.com/test", nil, 0, 1); err != nil {
					t.Error(err)
				}
			}
		}(c)
	}
	wg.Wait()
	if shared["session"] != "initial" {
		t.Fatal("client mutated caller's cookies")
	}
	clients[0].Cookies["session"] = "first"
	if clients[1].Cookies["session"] != "refreshed" {
		t.Fatal("clients share cookie storage")
	}
	blank, err := NewClient(nil, "icloud.com", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if blank.Cookies == nil {
		t.Fatal("nil input must produce writable cookie storage")
	}
}

func TestLoginReplaysServerChallenge(t *testing.T) {
	const challenge = "opaque-server-challenge"
	var submitted string
	c := &Client{httpc: mockHTTP{do: func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(req.URL.Path, "authorize/signin"), strings.HasSuffix(req.URL.Path, "/federate"):
			return mockResponse(200, `{}`), nil
		case strings.HasSuffix(req.URL.Path, "/signin/init"):
			return mockResponse(200, `{"iteration":1,"salt":"AQ==","b":"Ag==","c":"`+challenge+`","protocol":"s2k"}`), nil
		case strings.HasSuffix(req.URL.Path, "/signin/complete"):
			var body map[string]any
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				return nil, err
			}
			submitted, _ = body["c"].(string)
			return mockResponse(403, `{}`), nil
		default:
			return nil, fmt.Errorf("unexpected request: %s", req.URL)
		}
	}}}
	_ = c.Login("test@example.com", "password", nil)
	if submitted != challenge {
		t.Fatalf("expected server challenge %q, got %q", challenge, submitted)
	}
}

func TestLoginRejectsInvalidInitResponses(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"http error", 401, `{"serviceErrors":[{"code":"-20101"}]}`},
		{"missing fields", 200, `{}`},
		{"zero B", 200, `{"iteration":1,"salt":"AQ==","b":"AA==","c":"challenge","protocol":"s2k"}`},
		{"empty salt", 200, `{"iteration":1,"salt":"","b":"Ag==","c":"challenge","protocol":"s2k"}`},
		{"zero iterations", 200, `{"iteration":0,"salt":"AQ==","b":"Ag==","c":"challenge","protocol":"s2k"}`},
		{"unknown protocol", 200, `{"iteration":1,"salt":"AQ==","b":"Ag==","c":"challenge","protocol":"unknown"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &Client{httpc: mockHTTP{do: func(req *http.Request) (*http.Response, error) {
				if strings.HasSuffix(req.URL.Path, "/signin/init") {
					return mockResponse(tc.status, tc.body), nil
				}
				if strings.HasSuffix(req.URL.Path, "/signin/complete") {
					t.Fatal("invalid init must not reach complete")
				}
				return mockResponse(200, `{}`), nil
			}}}
			if err := c.Login("test@example.com", "password", nil); err == nil {
				t.Fatal("expected login error")
			}
		})
	}
}

func TestListAliasesRejectsFailedOrMalformedResponses(t *testing.T) {
	for _, tc := range []struct {
		body      string
		wantError bool
	}{
		{`{"success":false,"error":{"errorMessage":"temporary failure"}}`, true},
		{`not JSON`, true},
		{`{"success":true,"result":{}}`, true},
		{`{"success":true,"result":{"hmeEmails":[]}}`, false},
	} {
		c := &Client{serviceURL: "https://p68-maildomainws.icloud.com", httpc: mockHTTP{do: func(req *http.Request) (*http.Response, error) {
			return mockResponse(200, tc.body), nil
		}}}
		_, err := c.ListAliases()
		if (err != nil) != tc.wantError {
			t.Errorf("body %s: err=%v", tc.body, err)
		}
	}
}
