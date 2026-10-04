// Package bolt reads the Bolt Fleet "Zarobki na kierowcę" export.
package bolt

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"weekpay/internal/model"
	"weekpay/internal/sources"
)

// Columns of the export; the part after "|" is the currency.
const (
	colDriver   = "Kierowca"
	colNet      = "Zarobki netto"
	colCash     = "Pobrana gotówka"
	colExpected = "Przewidywana wypłata"
	currency    = "ZŁ"
)

// ColTotal is appended to the raw table: the driver's amount.
const ColTotal = "Итог (Zarobki netto − Pobrana gotówka)"

// The export's file name tells its period. "Ostatni tydzień" gives the ISO
// week: "Zarobki na kierowcę-2026W39-<company>.csv"; a custom period gives
// dates with Polish month abbreviations:
// "Zarobki na kierowcę-14 wrz 2026-20 wrz 2026-<company>.csv".
// Copying the file between computers may turn spaces and hyphens into
// underscores, so any of the three is accepted as a separator.
var (
	weekRe  = regexp.MustCompile(`[-_ ](\d{4})W(\d{2})[-_ ].*\.csv$`)
	rangeRe = regexp.MustCompile(`^Zarobki[-_ ]na[-_ ]kierowc[eę][-_ ]+(\d{1,2})[-_ ](\pL+)[-_ ](\d{4})[-_ ]+(\d{1,2})[-_ ](\pL+)[-_ ](\d{4})(?:[-_ ].*)?\.csv$`)
)

// months are the Polish month abbreviations used in file names, January first.
var months = []string{"sty", "lut", "mar", "kwi", "maj", "cze", "lip", "sie", "wrz", "paź", "lis", "gru"}

// CSV is the Bolt source backed by a downloaded export file.
type CSV struct{ Path string }

func (CSV) Name() string { return model.SourceBolt }

func (c CSV) Fetch(_ context.Context, w model.Week) (*sources.Data, error) {
	f, err := os.Open(c.Path)
	if err != nil {
		return nil, fmt.Errorf("Bolt: %w", err)
	}
	defer f.Close()
	d, err := Parse(f)
	if err != nil {
		return nil, fmt.Errorf("Bolt, файл %s: %w", c.Path, err)
	}
	d.Origin = c.Path

	name := filepath.Base(c.Path)
	if start, end, ok := filePeriod(name); ok {
		if !isWeek(start, end, w) {
			return nil, fmt.Errorf("Bolt: файл %s за период %s – %s, а нужна неделя %s",
				name, start.Format("02.01.2006"), end.Format("02.01.2006"), w)
		}
	} else {
		d.Warnings = append(d.Warnings, fmt.Sprintf("Bolt: по имени файла %s не удалось проверить неделю отчёта", name))
	}
	return d, nil
}

// Parse reads the export. The driver's amount is
// "Zarobki netto" − "Pobrana gotówka"; "Przewidywana wypłata" is only a check.
func Parse(r io.Reader) (*sources.Data, error) {
	t, err := sources.ReadCSV(r)
	if err != nil {
		return nil, err
	}
	driver, err := sources.Columns(t, colDriver)
	if err != nil {
		return nil, err
	}
	net, err := moneyColumn(t, colNet)
	if err != nil {
		return nil, err
	}
	cash, err := moneyColumn(t, colCash)
	if err != nil {
		return nil, err
	}
	expected, err := moneyColumn(t, colExpected)
	if err != nil {
		expected = -1 // control column only
	}

	d := &sources.Data{}
	raw := model.Table{Header: append(append([]string{}, t.Header...), ColTotal)}
	for i, row := range t.Rows {
		name := strings.TrimSpace(row[driver[0]])
		line := fmt.Sprintf("строка %d (%s)", i+2, name)
		n, err := model.ParseMoney(row[net])
		if err != nil {
			return nil, fmt.Errorf("%s: %w", line, err)
		}
		c, err := model.ParseMoney(row[cash])
		if err != nil {
			return nil, fmt.Errorf("%s: %w", line, err)
		}
		amount := n - c
		if expected >= 0 {
			if e, err := model.ParseMoney(row[expected]); err == nil && e != amount {
				d.Warnings = append(d.Warnings, fmt.Sprintf(
					"Bolt: у водителя «%s» Zarobki netto − Pobrana gotówka = %s, а Przewidywana wypłata = %s",
					name, amount, e))
			}
		}
		d.Earnings = append(d.Earnings, model.DriverEarning{Name: name, Amount: amount, Source: model.SourceBolt})
		raw.Rows = append(raw.Rows, append(append([]string{}, row...), amount.String()))
	}
	d.Raw = raw
	return d, nil
}

// moneyColumn finds a "<name>|<currency>" column and checks the currency is PLN.
func moneyColumn(t model.Table, name string) (int, error) {
	for i, h := range t.Header {
		base, cur, ok := strings.Cut(h, "|")
		if strings.TrimSpace(base) != name {
			continue
		}
		if !ok || strings.TrimSpace(cur) != currency {
			return -1, fmt.Errorf("колонка «%s»: ожидалась валюта %s (PLN), в файле «%s»", name, currency, h)
		}
		return i, nil
	}
	return -1, fmt.Errorf("в файле нет колонки «%s|%s» — возможно, выгружен не тот отчёт или изменился формат", name, currency)
}

// FindFile looks for the newest export for week w in dir. Returns "" if none.
func FindFile(dir string, w model.Week) (string, error) {
	return sources.FindNewest(dir, func(name string) bool {
		start, end, ok := filePeriod(name)
		return ok && isWeek(start, end, w)
	})
}

// FindAnyFile returns the newest export in dir for any period, "" if none.
func FindAnyFile(dir string) (string, error) {
	return sources.FindNewest(dir, func(name string) bool {
		_, _, ok := filePeriod(name)
		return ok
	})
}

// filePeriod reads the first and last day of the export from its file name.
func filePeriod(name string) (start, end time.Time, ok bool) {
	if m := weekRe.FindStringSubmatch(name); m != nil {
		year, _ := strconv.Atoi(m[1])
		week, _ := strconv.Atoi(m[2])
		// January 4th is always in ISO week 1.
		start = model.WeekOf(time.Date(year, 1, 4, 0, 0, 0, 0, time.Local)).Start.AddDate(0, 0, 7*(week-1))
		return start, start.AddDate(0, 0, 6), true
	}
	if m := rangeRe.FindStringSubmatch(name); m != nil {
		var err1, err2 error
		start, err1 = polishDate(m[1], m[2], m[3])
		end, err2 = polishDate(m[4], m[5], m[6])
		return start, end, err1 == nil && err2 == nil
	}
	return time.Time{}, time.Time{}, false
}

func polishDate(day, month, year string) (time.Time, error) {
	month = strings.ToLower(month)
	if month == "paz" {
		month = "paź"
	}
	for i, m := range months {
		if m == month {
			return time.ParseInLocation("2-1-2006", day+"-"+strconv.Itoa(i+1)+"-"+year, time.Local)
		}
	}
	return time.Time{}, fmt.Errorf("неизвестный месяц %q", month)
}

func isWeek(start, end time.Time, w model.Week) bool {
	const d = "2006-01-02"
	return start.Format(d) == w.Start.Format(d) && end.Format(d) == w.End.Format(d)
}

// isLastWeek reports whether w is the portal's "Ostatni tydzień".
func isLastWeek(w model.Week) bool {
	return w.Start.Equal(model.PreviousWeek(time.Now()).Start)
}

// ExpectedName is the export file name for week w.
func ExpectedName(w model.Week) string {
	if isLastWeek(w) {
		year, week := w.Start.ISOWeek()
		return fmt.Sprintf("Zarobki na kierowcę-%dW%02d-<фирма>.csv", year, week)
	}
	date := func(t time.Time) string {
		return fmt.Sprintf("%d %s %d", t.Day(), months[t.Month()-1], t.Year())
	}
	return "Zarobki na kierowcę-" + date(w.Start) + "-" + date(w.End) + "-<фирма>.csv"
}

// HowToDownload tells where to get the export for week w. The portal's
// "Ostatni tydzień" is only right when w is the week before today.
func HowToDownload(w model.Week) string {
	period := fmt.Sprintf("%s – %s", w.Start.Format("02.01.2006"), w.End.Format("02.01.2006"))
	if isLastWeek(w) {
		period = "«Ostatni tydzień» (" + period + ")"
	} else {
		period = "период " + period
	}
	return "fleets.bolt.eu → Finanse → Zarobki na kierowcę → " + period + " → Pobierz"
}
