// Command weekpay builds the weekly driver payout workbook from
// Uber, FREE NOW and Bolt exports.
//
// Started by double-click (or from the desktop shortcut) it needs no flags:
// it takes last week, looks for the files in Downloads, saves the report to
// the work folder, opens it in Excel and waits for Enter before closing.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"weekpay/internal/config"
	"weekpay/internal/desktop"
	"weekpay/internal/merge"
	"weekpay/internal/model"
	"weekpay/internal/report"
	"weekpay/internal/sources"
	"weekpay/internal/sources/bolt"
	"weekpay/internal/sources/freenow"
	"weekpay/internal/sources/uber"
)

const (
	defaultPartnerFee = 23000 // 230.00 PLN, used for the drivers.xlsx template
	driversFileName   = "drivers.xlsx"
	workDirName       = "Payouts" // inside Documents
	shortcutName      = "Выплаты водителям.lnk"
	appTitle          = "Выплаты водителям"
)

type options struct {
	week, uberFile, freenowFile, boltFile string
	downloads, driversPath, out, workDir  string
	force, initDrivers, install           bool
	open, pause                           bool
}

// stdin is shared by all questions so that buffered input is not lost.
var stdin = bufio.NewReader(os.Stdin)

func main() {
	ownConsole := desktop.OwnConsole()
	if ownConsole {
		desktop.SetTitle(appTitle)
	}
	o := parseFlags(ownConsole)

	err := safeRun(o)
	if err != nil {
		fmt.Fprintln(os.Stderr, "\nОШИБКА:", err)
	}
	if o.pause {
		fmt.Print("\nНажмите Enter, чтобы закрыть окно ")
		_, _ = stdin.ReadString('\n')
	}
	if err != nil {
		os.Exit(1)
	}
}

func parseFlags(ownConsole bool) options {
	var o options
	flag.StringVar(&o.week, "week", "", "любой день нужной недели, ГГГГ-ММ-ДД (по умолчанию — прошедшая неделя)")
	flag.StringVar(&o.uberFile, "uber-csv", "", "файл Uber «Платежі (водій)» (по умолчанию ищется в папке загрузок)")
	flag.StringVar(&o.freenowFile, "freenow-csv", "", "zip или *_with_VAT.csv FREE NOW (по умолчанию ищется в папке загрузок)")
	flag.StringVar(&o.boltFile, "bolt-csv", "", "файл Bolt «Zarobki na kierowcę» (по умолчанию ищется в папке загрузок)")
	flag.StringVar(&o.downloads, "downloads", "", "папка, где искать скачанные файлы (по умолчанию — из "+config.SettingsFileName+" или «Загрузки»)")
	flag.StringVar(&o.workDir, "workdir", "", "рабочая папка с drivers.xlsx и отчётами (по умолчанию — из "+config.SettingsFileName+" или Документы\\"+workDirName+")")
	flag.StringVar(&o.driversPath, "drivers", "", "таблица водителей (по умолчанию drivers.xlsx в рабочей папке)")
	flag.StringVar(&o.out, "out", "", "куда сохранить отчёт: папка или путь к .xlsx (по умолчанию — рабочая папка)")
	flag.BoolVar(&o.force, "force", false, "перезаписать отчёт без вопроса, если он уже существует")
	flag.BoolVar(&o.initDrivers, "init-drivers", false, "создать шаблон drivers.xlsx и выйти")
	flag.BoolVar(&o.install, "install", false, "создать рабочую папку и ярлык на рабочем столе и выйти")
	flag.BoolVar(&o.open, "open", ownConsole, "открыть готовый отчёт в Excel (по умолчанию — при запуске двойным кликом)")
	flag.BoolVar(&o.pause, "pause", ownConsole, "в конце ждать Enter (по умолчанию — при запуске двойным кликом)")
	flag.Parse()
	return o
}

// safeRun turns a panic into an error so that the window still waits for Enter.
func safeRun(o options) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("внутренняя ошибка программы: %v\n%s", r, debug.Stack())
		}
	}()
	return run(o)
}

func run(o options) error {
	p, err := resolvePaths(o)
	if err != nil {
		return err
	}
	if o.install {
		return install(o, p)
	}
	if o.initDrivers {
		if err := os.MkdirAll(filepath.Dir(p.drivers), 0o755); err != nil {
			return err
		}
		if err := config.WriteDriversTemplate(p.drivers, defaultPartnerFee); err != nil {
			return err
		}
		fmt.Println("Создан шаблон", p.drivers)
		return nil
	}

	week := model.PreviousWeek(time.Now())
	if o.week != "" {
		if week, err = model.ParseWeek(o.week); err != nil {
			return err
		}
	}
	fmt.Printf("Расчёт выплат за неделю %s – %s\n", week.Start.Format("02.01.2006"), week.End.Format("02.01.2006"))
	fmt.Println("Рабочая папка:", p.workDir)
	if err := prepareWorkDir(p, o.driversPath != ""); err != nil {
		return err
	}

	out := o.out
	if out == "" {
		out = p.workDir
	}
	path := outputPath(out, week)
	if exists(path) && !o.force {
		fmt.Printf("\nОтчёт за эту неделю уже есть: %s\nВ нём могут быть ручные правки.\n", path)
		overwrite, err := askYesNo("Перезаписать?")
		if err != nil {
			return fmt.Errorf("отчёт %s уже существует (возможно, с ручными правками); укажите --force, чтобы перезаписать", path)
		}
		if !overwrite {
			fmt.Println("Отчёт оставлен как был.")
			openReport(o, path)
			return nil
		}
	}

	fmt.Println()
	srcs, err := resolveSources(week, p.downloads, o.uberFile, o.freenowFile, o.boltFile)
	if err != nil {
		return err
	}

	var (
		earnings []model.DriverEarning
		warnings []string
		raw      = map[string]model.Table{}
	)
	fmt.Println("\nСуммы по источникам:")
	for _, s := range srcs {
		d, err := s.Fetch(context.Background(), week)
		if err != nil {
			return err
		}
		var sum model.Money
		for _, e := range d.Earnings {
			sum += e.Amount
		}
		fmt.Printf("  %-9s строк: %3d, сумма: %s\n", sourceTitle(s.Name()), len(d.Earnings), sum)
		earnings = append(earnings, d.Earnings...)
		warnings = append(warnings, d.Warnings...)
		raw[s.Name()] = d.Raw
	}

	drivers, found, dwarn, err := config.LoadDrivers(p.drivers)
	if err != nil {
		return err
	}
	warnings = append(warnings, dwarn...)
	if found {
		fmt.Println("\nТаблица водителей:", p.drivers)
	} else {
		warnings = append(warnings, fmt.Sprintf(
			"таблица водителей %s не найдена: партнёрский сбор и аренда = 0, никто не исключён "+
				"(создать шаблон: weekpay --init-drivers)", p.drivers))
	}

	rows, mwarn := merge.Merge(earnings, drivers)
	warnings = append(warnings, mwarn...)

	if err := writeReport(path, report.Input{Week: week, Rows: rows, Fees: drivers, Raw: raw}); err != nil {
		return err
	}

	if len(warnings) > 0 {
		fmt.Println()
	}
	for _, w := range warnings {
		fmt.Println("ВНИМАНИЕ:", w)
	}
	fmt.Printf("\nГотово: %s (водителей: %d)\n", path, len(rows))
	openReport(o, path)
	return nil
}

type paths struct {
	settingsFile string
	settings     config.Settings
	workDir      string
	downloads    string
	drivers      string
}

// resolvePaths applies flags, then weekpay.ini next to the exe, then defaults.
func resolvePaths(o options) (paths, error) {
	p := paths{settingsFile: filepath.Join(exeDir(), config.SettingsFileName)}
	var err error
	if p.settings, err = config.LoadSettings(p.settingsFile); err != nil {
		return p, err
	}
	p.workDir = firstNonEmpty(o.workDir, p.settings.WorkDir, filepath.Join(desktop.Path(desktop.Documents), workDirName))
	if p.workDir, err = filepath.Abs(p.workDir); err != nil {
		return p, err
	}
	p.downloads = firstNonEmpty(o.downloads, p.settings.Downloads, desktop.Path(desktop.Downloads))
	if p.downloads, err = filepath.Abs(p.downloads); err != nil {
		return p, err
	}
	p.drivers = firstNonEmpty(o.driversPath, filepath.Join(p.workDir, driversFileName))
	return p, nil
}

// prepareWorkDir creates the work folder and, unless the driver table was
// given explicitly, drivers.xlsx in it: a copy of the one from the previous
// location (current folder or next to the exe) or a new template.
func prepareWorkDir(p paths, explicitDrivers bool) error {
	if err := os.MkdirAll(p.workDir, 0o755); err != nil {
		return fmt.Errorf("не удалось создать рабочую папку %s: %w", p.workDir, err)
	}
	if explicitDrivers || exists(p.drivers) {
		return nil
	}
	for _, old := range []string{driversFileName, filepath.Join(exeDir(), driversFileName)} {
		if !exists(old) {
			continue
		}
		if err := copyFile(old, p.drivers); err != nil {
			return fmt.Errorf("не удалось скопировать таблицу водителей в рабочую папку: %w", err)
		}
		fmt.Printf("Таблица водителей скопирована в рабочую папку: %s\n", p.drivers)
		return nil
	}
	if err := config.WriteDriversTemplate(p.drivers, defaultPartnerFee); err != nil {
		return err
	}
	fmt.Printf("Создана таблица водителей %s.\nВпишите в неё партнёрский сбор, аренду и учётки, которые не нужно включать в отчёт.\n", p.drivers)
	return nil
}

// install sets the work folder up and puts a shortcut on the desktop.
func install(o options, p paths) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if o.workDir != "" || !exists(p.settingsFile) {
		s := p.settings
		s.WorkDir = p.workDir
		if err := config.SaveSettings(p.settingsFile, s); err != nil {
			return fmt.Errorf("не удалось сохранить настройки: %w", err)
		}
		fmt.Println("Настройки:", p.settingsFile)
	}
	fmt.Println("Рабочая папка:", p.workDir)
	if err := prepareWorkDir(p, false); err != nil {
		return err
	}
	lnk := filepath.Join(desktop.Path(desktop.Desktop), shortcutName)
	if err := desktop.CreateShortcut(lnk, exe, p.workDir, "Расчёт еженедельных выплат водителям"); err != nil {
		return err
	}
	fmt.Println("Ярлык на рабочем столе:", lnk)
	return nil
}

// sourceFile describes how to find one source's file and what to tell the
// user when it is missing.
type sourceFile struct {
	name     string
	path     *string
	find     func(string, model.Week) (string, error)
	findAny  func(string) (string, error)
	expected func(model.Week) string
	howTo    func(model.Week) string
}

// resolveSources picks explicit files or finds them in the downloads folder,
// printing what was found and, for missing files, what to download.
func resolveSources(w model.Week, dir, uberPath, freenowPath, boltPath string) ([]sources.Source, error) {
	files := []sourceFile{
		{model.SourceUber, &uberPath, uber.FindFile, uber.FindAnyFile, uber.ExpectedName, uber.HowToDownload},
		{model.SourceFreenow, &freenowPath, freenow.FindFile, freenow.FindAnyFile, freenow.ExpectedName, freenow.HowToDownload},
		{model.SourceBolt, &boltPath, bolt.FindFile, bolt.FindAnyFile, bolt.ExpectedName, bolt.HowToDownload},
	}
	fmt.Println("Файлы с порталов, папка", dir)
	const indent = "            "
	var missing []string
	for _, f := range files {
		title := fmt.Sprintf("  %-9s", sourceTitle(f.name))
		if *f.path != "" {
			fmt.Println(title, "указан  ", *f.path)
			continue
		}
		p, err := f.find(dir, w)
		if err != nil {
			return nil, err
		}
		if p != "" {
			fmt.Println(title, "найден  ", filepath.Base(p))
			*f.path = p
			continue
		}
		missing = append(missing, sourceTitle(f.name))
		fmt.Println(title, "НЕ НАЙДЕН")
		fmt.Println(indent + "Скачайте: " + f.howTo(w))
		fmt.Println(indent + "Имя файла: " + f.expected(w))
		if other, _ := f.findAny(dir); other != "" {
			fmt.Println(indent + "(в папке есть файл за другой период: " + filepath.Base(other) + ")")
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("не хватает файлов: %s. Скачайте их в папку %s и запустите программу ещё раз",
			strings.Join(missing, ", "), dir)
	}
	return []sources.Source{
		uber.CSV{Path: uberPath},
		freenow.CSV{Path: freenowPath},
		bolt.CSV{Path: boltPath},
	}, nil
}

func sourceTitle(name string) string {
	if name == model.SourceFreenow {
		return "FREE NOW"
	}
	return name
}

// writeReport saves the report; if the file is open in Excel, it asks to
// close it and tries again.
func writeReport(path string, in report.Input) error {
	for {
		err := report.Write(path, in)
		if err == nil || !desktop.IsLocked(err) {
			return err
		}
		fmt.Printf("\nОтчёт %s открыт в Excel.\nЗакройте его и нажмите Enter, чтобы попробовать ещё раз ", path)
		if _, err := stdin.ReadString('\n'); err != nil {
			return fmt.Errorf("отчёт %s открыт в другой программе — закройте его и запустите программу ещё раз", path)
		}
	}
}

func openReport(o options, path string) {
	if !o.open {
		return
	}
	if err := desktop.Open(path); err != nil {
		fmt.Println("ВНИМАНИЕ: не удалось открыть отчёт:", err)
	}
}

// askYesNo asks until it gets a clear answer. At end of input without one
// it returns an error.
func askYesNo(question string) (bool, error) {
	for {
		fmt.Print(question, " (д/н): ")
		line, err := stdin.ReadString('\n')
		if yes, ok := parseYesNo(line); ok {
			fmt.Println()
			return yes, nil
		}
		if err != nil {
			fmt.Println()
			return false, err
		}
		fmt.Println("Введите «д» (да) или «н» (нет).")
	}
}

// parseYesNo understands Russian and English answers, including Russian
// words typed with the English keyboard layout («l» = «д», «ytn» = «нет»).
// A bare «y» is ambiguous (it is «н» in the Russian layout), so it is rejected.
func parseYesNo(s string) (yes, ok bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "д", "да", "l", "lf", "yes":
		return true, true
	case "н", "нет", "n", "no", "ytn":
		return false, true
	}
	return false, false
}

func outputPath(out string, w model.Week) string {
	if strings.EqualFold(filepath.Ext(out), ".xlsx") {
		return out
	}
	return filepath.Join(out, report.FileName(w))
}

func exeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exe)
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func copyFile(from, to string) error {
	src, err := os.Open(from)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return err
	}
	return dst.Close()
}
