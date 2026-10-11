// Package cron parses cron schedules and works out their ticks: the standard 5 fields
// (minute hour day-of-month month day-of-week) with *, lists, ranges, steps and month/day
// names, the descriptors @hourly, @daily (@midnight), @weekly, @monthly, @yearly
// (@annually), and @every <duration>.
//
// Ticks are in the location of the time given to Next (the local time zone for times from
// time.Now). A wall-clock time skipped when the clock goes forward ticks right after the
// change, one repeated when it goes back ticks once.
package cron

import (
	"errors"
	"fmt"
	"math/bits"
	"strconv"
	"strings"
	"time"
)

// Schedule is a parsed schedule, the zero Schedule never ticks.
type Schedule struct {
	spec string
	// every is an @every schedule: ticks at the multiples of it since the zero time
	every time.Duration
	// bit n set = value n matches
	minute, hour, dom, month, dow uint64
	// a day field starting with * (Vixie cron): the day matches both fields, else either
	domStar, dowStar bool
}

var descriptors = map[string]string{
	"@yearly":   "0 0 1 1 *",
	"@annually": "0 0 1 1 *",
	"@monthly":  "0 0 1 * *",
	"@weekly":   "0 0 * * 0",
	"@daily":    "0 0 * * *",
	"@midnight": "0 0 * * *",
	"@hourly":   "0 * * * *",
}

type field struct {
	name     string
	min, max int
	names    []string
}

var fields = [5]field{
	{name: "minute", min: 0, max: 59},
	{name: "hour", min: 0, max: 23},
	{name: "day of month", min: 1, max: 31},
	{name: "month", min: 1, max: 12, names: []string{"", "jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec"}},
	// 7 is sunday too
	{name: "day of week", min: 0, max: 7, names: []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}},
}

// maxDays is how far Next looks: a schedule that only ticks on the 29th of February
// ticks at least once in 8 years.
const maxDays = 8*366 + 1

// Parse parses a schedule (see the package doc).
func Parse(spec string) (Schedule, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return Schedule{}, errors.New("empty schedule")
	}
	if d, ok := strings.CutPrefix(spec, "@every"); ok {
		every, err := time.ParseDuration(strings.TrimSpace(d))
		if err != nil {
			return Schedule{}, fmt.Errorf("schedule %q: %w", spec, err)
		}
		if every < time.Minute {
			return Schedule{}, fmt.Errorf("schedule %q: @every needs at least 1m", spec)
		}
		return Schedule{spec: spec, every: every}, nil
	}
	expanded := spec
	if strings.HasPrefix(spec, "@") {
		var ok bool
		if expanded, ok = descriptors[strings.ToLower(spec)]; !ok {
			return Schedule{}, fmt.Errorf("schedule %q: unknown descriptor (@hourly, @daily, @weekly, @monthly, @yearly, @every <duration>)", spec)
		}
	}
	parts := strings.Fields(expanded)
	if len(parts) != 5 {
		return Schedule{}, fmt.Errorf("schedule %q: %d fields, a cron schedule has 5 (minute hour day-of-month month day-of-week)", spec, len(parts))
	}
	s := Schedule{spec: spec}
	sets := [5]*uint64{&s.minute, &s.hour, &s.dom, &s.month, &s.dow}
	for i, part := range parts {
		set, err := parseField(part, fields[i])
		if err != nil {
			return Schedule{}, fmt.Errorf("schedule %q: %w", spec, err)
		}
		*sets[i] = set
	}
	if s.dow&(1<<7) != 0 {
		s.dow = s.dow&^(1<<7) | 1
	}
	s.domStar, s.dowStar = strings.HasPrefix(parts[2], "*"), strings.HasPrefix(parts[4], "*")
	if s.Next(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)).IsZero() {
		return Schedule{}, fmt.Errorf("schedule %q never happens", spec)
	}
	return s, nil
}

// MustParse is Parse for schedules known to be valid, it panics otherwise.
func MustParse(spec string) Schedule {
	s, err := Parse(spec)
	if err != nil {
		panic(err)
	}
	return s
}

// parseField is a comma separated list of *, n, a-b, each with an optional /step.
func parseField(s string, f field) (uint64, error) {
	var set uint64
	for item := range strings.SplitSeq(s, ",") {
		rng, stepStr, hasStep := strings.Cut(item, "/")
		step := 1
		if hasStep {
			n, err := strconv.Atoi(stepStr)
			if err != nil || n < 1 {
				return 0, fmt.Errorf("%s: step %q isn't a positive number", f.name, stepStr)
			}
			step = n
		}
		var lo, hi int
		switch a, b, isRange := strings.Cut(rng, "-"); {
		case rng == "*":
			lo, hi = f.min, f.max
		case isRange:
			var err error
			if lo, err = f.value(a); err != nil {
				return 0, err
			}
			if hi, err = f.value(b); err != nil {
				return 0, err
			}
			if lo > hi {
				return 0, fmt.Errorf("%s: range %q goes backwards", f.name, rng)
			}
		default:
			var err error
			if lo, err = f.value(rng); err != nil {
				return 0, err
			}
			hi = lo
			if hasStep {
				// n/step = from n to the end
				hi = f.max
			}
		}
		for v := lo; v <= hi; v += step {
			set |= 1 << v
		}
	}
	return set, nil
}

func (f field) value(s string) (int, error) {
	for i, name := range f.names {
		if name != "" && strings.EqualFold(s, name) {
			return i, nil
		}
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("%s: %q isn't a number", f.name, s)
	}
	if n < f.min || n > f.max {
		return 0, fmt.Errorf("%s: %d isn't in %d-%d", f.name, n, f.min, f.max)
	}
	return n, nil
}

// IsZero is the zero Schedule, which never ticks.
func (s Schedule) IsZero() bool { return s.spec == "" }

// String is the schedule as it was parsed.
func (s Schedule) String() string { return s.spec }

// Every is the duration of an @every schedule, 0 for the others.
func (s Schedule) Every() time.Duration { return s.every }

func has(set uint64, v int) bool { return set&(1<<v) != 0 }

func (s Schedule) day(dom int, dow time.Weekday) bool {
	d, w := has(s.dom, dom), has(s.dow, int(dow))
	if s.domStar || s.dowStar {
		return d && w
	}
	return d || w
}

// Next is the first tick after t, in t's location; the zero time for the zero Schedule
// (or none in 8 years, which Parse rejects).
func (s Schedule) Next(t time.Time) time.Time {
	if s.IsZero() {
		return time.Time{}
	}
	if s.every > 0 {
		return t.Truncate(s.every).Add(s.every)
	}
	loc := t.Location()
	y, m, d := t.Date()
	for i := range maxDays {
		// noon exists every day, midnight doesn't in every zone
		noon := time.Date(y, m, d+i, 12, 0, 0, 0, loc)
		yy, mm, dd := noon.Date()
		if !has(s.month, int(mm)) || !s.day(dd, noon.Weekday()) {
			continue
		}
		var best time.Time
		for h := range bits.Len64(s.hour) {
			if !has(s.hour, h) || !time.Date(yy, mm, dd, h, 59, 59, 0, loc).After(t) {
				continue
			}
			for mi := range bits.Len64(s.minute) {
				if !has(s.minute, mi) {
					continue
				}
				c, exists := wall(yy, mm, dd, h, mi, loc)
				if !c.After(t) {
					continue
				}
				if best.IsZero() || c.Before(best) {
					best = c
				}
				// a time that exists comes after the earlier ones of the day; one that
				// doesn't is moved past the change and an existing one may come first
				if exists {
					return best
				}
			}
		}
		if !best.IsZero() {
			return best
		}
	}
	return time.Time{}
}

// wall is the time with that wall clock in loc, false when the clock skips it (going
// forward): then it's that wall clock in the offset before the change, as much after the
// change as it was after its start.
func wall(y int, m time.Month, d, h, mi int, loc *time.Location) (time.Time, bool) {
	c := time.Date(y, m, d, h, mi, 0, 0, loc)
	if c.Hour() == h && c.Minute() == mi {
		return c, true
	}
	utc := time.Date(y, m, d, h, mi, 0, 0, time.UTC)
	_, before := c.Add(-12 * time.Hour).Zone()
	_, after := c.Add(12 * time.Hour).Zone()
	return utc.Add(-time.Duration(min(before, after)) * time.Second).In(loc), false
}

// FromInterval is the schedule closest to "every d" on wall-clock ticks: whole minutes
// or hours dividing an hour or a day (30m → "*/30 * * * *", 4h → "0 */4 * * *"), a
// day ("0 0 * * *"), a week ("0 0 * * 0"), 2 to 15 days ("0 0 */N * *", restarting
// on the 1st), else "@every <d>". "" for d <= 0.
func FromInterval(d time.Duration) string {
	const day = 24 * time.Hour
	switch {
	case d <= 0:
		return ""
	case d <= time.Minute:
		return "* * * * *"
	case d == day:
		return "0 0 * * *"
	case d == 7*day:
		return "0 0 * * 0"
	case d%day == 0 && d/day <= 15:
		return fmt.Sprintf("0 0 */%d * *", d/day)
	case d == time.Hour:
		return "0 * * * *"
	case d%time.Hour == 0 && day%d == 0:
		return fmt.Sprintf("0 */%d * * *", d/time.Hour)
	case d%time.Minute == 0 && time.Hour%d == 0:
		return fmt.Sprintf("*/%d * * * *", d/time.Minute)
	}
	return "@every " + FormatDuration(d)
}

// FormatDuration is d without its zero units: 36h, 90m → 1h30m, 1h30m15s.
func FormatDuration(d time.Duration) string {
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}
