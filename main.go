package main

import (
	"context"
	"database/sql"
	"embed"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
)

//go:embed web/* web/static/*
var webFS embed.FS

type App struct {
	cfg  *Config
	db   *sql.DB
	tmpl *Templates
}

func main() {
	configPath := flag.String("config", "config.json", "путь к файлу конфигурации")
	flag.Parse()
	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("не удалось загрузить конфигурацию из %q: %v", *configPath, err)
	}
	lock, err := acquireLock(cfg.LockFile)
	if err != nil {
		log.Fatalf("%v", err)
	}
	defer lock.Release()
	if cfg.LogFile != "" {
		f, err := os.OpenFile(cfg.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			log.Fatalf("не удалось открыть файл лога %q: %v", cfg.LogFile, err)
		}
		defer f.Close()
		log.SetOutput(f)
	}
	db, err := initDB(cfg.DBPath)
	if err != nil {
		log.Fatalf("не удалось открыть БД %q: %v", cfg.DBPath, err)
	}
	defer db.Close()
	app := &App{cfg: cfg, db: db}
	tmpl, err := buildTemplates(app)
	if err != nil {
		log.Fatalf("не удалось разобрать шаблоны: %v", err)
	}
	app.tmpl = tmpl
	ctx, stopBackup := context.WithCancel(context.Background())
	defer stopBackup()
	app.startBackupLoop(ctx)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		s := <-sigCh
		log.Printf("получен сигнал %s — завершаемся", s)
		stopBackup()
		lock.Release()
		os.Exit(0)
	}()
	staticRoot, err := fs.Sub(webFS, "web/static")
	if err != nil {
		log.Fatalf("не удалось получить поддерево web/static из embed.FS: %v", err)
	}
	staticFS := http.FileServer(http.FS(staticRoot))
	mux := http.NewServeMux()
	mux.Handle("/static/", http.StripPrefix("/static/", staticFS))
	mux.HandleFunc("/", app.loginHandler)
	mux.HandleFunc("/login", app.loginHandler)
	mux.HandleFunc("/logout", app.logoutHandler)
	mux.HandleFunc("/timesheets", app.requireAuth(app.timesheetsHandler))
	mux.HandleFunc("/timesheets/export", app.requireAuth(app.exportTimesheetsHandler))
	mux.HandleFunc("/timesheets/works", app.requireAuth(app.addWorkEntryHandler))
	mux.HandleFunc("/timesheets/works/edit", app.requireAuth(app.editWorkEntryHandler))
	mux.HandleFunc("/timesheets/works/delete", app.requireAuth(app.deleteWorkEntryHandler))
	mux.HandleFunc("/config", app.requireAdmin(app.configPageHandler))
	mux.HandleFunc("/groups", app.requireAdmin(app.addGroupHandler))
	mux.HandleFunc("/groups/edit", app.requireAdmin(app.editGroupHandler))
	mux.HandleFunc("/groups/delete", app.requireAdmin(app.deleteGroupHandler))
	mux.HandleFunc("/groups/export/csv", app.requireAdmin(app.exportGroupsCSVHandler))
	mux.HandleFunc("/groups/export/xlsx", app.requireAdmin(app.exportGroupsXLSXHandler))
	mux.HandleFunc("/groups/stats", app.requireAdmin(app.groupStatsExportHandler))
	mux.HandleFunc("/projects", app.requireAdmin(app.addProjectHandler))
	mux.HandleFunc("/projects/edit", app.requireAdmin(app.editProjectHandler))
	mux.HandleFunc("/projects/delete", app.requireAdmin(app.deleteProjectHandler))
	mux.HandleFunc("/projects/export/csv", app.requireAdmin(app.exportProjectsCSVHandler))
	mux.HandleFunc("/projects/export/xlsx", app.requireAdmin(app.exportProjectsXLSXHandler))
	mux.HandleFunc("/projects/stats", app.requireAdmin(app.projectStatsExportHandler))
	mux.HandleFunc("/employees", app.requireAdmin(app.addEmployeeHandler))
	mux.HandleFunc("/employees/edit", app.requireAdmin(app.editEmployeeHandler))
	mux.HandleFunc("/employees/delete", app.requireAdmin(app.deleteEmployeeHandler))
	mux.HandleFunc("/employees/export/csv", app.requireAdmin(app.exportEmployeesCSVHandler))
	mux.HandleFunc("/employees/export/xlsx", app.requireAdmin(app.exportEmployeesXLSXHandler))
	mux.HandleFunc("/employees/stats", app.requireAdmin(app.employeeStatsExportHandler))
	mux.HandleFunc("/employees/stats/individual", app.requireAdmin(app.individualEmployeeStatsExportHandler))
	mux.HandleFunc("/api/generate-password", app.requireAdmin(app.generatePasswordHandler))
	mux.HandleFunc("/api/next-employee-id", app.requireAdmin(app.nextEmployeeIDHandler))
	addr := cfg.ListenAddr + ":" + strconv.Itoa(cfg.ListenPort)
	log.Printf("Сервер запущен: http://%s", addr)
	log.Printf("База данных: %s", cfg.DBPath)
	if cfg.LogFile != "" {
		log.Printf("Логи пишутся в файл: %s", cfg.LogFile)
	}
	log.Printf("Граница редактирования табеля: %d дн.", cfg.WorkEntriesEditDays)
	if cfg.BackupDir != "" {
		log.Printf("Резервирование БД: dir=%s каждые %d мин (хранить %d последних)",
			cfg.BackupDir, cfg.BackupIntervalMinutes, cfg.BackupKeep)
	}
	handler := gzipMiddleware(mux)
	if err := http.ListenAndServe(addr, handler); err != nil {
		stopBackup()
		log.Fatal(err)
	}
}