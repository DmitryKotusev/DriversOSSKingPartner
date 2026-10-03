package freenow

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"weekpay/internal/model"
)

var week = model.WeekOf(time.Date(2026, 9, 21, 0, 0, 0, 0, time.Local))

const withVAT = "ID водія,Водій,Усього поїздок,Загальний дохід,Замовлення від Freenow\n" +
	"1014CX,Jan Kowalski,172,4422.56,4224.46\n" +
	"5003AG,Old Driver,0,0.00,0.00\n"

const withoutVAT = "ID водія,Водій,Усього поїздок,Загальний дохід,Замовлення від Freenow\n" +
	"1014CX,Jan Kowalski,172,4000.00,3900.00\n"

func writeZip(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range map[string]string{
		"earnings_2026-09-21_2026-09-27_without_VAT.csv": withoutVAT,
		"earnings_2026-09-21_2026-09-27_with_VAT.csv":    withVAT,
	} {
		w, _ := zw.Create(name)
		_, _ = w.Write([]byte(body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
}

func TestParse(t *testing.T) {
	d, err := Parse(strings.NewReader(withVAT))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Earnings) != 2 || d.Earnings[0] != (model.DriverEarning{Name: "Jan Kowalski", Amount: 422446, Source: model.SourceFreenow}) {
		t.Errorf("earnings = %+v", d.Earnings)
	}
}

func TestFetchZipTakesWithVAT(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "earnings_2026-09-21_2026-09-27 (1).zip")
	writeZip(t, path)

	if got, _ := FindFile(dir, week); got != path {
		t.Errorf("FindFile = %q", got)
	}
	d, err := (CSV{Path: path}).Fetch(context.Background(), week)
	if err != nil {
		t.Fatal(err)
	}
	if d.Earnings[0].Amount != 422446 || len(d.Warnings) != 0 {
		t.Errorf("earnings = %+v, warnings = %v", d.Earnings, d.Warnings)
	}

	other := model.WeekOf(week.Start.AddDate(0, 0, 7))
	if _, err := (CSV{Path: path}).Fetch(context.Background(), other); err == nil {
		t.Error("expected period mismatch error")
	}
}

func TestFetchRejectsWithoutVAT(t *testing.T) {
	path := filepath.Join(t.TempDir(), "earnings_2026-09-21_2026-09-27_without_VAT.csv")
	if err := os.WriteFile(path, []byte(withoutVAT), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (CSV{Path: path}).Fetch(context.Background(), week); err == nil {
		t.Error("expected error for without_VAT file")
	}
	if got, _ := FindFile(filepath.Dir(path), week); got != "" {
		t.Errorf("FindFile must ignore without_VAT, got %q", got)
	}
}

func TestRealSample(t *testing.T) {
	path := "../../../samples/earnings_2026-09-21_2026-09-27.zip"
	if _, err := os.Stat(path); err != nil {
		t.Skip("samples not available")
	}
	d, err := (CSV{Path: path}).Fetch(context.Background(), week)
	if err != nil {
		t.Fatal(err)
	}
	var sum model.Money
	nonZero := 0
	for _, e := range d.Earnings {
		sum += e.Amount
		if e.Amount != 0 {
			nonZero++
		}
	}
	if len(d.Earnings) != 118 || nonZero != 12 || sum != 1402899 {
		t.Errorf("rows=%d nonZero=%d sum=%s", len(d.Earnings), nonZero, sum)
	}
}
