// Package freenow reads the FREE NOW portal earnings export.
package freenow

import (
	"archive/zip"
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

// Columns of the earnings CSV.
const (
	colDriver = "Водій"
	colOrders = "Замовлення від Freenow"
)

// fileRe matches "earnings_<from>_<to>.zip" and the "_with_VAT.csv" file
// inside it, with an optional browser duplicate suffix like " (1)".
var fileRe = regexp.MustCompile(`^earnings_(\d{4}-\d{2}-\d{2})_(\d{4}-\d{2}-\d{2})(( \(\d+\))?\.zip|_with_VAT( \(\d+\))?\.csv)$`)

// CSV is the FREE NOW source backed by the downloaded zip or its
// "_with_VAT.csv" file. Amounts are taken with VAT.
type CSV struct{ Path string }

func (CSV) Name() string { return model.SourceFreenow }

func (c CSV) Fetch(_ context.Context, w model.Week) (*sources.Data, error) {
	name := filepath.Base(c.Path)
	if strings.Contains(strings.ToLower(name), "without_vat") {
		return nil, fmt.Errorf("FREE NOW: файл %s — суммы без ПДВ, нужен файл *_with_VAT.csv или весь zip", name)
	}

	var (
		d   *sources.Data
		err error
	)
	if strings.EqualFold(filepath.Ext(c.Path), ".zip") {
		d, err = parseZip(c.Path)
	} else {
		var f *os.File
		if f, err = os.Open(c.Path); err == nil {
			d, err = Parse(f)
			f.Close()
		}
	}
	if err != nil {
		return nil, fmt.Errorf("FREE NOW, файл %s: %w", c.Path, err)
	}
	d.Origin = c.Path

	if m := fileRe.FindStringSubmatch(name); m != nil {
		if m[1] != w.Start.Format("2006-01-02") || m[2] != w.End.Format("2006-01-02") {
			return nil, fmt.Errorf("FREE NOW: файл %s за период %s – %s, а нужна неделя %s", name, m[1], m[2], w)
		}
	} else {
		d.Warnings = append(d.Warnings, fmt.Sprintf("FREE NOW: по имени файла %s не удалось проверить период отчёта", name))
	}
	return d, nil
}

func parseZip(path string) (*sources.Data, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	for _, zf := range zr.File {
		if strings.HasSuffix(strings.ToLower(zf.Name), "_with_vat.csv") {
			rc, err := zf.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return Parse(rc)
		}
	}
	return nil, fmt.Errorf("в архиве нет файла *_with_VAT.csv")
}

// Parse reads the earnings CSV; the driver's amount is "Замовлення від Freenow".
func Parse(r io.Reader) (*sources.Data, error) {
	t, err := sources.ReadCSV(r)
	if err != nil {
		return nil, err
	}
	cols, err := sources.Columns(t, colDriver, colOrders)
	if err != nil {
		return nil, err
	}
	d := &sources.Data{Raw: t}
	for i, row := range t.Rows {
		name := strings.TrimSpace(row[cols[0]])
		amount, err := model.ParseMoney(row[cols[1]])
		if err != nil {
			return nil, fmt.Errorf("строка %d (%s): %w", i+2, name, err)
		}
		d.Earnings = append(d.Earnings, model.DriverEarning{Name: name, Amount: amount, Source: model.SourceFreenow})
	}
	return d, nil
}

// FindFile looks for the newest export for week w in dir. Returns "" if none.
func FindFile(dir string, w model.Week) (string, error) {
	prefix := "earnings_" + w.Start.Format("2006-01-02") + "_" + w.End.Format("2006-01-02")
	return sources.FindNewest(dir, func(name string) bool {
		return strings.HasPrefix(name, prefix) && fileRe.MatchString(name)
	})
}

// FindAnyFile returns the newest export in dir for any period, "" if none.
func FindAnyFile(dir string) (string, error) {
	return sources.FindNewest(dir, fileRe.MatchString)
}

// ExpectedName is the export file name for week w.
func ExpectedName(w model.Week) string {
	return "earnings_" + w.Start.Format("2006-01-02") + "_" + w.End.Format("2006-01-02") + ".zip"
}

// HowToDownload tells where to get the export for week w.
func HowToDownload(w model.Week) string {
	return fmt.Sprintf("portal.free-now.com → Earnings → период %s – %s → скачать (zip)",
		w.Start.Format("02.01.2006"), w.End.Format("02.01.2006"))
}
