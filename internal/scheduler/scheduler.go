// Package scheduler stores and fires time-based reminders. Each reminder is a
// pre-rendered message delivered to a set of Discord user IDs at a fixed time.
// Reminders can optionally be persisted to a JSON file so they survive a process
// restart (configure a durable path/volume for this to be meaningful).
package scheduler

import (
	"encoding/json"
	"log"
	"os"
	"sort"
	"sync"
	"time"
)

// staleGrace is how long after its scheduled time a reminder may still be fired
// when the bot was offline. Older missed reminders are dropped rather than sent
// far too late.
const staleGrace = time.Hour

// Reminder is a single scheduled DM delivery.
type Reminder struct {
	ID         string    `json:"id"`
	FireAt     time.Time `json:"fire_at"`
	Recipients []string  `json:"recipients"`
	Content    string    `json:"content"`
}

// SendFunc delivers a reminder's content to the given recipient user IDs.
type SendFunc func(recipients []string, content string)

// Scheduler holds pending reminders and fires them at their scheduled time.
type Scheduler struct {
	mu        sync.Mutex
	reminders map[string]*Reminder
	timers    map[string]*time.Timer
	storePath string
	send      SendFunc
}

// New creates a Scheduler. If storePath is non-empty, reminders are persisted to
// that file and reloaded on Start.
func New(storePath string, send SendFunc) *Scheduler {
	return &Scheduler{
		reminders: make(map[string]*Reminder),
		timers:    make(map[string]*time.Timer),
		storePath: storePath,
		send:      send,
	}
}

// Start loads any persisted reminders and arms timers for the pending ones.
func (s *Scheduler) Start() error {
	if err := s.load(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	var stale []string
	for id, r := range s.reminders {
		if now.Sub(r.FireAt) > staleGrace {
			stale = append(stale, id)
			continue
		}
		s.arm(r)
	}
	for _, id := range stale {
		delete(s.reminders, id)
	}
	s.persistLocked()
	return nil
}

// Stop cancels all pending timers. Persisted reminders remain on disk.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.timers {
		t.Stop()
	}
	s.timers = make(map[string]*time.Timer)
}

// Schedule registers reminders and arms their timers.
func (s *Scheduler) Schedule(reminders ...Reminder) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range reminders {
		r := reminders[i]
		if r.ID == "" {
			continue
		}
		rp := &r
		s.reminders[rp.ID] = rp
		s.arm(rp)
	}
	s.persistLocked()
}

// arm schedules the timer for r. Callers must hold s.mu.
func (s *Scheduler) arm(r *Reminder) {
	d := time.Until(r.FireAt)
	if d < 0 {
		d = 0
	}
	id := r.ID
	if t, ok := s.timers[id]; ok {
		t.Stop()
	}
	s.timers[id] = time.AfterFunc(d, func() { s.fire(id) })
}

func (s *Scheduler) fire(id string) {
	s.mu.Lock()
	r, ok := s.reminders[id]
	if !ok {
		s.mu.Unlock()
		return
	}
	recipients := append([]string(nil), r.Recipients...)
	content := r.Content
	delete(s.reminders, id)
	delete(s.timers, id)
	s.persistLocked()
	send := s.send
	s.mu.Unlock()

	if send != nil {
		send(recipients, content)
	}
}

func (s *Scheduler) load() error {
	if s.storePath == "" {
		return nil
	}
	data, err := os.ReadFile(s.storePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var list []*Reminder
	if err := json.Unmarshal(data, &list); err != nil {
		log.Printf("scheduler: cannot parse store %s, ignoring: %v", s.storePath, err)
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range list {
		if r == nil || r.ID == "" {
			continue
		}
		s.reminders[r.ID] = r
	}
	return nil
}

// persistLocked writes pending reminders to disk. Callers must hold s.mu.
func (s *Scheduler) persistLocked() {
	if s.storePath == "" {
		return
	}
	list := make([]*Reminder, 0, len(s.reminders))
	for _, r := range s.reminders {
		list = append(list, r)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].FireAt.Before(list[j].FireAt) })

	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		log.Printf("scheduler: marshal store: %v", err)
		return
	}
	tmp := s.storePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		log.Printf("scheduler: write store: %v", err)
		return
	}
	if err := os.Rename(tmp, s.storePath); err != nil {
		log.Printf("scheduler: rename store: %v", err)
	}
}
