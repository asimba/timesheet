package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func (a *App) startBackupLoop(ctx context.Context) {
	if a.cfg.BackupDir == "" {
		log.Printf("резервирование БД не настроено (backup_dir пуст) — пропускаем запуск")
		return
	}
	if err := os.MkdirAll(a.cfg.BackupDir, 0o755); err != nil {
		log.Printf("резервирование БД: не удалось создать backup_dir %q: %v", a.cfg.BackupDir, err)
		return
	}
	interval := time.Duration(a.cfg.BackupIntervalMinutes) * time.Minute
	if interval <= 0 {
		interval = time.Hour
	}
	log.Printf("резервирование БД включено: dir=%q interval=%s keep=%d", a.cfg.BackupDir, interval, a.cfg.BackupKeep)
	go func() {
		if err := a.makeBackup(); err != nil {
			log.Printf("резервирование БД (стартовое): %v", err)
		}
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				log.Printf("резервирование БД: остановка")
				return
			case <-t.C:
				if err := a.makeBackup(); err != nil {
					log.Printf("резервирование БД: %v", err)
				}
			}
		}
	}()
}

func (a *App) makeBackup() error {
	base := filepath.Base(a.cfg.DBPath)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	ts := time.Now().Format("20060102-150405")
	name := fmt.Sprintf("%s.%s.bak", stem, ts)
	dst := filepath.Join(a.cfg.BackupDir, name)

	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("файл резервной копии уже существует: %s", dst)
	}

	escaped := strings.ReplaceAll(dst, "'", "''")
	if _, err := a.db.Exec(`VACUUM INTO '` + escaped + `'`); err != nil {
		return fmt.Errorf("VACUUM INTO: %w", err)
	}
	if st, err := os.Stat(dst); err == nil {
		log.Printf("резервная копия БД создана: %s (%d байт)", dst, st.Size())
	} else {
		log.Printf("резервная копия БД создана: %s", dst)
	}
	if err := a.rotateBackups(); err != nil {
		return fmt.Errorf("ротация: %w", err)
	}
	return nil
}

func (a *App) rotateBackups() error {
	keep := a.cfg.BackupKeep
	if keep <= 0 {
		return nil
	}
	base := filepath.Base(a.cfg.DBPath)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	prefix := stem + "."
	suffix := ".bak"
	entries, err := os.ReadDir(a.cfg.BackupDir)
	if err != nil {
		return err
	}
	type item struct {
		name string
		mod  time.Time
	}
	var files []item
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if !strings.HasPrefix(n, prefix) || !strings.HasSuffix(n, suffix) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, item{name: n, mod: info.ModTime()})
	}
	if len(files) <= keep {
		return nil
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.Before(files[j].mod) })
	for _, f := range files[:len(files)-keep] {
		p := filepath.Join(a.cfg.BackupDir, f.name)
		if err := os.Remove(p); err != nil {
			log.Printf("резервирование БД: не удалось удалить старый бэкап %q: %v", p, err)
			continue
		}
		log.Printf("резервирование БД: удалён старый бэкап %s", p)
	}
	return nil
}