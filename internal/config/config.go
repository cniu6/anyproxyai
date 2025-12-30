package config

import (
	"encoding/json"
	"os"
	"path/filepath"

	log "github.com/sirupsen/logrus"
)

type Config struct {
	Host                  string `json:"host"`
	Port                  int    `json:"port"`
	DatabasePath          string `json:"database_path"`
	LocalAPIKey           string `json:"local_api_key"`
	RedirectEnabled       bool   `json:"redirect_enabled"`
	RedirectKeyword       string `json:"redirect_keyword"`
	RedirectTargetModel   string `json:"redirect_target_model"`
	RedirectTargetName    string `json:"redirect_target_name"`
	RedirectTargetRouteID int64  `json:"redirect_target_route_id"`
	MinimizeToTray        bool   `json:"minimize_to_tray"`
	AutoStart             bool   `json:"auto_start"`
	EnableFileLog         bool   `json:"enable_file_log"`
	Language              string `json:"language"`
	// proxy_auto 虚拟模型配置
	ProxyAutoPrimaryRouteID int64  `json:"proxy_auto_primary_route_id"`
	ProxyAutoBackupRouteID  int64  `json:"proxy_auto_backup_route_id"`
	configPath              string
}

func LoadConfig() *Config {
	configPath := "config.json"

	cfg := &Config{
		Host:                  "localhost",
		Port:                  5642,
		DatabasePath:          "routes.db",
		LocalAPIKey:           "sk-local-default-key",
		RedirectEnabled:       false,
		RedirectKeyword:       "proxy_auto",
		RedirectTargetModel:   "",
		RedirectTargetName:    "",
		RedirectTargetRouteID: 0,
		MinimizeToTray:        true,
		AutoStart:             false,
		EnableFileLog:         false,
		Language:              "en-US",
		configPath:            configPath,
	}

	// 尝试从文件加载配置
	if data, err := os.ReadFile(configPath); err == nil {
		if err := json.Unmarshal(data, cfg); err != nil {
			log.Warnf("Failed to parse config file: %v", err)
		} else {
			log.Info("Configuration loaded from config.json")
		}
	} else {
		log.Info("Config file not found, using default configuration")
		// 保存默认配置
		cfg.Save()
	}

	return cfg
}

func (c *Config) Save() error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(c.configPath)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}

	return os.WriteFile(c.configPath, data, 0644)
}
