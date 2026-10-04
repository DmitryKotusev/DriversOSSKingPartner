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

// weekRe finds the ISO week in names like "Zarobki na kierowcę-2026W39-<company>.csv".
var weekRe = regexp.MustCompile(`-(\d{4})W(\d{2})-.*\.csv$`)

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
	if m := weekRe.FindStringSubmatch(name); m != nil {
		year, week := w.Start.ISOWeek()
		if m[1] != strconv.Itoa(year) || m[2] != fmt.Sprintf("%02d", week) {
			return nil, fmt.Errorf("Bolt: файл %s за неделю %sW%s, а нужна %dW%02d (%s)", name, m[1], m[2], year, week, w)
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
	year, week := w.Start.ISOWeek()
	token := fmt.Sprintf("-%dW%02d-", year, week)
	return sources.FindNewest(dir, func(name string) bool {
		return strings.Contains(name, token) && weekRe.MatchString(name)
	})
}

// FindAnyFile returns the newest export in dir for any week, "" if none.
func FindAnyFile(dir string) (string, error) {
	return sources.FindNewest(dir, weekRe.MatchString)
}

// ExpectedName is the export file name for week w.
func ExpectedName(w model.Week) string {
	year, week := w.Start.ISOWeek()
	return fmt.Sprintf("Zarobki na kierowcę-%dW%02d-<фирма>.csv", year, week)
}

// HowToDownload tells where to get the export for week w. The portal's
// "Ostatni tydzień" is only right when w is the week before today.
func HowToDownload(w model.Week) string {
	period := fmt.Sprintf("%s – %s", w.Start.Format("02.01.2006"), w.End.Format("02.01.2006"))
	if w.Start.Equal(model.PreviousWeek(time.Now()).Start) {
		period = "«Ostatni tydzień» (" + period + ")"
	} else {
		period = "неделя " + period
	}
	return "fleets.bolt.eu → Finanse → Zarobki na kierowcę → " + period + " → Pobierz"
}
