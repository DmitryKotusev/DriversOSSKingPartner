package model

import (
	"fmt"
	"time"
)

// Source names, also used as report column and sheet names.
const (
	SourceUber    = "Uber"
	SourceFreenow = "Freenow"
	SourceBolt    = "Bolt"
)

// Sources lists all sources in report column order.
var Sources = []string{SourceUber, SourceFreenow, SourceBolt}

// DriverEarning is one driver's weekly amount from one source.
type DriverEarning struct {
	Name   string
	Amount Money
	Source string
}

// Table is a source file as it was downloaded: header plus data rows.
type Table struct {
	Header []string
	Rows   [][]string
}

// Week is a Monday–Sunday week. Start and End are dates (midnight, local time).
type Week struct {
	Start time.Time
	End   time.Time
}

// WeekOf returns the Monday–Sunday week containing the given day.
func WeekOf(day time.Time) Week {
	d := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
	offset := (int(d.Weekday()) + 6) % 7 // Monday = 0
	start := d.AddDate(0, 0, -offset)
	return Week{Start: start, End: start.AddDate(0, 0, 6)}
}

// PreviousWeek returns the full week before the week containing now.
func PreviousWeek(now time.Time) Week {
	return WeekOf(WeekOf(now).Start.AddDate(0, 0, -7))
}

// ParseWeek parses a YYYY-MM-DD date and returns the week containing it.
func ParseWeek(s string) (Week, error) {
	d, err := time.ParseInLocation("2006-01-02", s, time.Local)
	if err != nil {
		return Week{}, fmt.Errorf("неверная дата недели %q, нужен формат ГГГГ-ММ-ДД", s)
	}
	return WeekOf(d), nil
}

func (w Week) String() string {
	return w.Start.Format("2006-01-02") + " – " + w.End.Format("2006-01-02")
}
