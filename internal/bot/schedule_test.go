package bot

import (
	"testing"
	"time"
)

func TestParseSchedule(t *testing.T) {
	now := time.Date(2026, 7, 4, 12, 0, 0, 0, jst)

	tests := []struct {
		name string
		raw  string
		want time.Time
	}{
		{"full date", "2026-07-10 15:00", time.Date(2026, 7, 10, 15, 0, 0, 0, jst)},
		{"full date slash", "2026/07/10 15:00", time.Date(2026, 7, 10, 15, 0, 0, 0, jst)},
		{"single digit", "2026-7-5 9:30", time.Date(2026, 7, 5, 9, 30, 0, 0, jst)},
		{"month-day future", "07-10 15:00", time.Date(2026, 7, 10, 15, 0, 0, 0, jst)},
		{"month-day past rolls to next year", "01-02 09:00", time.Date(2027, 1, 2, 9, 0, 0, 0, jst)},
		{"time only later today", "15:00", time.Date(2026, 7, 4, 15, 0, 0, 0, jst)},
		{"time only past rolls to tomorrow", "09:00", time.Date(2026, 7, 5, 9, 0, 0, 0, jst)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSchedule(tt.raw, now)
			if err != nil {
				t.Fatalf("parseSchedule(%q) error: %v", tt.raw, err)
			}
			if !got.Equal(tt.want) {
				t.Errorf("parseSchedule(%q) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestParseScheduleInvalid(t *testing.T) {
	now := time.Date(2026, 7, 4, 12, 0, 0, 0, jst)
	for _, raw := range []string{"", "tomorrow", "2026-13-40 99:99", "abc"} {
		if _, err := parseSchedule(raw, now); err == nil {
			t.Errorf("parseSchedule(%q) expected error, got nil", raw)
		}
	}
}
