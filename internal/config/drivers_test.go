package config

import (
	"path/filepath"
	"strconv"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestLoadDriversMissing(t *testing.T) {
	d, found, _, err := LoadDrivers(filepath.Join(t.TempDir(), "nope.xlsx"))
	if err != nil || found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if d.Fee("Anyone") != 0 || d.Rent("Anyone") != 0 || d.Excluded("Anyone") {
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
	_ = f.SetSheetRow(sheet, "A5", &[]any{"Firm Account", nil, nil, "да"})
	_ = f.SetSheetRow(sheet, "A6", &[]any{"Piotr Zielinski", 150.3, nil, "нет"})
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
		if int64(d.Fee(c.name)) != c.fee || int64(d.Rent(c.name)) != c.rent || d.Excluded(c.name) != c.excluded {
			t.Errorf("%s: fee=%v rent=%v excluded=%v", c.name, d.Fee(c.name), d.Rent(c.name), d.Excluded(c.name))
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
	if d.Fee(d.Canonical("Amal Abbasov")) != 15000 || !d.Excluded(d.Canonical("firm acc")) {
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
