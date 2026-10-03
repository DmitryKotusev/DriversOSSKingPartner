package model

import (
	"fmt"
	"strconv"
	"strings"
)

// Money is an amount in PLN stored in grosze (1/100 PLN) to avoid float rounding.
type Money int64

// ParseMoney parses amounts like "1234.56", "-6.77", "1 234,56" or "" (zero).
// More than two fractional digits is an error: the sources never produce them.
func ParseMoney(s string) (Money, error) {
	s = strings.TrimSpace(s)
	s = strings.NewReplacer(" ", "", " ", "", " ", "").Replace(s)
	if s == "" {
		return 0, nil
	}
	if strings.Contains(s, ",") {
		if strings.Contains(s, ".") {
			return 0, fmt.Errorf("неожиданный формат суммы %q", s)
		}
		s = strings.Replace(s, ",", ".", 1)
	}

	neg := false
	switch {
	case strings.HasPrefix(s, "-"):
		neg = true
		s = s[1:]
	case strings.HasPrefix(s, "+"):
		s = s[1:]
	}

	intPart, frac, hasFrac := strings.Cut(s, ".")
	if intPart == "" || (hasFrac && (frac == "" || len(frac) > 2)) {
		return 0, fmt.Errorf("неожиданный формат суммы %q", s)
	}
	for len(frac) < 2 {
		frac += "0"
	}
	if !isDigits(intPart) || !isDigits(frac) {
		return 0, fmt.Errorf("неожиданный формат суммы %q", s)
	}
	v, err := strconv.ParseInt(intPart+frac, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("неожиданный формат суммы %q: %w", s, err)
	}
	if neg {
		v = -v
	}
	return Money(v), nil
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// Float returns the amount in PLN, for writing into Excel cells.
func (m Money) Float() float64 { return float64(m) / 100 }

// String formats the amount as "1234.56".
func (m Money) String() string {
	sign := ""
	v := int64(m)
	if v < 0 {
		sign = "-"
		v = -v
	}
	return fmt.Sprintf("%s%d.%02d", sign, v/100, v%100)
}
