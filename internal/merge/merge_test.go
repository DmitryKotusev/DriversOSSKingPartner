package merge

import (
	"strings"
	"testing"

	"weekpay/internal/model"
)

func e(src, name, amount string) model.DriverEarning {
	m, err := model.ParseMoney(amount)
	if err != nil {
		panic(err)
	}
	return model.DriverEarning{Name: name, Amount: m, Source: src}
}

func TestKey(t *testing.T) {
	for _, s := range []string{"Jan Kowalski", "  JAN   KOWALSKI ", "jan\tkowalski"} {
		if got := Key(s); got != "jan kowalski" {
			t.Errorf("Key(%q) = %q", s, got)
		}
	}
}

func TestMergeAcrossSources(t *testing.T) {
	rows, warnings := Merge([]model.DriverEarning{
		e(model.SourceUber, "Jan Kowalski", "100.10"),
		e(model.SourceFreenow, "jan  kowalski", "20.05"),
		e(model.SourceBolt, "JAN KOWALSKI", "-15.25"),
		e(model.SourceBolt, "ADAM NOWAK", "-6.77"),
		e(model.SourceFreenow, "Old Driver", "0.00"),
		e(model.SourceUber, "Firm Account", "-126648.63"),
	}, func(key string) bool { return key == "firm account" })

	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2: %+v", len(rows), rows)
	}
	// Sorted by name; ALL CAPS-only name is title-cased.
	if rows[0].Name != "Adam Nowak" || rows[0].Amounts[model.SourceBolt] != -677 {
		t.Errorf("row 0 = %+v", rows[0])
	}
	r := rows[1]
	if r.Name != "Jan Kowalski" {
		t.Errorf("name = %q", r.Name)
	}
	want := map[string]model.Money{model.SourceUber: 10010, model.SourceFreenow: 2005, model.SourceBolt: -1525}
	for src, m := range want {
		if r.Amounts[src] != m {
			t.Errorf("%s = %v, want %v", src, r.Amounts[src], m)
		}
	}
}

func TestMergeDuplicates(t *testing.T) {
	rows, warnings := Merge([]model.DriverEarning{
		// Zero-only duplicates (inactive accounts): no warning, no row.
		e(model.SourceFreenow, "Ghost Driver", "0.00"),
		e(model.SourceFreenow, "Ghost Driver", "0.00"),
		// Duplicate with money: amounts summed, warning shown.
		e(model.SourceBolt, "Ivan Petrov", "10.00"),
		e(model.SourceBolt, "ivan petrov", "5.50"),
		// Same name in different sources is not a duplicate.
		e(model.SourceUber, "Ivan Petrov", "1.00"),
	}, nil)

	if len(rows) != 1 || rows[0].Amounts[model.SourceBolt] != 1550 || rows[0].Amounts[model.SourceUber] != 100 {
		t.Fatalf("rows = %+v", rows)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "Ivan Petrov") || !strings.HasPrefix(warnings[0], "Bolt") {
		t.Errorf("warnings = %v", warnings)
	}
}
