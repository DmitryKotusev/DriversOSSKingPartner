// Package uber reads the Fleet Hub "Payments (driver)" report.
package uber

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"weekpay/internal/model"
	"weekpay/internal/sources"
)

// Columns of the "Płatności (kierowca)" CSV report.
const (
	colFirstName = "Imię kierowcy"
	colLastName  = "Nazwisko kierowcy"
	colPaid      = "Wypłacono Ci"
)

// fileRe matches e.g. "20260921_20260928_payments_driver_<company>.csv".
var fileRe = regexp.MustCompile(`^(\d{8})_(\d{8})_payments_driver_.*\.csv$`)

// CSV is the Uber source backed by a downloaded report file.
type CSV struct{ Path string }

func (CSV) Name() string { return model.SourceUber }

// Fetch parses the file and checks that its period is the requested week:
// Uber's payment period runs from Monday ~04:00 to next Monday ~04:00.
func (c CSV) Fetch(_ context.Context, w model.Week) (*sources.Data, error) {
	f, err := os.Open(c.Path)
	if err != nil {
		return nil, fmt.Errorf("Uber: %w", err)
	}
	defer f.Close()
	d, err := Parse(f)
	if err != nil {
		return nil, fmt.Errorf("Uber, файл %s: %w", c.Path, err)
	}
	d.Origin = c.Path

	name := filepath.Base(c.Path)
	if m := fileRe.FindStringSubmatch(name); m != nil {
		wantStart, wantEnd := w.Start.Format("20060102"), w.Start.AddDate(0, 0, 7).Format("20060102")
		if m[1] != wantStart || m[2] != wantEnd {
			return nil, fmt.Errorf("Uber: файл %s за период %s–%s, а нужна неделя %s", name, m[1], m[2], w)
		}
	} else {
		d.Warnings = append(d.Warnings, fmt.Sprintf("Uber: по имени файла %s не удалось проверить период отчёта", name))
	}
	return d, nil
}

// Parse reads the report; the driver's amount is "Wypłacono Ci".
func Parse(r io.Reader) (*sources.Data, error) {
	t, err := sources.ReadCSV(r)
	if err != nil {
		return nil, err
	}
	cols, err := sources.Columns(t, colFirstName, colLastName, colPaid)
	if err != nil {
		return nil, err
	}
	d := &sources.Data{Raw: t}
	for i, row := range t.Rows {
		name := strings.TrimSpace(row[cols[0]] + " " + row[cols[1]])
		amount, err := model.ParseMoney(row[cols[2]])
		if err != nil {
			return nil, fmt.Errorf("строка %d (%s): %w", i+2, name, err)
		}
		d.Earnings = append(d.Earnings, model.DriverEarning{Name: name, Amount: amount, Source: model.SourceUber})
	}
	return d, nil
}

// FindFile looks for the newest report for week w in dir. Returns "" if none.
func FindFile(dir string, w model.Week) (string, error) {
	prefix := w.Start.Format("20060102") + "_" + w.Start.AddDate(0, 0, 7).Format("20060102") + "_"
	return sources.FindNewest(dir, func(name string) bool {
		return strings.HasPrefix(name, prefix) && fileRe.MatchString(name)
	})
}
