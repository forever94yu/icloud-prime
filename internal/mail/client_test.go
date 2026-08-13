package mail

import (
	"bytes"
	"io"
	netmail "net/mail"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap"
)

func TestReadBodyDecodesMultipartQuotedPrintable(t *testing.T) {
	raw := strings.Join([]string{
		`Content-Type: multipart/alternative; boundary="part-boundary"`,
		"",
		"--part-boundary",
		`Content-Type: text/plain; charset=UTF-8`,
		`Content-Transfer-Encoding: quoted-printable`,
		"",
		"=E6=AC=A2=E8=BF=8E=E4=BD=BF=E7=94=A8 iCloud =E9=82=AE=E4=BB=B6",
		"--part-boundary",
		`Content-Type: text/html; charset=UTF-8`,
		"",
		"<p>fallback</p>",
		"--part-boundary--",
	}, "\r\n")

	msg, err := netmail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("read message: %v", err)
	}

	body, err := readBody(msg)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	if body != "欢迎使用 iCloud 邮件" {
		t.Fatalf("unexpected body %q", body)
	}
}

func TestReadBodyFallsBackToHTMLText(t *testing.T) {
	raw := strings.Join([]string{
		`Content-Type: multipart/alternative; boundary="part-boundary"`,
		"",
		"--part-boundary",
		`Content-Type: text/html; charset=UTF-8`,
		`Content-Transfer-Encoding: base64`,
		"",
		"PHA+5L2g5aW9Jm5ic3A7PC9wPg==",
		"--part-boundary--",
	}, "\r\n")

	msg, err := netmail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("read message: %v", err)
	}

	body, err := readBody(msg)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	if body != "你好" {
		t.Fatalf("unexpected body %q", body)
	}
}

func TestStripHTMLDropsStyleAndScriptContent(t *testing.T) {
	raw := strings.Join([]string{
		"<html>",
		"<head>",
		"<style>",
		"@font-face {",
		"font-family: Söhne;",
		"}",
		".ExternalClass td { line-height: 100%; }",
		"</style>",
		"<script>window.noise = true;</script>",
		"</head>",
		"<body>",
		"<h1>你的 ChatGPT 临时验证码</h1>",
		"<p>123456</p>",
		"</body>",
		"</html>",
	}, "\n")

	body := stripHTML(raw)

	if strings.Contains(body, "@font-face") || strings.Contains(body, ".ExternalClass") {
		t.Fatalf("style content leaked into body: %q", body)
	}
	if body != "你的 ChatGPT 临时验证码\n\n123456" {
		t.Fatalf("unexpected body %q", body)
	}
}

func TestFolderRoleDetectsJunkSpecialUse(t *testing.T) {
	if got := folderRole("Whatever", []string{imap.JunkAttr}); got != "junk" {
		t.Fatalf("expected junk role, got %q", got)
	}
}

func TestMessageMatchesAliasFromRawHeaders(t *testing.T) {
	message := Message{
		To:    "隐藏邮件地址",
		match: "X-Original-To: gratins.burners1e@icloud.com",
	}

	if !message.matches("gratins.burners1e@icloud.com") {
		t.Fatal("expected alias to match raw header text")
	}
}

func TestToMessageSummaryDoesNotReadFetchedBody(t *testing.T) {
	section := &imap.BodySectionName{}
	msg := &imap.Message{
		Uid: 42,
		Envelope: &imap.Envelope{
			Subject: "One time code",
			Date:    time.Date(2026, 8, 13, 9, 30, 0, 0, time.UTC),
			From:    []*imap.Address{{PersonalName: "Service", MailboxName: "noreply", HostName: "example.com"}},
			To:      []*imap.Address{{MailboxName: "alias", HostName: "icloud.com"}},
		},
		Body: map[*imap.BodySectionName]imap.Literal{
			section: bytes.NewBufferString("Content-Type: text/plain\r\n\r\nCode 123456"),
		},
	}

	summary := toMessageSummary(msg, "INBOX")

	if summary.Preview != "" {
		t.Fatalf("expected summary preview to stay empty, got %q", summary.Preview)
	}
	if summary.match != "" {
		t.Fatalf("expected summary match text to stay empty, got %q", summary.match)
	}
	body := msg.GetBody(section)
	if body == nil {
		t.Fatal("test setup should include a body literal")
	}
	raw, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read body literal: %v", err)
	}
	if !strings.Contains(string(raw), "Code 123456") {
		t.Fatalf("expected body literal to remain unread, got %q", string(raw))
	}
}
