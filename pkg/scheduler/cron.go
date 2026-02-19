// Package scheduler — cron expression parser.
// Supports standard 5-field cron: minute hour day-of-month month day-of-week
// Fields: *, N, N-M, N-M/step, */step, comma-separated lists.
// Day-of-week: 0=Sunday, 1=Monday, ..., 6=Saturday, 7=Sunday.
package scheduler

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// CronExpr represents a parsed cron expression.
type CronExpr struct {
	Minute     []bool // 0-59
	Hour       []bool // 0-23
	DayOfMonth []bool // 1-31
	Month      []bool // 1-12
	DayOfWeek  []bool // 0-6 (Sun=0)
}

// ParseCron parses a standard 5-field cron expression.
func ParseCron(expr string) (*CronExpr, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return nil, fmt.Errorf("cron: expected 5 fields, got %d", len(fields))
	}

	minute, err := parseField(fields[0], 0, 59)
	if err != nil {
		return nil, fmt.Errorf("cron minute: %w", err)
	}
	hour, err := parseField(fields[1], 0, 23)
	if err != nil {
		return nil, fmt.Errorf("cron hour: %w", err)
	}
	dom, err := parseField(fields[2], 1, 31)
	if err != nil {
		return nil, fmt.Errorf("cron day-of-month: %w", err)
	}
	month, err := parseField(fields[3], 1, 12)
	if err != nil {
		return nil, fmt.Errorf("cron month: %w", err)
	}
	dow, err := parseField(fields[4], 0, 7)
	if err != nil {
		return nil, fmt.Errorf("cron day-of-week: %w", err)
	}

	c := &CronExpr{
		Minute:     minute,
		Hour:       hour,
		DayOfMonth: dom,
		Month:      month,
		DayOfWeek:  make([]bool, 7),
	}

	// Normalize day-of-week: 7 maps to 0 (both = Sunday)
	for i := 0; i < len(dow) && i <= 7; i++ {
		if i < len(dow) && dow[i] {
			if i == 7 {
				c.DayOfWeek[0] = true
			} else {
				c.DayOfWeek[i] = true
			}
		}
	}

	return c, nil
}

// Matches returns true if the given time matches the cron expression.
func (c *CronExpr) Matches(t time.Time) bool {
	min := t.Minute()
	hr := t.Hour()
	dom := t.Day()
	mon := int(t.Month())
	dow := int(t.Weekday()) // 0=Sunday

	if min < len(c.Minute) && !c.Minute[min] {
		return false
	}
	if hr < len(c.Hour) && !c.Hour[hr] {
		return false
	}
	if dom < len(c.DayOfMonth) && !c.DayOfMonth[dom] {
		return false
	}
	if mon < len(c.Month) && !c.Month[mon] {
		return false
	}
	if dow < len(c.DayOfWeek) && !c.DayOfWeek[dow] {
		return false
	}

	return true
}

// parseField parses a single cron field into a boolean slice.
func parseField(field string, min, max int) ([]bool, error) {
	result := make([]bool, max+1)

	for _, part := range strings.Split(field, ",") {
		if err := parsePart(part, min, max, result); err != nil {
			return nil, err
		}
	}

	return result, nil
}

func parsePart(part string, min, max int, result []bool) error {
	// Handle step: */2, 1-5/2
	step := 1
	if idx := strings.Index(part, "/"); idx >= 0 {
		var err error
		step, err = strconv.Atoi(part[idx+1:])
		if err != nil || step <= 0 {
			return fmt.Errorf("invalid step: %s", part)
		}
		part = part[:idx]
	}

	var lo, hi int

	switch {
	case part == "*":
		lo, hi = min, max
	case strings.Contains(part, "-"):
		bounds := strings.SplitN(part, "-", 2)
		var err error
		lo, err = strconv.Atoi(bounds[0])
		if err != nil {
			return fmt.Errorf("invalid range: %s", part)
		}
		hi, err = strconv.Atoi(bounds[1])
		if err != nil {
			return fmt.Errorf("invalid range: %s", part)
		}
	default:
		val, err := strconv.Atoi(part)
		if err != nil {
			return fmt.Errorf("invalid value: %s", part)
		}
		lo, hi = val, val
	}

	if lo < min || hi > max || lo > hi {
		return fmt.Errorf("out of range: %d-%d (allowed %d-%d)", lo, hi, min, max)
	}

	for i := lo; i <= hi; i += step {
		result[i] = true
	}

	return nil
}
