package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"icloud-hme/internal/account"
	"icloud-hme/internal/createjob"
	"icloud-hme/internal/hme"
	"icloud-hme/internal/mail"
)

func jsonRequest(t *testing.T, srv *Server, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := localRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	return w
}

func TestAddAccountResponseDoesNotEraseCookies(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "test proxy refuses outbound traffic", http.StatusForbidden)
	}))
	defer proxy.Close()
	srv := newCreateJobTestServer(t)
	w := jsonRequest(t, srv, http.MethodPost, "/api/accounts", map[string]string{
		"name": "test", "cookies": "test_token=placeholder", "proxy": proxy.URL,
		"real_email": "test@example.com",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("add response: %d %s", w.Code, w.Body.String())
	}
	var response struct {
		Data account.Account `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Data.Cookies != nil || !response.Data.HasCookies || response.Data.Proxy != "" {
		t.Fatalf("invalid public account: %+v", response.Data)
	}
	acc, exists := srv.mgr.GetAccount(response.Data.ID)
	if !exists || acc.Cookies["test_token"] != "placeholder" {
		t.Fatal("new account lost cookies")
	}
	if _, err := srv.mgr.AddAccount("second", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := srv.mgr.Reload(); err != nil {
		t.Fatal(err)
	}
	acc, _ = srv.mgr.GetAccount(response.Data.ID)
	if acc.Cookies["test_token"] != "placeholder" {
		t.Fatal("later save erased cookies on disk")
	}
}

func TestDeletedAccountCannotReadCachedData(t *testing.T) {
	srv := newCreateJobTestServer(t)
	acc, err := srv.mgr.AddAccount("test", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	srv.setCachedMessage(acc.ID, "INBOX", 42, &mail.FullMessage{Body: "private test content"})
	srv.setCachedAliases(acc.ID, []hme.Alias{{Email: "test@example.com"}})
	srv.setCachedMailboxes(acc.ID, []mail.Folder{{Name: "INBOX"}})
	w := jsonRequest(t, srv, http.MethodDelete, "/api/accounts/"+acc.ID, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("delete: %d", w.Code)
	}
	w = jsonRequest(t, srv, http.MethodGet, "/api/messages/42?account_id="+acc.ID, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("deleted message status: %d", w.Code)
	}
	if _, cached := srv.cachedMessage(acc.ID, "INBOX", 42); cached {
		t.Fatal("message cache retained")
	}
	if _, cached := srv.cachedAliases(acc.ID); cached {
		t.Fatal("alias cache retained")
	}
	if _, cached := srv.cachedMailboxes(acc.ID); cached {
		t.Fatal("mailbox cache retained")
	}
	// A late in-flight fetch cannot make a deleted account readable again.
	srv.setCachedMessage(acc.ID, "INBOX", 42, &mail.FullMessage{Body: "late result"})
	srv.setCachedMailboxes(acc.ID, []mail.Folder{{Name: "INBOX"}})
	w = jsonRequest(t, srv, http.MethodGet, "/api/mailboxes?account_id="+acc.ID, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("deleted mailboxes status: %d", w.Code)
	}
	w = jsonRequest(t, srv, http.MethodPost, "/api/messages", map[string]any{
		"account_id": acc.ID, "messages": []map[string]string{{"uid": "42", "folder": "INBOX"}},
	})
	if w.Code != http.StatusNotFound {
		t.Fatalf("deleted batch status: %d", w.Code)
	}
}

type partialCreator struct{ calls int }

func (c *partialCreator) CreateAlias(ctx context.Context, id, label string) (*createjob.CreateResult, error) {
	c.calls++
	if c.calls > 1 {
		return nil, errors.New("upstream unavailable")
	}
	return &createjob.CreateResult{Email: "test@example.com", AccountID: id, Label: label}, nil
}

func TestBatchPartialSuccessReturnsCreatedAliases(t *testing.T) {
	dir := t.TempDir()
	mgr, err := account.NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	scheduler, err := createjob.NewScheduler(createjob.Config{
		StorePath: filepath.Join(dir, "create_jobs.json"), Creator: &partialCreator{},
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := NewWithScheduler(mgr, scheduler, false)
	srv.setCachedAliases("acc_test", []hme.Alias{{Email: "old@example.com"}})
	w := jsonRequest(t, srv, http.MethodPost, "/api/create/batch", map[string]any{"account_id": "acc_test", "count": 2})
	var response struct {
		Success bool                    `json:"success"`
		Data    createjob.BatchResponse `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || !response.Success || response.Data.CreatedCount != 1 || response.Data.LastError == "" {
		t.Fatalf("partial success lost: %d %s", w.Code, w.Body.String())
	}
	if len(response.Data.Created) != 1 || response.Data.Created[0].Email != "test@example.com" {
		t.Fatal("created email missing")
	}
	if _, cached := srv.cachedAliases("acc_test"); cached {
		t.Fatal("stale alias cache")
	}
}
