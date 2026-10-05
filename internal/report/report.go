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

// Adjustments gives the per-driver amounts of columns E–J
// (partner fee, rent, bonus, ZUS, debt, terminal).
type Adjustments interface {
	Amount(a model.Adjustment, name string) model.Money
}

// Input is everything that goes into the workbook.
type Input struct {
	Week model.Week
	Rows []merge.Row
	// Adjustments may be nil: columns E–J are then 0.
	Adjustments Adjustments
	// Raw source tables keyed by source name; each becomes its own sheet.
	Raw map[string]model.Table
}

// FileName returns e.g. "Report_2026-09-21_2026-09-27.xlsx".
func FileName(w model.Week) string {
	return fmt.Sprintf("Report_%s_%s.xlsx", w.Start.Format("2006-01-02"), w.End.Format("2006-01-02"))
}

// moneyFormat has no thousands separator: Excel copies the displayed text,
// and online banking rejects amounts like "2,399.22" pasted into a transfer.
const moneyFormat = `0.00;[Red]-0.00`

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

// writeReport fills the main sheet. Formula cells also get their computed
// value: viewers that do not recalculate (phone previews, Quick Look) show
// that value, Excel recalculates on open anyway.
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

	// Sheet settings must be in place before the stream writer takes over the sheet.
	last := len(in.Rows) + 1
	if last >= 2 {
		if err := stripeRows(f, s, "A2:"+cell("K", last)); err != nil {
			return err
		}
	}

	sw, err := f.NewStreamWriter(s)
	if err != nil {
		return err
	}
	// Panes and widths must be set before the first row.
	if err := sw.SetPanes(&excelize.Panes{Freeze: true, XSplit: 1, YSplit: 1, TopLeftCell: "B2", ActivePane: "bottomRight"}); err != nil {
		return err
	}
	_ = sw.SetColWidth(1, 1, 30)
	_ = sw.SetColWidth(2, len(Headers), 13)

	hdr := make([]any, len(Headers))
	for i, h := range Headers {
		hdr[i] = excelize.Cell{Value: h, StyleID: header}
	}
	if err := sw.SetRow("A1", hdr, excelize.RowOpts{Height: 32}); err != nil {
		return err
	}

	// Column totals B..K, as cached values of the totals row.
	totals := make([]model.Money, len(Headers)-1)
	for i, r := range in.Rows {
		row := i + 2
		amounts := []model.Money{
			r.Amounts[model.SourceUber],
			r.Amounts[model.SourceFreenow],
			r.Amounts[model.SourceBolt],
		}
		var adj [model.NumAdjustments]model.Money
		if in.Adjustments != nil {
			for a := range adj {
				adj[a] = in.Adjustments.Amount(model.Adjustment(a), r.Name)
			}
		}
		amounts = append(amounts, adj[:]...)
		// Same as PayoutFormula: B+C+D+G+J − E−F−H−I.
		pay := amounts[0] + amounts[1] + amounts[2] + adj[model.Bonus] + adj[model.Terminal] -
			adj[model.PartnerFee] - adj[model.CarRent] - adj[model.ZUS] - adj[model.Debt]

		values := []any{r.Name}
		for j, a := range amounts {
			values = append(values, excelize.Cell{Value: a.Float(), StyleID: money})
			totals[j] += a
		}
		values = append(values, excelize.Cell{Formula: PayoutFormula(row), Value: pay.Float(), StyleID: payout})
		totals[len(totals)-1] += pay
		if err := sw.SetRow(cell("A", row), values); err != nil {
			return err
		}
	}

	// Totals row; formulas so that manual edits are reflected.
	total := last + 1
	values := []any{excelize.Cell{Value: "Итого", StyleID: totalName}}
	for j, t := range totals {
		col, _ := excelize.ColumnNumberToName(j + 2)
		formula := "0"
		if last >= 2 {
			formula = fmt.Sprintf("SUM(%s2:%s%d)", col, col, last)
		}
		values = append(values, excelize.Cell{Formula: formula, Value: t.Float(), StyleID: totalMoney})
	}
	if err := sw.SetRow(cell("A", total), values); err != nil {
		return err
	}
	return sw.Flush()
}

// stripeRows shades every other row with a light fill. It is a conditional
// format, so stripes stay correct after sorting or inserting rows.
func stripeRows(f *excelize.File, sheet, rangeRef string) error {
	fill, err := f.NewConditionalStyle(&excelize.Style{
		Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"#F2F2F2"}},
	})
	if err != nil {
		return err
	}
	return f.SetConditionalFormat(sheet, rangeRef, []excelize.ConditionalFormatOptions{
		{Type: "formula", Criteria: "MOD(ROW(),2)=1", Format: &fill},
	})
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

func cell(col string, row int) string { return col + strconv.Itoa(row) }

func strPtr(s string) *string { return &s }
