// Package merge joins per-source driver earnings into one row per driver.
package merge

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"weekpay/internal/model"
)

// Row is one driver in the report.
type Row struct {
	Name    string
	Amounts map[string]model.Money // by source name; missing source = 0
}

// Key normalizes a driver name for matching: case-insensitive, trimmed,
// repeated whitespace collapsed.
func Key(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(name), " "))
}

// Rules maps alternative spellings to the main name and tells which
// accounts to leave out of the report.
type Rules interface {
	// Canonical returns the main name for an alias, or name unchanged.
	Canonical(name string) string
	Excluded(name string) bool
}

// Merge sums earnings per driver and source. Aliases are resolved to the
// main name first. Excluded drivers are dropped, as are drivers with zero in
// every source. rules may be nil.
// Returns rows sorted by name and warnings about duplicate names.
func Merge(earnings []model.DriverEarning, rules Rules) ([]Row, []string) {
	type dup struct {
		count   int
		nonZero bool
		name    string
	}
	byKey := map[string]*Row{}
	names := map[string][]string{} // spelling variants in input order
	dups := map[[2]string]*dup{}
	var dupOrder [][2]string

	for _, e := range earnings {
		if rules != nil {
			e.Name = rules.Canonical(e.Name)
		}
		key := Key(e.Name)
		if key == "" || (rules != nil && rules.Excluded(e.Name)) {
			continue
		}
		r := byKey[key]
		if r == nil {
			r = &Row{Amounts: map[string]model.Money{}}
			byKey[key] = r
		}
		r.Amounts[e.Source] += e.Amount
		names[key] = append(names[key], e.Name)

		dk := [2]string{e.Source, key}
		d := dups[dk]
		if d == nil {
			d = &dup{name: e.Name}
			dups[dk] = d
			dupOrder = append(dupOrder, dk)
		}
		d.count++
		d.nonZero = d.nonZero || e.Amount != 0
	}

	var warnings []string
	for _, dk := range dupOrder {
		if d := dups[dk]; d.count > 1 && d.nonZero {
			warnings = append(warnings, fmt.Sprintf(
				"%s: водитель «%s» встречается %d раз(а), суммы сложены",
				dk[0], displayName(names[dk[1]]), d.count))
		}
	}

	var rows []Row
	for key, r := range byKey {
		nonZero := false
		for _, a := range r.Amounts {
			nonZero = nonZero || a != 0
		}
		if !nonZero {
			continue
		}
		r.Name = displayName(names[key])
		rows = append(rows, *r)
	}
	sort.Slice(rows, func(i, j int) bool {
		a, b := Key(rows[i].Name), Key(rows[j].Name)
		if a != b {
			return a < b
		}
		return rows[i].Name < rows[j].Name
	})
	return rows, warnings
}

// displayName picks the first spelling that is not ALL CAPS; if all of them
// are, it converts the first one to "Title Case".
func displayName(variants []string) string {
	for _, v := range variants {
		if !isAllCaps(v) {
			return strings.Join(strings.Fields(v), " ")
		}
	}
	words := strings.Fields(variants[0])
	for i, w := range words {
		rs := []rune(strings.ToLower(w))
		rs[0] = unicode.ToUpper(rs[0])
		words[i] = string(rs)
	}
	return strings.Join(words, " ")
}

func isAllCaps(s string) bool {
	hasLetter := false
	for _, r := range s {
		if unicode.IsLower(r) {
			return false
		}
		hasLetter = hasLetter || unicode.IsLetter(r)
	}
	return hasLetter
}
