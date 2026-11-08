package main

import (
	"encoding/json"
	"errors"
	"os"
)

type AdminAccount struct {
	User     string `json:"user"`
	Password string `json:"password"`
}

type Config struct {
	ListenAddr string `json:"listen_addr"`
	ListenPort int    `json:"listen_port"`
	DBPath     string `json:"db_path"`
	LogFile    string `json:"log_file,omitempty"`

	Admins []AdminAccount `json:"admins"`

	AdminUser     string `json:"admin_user,omitempty"`
	AdminPassword string `json:"admin_password,omitempty"`

	WorkEntriesEditDays int `json:"work_entries_edit_days"`

	BackupDir             string `json:"backup_dir,omitempty"`
	BackupIntervalMinutes int    `json:"backup_interval_minutes,omitempty"`
	BackupKeep            int    `json:"backup_keep,omitempty"`

	LockFile string `json:"lock_file,omitempty"`
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = "127.0.0.1"
	}
	if cfg.ListenPort == 0 {
		cfg.ListenPort = 8080
	}
	if cfg.DBPath == "" {
		cfg.DBPath = "base.db"
	}

	if cfg.AdminUser != "" && cfg.AdminPassword != "" {
		cfg.Admins = append(cfg.Admins, AdminAccount{
			User:     cfg.AdminUser,
			Password: cfg.AdminPassword,
		})
	}

	if len(cfg.Admins) == 0 {
		return nil, errors.New("в конфигурации должен быть задан хотя бы один администратор (admins или admin_user/admin_password)")
	}

	for _, a := range cfg.Admins {
		if a.User == "" || a.Password == "" {
			return nil, errors.New("у администратора в списке admins должны быть заданы user и password")
		}
	}

	if cfg.WorkEntriesEditDays < 0 {
		cfg.WorkEntriesEditDays = 5
	}
	if cfg.BackupIntervalMinutes <= 0 {
		cfg.BackupIntervalMinutes = 60
	}
	if cfg.BackupKeep <= 0 {
		cfg.BackupKeep = 7
	}
	if cfg.LockFile == "" {
		cfg.LockFile = cfg.DBPath + ".lock"
	}
	return &cfg, nil
}