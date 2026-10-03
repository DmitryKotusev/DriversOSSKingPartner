// Package report writes the weekly payout workbook.
package report

import (
	"fmt"
	"regexp"
	"strconv"

	"github.com/xuri/excelize/v2"

	"weekpay/internal/merge"
	"weekpay/internal/model"
)

// SheetReport is the main sheet name.
const SheetReport = "Report"

// Headers of the main sheet, columns A–K.
var Headers = []string{
	"Водитель", "Uber", "Freenow", "Bolt", "Партнёрский сбор", "Аренда автомобиля",
	"Бонус", "ZUS", "Долг", "Терминал", "Итоговая выплата",
}

// PayoutFormula is the formula of column K for the given row:
// columns 2+3+4+7+10 − 5−6−8−9.
func PayoutFormula(row int) string {
	return fmt.Sprintf("B%[1]d+C%[1]d+D%[1]d+G%[1]d+J%[1]d-E%[1]d-F%[1]d-H%[1]d-I%[1]d", row)
}

// Fees gives the partner fee and rent per driver.
type Fees interface {
	Fee(name string) model.Money
	Rent(name string) model.Money
}

// Input is everything that goes into the workbook.
type Input struct {
	Week model.Week
	Rows []merge.Row
	Fees Fees
	// Raw source tables keyed by source name; each becomes its own sheet.
	Raw map[string]model.Table
}

// FileName returns e.g. "Report_2026-09-21_2026-09-27.xlsx".
func FileName(w model.Week) string {
	return fmt.Sprintf("Report_%s_%s.xlsx", w.Start.Format("2006-01-02"), w.End.Format("2006-01-02"))
}

const moneyFormat = `#,##0.00;[Red]-#,##0.00`

// Write creates the workbook at path.
func Write(path string, in Input) error {
	f := excelize.NewFile()
	defer f.Close()

	if err := f.SetSheetName("Sheet1", SheetReport); err != nil {
		return err
	}
	if err := writeReport(f, in); err != nil {
		return err
	}
	for _, src := range model.Sources {
		if t, ok := in.Raw[src]; ok {
			if err := writeRaw(f, src, t); err != nil {
				return fmt.Errorf("лист %s: %w", src, err)
			}
		}
	}
	fullCalc := true
	if err := f.SetCalcProps(&excelize.CalcPropsOptions{FullCalcOnLoad: &fullCalc}); err != nil {
		return err
	}
	f.SetActiveSheet(0)
	if err := f.SaveAs(path); err != nil {
		return fmt.Errorf("не удалось сохранить %s: %w", path, err)
	}
	return nil
}

func writeReport(f *excelize.File, in Input) error {
	s := SheetReport
	header, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true},
		Alignment: &excelize.Alignment{WrapText: true, Vertical: "center", Horizontal: "center"},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"#DDEBF7"}},
	})
	money, _ := f.NewStyle(&excelize.Style{CustomNumFmt: strPtr(moneyFormat)})
	payout, _ := f.NewStyle(&excelize.Style{CustomNumFmt: strPtr(moneyFormat), Font: &excelize.Font{Bold: true}})
	totalName, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true},
		Border: []excelize.Border{{Type: "top", Color: "#000000", Style: 1}}})
	totalMoney, _ := f.NewStyle(&excelize.Style{CustomNumFmt: strPtr(moneyFormat), Font: &excelize.Font{Bold: true},
		Border: []excelize.Border{{Type: "top", Color: "#000000", Style: 1}}})

	hdr := make([]any, len(Headers))
	for i, h := range Headers {
		hdr[i] = h
	}
	if err := f.SetSheetRow(s, "A1", &hdr); err != nil {
		return err
	}
	_ = f.SetCellStyle(s, "A1", "K1", header)
	_ = f.SetRowHeight(s, 1, 32)

	for i, r := range in.Rows {
		row := i + 2
		values := []any{
			r.Name,
			r.Amounts[model.SourceUber].Float(),
			r.Amounts[model.SourceFreenow].Float(),
			r.Amounts[model.SourceBolt].Float(),
			fee(in.Fees, r.Name).Float(),
			rent(in.Fees, r.Name).Float(),
			0, 0, 0, 0,
		}
		if err := f.SetSheetRow(s, cell("A", row), &values); err != nil {
			return err
		}
		if err := f.SetCellFormula(s, cell("K", row), PayoutFormula(row)); err != nil {
			return err
		}
	}

	last := len(in.Rows) + 1
	if last >= 2 {
		_ = f.SetCellStyle(s, "B2", cell("J", last), money)
		_ = f.SetCellStyle(s, "K2", cell("K", last), payout)
	}

	// Totals row; formulas so that manual edits are reflected.
	total := last + 1
	_ = f.SetCellValue(s, cell("A", total), "Итого")
	for _, col := range []string{"B", "C", "D", "E", "F", "G", "H", "I", "J", "K"} {
		formula := "0"
		if last >= 2 {
			formula = fmt.Sprintf("SUM(%s2:%s%d)", col, col, last)
		}
		if err := f.SetCellFormula(s, cell(col, total), formula); err != nil {
			return err
		}
	}
	_ = f.SetCellStyle(s, cell("A", total), cell("A", total), totalName)
	_ = f.SetCellStyle(s, cell("B", total), cell("K", total), totalMoney)

	_ = f.SetColWidth(s, "A", "A", 30)
	_ = f.SetColWidth(s, "B", "K", 13)
	_ = f.SetPanes(s, &excelize.Panes{Freeze: true, XSplit: 1, YSplit: 1, TopLeftCell: "B2", ActivePane: "bottomRight"})
	return nil
}

var numberRe = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?$`)

// writeRaw copies a source table as is; numeric-looking cells become numbers
// so they can be summed in Excel.
func writeRaw(f *excelize.File, name string, t model.Table) error {
	if _, err := f.NewSheet(name); err != nil {
		return err
	}
	sw, err := f.NewStreamWriter(name)
	if err != nil {
		return err
	}
	if err := sw.SetPanes(&excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"}); err != nil {
		return err
	}
	bold, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	hdr := make([]any, len(t.Header))
	for i, h := range t.Header {
		hdr[i] = excelize.Cell{Value: h, StyleID: bold}
	}
	if err := sw.SetRow("A1", hdr, excelize.RowOpts{}); err != nil {
		return err
	}
	for i, r := range t.Rows {
		vals := make([]any, len(r))
		for j, v := range r {
			if numberRe.MatchString(v) {
				if fv, err := strconv.ParseFloat(v, 64); err == nil {
					vals[j] = fv
					continue
				}
			}
			vals[j] = v
		}
		if err := sw.SetRow(cell("A", i+2), vals); err != nil {
			return err
		}
	}
	return sw.Flush()
}

func fee(f Fees, name string) model.Money {
	if f == nil {
		return 0
	}
	return f.Fee(name)
}

func rent(f Fees, name string) model.Money {
	if f == nil {
		return 0
	}
	return f.Rent(name)
}

func cell(col string, row int) string { return col + strconv.Itoa(row) }

func strPtr(s string) *string { return &s }
