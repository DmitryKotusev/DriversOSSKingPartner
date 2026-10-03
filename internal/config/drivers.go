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
	HeaderName    = "Водитель"
	HeaderFee     = "Партнёрский сбор"
	HeaderRent    = "Аренда"
	HeaderExclude = "Не включать"
	HeaderAliases = "Другие имена"
)

type entry struct {
	fee, rent *model.Money
	exclude   bool
}

// Drivers holds per-driver partner fee, car rent, accounts to skip and
// other spellings of driver names. The zero value (no file) gives 0
// everywhere, skips nobody and has no aliases.
type Drivers struct {
	DefaultFee  model.Money
	DefaultRent model.Money
	entries     map[string]entry  // by merge.Key
	aliases     map[string]string // merge.Key of alias → main name
}

// Canonical returns the main name if name is listed in «Другие имена»,
// otherwise name unchanged.
func (d *Drivers) Canonical(name string) string {
	if main, ok := d.aliases[merge.Key(name)]; ok {
		return main
	}
	return name
}

// Fee returns the partner fee for a driver.
func (d *Drivers) Fee(name string) model.Money {
	if e, ok := d.entries[merge.Key(name)]; ok && e.fee != nil {
		return *e.fee
	}
	return d.DefaultFee
}

// Rent returns the weekly car rent for a driver.
func (d *Drivers) Rent(name string) model.Money {
	if e, ok := d.entries[merge.Key(name)]; ok && e.rent != nil {
		return *e.rent
	}
	return d.DefaultRent
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

	nameCol, feeCol, rentCol, exclCol, aliasCol := -1, -1, -1, -1, -1
	for i, h := range rows[0] {
		h = strings.ToLower(strings.TrimSpace(h))
		switch {
		case strings.Contains(h, "водител"):
			nameCol = i
		case strings.Contains(h, "сбор"):
			feeCol = i
		case strings.Contains(h, "аренд"):
			rentCol = i
		case strings.Contains(h, "не включать"), strings.Contains(h, "исключ"):
			exclCol = i
		case strings.Contains(h, "другие имена"), strings.Contains(h, "алиас"):
			aliasCol = i
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
		fee, err := money(row, feeCol, rowIdx)
		if err != nil {
			return nil, false, nil, err
		}
		rent, err := money(row, rentCol, rowIdx)
		if err != nil {
			return nil, false, nil, err
		}

		if strings.Contains(strings.ToLower(name), "по умолчанию") {
			if fee != nil {
				d.DefaultFee = *fee
			}
			if rent != nil {
				d.DefaultRent = *rent
			}
			continue
		}

		key := merge.Key(name)
		if _, dup := d.entries[key]; dup {
			warnings = append(warnings, fmt.Sprintf("%s: водитель «%s» указан несколько раз, берётся последняя строка", path, name))
		}
		d.entries[key] = entry{fee: fee, rent: rent, exclude: isYes(cell(row, exclCol))}

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
	_ = f.SetSheetRow(sheet, "A1", &[]any{HeaderName, HeaderFee, HeaderRent, HeaderExclude, HeaderAliases})
	_ = f.SetSheetRow(sheet, "A2", &[]any{DefaultRowName, defaultFee.Float(), 0})
	_ = f.SetColWidth(sheet, "A", "A", 30)
	_ = f.SetColWidth(sheet, "B", "D", 18)
	_ = f.SetColWidth(sheet, "E", "E", 40)
	style, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	_ = f.SetRowStyle(sheet, 1, 1, style)
	_ = f.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})
	return f.SaveAs(path)
}
