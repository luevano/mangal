package cron

import (
	"testing"
	"time"
)

func chicago(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Skip("no tz database:", err)
	}
	return loc
}

func TestParseInvalid(t *testing.T) {
	for _, spec := range []string{
		"", "* * * *", "* * * * * *", "60 * * * *", "* 24 * * *", "* * 0 * *", "* * 32 * *",
		"* * * 13 *", "* * * * 8", "*/0 * * * *", "*/x * * * *", "5-1 * * * *", "a * * * *",
		"1,,2 * * * *", "@often", "@every", "@every 10s", "@every x", "0 0 30 2 *", "0 0 31 4,6 *",
		"* * * foo *", "-1 * * * *",
	} {
		if _, err := Parse(spec); err == nil {
			t.Errorf("Parse(%q) = nil error", spec)
		}
	}
}

func TestParseFields(t *testing.T) {
	bitsOf := func(vs ...int) uint64 {
		var s uint64
		for _, v := range vs {
			s |= 1 << v
		}
		return s
	}
	for _, c := range []struct {
		spec                     string
		minute, hour, dom, month uint64
		dow                      uint64
	}{
		{"0 */4 * * *", bitsOf(0), bitsOf(0, 4, 8, 12, 16, 20), 0xfffffffe, 0x1ffe, 0x7f},
		{"5,10-12 1-5/2 1 jan,MAR-may mon-fri", bitsOf(5, 10, 11, 12), bitsOf(1, 3, 5), bitsOf(1), bitsOf(1, 3, 4, 5), bitsOf(1, 2, 3, 4, 5)},
		{"30/10 22/1 15 */6 7", bitsOf(30, 40, 50), bitsOf(22, 23), bitsOf(15), bitsOf(1, 7), bitsOf(0)},
		{"@hourly", bitsOf(0), 0xffffff, 0xfffffffe, 0x1ffe, 0x7f},
		{"@weekly", bitsOf(0), bitsOf(0), 0xfffffffe, 0x1ffe, bitsOf(0)},
		{"@daily", bitsOf(0), bitsOf(0), 0xfffffffe, 0x1ffe, 0x7f},
		{"@monthly", bitsOf(0), bitsOf(0), bitsOf(1), 0x1ffe, 0x7f},
		{"@yearly", bitsOf(0), bitsOf(0), bitsOf(1), bitsOf(1), 0x7f},
	} {
		s, err := Parse(c.spec)
		if err != nil {
			t.Errorf("Parse(%q): %v", c.spec, err)
			continue
		}
		if s.minute != c.minute || s.hour != c.hour || s.dom != c.dom || s.month != c.month || s.dow != c.dow {
			t.Errorf("Parse(%q) = %b %b %b %b %b", c.spec, s.minute, s.hour, s.dom, s.month, s.dow)
		}
		if s.String() != c.spec {
			t.Errorf("String() = %q, want %q", s.String(), c.spec)
		}
	}
	if s, err := Parse("@every 90m"); err != nil || s.Every() != 90*time.Minute {
		t.Errorf("@every 90m = %v, %v", s.Every(), err)
	}
}

func TestNext(t *testing.T) {
	utc := func(s string) time.Time {
		v, err := time.Parse("2006-01-02 15:04:05", s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	for _, c := range []struct{ spec, from, want string }{
		{"0 */4 * * *", "2026-10-11 13:59:00", "2026-10-11 16:00:00"},
		{"0 */4 * * *", "2026-10-11 16:00:00", "2026-10-11 20:00:00"},
		{"0 */4 * * *", "2026-10-11 15:59:59", "2026-10-11 16:00:00"},
		{"0 */4 * * *", "2026-10-11 21:00:00", "2026-10-12 00:00:00"},
		{"*/2 * * * *", "2026-10-11 10:01:30", "2026-10-11 10:02:00"},
		{"30 2 1 * *", "2026-12-15 00:00:00", "2027-01-01 02:30:00"},
		{"0 0 29 2 *", "2026-03-01 00:00:00", "2028-02-29 00:00:00"},
		// either day field when both are set: the 13th or a friday
		{"0 0 13 * 5", "2026-10-11 00:00:00", "2026-10-13 00:00:00"},
		{"0 0 13 * 5", "2026-10-13 00:00:00", "2026-10-16 00:00:00"},
		// both when one starts with *
		{"0 0 */2 * 5", "2026-10-11 00:00:00", "2026-10-23 00:00:00"},
		{"0 9 * * mon-fri", "2026-10-09 10:00:00", "2026-10-12 09:00:00"},
		{"@weekly", "2026-10-11 00:00:00", "2026-10-18 00:00:00"},
		{"0 0 * * 7", "2026-10-11 00:00:00", "2026-10-18 00:00:00"},
		{"@every 90m", "2026-10-11 00:10:00", "2026-10-11 01:30:00"},
		{"@every 4h", "2026-10-11 05:00:00", "2026-10-11 08:00:00"},
	} {
		got := MustParse(c.spec).Next(utc(c.from))
		if !got.Equal(utc(c.want)) {
			t.Errorf("%q after %s = %s, want %s", c.spec, c.from, got, c.want)
		}
	}
	if !(Schedule{}).Next(time.Now()).IsZero() {
		t.Error("the zero schedule ticks")
	}
}

func TestNextDST(t *testing.T) {
	loc := chicago(t)
	at := func(y int, m time.Month, d, h, min int) time.Time { return time.Date(y, m, d, h, min, 0, 0, loc) }
	every4 := MustParse("0 */4 * * *")

	// 2026-03-08 02:00 CST → 03:00 CDT: the wall-clock ticks stay on 0, 4, 8...
	ticks := []time.Time{at(2026, 3, 8, 0, 0), at(2026, 3, 8, 4, 0), at(2026, 3, 8, 8, 0)}
	from := at(2026, 3, 7, 23, 0)
	for _, want := range ticks {
		got := every4.Next(from)
		if !got.Equal(want) || got.Hour() != want.Hour() {
			t.Errorf("spring: after %s = %s, want %s", from, got, want)
		}
		from = got
	}
	if d := ticks[1].Sub(ticks[0]); d != 3*time.Hour {
		t.Errorf("spring: 00:00 to 04:00 = %s", d)
	}

	// a skipped time ticks right after the change, once
	got := MustParse("30 2 * * *").Next(at(2026, 3, 8, 1, 0))
	if want := time.Date(2026, 3, 8, 8, 30, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("skipped 02:30 = %s, want %s", got, want)
	}
	if next := MustParse("30 2 * * *").Next(got); !next.Equal(at(2026, 3, 9, 2, 30)) {
		t.Errorf("after the skipped 02:30 = %s", next)
	}

	// 2026-11-01 02:00 CDT → 01:00 CST: 01:30 happens twice, ticks once
	half := MustParse("30 1 * * *")
	first := half.Next(at(2026, 11, 1, 0, 0))
	if first.Hour() != 1 || first.Minute() != 30 || first.Day() != 1 {
		t.Fatalf("fall: 01:30 = %s", first)
	}
	if next := half.Next(first); next.Day() != 2 || next.Hour() != 1 {
		t.Errorf("fall: after %s = %s, want the next day's", first, next)
	}
	// from inside the repeated hour (the second 01:10), the next tick is still ahead
	second := time.Date(2026, 11, 1, 7, 10, 0, 0, time.UTC).In(loc)
	if next := every4.Next(second); !next.Equal(at(2026, 11, 1, 4, 0)) {
		t.Errorf("fall: after %s = %s", second, next)
	}
	if d := at(2026, 11, 1, 4, 0).Sub(at(2026, 11, 1, 0, 0)); d != 5*time.Hour {
		t.Errorf("fall: 00:00 to 04:00 = %s", d)
	}
}

func TestFromInterval(t *testing.T) {
	for d, want := range map[time.Duration]string{
		0:                   "",
		-time.Hour:          "",
		30 * time.Second:    "* * * * *",
		time.Minute:         "* * * * *",
		15 * time.Minute:    "*/15 * * * *",
		45 * time.Minute:    "@every 45m",
		90 * time.Minute:    "@every 1h30m",
		time.Hour:           "0 * * * *",
		4 * time.Hour:       "0 */4 * * *",
		12 * time.Hour:      "0 */12 * * *",
		5 * time.Hour:       "@every 5h",
		24 * time.Hour:      "0 0 * * *",
		36 * time.Hour:      "@every 36h",
		48 * time.Hour:      "0 0 */2 * *",
		7 * 24 * time.Hour:  "0 0 * * 0",
		30 * 24 * time.Hour: "@every 720h",
	} {
		got := FromInterval(d)
		if got != want {
			t.Errorf("FromInterval(%s) = %q, want %q", d, got, want)
		}
		if got != "" {
			if _, err := Parse(got); err != nil {
				t.Errorf("FromInterval(%s) = %q: %v", d, got, err)
			}
		}
	}
}
