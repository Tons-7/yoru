package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"runtime/debug"
	"sync"
	"time"
)

type Func func(context.Context) error

type Job struct {
	Name    string
	Every   time.Duration
	OnStart bool
	Run     Func

	Interval func() time.Duration
}

func (j Job) every() time.Duration {
	if j.Interval != nil {
		if d := j.Interval(); d > 0 {
			return d
		}
	}
	return j.Every
}

type Status struct {
	Name     string    `json:"name"`
	Every    string    `json:"every"`
	Running  bool      `json:"running"`
	LastRun  time.Time `json:"lastRun"`
	LastOK   time.Time `json:"lastOk"`
	NextRun  time.Time `json:"nextRun"`
	Runs     int       `json:"runs"`
	Failures int       `json:"failures"`
	LastErr  string    `json:"lastError,omitempty"`
	Took     string    `json:"took,omitempty"`
}

type entry struct {
	job Job

	mu          sync.Mutex
	running     bool
	lastRun     time.Time
	lastOK      time.Time
	nextRun     time.Time
	runs        int
	failures    int
	lastErr     string
	took        time.Duration
	consecutive int
	retime      chan struct{}
}

var ErrRunning = errors.New("already running")

type Scheduler struct {
	log *slog.Logger
	ctx context.Context

	mu      sync.Mutex
	entries map[string]*entry
	order   []string
	running sync.WaitGroup
}

func (s *Scheduler) Wait(limit time.Duration) bool {
	done := make(chan struct{})
	go func() {
		s.running.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(limit):
		return false
	}
}

func New(log *slog.Logger) *Scheduler {
	return &Scheduler{log: log, entries: map[string]*entry{}}
}

func (s *Scheduler) Add(job Job) {
	if job.Run == nil || job.Name == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.entries[job.Name]; exists {
		return
	}
	s.entries[job.Name] = &entry{job: job, retime: make(chan struct{}, 1)}
	s.order = append(s.order, job.Name)
}

func (s *Scheduler) Start(ctx context.Context) {
	s.mu.Lock()
	s.ctx = ctx
	names := append([]string(nil), s.order...)
	s.mu.Unlock()

	for _, name := range names {
		s.running.Go(func() { s.loop(ctx, name) })
	}
}

func (s *Scheduler) loop(ctx context.Context, name string) {
	e := s.entry(name)
	if e == nil {
		return
	}

	startRetry := false
	if e.job.OnStart {
		s.execute(ctx, e)
		e.mu.Lock()
		startRetry = e.consecutive > 0
		e.mu.Unlock()
	}

	for {
		retry := startRetry
		startRetry = false
		select {
		case <-e.retime:
		default:
		}
		wait := e.due(retry)
		e.mu.Lock()
		e.nextRun = time.Now().Add(wait)
		e.mu.Unlock()

		if !s.sleep(ctx, e, wait, retry) {
			return
		}
		s.execute(ctx, e)
	}
}

func (e *entry) due(retry bool) time.Duration {
	every := e.job.every()
	if retry {
		return min(every, startRetryAfter)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.consecutive > 0 {
		return backoff(every, e.consecutive)
	}
	return every
}

func (s *Scheduler) sleep(ctx context.Context, e *entry, wait time.Duration, retry bool) bool {
	started := time.Now()
	timer := time.NewTimer(wait)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-timer.C:
			return true
		case <-e.retime:
			next := started.Add(e.due(retry))
			e.mu.Lock()
			e.nextRun = next
			e.mu.Unlock()
			timer.Reset(max(0, time.Until(next)))
		}
	}
}

func (s *Scheduler) Retime(names ...string) {
	for _, name := range names {
		if e := s.entry(name); e != nil {
			select {
			case e.retime <- struct{}{}:
			default:
			}
		}
	}
}

var startRetryAfter = 5 * time.Minute

func backoff(base time.Duration, failures int) time.Duration {
	capped := math.Min(float64(failures), 5)
	wait := time.Duration(float64(base) * math.Pow(2, capped))
	return max(min(wait, 6*time.Hour), base)
}

func (s *Scheduler) execute(ctx context.Context, e *entry) error {
	if !e.claim() {
		return ErrRunning
	}
	return s.finish(ctx, e, e.job.Run)
}

func (e *entry) claim() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.running {
		return false
	}
	e.running, e.lastRun = true, time.Now()
	return true
}

func (s *Scheduler) finish(ctx context.Context, e *entry, run Func) error {
	start := time.Now()
	err := safely(s.log, e.job.Name, ctx, run)
	took := time.Since(start)

	e.mu.Lock()
	e.running, e.runs, e.took = false, e.runs+1, took
	if err != nil {
		e.failures++
		e.consecutive++
		e.lastErr = err.Error()
	} else {
		e.consecutive = 0
		e.lastErr, e.lastOK = "", time.Now()
	}
	e.mu.Unlock()

	if err != nil {
		s.log.Warn("job failed", "job", e.job.Name, "err", err, "took", took.Round(time.Millisecond))
	}
	return err
}

func safely(log *slog.Logger, name string, ctx context.Context, run Func) (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Error("job panicked", "job", name, "err", r, "stack", string(debug.Stack()))
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return run(ctx)
}

func (s *Scheduler) Trigger(name string) error {
	return s.RunAs(name, nil)
}

func (s *Scheduler) RunAs(name string, run Func) error {
	e := s.entry(name)
	if e == nil {
		return fmt.Errorf("no such job: %s", name)
	}
	if run == nil {
		run = e.job.Run
	}
	s.mu.Lock()
	ctx := s.ctx
	s.mu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !e.claim() {
		return ErrRunning
	}
	s.running.Go(func() { s.finish(ctx, e, run) })
	return nil
}

func (s *Scheduler) Status() []Status {
	s.mu.Lock()
	names := append([]string(nil), s.order...)
	s.mu.Unlock()

	out := make([]Status, 0, len(names))
	for _, name := range names {
		e := s.entry(name)
		if e == nil {
			continue
		}

		every := e.job.every()
		e.mu.Lock()
		st := Status{
			Name: name, Every: every.String(),
			Running: e.running, LastRun: e.lastRun, LastOK: e.lastOK,
			NextRun: e.nextRun, Runs: e.runs, Failures: e.failures,
			LastErr: e.lastErr,
		}
		if e.took > 0 {
			st.Took = e.took.Round(time.Millisecond).String()
		}
		e.mu.Unlock()

		out = append(out, st)
	}
	return out
}

func (s *Scheduler) entry(name string) *entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.entries[name]
}
