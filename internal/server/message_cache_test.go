package server

import (
	"testing"
	"time"

	"icloud-hme/internal/mail"
)

func TestMessageCacheReturnsCopy(t *testing.T) {
	srv := newCreateJobTestServer(t)
	srv.messageTTL = time.Minute
	original := &mail.FullMessage{
		Message: mail.Message{
			ID:      "INBOX:42",
			UID:     "42",
			Folder:  "INBOX",
			Subject: "Code",
			Preview: "123456",
		},
		Body:        "123456",
		ContentType: "text/plain",
	}

	srv.setCachedMessage("acc_1", "INBOX", 42, original)

	cached, ok := srv.cachedMessage("acc_1", "INBOX", 42)
	if !ok {
		t.Fatal("expected cached message")
	}
	cached.Body = "mutated"

	cachedAgain, ok := srv.cachedMessage("acc_1", "INBOX", 42)
	if !ok {
		t.Fatal("expected cached message on second read")
	}
	if cachedAgain.Body != "123456" {
		t.Fatalf("expected cached body copy to stay unchanged, got %q", cachedAgain.Body)
	}
}
