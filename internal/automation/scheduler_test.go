package automation

import (
	"testing"
	"time"
)

func TestLatestDueUsesLocalWallClockAndTaskUpdateBoundary(t *testing.T) {
	t.Parallel()

	location := time.FixedZone("UTC+8", 8*60*60)
	task := Task{
		ID:        "daily-report",
		RRule:     "FREQ=DAILY;BYHOUR=17;BYMINUTE=50",
		CreatedAt: time.Date(2026, 8, 31, 14, 6, 0, 0, location),
		UpdatedAt: time.Date(2026, 8, 31, 14, 6, 0, 0, location),
	}

	if _, ok, err := LatestDue(task, time.Date(2026, 8, 31, 17, 49, 59, 0, location)); err != nil || ok {
		t.Fatalf("LatestDue(before): ok=%v err=%v, want no due occurrence", ok, err)
	}
	due, ok, err := LatestDue(task, time.Date(2026, 8, 31, 17, 50, 5, 0, location))
	if err != nil || !ok {
		t.Fatalf("LatestDue(at): ok=%v err=%v", ok, err)
	}
	want := time.Date(2026, 8, 31, 17, 50, 0, 0, location)
	if !due.Equal(want) {
		t.Fatalf("due = %s, want %s", due, want)
	}

	task.UpdatedAt = time.Date(2026, 8, 31, 18, 0, 0, 0, location)
	if _, ok, err := LatestDue(task, time.Date(2026, 8, 31, 18, 1, 0, 0, location)); err != nil || ok {
		t.Fatalf("LatestDue(after post-slot update): ok=%v err=%v, want next schedule only", ok, err)
	}
}

func TestLatestDueSupportsHourlyAndWeeklyIntervals(t *testing.T) {
	t.Parallel()

	location := time.UTC
	created := time.Date(2026, 8, 3, 9, 10, 0, 0, location) // Monday.
	for name, tc := range map[string]struct {
		rrule string
		now   time.Time
		want  time.Time
	}{
		"hourly": {
			rrule: "FREQ=HOURLY;INTERVAL=3;BYMINUTE=15",
			now:   time.Date(2026, 8, 3, 18, 20, 0, 0, location),
			want:  time.Date(2026, 8, 3, 18, 15, 0, 0, location),
		},
		"weekly": {
			rrule: "FREQ=WEEKLY;INTERVAL=2;BYDAY=MO,WE;BYHOUR=8;BYMINUTE=30",
			now:   time.Date(2026, 8, 19, 12, 0, 0, 0, location),
			want:  time.Date(2026, 8, 19, 8, 30, 0, 0, location),
		},
	} {
		t.Run(name, func(t *testing.T) {
			task := Task{ID: name, RRule: tc.rrule, CreatedAt: created, UpdatedAt: created}
			due, ok, err := LatestDue(task, tc.now)
			if err != nil || !ok {
				t.Fatalf("LatestDue: ok=%v err=%v", ok, err)
			}
			if !due.Equal(tc.want) {
				t.Fatalf("due = %s, want %s", due, tc.want)
			}
		})
	}
}
