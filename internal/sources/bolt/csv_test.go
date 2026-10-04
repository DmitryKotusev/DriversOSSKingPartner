package bolt

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

const sample = "\xef\xbb\xbf\"Kierowca\",\"Numer telefonu\",\"Pobrana gotówka|ZŁ\",\"Zarobki netto|ZŁ\",\"Przewidywana wypłata|ZŁ\"\n" +
	"\"JAN KOWALSKI\",\"+48111222333\",\"574.80\",\"4589.05\",\"4014.25\"\n" +
	"\"Adam Nowak\",\"+48111222334\",\"83.90\",\"68.65\",\"-15.25\"\n" +
	"\"Piotr Zielinski\",\"+48111222335\",\"0.00\",\"-6.77\",\"-6.00\"\n"

func TestParse(t *testing.T) {
	d, err := Parse(strings.NewReader(sample))
	if err != nil {
		t.Fatal(err)
	}
	want := []model.Money{401425, -1525, -677}
	for i, m := range want {
		if d.Earnings[i].Amount != m {
			t.Errorf("row %d amount = %s, want %s", i, d.Earnings[i].Amount, m)
		}
	}
	// Control column mismatch for the 3rd driver only.
	if len(d.Warnings) != 1 || !strings.Contains(d.Warnings[0], "Piotr Zielinski") {
		t.Errorf("warnings = %v", d.Warnings)
	}
	if h := d.Raw.Header; h[len(h)-1] != ColTotal || d.Raw.Rows[1][len(h)-1] != "-15.25" {
		t.Errorf("raw = %+v", d.Raw)
	}
}

func TestParseWrongCurrency(t *testing.T) {
	csv := "Kierowca,Pobrana gotówka|EUR,Zarobki netto|EUR\nJan,1.00,2.00\n"
	if _, err := Parse(strings.NewReader(csv)); err == nil || !strings.Contains(err.Error(), "PLN") {
		t.Errorf("expected currency error, got %v", err)
	}
}

func TestFetchAndFind(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "Zarobki na kierowcę-2026W39-Firm.csv")
	other := filepath.Join(dir, "Zarobki na kierowcę-2026W38-Firm.csv")
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
		t.Error("expected week mismatch error")
	}
}

func TestFilePeriod(t *testing.T) {
	cases := map[string]string{
		"Zarobki na kierowcę-2026W39-The King Sp_ z o_o_.csv":                 "2026-09-21 2026-09-27",
		"Zarobki na kierowcę-2026W01-Firm (1).csv":                            "2025-12-29 2026-01-04",
		"Zarobki_na_kierowcę_14_wrz_2026_20_wrz_2026_The_King_Sp_z_o_o_.csv":  "2026-09-14 2026-09-20",
		"Zarobki na kierowcę-14 wrz 2026-20 wrz 2026-The King Sp_ z o_o_.csv": "2026-09-14 2026-09-20",
		"Zarobki na kierowcę-14 wrz 2026-20 wrz 2026-Firm (1).csv":            "2026-09-14 2026-09-20",
		"Zarobki_na_kierowcę_2026W39_Firm.csv":                                "2026-09-21 2026-09-27",
		"Zarobki_na_kierowcę_29_wrz_2026_5_paź_2026_Firm (1).csv":             "2026-09-29 2026-10-05",
		"Zarobki_na_kierowcę_28_gru_2026_3_sty_2027.csv":                      "2026-12-28 2027-01-03",
		"Zarobki_na_kierowcę_14_xyz_2026_20_wrz_2026_Firm.csv":                "",
		"report.csv": "",
	}
	for name, want := range cases {
		start, end, ok := filePeriod(name)
		got := ""
		if ok {
			got = start.Format("2006-01-02") + " " + end.Format("2006-01-02")
		}
		if got != want {
			t.Errorf("filePeriod(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestFindDateRangeFile(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "Zarobki na kierowcę-21 wrz 2026-27 wrz 2026-Firm.csv")
	longer := filepath.Join(dir, "Zarobki_na_kierowcę_21_wrz_2026_28_wrz_2026_Firm.csv")
	for _, p := range []string{good, longer} {
		if err := os.WriteFile(p, []byte(sample), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := FindFile(dir, week); got != good {
		t.Errorf("FindFile = %q", got)
	}
	if _, err := (CSV{Path: longer}).Fetch(context.Background(), week); err == nil {
		t.Error("expected period mismatch error")
	}
}

func TestRealSample(t *testing.T) {
	path := "../../../samples/Zarobki na kierowcę-2026W39-The King Sp_ z o_o_.csv"
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
	// "Przewidywana wypłata" matches the formula for every driver.
	if len(d.Earnings) != 73 || sum != 9949936 || len(d.Warnings) != 0 {
		t.Errorf("rows=%d sum=%s warnings=%v", len(d.Earnings), sum, d.Warnings)
	}
}
