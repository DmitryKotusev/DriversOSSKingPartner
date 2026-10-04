package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSettingsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, SettingsFileName)

	s, err := LoadSettings(path)
	if err != nil || s != (Settings{}) {
		t.Fatalf("missing file: %+v, %v", s, err)
	}

	want := Settings{WorkDir: filepath.Join(dir, "Выплаты")}
	if err := SaveSettings(path, want); err != nil {
		t.Fatal(err)
	}
	if s, err = LoadSettings(path); err != nil || s != want {
		t.Fatalf("got %+v, %v; want %+v", s, err, want)
	}
}

func TestLoadSettings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, SettingsFileName)
	content := "\xef\xbb\xbf# comment\r\n; another\r\n\r\n WorkDir = \"Payouts\" \r\ndownloads=D:\\Down\r\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := LoadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.WorkDir != filepath.Join(dir, "Payouts") {
		t.Errorf("relative workdir = %q", s.WorkDir)
	}
	if s.Downloads != `D:\Down` {
		t.Errorf("downloads = %q", s.Downloads)
	}

	for _, bad := range []string{"workdir", "outdir = x"} {
		if err := os.WriteFile(path, []byte(bad), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadSettings(path); err == nil {
			t.Errorf("%q: expected error", bad)
		}
	}
}
