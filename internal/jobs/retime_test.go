package jobs

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestRetimeAppliesANewIntervalWithoutWaitingOutTheOld(t *testing.T) {
	s := newScheduler()
	var runs atomic.Int32
	var every atomic.Int64
	every.Store(int64(time.Hour))

	s.Add(Job{
		Name: "sync", Every: time.Hour, OnStart: true,
		Interval: func() time.Duration { return time.Duration(every.Load()) },
		Run:      func(context.Context) error { runs.Add(1); return nil },
	})
	s.Start(t.Context())
	waitFor(t, func() bool { return runs.Load() == 1 })

	s.Retime("sync")
	time.Sleep(100 * time.Millisecond)
	if runs.Load() != 1 {
		t.Fatal("a retime with the same hour-long interval ran the job early")
	}
	if next := s.Status()[0].NextRun; time.Until(next) < 59*time.Minute {
		t.Fatalf("next run moved to %v from now", time.Until(next))
	}

	every.Store(int64(50 * time.Millisecond))
	s.Retime("sync", "no such job")
	waitFor(t, func() bool { return runs.Load() >= 2 })
}

func TestRetimeKeepsTheShortRetryAfterAFailedStart(t *testing.T) {
	old := startRetryAfter
	startRetryAfter = 100 * time.Millisecond
	t.Cleanup(func() { startRetryAfter = old })

	s := newScheduler()
	var runs atomic.Int32
	s.Add(Job{
		Name: "sync", Every: time.Hour, OnStart: true,
		Run: func(context.Context) error {
			if runs.Add(1) == 1 {
				return errors.New("offline")
			}
			return nil
		},
	})
	s.Start(t.Context())
	waitFor(t, func() bool {
		st := s.Status()[0]
		return runs.Load() == 1 && !st.Running && st.NextRun.After(st.LastRun)
	})
	s.Retime("sync")
	waitFor(t, func() bool { return runs.Load() == 2 })
}
