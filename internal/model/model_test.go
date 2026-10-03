package model

import (
	"testing"
	"time"
)

func TestParseMoney(t *testing.T) {
	cases := []struct {
		in   string
		want Money
	}{
		{"1234.56", 123456},
		{"-6.77", -677},
		{"-126648.63", -12664863},
		{"0.00", 0},
		{"", 0},
		{"230", 23000},
		{"1.5", 150},
		{"1 234,50", 123450},
		{"+12.30", 1230},
	}
	for _, c := range cases {
		got, err := ParseMoney(c.in)
		if err != nil {
			t.Errorf("ParseMoney(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseMoney(%q) = %d, want %d", c.in, got, c.want)
		}
	}
	for _, bad := range []string{"abc", "1.234", "1.2.3", "1,234.5", "-", ".5", "12."} {
		if _, err := ParseMoney(bad); err == nil {
			t.Errorf("ParseMoney(%q): expected error", bad)
		}
	}
}

func TestMoneyString(t *testing.T) {
	for m, want := range map[Money]string{0: "0.00", 5: "0.05", -677: "-6.77", 123456: "1234.56", -5: "-0.05"} {
		if got := m.String(); got != want {
			t.Errorf("Money(%d).String() = %q, want %q", m, got, want)
		}
	}
}

func TestWeeks(t *testing.T) {
	d := func(s string) time.Time {
		v, _ := time.ParseInLocation("2006-01-02", s, time.Local)
		return v
	}
	// Run on Monday 2026-09-28 → previous week 21–27.09.
	for _, now := range []string{"2026-09-28", "2026-09-30", "2026-10-04"} {
		w := PreviousWeek(d(now).Add(5 * time.Hour))
		if !w.Start.Equal(d("2026-09-21")) || !w.End.Equal(d("2026-09-27")) {
			t.Errorf("PreviousWeek(%s) = %s", now, w)
		}
	}
	w, err := ParseWeek("2026-09-24")
	if err != nil || !w.Start.Equal(d("2026-09-21")) {
		t.Errorf("ParseWeek = %s, %v", w, err)
	}
	if _, wk := w.Start.ISOWeek(); wk != 39 {
		t.Errorf("ISO week = %d, want 39", wk)
	}
}
