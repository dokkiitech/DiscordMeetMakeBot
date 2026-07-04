package scheduler

import (
	"sync"
	"testing"
	"time"
)

func TestScheduleFires(t *testing.T) {
	var mu sync.Mutex
	got := map[string][]string{}
	done := make(chan struct{}, 1)

	s := New("", func(recipients []string, content string) {
		mu.Lock()
		got[content] = recipients
		mu.Unlock()
		done <- struct{}{}
	})
	if err := s.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Stop()

	s.Schedule(Reminder{
		ID:         "r1",
		FireAt:     time.Now().Add(20 * time.Millisecond),
		Recipients: []string{"u1", "u2"},
		Content:    "hello",
	})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("reminder did not fire")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(got["hello"]) != 2 {
		t.Fatalf("expected 2 recipients, got %v", got["hello"])
	}
}

func TestStalePastReminderDropped(t *testing.T) {
	fired := make(chan struct{}, 1)
	s := New("", func(recipients []string, content string) { fired <- struct{}{} })

	// Directly seed a reminder far in the past (older than the grace window).
	s.reminders["old"] = &Reminder{
		ID:         "old",
		FireAt:     time.Now().Add(-2 * staleGrace),
		Recipients: []string{"u1"},
		Content:    "stale",
	}
	if err := s.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Stop()

	select {
	case <-fired:
		t.Fatal("stale reminder should not fire")
	case <-time.After(200 * time.Millisecond):
	}
	if _, ok := s.reminders["old"]; ok {
		t.Fatal("stale reminder should have been pruned")
	}
}
