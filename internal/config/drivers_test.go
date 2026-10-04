package config

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	"weekpay/internal/model"
)

func TestLoadDriversMissing(t *testing.T) {
	d, found, _, err := LoadDrivers(filepath.Join(t.TempDir(), "nope.xlsx"))
	if err != nil || found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if d.Amount(model.PartnerFee, "Anyone") != 0 || d.Amount(model.CarRent, "Anyone") != 0 || d.Excluded("Anyone") {
		t.Error("missing file must give zeros")
	}
}

func TestLoadDrivers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "drivers.xlsx")
	if err := WriteDriversTemplate(path, 23000); err != nil {
		t.Fatal(err)
	}
	if err := WriteDriversTemplate(path, 23000); err == nil {
		t.Error("template must not overwrite an existing file")
	}

	f, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sheet := f.GetSheetName(0)
	_ = f.SetSheetRow(sheet, "A3", &[]any{"Jan Kowalski", 150, 600})
	_ = f.SetSheetRow(sheet, "A4", &[]any{"  adam   NOWAK ", nil, 500.5})
	_ = f.SetSheetRow(sheet, "A5", &[]any{"Firm Account", nil, nil, nil, nil, nil, nil, "да"})
	_ = f.SetSheetRow(sheet, "A6", &[]any{"Piotr Zielinski", 150.3, nil, nil, nil, nil, nil, "нет"})
	if err := f.Save(); err != nil {
		t.Fatal(err)
	}
	f.Close()

	d, found, warnings, err := LoadDrivers(path)
	if err != nil || !found || len(warnings) != 0 {
		t.Fatalf("found=%v warnings=%v err=%v", found, warnings, err)
	}
	checks := []struct {
		name      string
		fee, rent int64
		excluded  bool
	}{
		{"Jan Kowalski", 15000, 60000, false},
		{"Adam Nowak", 23000, 50050, false}, // empty fee → default
		{"FIRM ACCOUNT", 23000, 0, true},
		{"Piotr Zielinski", 15030, 0, false},
		{"Somebody Else", 23000, 0, false},
	}
	for _, c := range checks {
		if int64(d.Amount(model.PartnerFee, c.name)) != c.fee || int64(d.Amount(model.CarRent, c.name)) != c.rent || d.Excluded(c.name) != c.excluded {
			t.Errorf("%s: fee=%v rent=%v excluded=%v", c.name, d.Amount(model.PartnerFee, c.name), d.Amount(model.CarRent, c.name), d.Excluded(c.name))
		}
	}
}

func TestLoadDriversBadAmount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "drivers.xlsx")
	f := excelize.NewFile()
	_ = f.SetSheetRow("Sheet1", "A1", &[]any{"Водитель", "Партнёрский сбор"})
	_ = f.SetSheetRow("Sheet1", "A2", &[]any{"Jan Kowalski", "сто"})
	if err := f.SaveAs(path); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := LoadDrivers(path); err == nil {
		t.Error("expected error for non-numeric fee")
	}
}

func writeDrivers(t *testing.T, rows ...[]any) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "drivers.xlsx")
	f := excelize.NewFile()
	_ = f.SetSheetRow("Sheet1", "A1", &[]any{HeaderName, HeaderFee, HeaderRent, HeaderExclude, HeaderAliases})
	for i, r := range rows {
		_ = f.SetSheetRow("Sheet1", "A"+strconv.Itoa(i+2), &r)
	}
	if err := f.SaveAs(path); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAliases(t *testing.T) {
	path := writeDrivers(t,
		[]any{"Amal  Abasov", 150, nil, nil, "AMAL ABBASOV; Amal Abassov\nAmal Abasov"},
		[]any{"Firm Account", nil, nil, "да", "Firm Acc"},
		[]any{"Jan Kowalski"},
	)
	d, _, warnings, err := LoadDrivers(path)
	if err != nil || len(warnings) != 0 {
		t.Fatalf("warnings=%v err=%v", warnings, err)
	}
	for in, want := range map[string]string{
		"amal abbasov":  "Amal Abasov",
		"Amal Abassov":  "Amal Abasov",
		"Amal Abasov":   "Amal Abasov", // own name, unchanged
		"FIRM ACC":      "Firm Account",
		"Jan Kowalski":  "Jan Kowalski",
		"Somebody Else": "Somebody Else",
	} {
		if got := d.Canonical(in); got != want {
			t.Errorf("Canonical(%q) = %q, want %q", in, got, want)
		}
	}
	if d.Amount(model.PartnerFee, d.Canonical("Amal Abbasov")) != 15000 || !d.Excluded(d.Canonical("firm acc")) {
		t.Error("fee/exclusion must apply through the main name")
	}
}

func TestAliasConflicts(t *testing.T) {
	cases := map[string][][]any{
		"alias of two drivers": {
			{"Jan Kowalski", nil, nil, nil, "J Kowalski"},
			{"Jakub Kowalski", nil, nil, nil, "j kowalski"},
		},
		"alias is another driver": {
			{"Jan Kowalski", nil, nil, nil, "Adam Nowak"},
			{"Adam Nowak"},
		},
	}
	for name, rows := range cases {
		if _, _, _, err := LoadDrivers(writeDrivers(t, rows...)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestDriversWithoutFile(t *testing.T) {
	d, _, _, _ := LoadDrivers(filepath.Join(t.TempDir(), "nope.xlsx"))
	if d.Canonical("Jan Kowalski") != "Jan Kowalski" {
		t.Error("no file → names unchanged")
	}
}

func TestManualColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "drivers.xlsx")
	if err := WriteDriversTemplate(path, 23000); err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sheet := f.GetSheetName(0)
	header, _ := f.GetRows(sheet)
	want := []string{HeaderName, HeaderFee, HeaderRent, HeaderBonus, HeaderZUS, HeaderDebt, HeaderTerminal, HeaderExclude, HeaderAliases}
	if strings.Join(header[0], "|") != strings.Join(want, "|") {
		t.Errorf("template header = %v", header[0])
	}
	// Defaults: ZUS 50, terminal 5. Jan: own bonus, debt and ZUS 0.
	_ = f.SetSheetRow(sheet, "A2", &[]any{DefaultRowName, 230, 0, 0, 50, 0, 5})
	_ = f.SetSheetRow(sheet, "A3", &[]any{"Jan Kowalski", nil, nil, 100, 0, 20.5})
	if err := f.Save(); err != nil {
		t.Fatal(err)
	}
	f.Close()

	d, _, _, err := LoadDrivers(path)
	if err != nil {
		t.Fatal(err)
	}
	checks := []struct {
		name string
		a    model.Adjustment
		want model.Money
	}{
		{"Jan Kowalski", model.PartnerFee, 23000},
		{"Jan Kowalski", model.Bonus, 10000},
		{"Jan Kowalski", model.ZUS, 0}, // explicit 0 overrides the default
		{"Jan Kowalski", model.Debt, 2050},
		{"Jan Kowalski", model.Terminal, 500},
		{"Adam Nowak", model.Bonus, 0},
		{"Adam Nowak", model.ZUS, 5000},
		{"Adam Nowak", model.Terminal, 500},
	}
	for _, c := range checks {
		if got := d.Amount(c.a, c.name); got != c.want {
			t.Errorf("%s, column %d = %s, want %s", c.name, c.a, got, c.want)
		}
	}
}

func TestOldFileWithoutManualColumns(t *testing.T) {
	d, _, _, err := LoadDrivers(writeDrivers(t, []any{"Jan Kowalski", 150, 600}))
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range []model.Adjustment{model.Bonus, model.ZUS, model.Debt, model.Terminal} {
		if d.Amount(a, "Jan Kowalski") != 0 {
			t.Errorf("column %d must be 0 when absent", a)
		}
	}
}
