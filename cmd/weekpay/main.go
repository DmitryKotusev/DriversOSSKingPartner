// Command weekpay builds the weekly driver payout workbook from
// Uber, FREE NOW and Bolt exports.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"weekpay/internal/config"
	"weekpay/internal/merge"
	"weekpay/internal/model"
	"weekpay/internal/report"
	"weekpay/internal/sources"
	"weekpay/internal/sources/bolt"
	"weekpay/internal/sources/freenow"
	"weekpay/internal/sources/uber"
)

const defaultPartnerFee = 23000 // 230.00 PLN, used for the drivers.xlsx template

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "ОШИБКА:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		weekFlag    = flag.String("week", "", "любой день нужной недели, ГГГГ-ММ-ДД (по умолчанию — прошедшая неделя)")
		uberFile    = flag.String("uber-csv", "", "файл Uber «Платежі (водій)» (по умолчанию ищется в папке загрузок)")
		freenowFile = flag.String("freenow-csv", "", "zip или *_with_VAT.csv FREE NOW (по умолчанию ищется в папке загрузок)")
		boltFile    = flag.String("bolt-csv", "", "файл Bolt «Zarobki na kierowcę» (по умолчанию ищется в папке загрузок)")
		downloads   = flag.String("downloads", defaultDownloads(), "папка, где искать скачанные файлы")
		driversPath = flag.String("drivers", "", "таблица водителей (по умолчанию drivers.xlsx в текущей папке или рядом с программой)")
		out         = flag.String("out", ".", "куда сохранить отчёт: папка или путь к .xlsx")
		force       = flag.Bool("force", false, "перезаписать отчёт, если он уже существует")
		initDrivers = flag.Bool("init-drivers", false, "создать шаблон drivers.xlsx и выйти")
	)
	flag.Parse()

	if *initDrivers {
		path := *driversPath
		if path == "" {
			path = "drivers.xlsx"
		}
		if err := config.WriteDriversTemplate(path, defaultPartnerFee); err != nil {
			return err
		}
		fmt.Println("Создан шаблон", path)
		return nil
	}

	week := model.PreviousWeek(time.Now())
	if *weekFlag != "" {
		var err error
		if week, err = model.ParseWeek(*weekFlag); err != nil {
			return err
		}
	}
	fmt.Println("Неделя:", week)

	srcs, err := resolveSources(week, *downloads, *uberFile, *freenowFile, *boltFile)
	if err != nil {
		return err
	}

	var (
		earnings []model.DriverEarning
		warnings []string
		raw      = map[string]model.Table{}
	)
	for _, s := range srcs {
		d, err := s.Fetch(context.Background(), week)
		if err != nil {
			return err
		}
		var sum model.Money
		for _, e := range d.Earnings {
			sum += e.Amount
		}
		fmt.Printf("  %-8s %s (строк: %d, сумма: %s)\n", s.Name()+":", d.Origin, len(d.Earnings), sum)
		earnings = append(earnings, d.Earnings...)
		warnings = append(warnings, d.Warnings...)
		raw[s.Name()] = d.Raw
	}

	dpath := findDriversFile(*driversPath)
	drivers, found, dwarn, err := config.LoadDrivers(dpath)
	if err != nil {
		return err
	}
	warnings = append(warnings, dwarn...)
	if found {
		fmt.Println("Таблица водителей:", dpath)
	} else {
		warnings = append(warnings, fmt.Sprintf(
			"таблица водителей %s не найдена: партнёрский сбор и аренда = 0, никто не исключён "+
				"(создать шаблон: weekpay --init-drivers)", dpath))
	}

	rows, mwarn := merge.Merge(earnings, drivers)
	warnings = append(warnings, mwarn...)

	path := outputPath(*out, week)
	if _, err := os.Stat(path); err == nil && !*force {
		return fmt.Errorf("отчёт %s уже существует (возможно, с ручными правками); укажите --force, чтобы перезаписать", path)
	}
	if err := report.Write(path, report.Input{Week: week, Rows: rows, Fees: drivers, Raw: raw}); err != nil {
		return err
	}

	for _, w := range warnings {
		fmt.Println("ВНИМАНИЕ:", w)
	}
	fmt.Printf("Готово: %s (водителей: %d)\n", path, len(rows))
	return nil
}

// resolveSources picks explicit files or finds them in the downloads folder.
func resolveSources(w model.Week, dir, uberPath, freenowPath, boltPath string) ([]sources.Source, error) {
	type finder struct {
		name     string
		path     *string
		find     func(string, model.Week) (string, error)
		expected string
	}
	year, isoWeek := w.Start.ISOWeek()
	finders := []finder{
		{model.SourceUber, &uberPath, uber.FindFile,
			fmt.Sprintf("%s_%s_payments_driver_*.csv", w.Start.Format("20060102"), w.Start.AddDate(0, 0, 7).Format("20060102"))},
		{model.SourceFreenow, &freenowPath, freenow.FindFile,
			fmt.Sprintf("earnings_%s_%s.zip", w.Start.Format("2006-01-02"), w.End.Format("2006-01-02"))},
		{model.SourceBolt, &boltPath, bolt.FindFile,
			fmt.Sprintf("Zarobki na kierowcę-%dW%02d-*.csv", year, isoWeek)},
	}
	var missing []string
	for _, f := range finders {
		if *f.path != "" {
			continue
		}
		p, err := f.find(dir, w)
		if err != nil {
			return nil, err
		}
		if p == "" {
			missing = append(missing, fmt.Sprintf("  %s: %s", f.name, f.expected))
		}
		*f.path = p
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("в папке %s не найдены файлы за неделю %s:\n%s\nСкачайте их с порталов или укажите путь флагами --uber-csv / --freenow-csv / --bolt-csv",
			dir, w, strings.Join(missing, "\n"))
	}
	return []sources.Source{
		uber.CSV{Path: uberPath},
		freenow.CSV{Path: freenowPath},
		bolt.CSV{Path: boltPath},
	}, nil
}

func defaultDownloads() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, "Downloads")
}

// findDriversFile returns the explicit path, or drivers.xlsx from the current
// folder, or the one next to the executable.
func findDriversFile(explicit string) string {
	if explicit != "" {
		return explicit
	}
	const name = "drivers.xlsx"
	if _, err := os.Stat(name); err == nil {
		return name
	}
	if exe, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(exe), name)
		if _, err := os.Stat(p); err == nil || !errors.Is(err, os.ErrNotExist) {
			return p
		}
	}
	return name
}

func outputPath(out string, w model.Week) string {
	if strings.EqualFold(filepath.Ext(out), ".xlsx") {
		return out
	}
	return filepath.Join(out, report.FileName(w))
}
