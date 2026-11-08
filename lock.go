package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

type lockFile struct {
	path string
	f    *os.File
}

func acquireLock(path string) (*lockFile, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("не удалось создать каталог для lock-файла %q: %w", path, err)
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			data, _ := os.ReadFile(path)
			return nil, fmt.Errorf("lock-файл %q уже существует, вероятно, приложение уже запущено.\n"+
				"Содержимое lock-файла:\n%s\n"+
				"Если приложение не запущено (аварийное завершение), удалите файл вручную: rm %q",
				path, string(data), path)
		}
		return nil, fmt.Errorf("не удалось создать lock-файл %q: %w", path, err)
	}
	content := fmt.Sprintf("pid=%d\nstarted=%s\nhost=%s\n",
		os.Getpid(), time.Now().Format(time.RFC3339), hostnameOrUnknown())
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		os.Remove(path)
		return nil, fmt.Errorf("не удалось записать в lock-файл %q: %w", path, err)
	}
	if err := f.Sync(); err != nil {
		log.Printf("lock-файл: Sync не удался: %v", err)
	}
	log.Printf("lock-файл создан: %s (pid=%d)", path, os.Getpid())
	return &lockFile{path: path, f: f}, nil
}

func (l *lockFile) Release() {
	if l == nil {
		return
	}
	if l.f != nil {
		_ = l.f.Close()
		l.f = nil
	}
	if err := os.Remove(l.path); err != nil && !os.IsNotExist(err) {
		log.Printf("lock-файл: не удалось удалить %q: %v", l.path, err)
		return
	}
	log.Printf("lock-файл удалён: %s", l.path)
}

func hostnameOrUnknown() string {
	h, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return h
}