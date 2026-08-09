package createjob

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const hourlyLimit = 5

var ErrHourlyQuotaExceeded = errors.New("当前小时创建额度已用完")

type Config struct {
	StorePath string
	Creator   AliasCreator
	Now       func() time.Time
}

type Scheduler struct {
	mu      sync.Mutex
	store   *Store
	limiter *Limiter
	creator AliasCreator
	jobs    map[string]*Job
	now     func() time.Time
	rand    *rand.Rand
}

func NewScheduler(cfg Config) (*Scheduler, error) {
	if cfg.Creator == nil {
		return nil, errors.New("creator is required")
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	store := NewStore(cfg.StorePath)
	loaded, err := store.Load()
	if err != nil {
		return nil, err
	}
	s := &Scheduler{
		store:   store,
		limiter: NewLimiter(hourlyLimit),
		creator: cfg.Creator,
		jobs:    make(map[string]*Job, len(loaded)),
		now:     now,
		rand:    rand.New(rand.NewSource(time.Now().UnixNano())),
	}
	for _, job := range loaded {
		cp := cloneJob(job)
		s.normalizeLoadedJob(cp)
		s.jobs[cp.ID] = cp
	}
	return s, nil
}

func (s *Scheduler) CreateOne(ctx context.Context, accountID, label string) (*CreateResult, error) {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return nil, errors.New("account_id 必填")
	}
	at := s.now()
	if !s.limiter.TryReserve(accountID, at, 1) {
		return nil, ErrHourlyQuotaExceeded
	}
	result, err := s.creator.CreateAlias(ctx, accountID, strings.TrimSpace(label))
	if err != nil {
		s.limiter.Release(accountID, at, 1)
		return nil, err
	}
	return result, nil
}

func (s *Scheduler) BatchCreate(ctx context.Context, req BatchRequest) (*BatchResponse, error) {
	req.AccountID = strings.TrimSpace(req.AccountID)
	req.LabelPrefix = strings.TrimSpace(req.LabelPrefix)
	if req.AccountID == "" {
		return nil, errors.New("account_id 必填")
	}
	if req.Count < 1 || req.Count > hourlyLimit {
		return nil, fmt.Errorf("count 必须在 1-%d 之间", hourlyLimit)
	}

	at := s.now()
	remaining := s.limiter.Remaining(req.AccountID, at)
	toCreate := req.Count
	if toCreate > remaining {
		toCreate = remaining
	}
	resp := &BatchResponse{
		AccountID:         req.AccountID,
		Requested:         req.Count,
		Created:           make([]CreateResult, 0, toCreate),
		SkippedCount:      req.Count - toCreate,
		RemainingThisHour: remaining,
	}
	if toCreate == 0 {
		resp.Message = "当前小时创建额度已用完"
		return resp, nil
	}

	for i := 0; i < toCreate; i++ {
		if !s.limiter.TryReserve(req.AccountID, at, 1) {
			resp.SkippedCount = req.Count - resp.CreatedCount
			resp.RemainingThisHour = s.limiter.Remaining(req.AccountID, at)
			resp.Message = "当前小时创建额度不足"
			return resp, nil
		}
		result, err := s.creator.CreateAlias(ctx, req.AccountID, labelFor(req.LabelPrefix, resp.CreatedCount+1))
		if err != nil {
			s.limiter.Release(req.AccountID, at, 1)
			resp.LastError = err.Error()
			resp.SkippedCount = req.Count - resp.CreatedCount
			resp.RemainingThisHour = s.limiter.Remaining(req.AccountID, at)
			return resp, err
		}
		resp.Created = append(resp.Created, *result)
		resp.CreatedCount++
	}
	resp.SkippedCount = req.Count - resp.CreatedCount
	resp.RemainingThisHour = s.limiter.Remaining(req.AccountID, at)
	if resp.SkippedCount > 0 {
		resp.Message = fmt.Sprintf("当前小时额度不足，已创建 %d 个", resp.CreatedCount)
	}
	return resp, nil
}

func (s *Scheduler) UpsertJob(req JobRequest) (*Job, error) {
	req.AccountID = strings.TrimSpace(req.AccountID)
	req.LabelPrefix = strings.TrimSpace(req.LabelPrefix)
	req.Mode = strings.TrimSpace(req.Mode)
	if req.AccountID == "" {
		return nil, errors.New("account_id 必填")
	}
	if req.Mode != ModeDuration && req.Mode != ModeDailyWindow {
		return nil, errors.New("mode 必须是 duration 或 daily_window")
	}
	if req.Mode == ModeDuration && req.DurationHours <= 0 {
		return nil, errors.New("duration_hours 必须大于 0")
	}
	if req.Mode == ModeDailyWindow {
		if !validClock(req.StartTime) || !validClock(req.EndTime) {
			return nil, errors.New("start_time 和 end_time 必须是 HH:mm")
		}
	}

	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()

	id := strings.TrimSpace(req.ID)
	if id == "" {
		id = "job_" + uuid.New().String()[:8]
	}
	job, ok := s.jobs[id]
	if !ok {
		job = &Job{
			ID:        id,
			CreatedAt: now,
		}
		s.jobs[id] = job
	}
	job.AccountID = req.AccountID
	job.LabelPrefix = req.LabelPrefix
	job.Mode = req.Mode
	job.DurationHours = req.DurationHours
	job.StartTime = req.StartTime
	job.EndTime = req.EndTime
	job.Status = StatusRunning
	job.LastError = ""
	if job.StartedAt == nil {
		job.StartedAt = timePtr(now)
	}
	if job.Mode == ModeDuration {
		job.EndedAt = timePtr(job.StartedAt.Add(time.Duration(job.DurationHours) * time.Hour))
	} else {
		job.EndedAt = nil
	}
	job.NextRunAt = timePtr(now)
	job.UpdatedAt = now
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return cloneJob(job), nil
}

func (s *Scheduler) ListJobs(accountID string) []*Job {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]*Job, 0, len(s.jobs))
	for _, job := range s.jobs {
		if accountID == "" || job.AccountID == accountID {
			out = append(out, cloneJob(job))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

func (s *Scheduler) GetJob(id string) (*Job, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return nil, false
	}
	return cloneJob(job), true
}

func (s *Scheduler) PauseJob(id string) (*Job, error) {
	return s.setStatus(id, StatusPaused)
}

func (s *Scheduler) ResumeJob(id string) (*Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return nil, errors.New("任务不存在")
	}
	now := s.now()
	job.Status = StatusRunning
	job.LastError = ""
	job.NextRunAt = timePtr(now)
	if job.Mode == ModeDuration {
		job.StartedAt = timePtr(now)
		job.EndedAt = timePtr(now.Add(time.Duration(job.DurationHours) * time.Hour))
	}
	job.UpdatedAt = now
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return cloneJob(job), nil
}

func (s *Scheduler) DeleteJob(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.jobs[id]; !ok {
		return errors.New("任务不存在")
	}
	delete(s.jobs, id)
	return s.saveLocked()
}

func (s *Scheduler) RunDue(ctx context.Context) error {
	now := s.now()
	dueJobs := s.snapshotDue(now)
	for _, job := range dueJobs {
		s.runJob(ctx, job.ID, now)
	}
	return nil
}

func (s *Scheduler) Start(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	go func() {
		_ = s.RunDue(ctx)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = s.RunDue(ctx)
			}
		}
	}()
}

func (s *Scheduler) RemainingThisHour(accountID string) int {
	return s.limiter.Remaining(accountID, s.now())
}

func (s *Scheduler) runJob(ctx context.Context, id string, at time.Time) {
	s.mu.Lock()
	job, ok := s.jobs[id]
	if !ok || job.Status != StatusRunning {
		s.mu.Unlock()
		return
	}
	if job.Mode == ModeDuration && job.EndedAt != nil && !at.Before(*job.EndedAt) {
		job.Status = StatusCompleted
		job.UpdatedAt = at
		_ = s.saveLocked()
		s.mu.Unlock()
		return
	}
	if job.Mode == ModeDailyWindow && !s.isInDailyWindow(at, job.StartTime, job.EndTime) {
		job.NextRunAt = timePtr(nextHour(at))
		job.UpdatedAt = at
		_ = s.saveLocked()
		s.mu.Unlock()
		return
	}
	accountID := job.AccountID
	labelPrefix := job.LabelPrefix
	nextIndex := job.CreatedCount + 1
	s.mu.Unlock()

	if !s.limiter.TryReserve(accountID, at, 1) {
		s.updateJob(id, at, func(job *Job) {
			job.NextRunAt = timePtr(nextHour(at))
		})
		return
	}
	result, err := s.creator.CreateAlias(ctx, accountID, labelFor(labelPrefix, nextIndex))
	if err != nil {
		s.limiter.Release(accountID, at, 1)
		s.updateJob(id, at, func(job *Job) {
			job.Status = StatusError
			job.LastError = err.Error()
			job.NextRunAt = nil
		})
		return
	}
	_ = result
	s.updateJob(id, at, func(job *Job) {
		job.CreatedCount++
		job.LastError = ""
		job.NextRunAt = timePtr(s.nextAutomaticRun(at, job.AccountID))
	})
}

func (s *Scheduler) snapshotDue(now time.Time) []*Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Job, 0, len(s.jobs))
	for _, job := range s.jobs {
		if job.Status != StatusRunning {
			continue
		}
		if job.NextRunAt == nil || !job.NextRunAt.After(now) {
			out = append(out, cloneJob(job))
		}
	}
	return out
}

func (s *Scheduler) updateJob(id string, at time.Time, update func(*Job)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return
	}
	update(job)
	job.UpdatedAt = at
	_ = s.saveLocked()
}

func (s *Scheduler) setStatus(id, status string) (*Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return nil, errors.New("任务不存在")
	}
	job.Status = status
	job.UpdatedAt = s.now()
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return cloneJob(job), nil
}

func (s *Scheduler) saveLocked() error {
	jobs := make([]*Job, 0, len(s.jobs))
	for _, job := range s.jobs {
		jobs = append(jobs, cloneJob(job))
	}
	sort.Slice(jobs, func(i, j int) bool {
		return jobs[i].CreatedAt.Before(jobs[j].CreatedAt)
	})
	return s.store.Save(jobs)
}

func (s *Scheduler) normalizeLoadedJob(job *Job) {
	now := s.now()
	if job.Status == "" {
		job.Status = StatusPaused
	}
	if job.Mode == ModeDuration && job.Status == StatusRunning && job.EndedAt != nil && !now.Before(*job.EndedAt) {
		job.Status = StatusCompleted
	}
	if job.Status == StatusRunning && job.NextRunAt == nil {
		job.NextRunAt = timePtr(now)
	}
}

func (s *Scheduler) nextAutomaticRun(at time.Time, accountID string) time.Time {
	if s.limiter.Remaining(accountID, at) <= 0 {
		return nextHour(at)
	}
	hourEnd := nextHour(at)
	remaining := hourEnd.Sub(at)
	if remaining <= time.Minute {
		return hourEnd
	}
	return at.Add(time.Duration(s.rand.Int63n(int64(remaining))))
}

func (s *Scheduler) isInDailyWindow(at time.Time, start, end string) bool {
	startDur, ok := parseClock(start)
	if !ok {
		return false
	}
	endDur, ok := parseClock(end)
	if !ok {
		return false
	}
	current := time.Duration(at.Hour())*time.Hour + time.Duration(at.Minute())*time.Minute
	if startDur == endDur {
		return true
	}
	if startDur < endDur {
		return current >= startDur && current < endDur
	}
	return current >= startDur || current < endDur
}

func validClock(value string) bool {
	_, ok := parseClock(value)
	return ok
}

func parseClock(value string) (time.Duration, bool) {
	parsed, err := time.Parse("15:04", value)
	if err != nil {
		return 0, false
	}
	return time.Duration(parsed.Hour())*time.Hour + time.Duration(parsed.Minute())*time.Minute, true
}

func labelFor(prefix string, index int) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "Created"
	}
	if index <= 0 {
		index = 1
	}
	return fmt.Sprintf("%s %d", prefix, index)
}

func nextHour(at time.Time) time.Time {
	return at.Local().Truncate(time.Hour).Add(time.Hour)
}

func cloneJob(job *Job) *Job {
	if job == nil {
		return nil
	}
	cp := *job
	cp.StartedAt = cloneTimePtr(job.StartedAt)
	cp.EndedAt = cloneTimePtr(job.EndedAt)
	cp.NextRunAt = cloneTimePtr(job.NextRunAt)
	return &cp
}

func timePtr(value time.Time) *time.Time {
	return &value
}

func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cp := *value
	return &cp
}
