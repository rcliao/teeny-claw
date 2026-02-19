package scheduler

import (
	"testing"
	"time"
)

func TestParseCronValid(t *testing.T) {
	tests := []struct {
		expr string
	}{
		{"* * * * *"},
		{"0 * * * *"},
		{"*/5 * * * *"},
		{"0 9 * * 1-5"},
		{"30 14 1 * *"},
		{"0 0 * * 0"},
		{"0 0 * * 7"}, // 7 = Sunday alias
		{"0,30 * * * *"},
		{"0 9-17 * * *"},
		{"0 9-17/2 * * *"},
	}

	for _, tt := range tests {
		c, err := ParseCron(tt.expr)
		if err != nil {
			t.Errorf("ParseCron(%q) error: %v", tt.expr, err)
		}
		if c == nil {
			t.Errorf("ParseCron(%q) returned nil", tt.expr)
		}
	}
}

func TestParseCronInvalid(t *testing.T) {
	tests := []string{
		"",
		"* * *",
		"* * * * * *",
		"60 * * * *",
		"* 25 * * *",
		"@every 5m",
		"abc * * * *",
	}

	for _, expr := range tests {
		_, err := ParseCron(expr)
		if err == nil {
			t.Errorf("ParseCron(%q) expected error", expr)
		}
	}
}

func TestCronMatches(t *testing.T) {
	// "0 9 * * 1-5" = weekdays at 9:00 AM
	c, _ := ParseCron("0 9 * * 1-5")

	// Monday 9:00 AM
	mon9 := time.Date(2026, 2, 16, 9, 0, 0, 0, time.UTC) // Monday
	if !c.Matches(mon9) {
		t.Error("expected match: Monday 9:00")
	}

	// Monday 9:30 AM — minute doesn't match
	mon930 := time.Date(2026, 2, 16, 9, 30, 0, 0, time.UTC)
	if c.Matches(mon930) {
		t.Error("expected no match: Monday 9:30")
	}

	// Sunday 9:00 AM — day doesn't match
	sun9 := time.Date(2026, 2, 15, 9, 0, 0, 0, time.UTC) // Sunday
	if c.Matches(sun9) {
		t.Error("expected no match: Sunday 9:00")
	}
}

func TestCronMatchesEveryFiveMinutes(t *testing.T) {
	c, _ := ParseCron("*/5 * * * *")

	for min := 0; min < 60; min++ {
		tm := time.Date(2026, 1, 1, 12, min, 0, 0, time.UTC)
		want := min%5 == 0
		if c.Matches(tm) != want {
			t.Errorf("*/5 at minute %d: got %v, want %v", min, !want, want)
		}
	}
}

func TestCronSunday7Alias(t *testing.T) {
	c, _ := ParseCron("0 0 * * 7")
	sun := time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC) // Sunday
	if !c.Matches(sun) {
		t.Error("expected 7 to match Sunday")
	}
	mon := time.Date(2026, 2, 16, 0, 0, 0, 0, time.UTC) // Monday
	if c.Matches(mon) {
		t.Error("expected 7 to not match Monday")
	}
}

func TestShouldRunCron(t *testing.T) {
	schedule := "0 10 * * *"

	now := time.Date(2026, 2, 17, 10, 0, 30, 0, time.UTC)
	lastBefore := time.Date(2026, 2, 16, 10, 0, 0, 0, time.UTC)
	lastSameMinute := time.Date(2026, 2, 17, 10, 0, 0, 0, time.UTC)

	if !shouldRun(schedule, lastBefore, now) {
		t.Error("expected shouldRun=true: cron matches and last was yesterday")
	}
	if shouldRun(schedule, lastSameMinute, now) {
		t.Error("expected shouldRun=false: already ran this minute")
	}
}

func TestShouldRunInitialCron(t *testing.T) {
	matching := time.Date(2026, 2, 17, 10, 0, 0, 0, time.UTC)
	nonMatching := time.Date(2026, 2, 17, 10, 5, 0, 0, time.UTC)

	if !shouldRunInitial("0 10 * * *", matching) {
		t.Error("expected shouldRunInitial=true at matching time")
	}
	if shouldRunInitial("0 10 * * *", nonMatching) {
		t.Error("expected shouldRunInitial=false at non-matching time")
	}
}

func TestCronCommaList(t *testing.T) {
	c, _ := ParseCron("0,15,30,45 * * * *")
	for _, min := range []int{0, 15, 30, 45} {
		tm := time.Date(2026, 1, 1, 12, min, 0, 0, time.UTC)
		if !c.Matches(tm) {
			t.Errorf("expected match at minute %d", min)
		}
	}
	tm := time.Date(2026, 1, 1, 12, 10, 0, 0, time.UTC)
	if c.Matches(tm) {
		t.Error("expected no match at minute 10")
	}
}
