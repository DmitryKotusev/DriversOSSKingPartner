package uber

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"weekpay/internal/model"
)

var week = model.WeekOf(time.Date(2026, 9, 21, 0, 0, 0, 0, time.Local))

const sample = "\xef\xbb\xbfIdentyfikator UUID kierowcy,Imię kierowcy,Nazwisko kierowcy,Wypłacono Ci,Wypłacono Ci : Twój przychód\n" +
	"u1,Jan,Kowalski,1866.31,2000.00\n" +
	"u2,Firm,Account,-126648.63,0.00\n" +
	"u3,Adam,Nowak,,\n"

func TestParse(t *testing.T) {
	d, err := Parse(strings.NewReader(sample))
	if err != nil {
		t.Fatal(err)
	}
	want := []model.DriverEarning{
		{Name: "Jan Kowalski", Amount: 186631, Source: model.SourceUber},
		{Name: "Firm Account", Amount: -12664863, Source: model.SourceUber},
		{Name: "Adam Nowak", Amount: 0, Source: model.SourceUber},
	}
	if len(d.Earnings) != len(want) {
		t.Fatalf("got %+v", d.Earnings)
	}
	for i := range want {
		if d.Earnings[i] != want[i] {
			t.Errorf("row %d = %+v, want %+v", i, d.Earnings[i], want[i])
		}
	}
	if len(d.Raw.Rows) != 3 || d.Raw.Header[0] != "Identyfikator UUID kierowcy" {
		t.Errorf("raw = %+v", d.Raw)
	}
}

func TestParseMissingColumn(t *testing.T) {
	if _, err := Parse(strings.NewReader("Imię kierowcy,Nazwisko kierowcy\nJan,Kowalski\n")); err == nil {
		t.Error("expected error")
	}
}

func TestFetchAndFind(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "20260921_20260928_payments_driver_FIRM.csv")
	other := filepath.Join(dir, "20260914_20260921_payments_driver_FIRM.csv")
	for _, p := range []string{good, other} {
		if err := os.WriteFile(p, []byte(sample), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := FindFile(dir, week); got != good {
		t.Errorf("FindFile = %q", got)
	}
	if _, err := (CSV{Path: good}).Fetch(context.Background(), week); err != nil {
		t.Error(err)
	}
	if _, err := (CSV{Path: other}).Fetch(context.Background(), week); err == nil {
		t.Error("expected period mismatch error")
	}
}

func TestRealSample(t *testing.T) {
	path := "../../../samples/20260921_20260928_payments_driver_THE_KING_SPKA_Z_OGRANICZON_ODPOWIEDZIALNOCI.csv"
	if _, err := os.Stat(path); err != nil {
		t.Skip("samples not available")
	}
	d, err := (CSV{Path: path}).Fetch(context.Background(), week)
	if err != nil {
		t.Fatal(err)
	}
	var sum model.Money
	for _, e := range d.Earnings {
		sum += e.Amount
	}
	if len(d.Earnings) != 79 || sum != -2466 || len(d.Warnings) != 0 {
		t.Errorf("rows=%d sum=%s warnings=%v", len(d.Earnings), sum, d.Warnings)
	}
}
