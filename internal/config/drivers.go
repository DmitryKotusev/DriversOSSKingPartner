// Package config reads the driver table (drivers.xlsx) that the user edits in Excel.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"

	"weekpay/internal/merge"
	"weekpay/internal/model"
)

// DefaultRowName marks the row with default values in drivers.xlsx.
const DefaultRowName = "(по умолчанию)"

// Column headers of drivers.xlsx.
const (
	HeaderName     = "Водитель"
	HeaderFee      = "Партнёрский сбор"
	HeaderRent     = "Аренда"
	HeaderBonus    = "Бонус"
	HeaderZUS      = "ZUS"
	HeaderDebt     = "Долг"
	HeaderTerminal = "Терминал"
	HeaderExclude  = "Не включать"
	HeaderAliases  = "Другие имена"
)

// amountColumns are the headers of the amount columns, and the word
// (lower case) by which each is recognized in the file.
var amountColumns = [model.NumAdjustments]struct{ header, word string }{
	model.PartnerFee: {HeaderFee, "сбор"},
	model.CarRent:    {HeaderRent, "аренд"},
	model.Bonus:      {HeaderBonus, "бонус"},
	model.ZUS:        {HeaderZUS, "zus"},
	model.Debt:       {HeaderDebt, "долг"},
	model.Terminal:   {HeaderTerminal, "терминал"},
}

type entry struct {
	amounts [model.NumAdjustments]*model.Money // nil: take the default
	exclude bool
}

// Drivers holds per-driver amounts (partner fee, car rent, bonus, ZUS,
// debt, terminal), accounts to skip and other spellings of driver names.
// The zero value (no file) gives 0 everywhere, skips nobody and has no aliases.
type Drivers struct {
	Defaults [model.NumAdjustments]model.Money
	entries  map[string]entry  // by merge.Key
	aliases  map[string]string // merge.Key of alias → main name
}

// Canonical returns the main name if name is listed in «Другие имена»,
// otherwise name unchanged.
func (d *Drivers) Canonical(name string) string {
	if main, ok := d.aliases[merge.Key(name)]; ok {
		return main
	}
	return name
}

// Amount returns the driver's own value of a column, or the default.
func (d *Drivers) Amount(a model.Adjustment, name string) model.Money {
	if e, ok := d.entries[merge.Key(name)]; ok && e.amounts[a] != nil {
		return *e.amounts[a]
	}
	return d.Defaults[a]
}

// Excluded reports whether the account must not appear in the report
// (e.g. the company's own Uber account). Takes a name or merge.Key.
func (d *Drivers) Excluded(name string) bool {
	return d.entries[merge.Key(name)].exclude
}

// LoadDrivers reads drivers.xlsx. A missing file is not an error: it returns
// an empty table and found=false.
func LoadDrivers(path string) (d *Drivers, found bool, warnings []string, err error) {
	d = &Drivers{entries: map[string]entry{}, aliases: map[string]string{}}
	f, err := excelize.OpenFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return d, false, nil, nil
	}
	if err != nil {
		return nil, false, nil, fmt.Errorf("не удалось открыть %s: %w", path, err)
	}
	defer f.Close()

	sheet := f.GetSheetName(0)
	rows, err := f.GetRows(sheet, excelize.Options{RawCellValue: true})
	if err != nil {
		return nil, false, nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(rows) == 0 {
		return d, true, nil, nil
	}

	nameCol, exclCol, aliasCol := -1, -1, -1
	var amountCols [model.NumAdjustments]int
	for a := range amountCols {
		amountCols[a] = -1
	}
	for i, h := range rows[0] {
		h = strings.ToLower(strings.TrimSpace(h))
		switch {
		case strings.Contains(h, "водител"):
			nameCol = i
		case strings.Contains(h, "не включать"), strings.Contains(h, "исключ"):
			exclCol = i
		case strings.Contains(h, "другие имена"), strings.Contains(h, "алиас"):
			aliasCol = i
		default:
			for a, c := range amountColumns {
				if strings.Contains(h, c.word) {
					amountCols[a] = i
					break
				}
			}
		}
	}
	if nameCol < 0 {
		return nil, false, nil, fmt.Errorf("%s: в первой строке нет колонки «%s»", path, HeaderName)
	}

	cell := func(row []string, col int) string {
		if col < 0 || col >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[col])
	}
	money := func(row []string, col, rowIdx int) (*model.Money, error) {
		s := cell(row, col)
		if s == "" {
			return nil, nil
		}
		m, err := parseCellMoney(s)
		if err != nil {
			ref, _ := excelize.CoordinatesToCellName(col+1, rowIdx+1)
			return nil, fmt.Errorf("%s, ячейка %s: %w", path, ref, err)
		}
		return &m, nil
	}

	for i, row := range rows[1:] {
		rowIdx := i + 1
		name := cell(row, nameCol)
		if name == "" {
			continue
		}
		var amounts [model.NumAdjustments]*model.Money
		for a, col := range amountCols {
			if amounts[a], err = money(row, col, rowIdx); err != nil {
				return nil, false, nil, err
			}
		}

		if strings.Contains(strings.ToLower(name), "по умолчанию") {
			for a, m := range amounts {
				if m != nil {
					d.Defaults[a] = *m
				}
			}
			continue
		}

		key := merge.Key(name)
		if _, dup := d.entries[key]; dup {
			warnings = append(warnings, fmt.Sprintf("%s: водитель «%s» указан несколько раз, берётся последняя строка", path, name))
		}
		d.entries[key] = entry{amounts: amounts, exclude: isYes(cell(row, exclCol))}

		main := strings.Join(strings.Fields(name), " ")
		for _, alias := range splitAliases(cell(row, aliasCol)) {
			akey := merge.Key(alias)
			if akey == key {
				continue
			}
			if prev, ok := d.aliases[akey]; ok && merge.Key(prev) != key {
				return nil, false, nil, fmt.Errorf("%s: имя «%s» указано в «%s» у двух водителей: «%s» и «%s»",
					path, alias, HeaderAliases, prev, main)
			}
			d.aliases[akey] = main
		}
	}

	// An alias must not be another driver's main name, or two drivers would merge.
	for akey, main := range d.aliases {
		if _, ok := d.entries[akey]; ok {
			return nil, false, nil, fmt.Errorf("%s: «%s» — это отдельный водитель в таблице и одновременно другое имя водителя «%s»",
				path, akey, main)
		}
	}
	return d, true, warnings, nil
}

// parseCellMoney parses a raw Excel number, which may have float artifacts
// like "150.30000000000001".
func parseCellMoney(s string) (model.Money, error) {
	if _, frac, ok := strings.Cut(s, "."); ok && len(frac) > 2 {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0, fmt.Errorf("неожиданный формат суммы %q", s)
		}
		return model.ParseMoney(strconv.FormatFloat(f, 'f', 2, 64))
	}
	return model.ParseMoney(s)
}

// splitAliases splits «Другие имена»: names separated by ";", "," or line breaks.
func splitAliases(s string) []string {
	var out []string
	for _, a := range strings.FieldsFunc(s, isAliasSep) {
		if a = strings.TrimSpace(a); a != "" {
			out = append(out, a)
		}
	}
	return out
}

func isAliasSep(r rune) bool {
	return r == ';' || r == ',' || r == 0x0A || r == 0x0D
}

func isYes(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "нет", "no", "0", "false", "-":
		return false
	}
	return true
}

// WriteDriversTemplate creates a drivers.xlsx with headers and the default row.
// It refuses to overwrite an existing file.
func WriteDriversTemplate(path string, defaultFee model.Money) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("файл %s уже существует", path)
	}
	f := excelize.NewFile()
	defer f.Close()
	sheet := "Водители"
	if err := f.SetSheetName("Sheet1", sheet); err != nil {
		return err
	}
	header := []any{HeaderName}
	defaults := []any{DefaultRowName}
	for a, c := range amountColumns {
		header = append(header, c.header)
		if model.Adjustment(a) == model.PartnerFee {
			defaults = append(defaults, defaultFee.Float())
		} else {
			defaults = append(defaults, 0)
		}
	}
	header = append(header, HeaderExclude, HeaderAliases)
	_ = f.SetSheetRow(sheet, "A1", &header)
	_ = f.SetSheetRow(sheet, "A2", &defaults)
	_ = f.SetColWidth(sheet, "A", "A", 30)
	_ = f.SetColWidth(sheet, "B", "H", 18)
	_ = f.SetColWidth(sheet, "I", "I", 40)
	style, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	_ = f.SetRowStyle(sheet, 1, 1, style)
	_ = f.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})
	return f.SaveAs(path)
}
