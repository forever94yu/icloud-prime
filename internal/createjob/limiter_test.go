package createjob

import (
	"testing"
	"time"
)

func TestLimiterSharesHourlyQuotaPerAccount(t *testing.T) {
	limiter := NewLimiter(5)
	now := time.Date(2026, 8, 9, 10, 30, 0, 0, time.Local)

	if got := limiter.Remaining("acc_1", now); got != 5 {
		t.Fatalf("expected 5 remaining, got %d", got)
	}
	if ok := limiter.TryReserve("acc_1", now, 3); !ok {
		t.Fatal("expected first reservation to succeed")
	}
	if got := limiter.Remaining("acc_1", now); got != 2 {
		t.Fatalf("expected 2 remaining, got %d", got)
	}
	if ok := limiter.TryReserve("acc_1", now, 3); ok {
		t.Fatal("expected reservation beyond quota to fail")
	}
	if got := limiter.Remaining("acc_1", now); got != 2 {
		t.Fatalf("failed reservation should not consume quota, got %d", got)
	}
}

func TestLimiterResetsOnNextHour(t *testing.T) {
	limiter := NewLimiter(5)
	firstHour := time.Date(2026, 8, 9, 10, 59, 0, 0, time.Local)
	nextHour := time.Date(2026, 8, 9, 11, 0, 0, 0, time.Local)

	if ok := limiter.TryReserve("acc_1", firstHour, 5); !ok {
		t.Fatal("expected full quota reservation to succeed")
	}
	if got := limiter.Remaining("acc_1", firstHour); got != 0 {
		t.Fatalf("expected exhausted quota, got %d", got)
	}
	if got := limiter.Remaining("acc_1", nextHour); got != 5 {
		t.Fatalf("expected quota reset next hour, got %d", got)
	}
}
