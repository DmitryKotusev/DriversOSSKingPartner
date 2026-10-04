package config

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// SettingsFileName is the settings file kept next to weekpay.exe.
const SettingsFileName = "weekpay.ini"

// Settings are the paths set once for a computer. Empty means default.
type Settings struct {
	// WorkDir holds drivers.xlsx and the reports.
	WorkDir string
	// Downloads is where the downloaded source files are looked for.
	Downloads string
}

// LoadSettings reads a settings file of "key = value" lines; "#" and ";"
// start comments. Relative paths are taken relative to the file's folder.
// A missing file is not an error.
func LoadSettings(path string) (Settings, error) {
	var s Settings
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, fmt.Errorf("не удалось прочитать настройки %s: %w", path, err)
	}
	b = bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")) // Notepad may add a BOM
	sc := bufio.NewScanner(bytes.NewReader(b))
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return s, fmt.Errorf("%s, строка %d: нужен формат «имя = значение»", path, n)
		}
		value = strings.Trim(strings.TrimSpace(value), `"`)
		if value != "" && !filepath.IsAbs(value) {
			value = filepath.Join(filepath.Dir(path), value)
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "workdir":
			s.WorkDir = value
		case "downloads":
			s.Downloads = value
		default:
			return s, fmt.Errorf("%s, строка %d: неизвестная настройка «%s» (есть workdir и downloads)", path, n, strings.TrimSpace(key))
		}
	}
	return s, sc.Err()
}

// SaveSettings writes the settings file with explanatory comments.
func SaveSettings(path string, s Settings) error {
	var b strings.Builder
	b.WriteString("# Настройки weekpay. Строки, начинающиеся с #, — комментарии.\r\n")
	b.WriteString("\r\n")
	b.WriteString("# Рабочая папка: здесь лежат drivers.xlsx и готовые отчёты.\r\n")
	b.WriteString("workdir = " + s.WorkDir + "\r\n")
	b.WriteString("\r\n")
	b.WriteString("# Где искать файлы, скачанные с порталов. Пусто — папка «Загрузки».\r\n")
	b.WriteString("downloads = " + s.Downloads + "\r\n")
	return os.WriteFile(path, []byte(b.String()), 0o644)
}
