package sources

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"weekpay/internal/model"
)

// ReadCSV reads a comma-separated file, stripping the UTF-8 BOM.
// Rows shorter than the header are padded with empty cells.
func ReadCSV(r io.Reader) (model.Table, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return model.Table{}, err
	}
	b = bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))
	cr := csv.NewReader(bytes.NewReader(b))
	cr.FieldsPerRecord = -1
	records, err := cr.ReadAll()
	if err != nil {
		return model.Table{}, fmt.Errorf("ошибка разбора CSV: %w", err)
	}
	if len(records) == 0 {
		return model.Table{}, fmt.Errorf("пустой файл")
	}
	t := model.Table{Header: records[0]}
	for i := range t.Header {
		t.Header[i] = strings.TrimSpace(t.Header[i])
	}
	for _, rec := range records[1:] {
		if len(rec) == 1 && strings.TrimSpace(rec[0]) == "" {
			continue
		}
		for len(rec) < len(t.Header) {
			rec = append(rec, "")
		}
		t.Rows = append(t.Rows, rec)
	}
	return t, nil
}

// Columns finds the index of each named column in the header (exact match).
func Columns(t model.Table, names ...string) ([]int, error) {
	idx := make([]int, len(names))
	var missing []string
	for i, n := range names {
		idx[i] = -1
		for j, h := range t.Header {
			if h == n {
				idx[i] = j
				break
			}
		}
		if idx[i] < 0 {
			missing = append(missing, "«"+n+"»")
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("в файле нет колонок %s — возможно, выгружен не тот отчёт или изменился формат", strings.Join(missing, ", "))
	}
	return idx, nil
}

// FindNewest returns the most recently modified file in dir whose name
// satisfies match. Returns "" if there is none.
func FindNewest(dir string, match func(name string) bool) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("не удалось прочитать папку %s: %w", dir, err)
	}
	var best string
	var bestTime int64
	for _, e := range entries {
		if e.IsDir() || !match(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if t := info.ModTime().UnixNano(); best == "" || t > bestTime {
			best, bestTime = filepath.Join(dir, e.Name()), t
		}
	}
	return best, nil
}
