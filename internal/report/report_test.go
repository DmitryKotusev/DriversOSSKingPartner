package report

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"weekpay/internal/merge"
	"weekpay/internal/model"
)

type adjustments map[string][model.NumAdjustments]model.Money

func (a adjustments) Amount(adj model.Adjustment, n string) model.Money { return a[n][adj] }

func TestPayoutFormula(t *testing.T) {
	if got, want := PayoutFormula(7), "B7+C7+D7+G7+J7-E7-F7-H7-I7"; got != want {
		t.Errorf("PayoutFormula(7) = %q, want %q", got, want)
	}
}

func TestFileName(t *testing.T) {
	w := model.WeekOf(time.Date(2026, 9, 23, 0, 0, 0, 0, time.Local))
	if got := FileName(w); got != "Report_2026-09-21_2026-09-27.xlsx" {
		t.Errorf("FileName = %q", got)
	}
}

func TestWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "r.xlsx")
	in := Input{
		Week: model.WeekOf(time.Date(2026, 9, 23, 0, 0, 0, 0, time.Local)),
		Rows: []merge.Row{
			{Name: "Adam Nowak", Amounts: map[string]model.Money{model.SourceBolt: -1525}},
			{Name: "Jan Kowalski", Amounts: map[string]model.Money{
				model.SourceUber: 100010, model.SourceFreenow: 20005, model.SourceBolt: 30033}},
		},
		Adjustments: adjustments{
			"Jan Kowalski": {model.PartnerFee: 23000, model.CarRent: 60000},
			// fee 230, bonus 100, ZUS 50, debt 20, terminal 10
			"Adam Nowak": {model.PartnerFee: 23000, model.Bonus: 10000, model.ZUS: 5000, model.Debt: 2000, model.Terminal: 1000},
		},
		Raw: map[string]model.Table{
			model.SourceBolt: {Header: []string{"Kierowca", "Zarobki netto|ZŁ", "Numer telefonu"},
				Rows: [][]string{{"ADAM NOWAK", "-6.77", "+48111222333"}}},
		},
	}
	if err := Write(path, in); err != nil {
		t.Fatal(err)
	}

	f, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	if got := f.GetSheetList(); len(got) != 2 || got[0] != SheetReport || got[1] != model.SourceBolt {
		t.Errorf("sheets = %v", got)
	}
	hdr, _ := f.GetRows(SheetReport)
	if len(hdr[0]) != 11 || hdr[0][10] != "Итоговая выплата" {
		t.Errorf("header = %v", hdr[0])
	}

	// Every other data row is striped via a conditional format.
	cf, _ := f.GetConditionalFormats(SheetReport)
	if opts := cf["A2:K3"]; len(opts) != 1 || opts[0].Type != "formula" || opts[0].Criteria != "MOD(ROW(),2)=1" {
		t.Errorf("conditional formats = %+v", cf)
	}

	// Column K is a formula, not a number.
	if formula, _ := f.GetCellFormula(SheetReport, "K3"); formula != PayoutFormula(3) {
		t.Errorf("K3 formula = %q", formula)
	}

	// Formula cells carry a precomputed value for viewers that don't
	// recalculate (phone previews): K3 = 1000.10 + 200.05 + 300.33 − 230 − 600,
	// K2 = −15.25 + 100 + 10 − 230 − 50 − 20.
	for c, want := range map[string]string{"K2": "-205.25", "K3": "670.48", "K4": "465.23", "D4": "285.08", "G2": "100", "H2": "50"} {
		if got, _ := f.GetCellValue(SheetReport, c, excelize.Options{RawCellValue: true}); got != want {
			t.Errorf("cached %s = %q, want %q", c, got, want)
		}
		if typ, _ := f.GetCellType(SheetReport, c); typ != excelize.CellTypeUnset && typ != excelize.CellTypeNumber {
			t.Errorf("cached %s has type %v, want number", c, typ)
		}
	}

	// Simulate manual edits of columns G–J, then recalculate.
	_ = f.SetCellValue(SheetReport, "G3", 50)  // bonus +
	_ = f.SetCellValue(SheetReport, "H3", 100) // ZUS −
	_ = f.SetCellValue(SheetReport, "I3", 10)  // debt −
	_ = f.SetCellValue(SheetReport, "J3", 5.5) // terminal +
	calc := func(c string) string {
		v, err := f.CalcCellValue(SheetReport, c, excelize.Options{RawCellValue: true})
		if err != nil {
			t.Fatalf("%s: %v", c, err)
		}
		return v
	}
	// 1000.10 + 200.05 + 300.33 + 50 + 5.5 − 230 − 600 − 100 − 10 = 615.98
	if got := calc("K3"); got != "615.98" {
		t.Errorf("K3 = %s, want 615.98", got)
	}
	if got := calc("K2"); got != "-205.25" {
		t.Errorf("K2 = %s, want -205.25", got)
	}
	if a, _ := f.GetCellValue(SheetReport, "A4"); a != "Итого" {
		t.Errorf("A4 = %q", a)
	}
	if got := calc("B4"); got != "1000.1" {
		t.Errorf("B4 = %s", got)
	}

	// Raw sheet: numbers stay numbers, phone stays text.
	raw, _ := f.GetRows(model.SourceBolt, excelize.Options{RawCellValue: true})
	if raw[1][1] != "-6.77" || raw[1][2] != "+48111222333" {
		t.Errorf("raw row = %v", raw[1])
	}
	if typ, _ := f.GetCellType(model.SourceBolt, "B2"); typ == excelize.CellTypeSharedString || typ == excelize.CellTypeInlineString {
		t.Errorf("B2 should be numeric, got type %v", typ)
	}
}
