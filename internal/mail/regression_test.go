package mail

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	netmail "net/mail"
	"net/url"
	"strings"
	"testing"

	http "github.com/bogdanfinn/fhttp"
	tls_client "github.com/bogdanfinn/tls-client"
	"github.com/emersion/go-imap"
	imapclient "github.com/emersion/go-imap/client"
)

type mockHTTP struct {
	tls_client.HttpClient
	do func(*http.Request) (*http.Response, error)
}

func (m mockHTTP) Do(req *http.Request) (*http.Response, error) { return m.do(req) }

func TestIMAPFolderErrorsAreReturned(t *testing.T) {
	local, remote := net.Pipe()
	defer local.Close()
	go func() {
		defer remote.Close()
		fmt.Fprint(remote, "* PREAUTH [CAPABILITY IMAP4rev1] ready\r\n")
		scanner := bufio.NewScanner(remote)
		for scanner.Scan() {
			command := strings.Fields(scanner.Text())
			fmt.Fprintf(remote, "%s NO temporarily unavailable\r\n", command[0])
		}
	}()
	cli, err := imapclient.New(local)
	if err != nil {
		t.Fatal(err)
	}
	c := &Client{cli: cli}
	if _, err := c.ListFolder("inbox", 20, 7); err == nil {
		t.Fatal("SELECT failure must return an error")
	}
	if _, err := c.FindByRecipientInFolder("alias@icloud.com", "inbox", 20, 7); err == nil {
		t.Fatal("recipient SELECT failure must return an error")
	}
}

func TestIMAPPartialFoldersPreserveMessagesAndFlags(t *testing.T) {
	local, remote := net.Pipe()
	defer local.Close()
	fetches := make(chan string, 1)
	go func() {
		defer remote.Close()
		fmt.Fprint(remote, "* PREAUTH [CAPABILITY IMAP4rev1] ready\r\n")
		scanner := bufio.NewScanner(remote)
		for scanner.Scan() {
			line := scanner.Text()
			command := strings.Fields(line)
			switch {
			case command[1] == "EXAMINE" && strings.Contains(line, "INBOX"):
				fmt.Fprintf(remote, "* FLAGS (\\Seen)\r\n* 1 EXISTS\r\n%s OK [READ-ONLY] selected\r\n", command[0])
			case command[1] == "FETCH":
				fetches <- line
				fmt.Fprintf(remote, "* 1 FETCH (UID 42 FLAGS ())\r\n%s OK fetched\r\n", command[0])
			default:
				fmt.Fprintf(remote, "%s NO folder unavailable\r\n", command[0])
			}
		}
	}()
	cli, err := imapclient.New(local)
	if err != nil {
		t.Fatal(err)
	}
	c := &Client{cli: cli}
	messages, err := c.ListFolder("all", 20, 0)
	if err == nil || !strings.Contains(err.Error(), "Junk") {
		t.Fatalf("expected Junk error, got %v", err)
	}
	if len(messages) != 1 || messages[0].UID != "42" {
		t.Fatalf("successful folder lost: %+v", messages)
	}
	if messages[0].Unread == nil || !*messages[0].Unread {
		t.Fatal("missing IMAP unread state")
	}
	if line := <-fetches; !strings.Contains(line, "FLAGS") {
		t.Fatalf("FLAGS not fetched: %s", line)
	}
}

func TestReadBodyIgnoresTextAttachments(t *testing.T) {
	raw := "Content-Type: multipart/mixed; boundary=x\r\n\r\n" +
		"--x\r\nContent-Type: text/html\r\n\r\n<p>Your login code is 123456</p>\r\n" +
		"--x\r\nContent-Type: text/plain\r\nContent-Disposition: attachment; filename=receipt.txt\r\n\r\nOrder number 654321\r\n" +
		"--x--\r\n"
	message, err := netmail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	body, err := readBody(message)
	if err != nil {
		t.Fatal(err)
	}
	if body != "Your login code is 123456" {
		t.Fatalf("wrong body: %q", body)
	}
}

func TestIMAPUnreadDistinguishesUnknownAndSeen(t *testing.T) {
	unknown := toMessage(&imap.Message{}, "INBOX")
	if unknown.Unread != nil {
		t.Fatal("missing FLAGS should remain unknown")
	}
	read := toMessage(&imap.Message{Flags: []string{imap.SeenFlag}}, "INBOX")
	if read.Unread == nil || *read.Unread {
		t.Fatal("seen mail should be read")
	}
}

func TestWebAliasUsesRecipientSearchAndFlags(t *testing.T) {
	c := &WebClient{mccGatewayURL: "https://p68-mccgateway.icloud.com", httpc: mockHTTP{do: func(req *http.Request) (*http.Response, error) {
		var payload map[string]any
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			return nil, err
		}
		if payload["searchText"] != "alias@icloud.com" || payload["searchType"] != "recipient" {
			t.Fatalf("wrong recipient search: %v", payload)
		}
		body := `{"threadList":[{"threadId":"1","subject":"Your login code","senders":["service@example.com"],"preview":"123456","flags":[]},{"threadId":"2","flags":["\\Seen"]},{"threadId":"3"}]}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	}}}
	messages, err := c.FindByAlias("alias@icloud.com", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 3 {
		t.Fatalf("recipient search results discarded: %v", messages)
	}
	if messages[0].Unread == nil || !*messages[0].Unread || messages[1].Unread == nil || *messages[1].Unread || messages[2].Unread != nil {
		t.Fatalf("incorrect unread states: %+v", messages)
	}
}

func TestWebCookiesReachResolvedChinaGateway(t *testing.T) {
	c := NewWebClient(map[string]string{"session": "initial"}, "123", "icloud.com.cn")
	c.httpc = mockHTTP{HttpClient: c.httpc, do: func(req *http.Request) (*http.Response, error) {
		resp := &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"webservices":{"mccgateway":{"url":"https://p68-mccgateway.icloud.com.cn:443"}}}`))}
		resp.Header.Set("Set-Cookie", "session=refreshed; Path=/")
		return resp, nil
	}}
	if err := c.resolveMccGateway(); err != nil {
		t.Fatal(err)
	}
	gateway, _ := url.Parse(c.mccGatewayURL)
	cookies := c.httpc.GetCookies(gateway)
	if len(cookies) != 1 || cookies[0].Value != "refreshed" {
		t.Fatalf("resolved gateway missing refreshed cookie: %v", cookies)
	}
	snapshot := c.CookiesSnapshot()
	if snapshot["session"] != "refreshed" {
		t.Fatal("snapshot omitted refreshed session")
	}
	snapshot["session"] = "modified"
	if c.CookiesSnapshot()["session"] != "refreshed" {
		t.Fatal("snapshot mutation changed client cookies")
	}
}

func TestWebBusinessErrorsAreNotEmptySuccess(t *testing.T) {
	for _, body := range []string{`{"success": false}`, `{"errorCode":"AUTHENTICATION_FAILED","errorDescription":"expired"}`, `{}`} {
		c := &WebClient{mccGatewayURL: "https://p68-mccgateway.icloud.com", httpc: mockHTTP{do: func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
		}}}
		if _, err := c.ListInbox(20); err == nil {
			t.Errorf("error response was accepted: %s", body)
		}
	}
}
