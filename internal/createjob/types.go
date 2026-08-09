package createjob

import (
	"context"
	"time"
)

const (
	ModeDuration    = "duration"
	ModeDailyWindow = "daily_window"

	StatusRunning   = "running"
	StatusPaused    = "paused"
	StatusCompleted = "completed"
	StatusError     = "error"
)

// AliasCreator creates one Hide My Email alias for an account.
type AliasCreator interface {
	CreateAlias(ctx context.Context, accountID, label string) (*CreateResult, error)
}

type CreateResult struct {
	Email     string `json:"email"`
	Label     string `json:"label"`
	CreatedAt string `json:"created_at"`
	AccountID string `json:"account_id"`
}

type BatchRequest struct {
	AccountID   string `json:"account_id"`
	Count       int    `json:"count"`
	LabelPrefix string `json:"label_prefix"`
}

type BatchResponse struct {
	AccountID         string         `json:"account_id"`
	Requested         int            `json:"requested"`
	Created           []CreateResult `json:"created"`
	CreatedCount      int            `json:"created_count"`
	SkippedCount      int            `json:"skipped_count"`
	RemainingThisHour int            `json:"remaining_this_hour"`
	Message           string         `json:"message,omitempty"`
	LastError         string         `json:"last_error,omitempty"`
}

type JobRequest struct {
	ID            string `json:"id,omitempty"`
	AccountID     string `json:"account_id"`
	LabelPrefix   string `json:"label_prefix"`
	Mode          string `json:"mode"`
	DurationHours int    `json:"duration_hours,omitempty"`
	StartTime     string `json:"start_time,omitempty"`
	EndTime       string `json:"end_time,omitempty"`
}

type Job struct {
	ID            string     `json:"id"`
	AccountID     string     `json:"account_id"`
	LabelPrefix   string     `json:"label_prefix,omitempty"`
	Mode          string     `json:"mode"`
	Status        string     `json:"status"`
	DurationHours int        `json:"duration_hours,omitempty"`
	StartTime     string     `json:"start_time,omitempty"`
	EndTime       string     `json:"end_time,omitempty"`
	CreatedCount  int        `json:"created_count"`
	LastError     string     `json:"last_error,omitempty"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
	EndedAt       *time.Time `json:"ended_at,omitempty"`
	NextRunAt     *time.Time `json:"next_run_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}
