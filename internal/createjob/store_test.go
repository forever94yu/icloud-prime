package createjob

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStoreSavesAndLoadsJobs(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "create_jobs.json"))
	started := time.Date(2026, 8, 9, 9, 0, 0, 0, time.Local)
	jobs := []*Job{
		{
			ID:            "job_1",
			AccountID:     "acc_1",
			LabelPrefix:   "自动创建",
			Mode:          ModeDuration,
			Status:        StatusRunning,
			DurationHours: 2,
			StartedAt:     &started,
			CreatedAt:     started,
			UpdatedAt:     started,
		},
	}

	if err := store.Save(jobs); err != nil {
		t.Fatalf("save failed: %v", err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected 1 job, got %d", len(loaded))
	}
	if loaded[0].ID != "job_1" || loaded[0].Mode != ModeDuration || loaded[0].DurationHours != 2 {
		t.Fatalf("loaded job mismatch: %+v", loaded[0])
	}
}

func TestStoreLoadMissingFileReturnsEmptyList(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "missing.json"))

	jobs, err := store.Load()
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("expected empty jobs, got %d", len(jobs))
	}
}

func TestJobJSONOmitsEmptyOptionalTimes(t *testing.T) {
	raw, err := json.Marshal(Job{
		ID:        "job_1",
		AccountID: "acc_1",
		Mode:      ModeDuration,
		Status:    StatusError,
		CreatedAt: time.Date(2026, 8, 9, 10, 0, 0, 0, time.Local),
		UpdatedAt: time.Date(2026, 8, 9, 10, 0, 0, 0, time.Local),
	})
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	text := string(raw)
	if strings.Contains(text, "0001-01-01") {
		t.Fatalf("expected zero times to be omitted, got %s", text)
	}
	if strings.Contains(text, "next_run_at") || strings.Contains(text, "ended_at") {
		t.Fatalf("expected empty optional time fields to be omitted, got %s", text)
	}
}
