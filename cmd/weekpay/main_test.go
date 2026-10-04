package main

import "testing"

func TestParseYesNo(t *testing.T) {
	cases := map[string]struct{ yes, ok bool }{
		"д\r\n": {true, true},
		" Да ":  {true, true},
		"l":     {true, true}, // «д» in the English layout
		"yes":   {true, true},
		"н":     {false, true},
		"НЕТ":   {false, true},
		"ytn":   {false, true}, // «нет» in the English layout
		"n":     {false, true},
		"y":     {false, false}, // «н» in the Russian layout: ambiguous
		"":      {false, false},
		"может": {false, false},
	}
	for in, want := range cases {
		if yes, ok := parseYesNo(in); yes != want.yes || ok != want.ok {
			t.Errorf("parseYesNo(%q) = %v, %v; want %v, %v", in, yes, ok, want.yes, want.ok)
		}
	}
}
