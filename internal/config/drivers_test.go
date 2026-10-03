package config

import (
	"path/filepath"
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
