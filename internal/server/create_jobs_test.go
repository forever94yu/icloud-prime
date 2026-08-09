package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"icloud-hme/internal/account"
	"icloud-hme/internal/createjob"
)

type fakeAliasCreator struct {
	created int
	labels  []string
}

func (f *fakeAliasCreator) CreateAlias(ctx context.Context, accountID, label string) (*createjob.CreateResult, error) {
	f.created++
	f.labels = append(f.labels, label)
	return &createjob.CreateResult{
		Email:     "alias@example.com",
		Label:     label,
		CreatedAt: time.Date(2026, 8, 9, 10, 0, 0, 0, time.Local).Format(time.RFC3339),
		AccountID: accountID,
	}, nil
}

func newCreateJobTestServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	mgr, err := account.NewManager(dir)
	if err != nil {
		t.Fatalf("new manager failed: %v", err)
	}
	scheduler, err := createjob.NewScheduler(createjob.Config{
		StorePath: filepath.Join(dir, "create_jobs.json"),
		Creator:   &fakeAliasCreator{},
		Now:       func() time.Time { return time.Date(2026, 8, 9, 10, 0, 0, 0, time.Local) },
	})
	if err != nil {
		t.Fatalf("new scheduler failed: %v", err)
	}
	return NewWithScheduler(mgr, scheduler, false)
}

func TestCreateBatchRejectsCountAboveFive(t *testing.T) {
	srv := newCreateJobTestServer(t)
	body := bytes.NewBufferString(`{"account_id":"acc_1","count":6}`)
	req := httptest.NewRequest(http.MethodPost, "/api/create/batch", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestCreateAliasKeepsExactLabel(t *testing.T) {
	creator := &fakeAliasCreator{}
	dir := t.TempDir()
	mgr, err := account.NewManager(dir)
	if err != nil {
		t.Fatalf("new manager failed: %v", err)
	}
	scheduler, err := createjob.NewScheduler(createjob.Config{
		StorePath: filepath.Join(dir, "create_jobs.json"),
		Creator:   creator,
		Now:       func() time.Time { return time.Date(2026, 8, 9, 10, 0, 0, 0, time.Local) },
	})
	if err != nil {
		t.Fatalf("new scheduler failed: %v", err)
	}
	srv := NewWithScheduler(mgr, scheduler, false)
	body := bytes.NewBufferString(`{"account_id":"acc_1","label":"GitHub 注册"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/create", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if len(creator.labels) != 1 || creator.labels[0] != "GitHub 注册" {
		t.Fatalf("expected exact label, got %+v", creator.labels)
	}
}

func TestCreateJobsCreatesDurationJob(t *testing.T) {
	srv := newCreateJobTestServer(t)
	body := bytes.NewBufferString(`{"account_id":"acc_1","mode":"duration","duration_hours":2,"label_prefix":"自动"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/create/jobs", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp apiResp
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success response, got %+v", resp)
	}
}

func TestCreateJobsListIncludesRemainingQuota(t *testing.T) {
	srv := newCreateJobTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/create/jobs?account_id=acc_1", nil)
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Success bool `json:"success"`
		Data    struct {
			RemainingThisHour int `json:"remaining_this_hour"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if body.Data.RemainingThisHour != 5 {
		t.Fatalf("expected 5 remaining, got %d", body.Data.RemainingThisHour)
	}
}
