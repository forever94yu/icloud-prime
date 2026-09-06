package createjob

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"
)

type fakeCreator struct {
	created int
	fail    error
	labels  []string
}

type creatorFunc func(context.Context, string, string) (*CreateResult, error)

func (f creatorFunc) CreateAlias(ctx context.Context, accountID, label string) (*CreateResult, error) {
	return f(ctx, accountID, label)
}

func (f *fakeCreator) CreateAlias(ctx context.Context, accountID, label string) (*CreateResult, error) {
	if f.fail != nil {
		return nil, f.fail
	}
	f.created++
	f.labels = append(f.labels, label)
	return &CreateResult{
		Email:     "alias" + strconv.Itoa(f.created) + "@icloud.com",
		Label:     label,
		CreatedAt: time.Now().Format(time.RFC3339),
		AccountID: accountID,
	}, nil
}

func TestCreateOneUsesExactLabel(t *testing.T) {
	now := time.Date(2026, 8, 9, 10, 0, 0, 0, time.Local)
	creator := &fakeCreator{}
	s := newTestScheduler(t, creator, now)

	result, err := s.CreateOne(context.Background(), "acc_1", "GitHub 注册")
	if err != nil {
		t.Fatalf("create one failed: %v", err)
	}
	if result.Label != "GitHub 注册" {
		t.Fatalf("expected exact result label, got %q", result.Label)
	}
	if len(creator.labels) != 1 || creator.labels[0] != "GitHub 注册" {
		t.Fatalf("expected exact creator label, got %+v", creator.labels)
	}
}

func newTestScheduler(t *testing.T, creator AliasCreator, now time.Time) *Scheduler {
	t.Helper()
	s, err := NewScheduler(Config{
		StorePath: filepath.Join(t.TempDir(), "create_jobs.json"),
		Creator:   creator,
		Now:       func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("new scheduler failed: %v", err)
	}
	return s
}

func TestBatchCreateRejectsCountAboveFive(t *testing.T) {
	s := newTestScheduler(t, &fakeCreator{}, time.Date(2026, 8, 9, 10, 0, 0, 0, time.Local))

	_, err := s.BatchCreate(context.Background(), BatchRequest{AccountID: "acc_1", Count: 6})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBatchCreateUsesRemainingQuota(t *testing.T) {
	now := time.Date(2026, 8, 9, 10, 0, 0, 0, time.Local)
	creator := &fakeCreator{}
	s := newTestScheduler(t, creator, now)

	first, err := s.BatchCreate(context.Background(), BatchRequest{AccountID: "acc_1", Count: 3, LabelPrefix: "手动"})
	if err != nil {
		t.Fatalf("first batch failed: %v", err)
	}
	second, err := s.BatchCreate(context.Background(), BatchRequest{AccountID: "acc_1", Count: 5, LabelPrefix: "手动"})
	if err != nil {
		t.Fatalf("second batch failed: %v", err)
	}

	if first.CreatedCount != 3 || second.CreatedCount != 2 {
		t.Fatalf("expected 3 then 2 created, got %d then %d", first.CreatedCount, second.CreatedCount)
	}
	if second.SkippedCount != 3 || second.RemainingThisHour != 0 {
		t.Fatalf("expected 3 skipped and 0 remaining, got %+v", second)
	}
	if creator.created != 5 {
		t.Fatalf("expected 5 real creates, got %d", creator.created)
	}
}

func TestHourlyQuotaPersistsAcrossSchedulerRestart(t *testing.T) {
	now := time.Date(2026, 8, 9, 10, 30, 0, 0, time.Local)
	storePath := filepath.Join(t.TempDir(), "create_jobs.json")
	first, err := NewScheduler(Config{
		StorePath: storePath,
		Creator:   &fakeCreator{},
		Now:       func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("new scheduler failed: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, err := first.CreateOne(context.Background(), "acc_1", "手动"); err != nil {
			t.Fatalf("create one %d failed: %v", i+1, err)
		}
	}

	restarted, err := NewScheduler(Config{
		StorePath: storePath,
		Creator:   &fakeCreator{},
		Now:       func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("restart scheduler failed: %v", err)
	}
	if got := restarted.RemainingThisHour("acc_1"); got != 2 {
		t.Fatalf("expected persisted quota to leave 2 remaining, got %d", got)
	}
	resp, err := restarted.BatchCreate(context.Background(), BatchRequest{AccountID: "acc_1", Count: 5, LabelPrefix: "重启后"})
	if err != nil {
		t.Fatalf("batch after restart failed: %v", err)
	}
	if resp.CreatedCount != 2 || resp.SkippedCount != 3 || resp.RemainingThisHour != 0 {
		t.Fatalf("expected persisted quota to allow only 2 more creates, got %+v", resp)
	}
}

func TestBatchCreateAccountsForEachRequestInItsCurrentHour(t *testing.T) {
	now := time.Date(2026, 9, 6, 10, 59, 59, 0, time.Local)
	calls := 0
	creator := creatorFunc(func(context.Context, string, string) (*CreateResult, error) {
		calls++
		if calls == 1 {
			now = now.Add(time.Second)
		}
		return &CreateResult{}, nil
	})
	s := newTestScheduler(t, creator, now)
	s.now = func() time.Time { return now }

	resp, err := s.BatchCreate(context.Background(), BatchRequest{AccountID: "acc_1", Count: 5})
	if err != nil {
		t.Fatalf("batch failed: %v", err)
	}
	if resp.CreatedCount != 5 || resp.RemainingThisHour != 1 {
		t.Fatalf("expected four new-hour requests to leave one quota, got %+v", resp)
	}
	restarted, err := NewScheduler(Config{StorePath: s.store.path, Creator: creator, Now: s.now})
	if err != nil {
		t.Fatalf("restart failed: %v", err)
	}
	if got := restarted.RemainingThisHour("acc_1"); got != 1 {
		t.Fatalf("expected one persisted quota, got %d", got)
	}
	resp, err = restarted.BatchCreate(context.Background(), BatchRequest{AccountID: "acc_1", Count: 5})
	if err != nil {
		t.Fatalf("second batch failed: %v", err)
	}
	if resp.CreatedCount != 1 || resp.SkippedCount != 4 || calls != 6 {
		t.Fatalf("expected only one additional create, got %+v and %d calls", resp, calls)
	}
}

func TestBatchCreateReleasesFailedReservationAcrossHourBoundary(t *testing.T) {
	now := time.Date(2026, 9, 6, 10, 59, 59, 0, time.Local)
	calls := 0
	s := newTestScheduler(t, creatorFunc(func(context.Context, string, string) (*CreateResult, error) {
		calls++
		if calls == 1 {
			now = now.Add(time.Second)
			return &CreateResult{}, nil
		}
		return nil, errors.New("HTTP 503")
	}), now)
	s.now = func() time.Time { return now }

	resp, err := s.BatchCreate(context.Background(), BatchRequest{AccountID: "acc_1", Count: 5})
	if err == nil || resp.CreatedCount != 1 || resp.SkippedCount != 4 || resp.RemainingThisHour != 5 {
		t.Fatalf("expected partial result and released current-hour quota, got %+v, %v", resp, err)
	}
}

func TestDurationJobCompletesAfterEndTime(t *testing.T) {
	now := time.Date(2026, 8, 9, 10, 0, 0, 0, time.Local)
	creator := &fakeCreator{}
	s := newTestScheduler(t, creator, now)

	job, err := s.UpsertJob(JobRequest{AccountID: "acc_1", Mode: ModeDuration, DurationHours: 1})
	if err != nil {
		t.Fatalf("upsert failed: %v", err)
	}
	s.now = func() time.Time { return now.Add(2 * time.Hour) }

	if err := s.RunDue(context.Background()); err != nil {
		t.Fatalf("run due failed: %v", err)
	}
	updated, ok := s.GetJob(job.ID)
	if !ok {
		t.Fatal("job missing")
	}
	if updated.Status != StatusCompleted {
		t.Fatalf("expected completed, got %s", updated.Status)
	}
}

func TestDailyWindowAllowsCrossMidnight(t *testing.T) {
	now := time.Date(2026, 8, 9, 23, 15, 0, 0, time.Local)
	s := newTestScheduler(t, &fakeCreator{}, now)

	if !s.isInDailyWindow(now, "22:00", "02:00") {
		t.Fatal("expected 23:15 to be inside cross-midnight window")
	}
	if s.isInDailyWindow(time.Date(2026, 8, 9, 15, 0, 0, 0, time.Local), "22:00", "02:00") {
		t.Fatal("expected 15:00 outside cross-midnight window")
	}
}

func TestDailyWindowSchedulesNonHourlyStart(t *testing.T) {
	for _, tc := range []struct {
		name  string
		start string
		end   string
		at    time.Time
		next  time.Time
	}{
		{
			name:  "short window",
			start: "09:15", end: "09:45",
			at:   time.Date(2026, 9, 6, 8, 0, 0, 0, time.Local),
			next: time.Date(2026, 9, 6, 9, 15, 0, 0, time.Local),
		},
		{
			name:  "next day",
			start: "09:15", end: "09:45",
			at:   time.Date(2026, 9, 6, 9, 45, 0, 0, time.Local),
			next: time.Date(2026, 9, 7, 9, 15, 0, 0, time.Local),
		},
		{
			name:  "cross midnight",
			start: "22:15", end: "02:45",
			at:   time.Date(2026, 9, 6, 12, 0, 0, 0, time.Local),
			next: time.Date(2026, 9, 6, 22, 15, 0, 0, time.Local),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := tc.at
			creator := &fakeCreator{}
			s := newTestScheduler(t, creator, now)
			s.now = func() time.Time { return now }
			job, err := s.UpsertJob(JobRequest{AccountID: "acc_1", Mode: ModeDailyWindow, StartTime: tc.start, EndTime: tc.end})
			if err != nil {
				t.Fatalf("upsert failed: %v", err)
			}
			if err := s.RunDue(context.Background()); err != nil {
				t.Fatalf("initial run failed: %v", err)
			}
			updated, _ := s.GetJob(job.ID)
			if updated.NextRunAt == nil || !updated.NextRunAt.Equal(tc.next) || creator.created != 0 {
				t.Fatalf("expected next run at %v, got %+v", tc.next, updated)
			}
			now = tc.next.Add(-time.Minute)
			if err := s.RunDue(context.Background()); err != nil {
				t.Fatal(err)
			}
			if creator.created != 0 {
				t.Fatal("created an alias before the window opened")
			}
			now = tc.next
			if err := s.RunDue(context.Background()); err != nil {
				t.Fatal(err)
			}
			if creator.created != 1 {
				t.Fatalf("expected one create at window start, got %d", creator.created)
			}
		})
	}
}

func TestRunDueRechecksWindowAfterPreviousRequestCompletes(t *testing.T) {
	now := time.Date(2026, 9, 6, 10, 59, 59, 0, time.Local)
	calls := 0
	s := newTestScheduler(t, creatorFunc(func(context.Context, string, string) (*CreateResult, error) {
		calls++
		now = now.Add(time.Second)
		return &CreateResult{}, nil
	}), now)
	s.now = func() time.Time { return now }
	for i := 0; i < 2; i++ {
		if _, err := s.UpsertJob(JobRequest{AccountID: "acc_1", Mode: ModeDailyWindow, StartTime: "10:00", EndTime: "11:00"}); err != nil {
			t.Fatalf("upsert failed: %v", err)
		}
	}
	if err := s.RunDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("expected the second job to wait until the next window, got %d creates", calls)
	}
}

func TestPauseSurvivesInFlightCreationResult(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{name: "transient error", err: errors.New("HTTP 429")},
		{name: "permanent error", err: errors.New("HTTP 401")},
		{name: "success"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Date(2026, 9, 6, 10, 0, 0, 0, time.Local)
			started := make(chan struct{})
			release := make(chan struct{})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			creator := creatorFunc(func(ctx context.Context, _, _ string) (*CreateResult, error) {
				close(started)
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-release:
					return &CreateResult{}, tc.err
				}
			})
			s := newTestScheduler(t, creator, now)
			job, err := s.UpsertJob(JobRequest{AccountID: "acc_1", Mode: ModeDuration, DurationHours: 12})
			if err != nil {
				t.Fatalf("upsert failed: %v", err)
			}
			done := make(chan struct{})
			go func() {
				_ = s.RunDue(ctx)
				close(done)
			}()
			t.Cleanup(func() {
				cancel()
				<-done
			})
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("scheduled create did not start")
			}
			paused, err := s.PauseJob(job.ID)
			if err != nil {
				t.Fatalf("pause failed: %v", err)
			}
			close(release)
			<-done
			updated, _ := s.GetJob(job.ID)
			if updated.Status != StatusPaused || !reflect.DeepEqual(updated.NextRunAt, paused.NextRunAt) {
				t.Fatalf("creation result changed the paused schedule: %+v", updated)
			}
			wantCount, wantQuota := 0, 5
			if tc.err == nil {
				wantCount, wantQuota = 1, 4
			}
			if updated.CreatedCount != wantCount || s.RemainingThisHour("acc_1") != wantQuota {
				t.Fatalf("unexpected count or quota after paused creation: %+v", updated)
			}
			state, err := s.store.LoadState()
			if err != nil || len(state.Jobs) != 1 || state.Jobs[0].Status != StatusPaused {
				t.Fatalf("pause was not persisted: %+v, %v", state, err)
			}
		})
	}
}

func TestJobMutationsRollBackAfterStoreFailure(t *testing.T) {
	for _, operation := range []string{"create", "update", "pause", "resume", "delete"} {
		t.Run(operation, func(t *testing.T) {
			now := time.Date(2026, 9, 6, 10, 0, 0, 0, time.Local)
			creator := &fakeCreator{}
			s := newTestScheduler(t, creator, now)
			s.now = func() time.Time { return now }
			req := JobRequest{AccountID: "acc_1", LabelPrefix: "original", Mode: ModeDuration, DurationHours: 3}
			var before *Job
			if operation != "create" {
				var err error
				before, err = s.UpsertJob(req)
				if err != nil {
					t.Fatalf("setup failed: %v", err)
				}
				req.ID = before.ID
				if operation == "resume" {
					before, err = s.PauseJob(before.ID)
					if err != nil {
						t.Fatalf("setup pause failed: %v", err)
					}
				}
			}
			now = now.Add(10 * time.Minute)
			if err := os.Mkdir(s.store.path+".tmp", 0700); err != nil {
				t.Fatalf("block store: %v", err)
			}
			var err error
			switch operation {
			case "create", "update":
				req.LabelPrefix = "changed"
				req.DurationHours = 8
				_, err = s.UpsertJob(req)
			case "pause":
				_, err = s.PauseJob(req.ID)
			case "resume":
				_, err = s.ResumeJob(req.ID)
			case "delete":
				err = s.DeleteJob(req.ID)
			}
			if err == nil {
				t.Fatal("expected persistence failure")
			}
			jobs := s.ListJobs("")
			if before == nil {
				if len(jobs) != 0 {
					t.Fatalf("failed create left live jobs: %+v", jobs)
				}
			} else if len(jobs) != 1 || !reflect.DeepEqual(before, jobs[0]) {
				t.Fatalf("failed %s changed live state: before=%+v, after=%+v", operation, before, jobs)
			}
			state, err := s.store.LoadState()
			if err != nil || !reflect.DeepEqual(state.Jobs, jobs) {
				t.Fatalf("memory and disk differ after %s failure: %+v, %v", operation, state, err)
			}
			if err := os.Remove(s.store.path + ".tmp"); err != nil {
				t.Fatalf("unblock store: %v", err)
			}
			if err := s.RunDue(context.Background()); err != nil {
				t.Fatalf("run due failed: %v", err)
			}
			wantCreates := 1
			if operation == "create" || operation == "resume" {
				wantCreates = 0
			}
			if creator.created != wantCreates {
				t.Fatalf("failed %s changed scheduling: created %d, want %d", operation, creator.created, wantCreates)
			}
		})
	}
}

func TestJobRecordsErrorWithoutConsumingQuota(t *testing.T) {
	now := time.Date(2026, 8, 9, 10, 0, 0, 0, time.Local)
	s := newTestScheduler(t, &fakeCreator{fail: errors.New("HTTP 401")}, now)

	_, err := s.UpsertJob(JobRequest{AccountID: "acc_1", Mode: ModeDuration, DurationHours: 1})
	if err != nil {
		t.Fatalf("upsert failed: %v", err)
	}
	if err := s.RunDue(context.Background()); err != nil {
		t.Fatalf("run due failed: %v", err)
	}
	jobs := s.ListJobs("")
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	if jobs[0].Status != StatusError || jobs[0].LastError == "" {
		t.Fatalf("expected error status with last error, got %+v", jobs[0])
	}
	if got := s.RemainingThisHour("acc_1"); got != 5 {
		t.Fatalf("failed create should not consume quota, got %d", got)
	}
}

func TestJobBacksOffAfterHTTP421WithoutStopping(t *testing.T) {
	now := time.Date(2026, 8, 9, 10, 0, 0, 0, time.Local)
	s := newTestScheduler(t, &fakeCreator{fail: errors.New("HTTP 421: request throttled")}, now)

	job, err := s.UpsertJob(JobRequest{AccountID: "acc_1", Mode: ModeDuration, DurationHours: 1})
	if err != nil {
		t.Fatalf("upsert failed: %v", err)
	}
	if err := s.RunDue(context.Background()); err != nil {
		t.Fatalf("run due failed: %v", err)
	}
	updated, ok := s.GetJob(job.ID)
	if !ok {
		t.Fatal("job missing")
	}
	if updated.Status != StatusRunning {
		t.Fatalf("expected transient 421 to keep job running, got %s", updated.Status)
	}
	if updated.LastError == "" {
		t.Fatal("expected last error to be recorded")
	}
	if updated.NextRunAt == nil || !updated.NextRunAt.After(now) {
		t.Fatalf("expected retry to be scheduled after now, got %v", updated.NextRunAt)
	}
	if got := s.RemainingThisHour("acc_1"); got != 5 {
		t.Fatalf("failed create should not consume quota, got %d", got)
	}
}

func TestAutomaticJobPacesNextRunAcrossRemainingHourlyQuota(t *testing.T) {
	now := time.Date(2026, 8, 9, 10, 0, 0, 0, time.Local)
	s := newTestScheduler(t, &fakeCreator{}, now)

	job, err := s.UpsertJob(JobRequest{AccountID: "acc_1", Mode: ModeDuration, DurationHours: 1})
	if err != nil {
		t.Fatalf("upsert failed: %v", err)
	}
	if err := s.RunDue(context.Background()); err != nil {
		t.Fatalf("run due failed: %v", err)
	}
	updated, ok := s.GetJob(job.ID)
	if !ok {
		t.Fatal("job missing")
	}
	expected := now.Add(12 * time.Minute)
	if updated.NextRunAt == nil || !updated.NextRunAt.Equal(expected) {
		t.Fatalf("expected next run at %v, got %v", expected, updated.NextRunAt)
	}
}

func TestAutomaticJobsUseSeparateHourlyQuotaPerAccount(t *testing.T) {
	now := time.Date(2026, 8, 9, 10, 0, 0, 0, time.Local)
	s := newTestScheduler(t, &fakeCreator{}, now)

	if _, err := s.BatchCreate(context.Background(), BatchRequest{AccountID: "acc_1", Count: 5, LabelPrefix: "满额"}); err != nil {
		t.Fatalf("exhaust first account quota failed: %v", err)
	}
	job, err := s.UpsertJob(JobRequest{AccountID: "acc_2", Mode: ModeDuration, DurationHours: 1})
	if err != nil {
		t.Fatalf("upsert second account job failed: %v", err)
	}
	if err := s.RunDue(context.Background()); err != nil {
		t.Fatalf("run due failed: %v", err)
	}
	updated, ok := s.GetJob(job.ID)
	if !ok {
		t.Fatal("job missing")
	}
	if updated.CreatedCount != 1 || updated.Status != StatusRunning {
		t.Fatalf("expected second account job to keep running after creating one alias, got %+v", updated)
	}
	if got := s.RemainingThisHour("acc_1"); got != 0 {
		t.Fatalf("expected first account quota exhausted, got %d remaining", got)
	}
	if got := s.RemainingThisHour("acc_2"); got != 4 {
		t.Fatalf("expected second account to keep independent quota, got %d remaining", got)
	}
}

func TestSchedulerStartRunsDueJobs(t *testing.T) {
	now := time.Date(2026, 8, 9, 10, 0, 0, 0, time.Local)
	creator := &fakeCreator{}
	s := newTestScheduler(t, creator, now)

	if _, err := s.UpsertJob(JobRequest{AccountID: "acc_1", Mode: ModeDuration, DurationHours: 1}); err != nil {
		t.Fatalf("upsert failed: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s.Start(ctx, 10*time.Millisecond)
	deadline := time.After(500 * time.Millisecond)
	for s.ListJobs("")[0].CreatedCount == 0 {
		select {
		case <-deadline:
			t.Fatal("expected background scheduler to create an alias")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}
