package config

import (
	"encoding/json"
	"os"
	"path/filepath"

	log "github.com/sirupsen/logrus"
)

type Config struct {
	Host                string `json:"host"`
	Port                int    `json:"port"`
	DatabasePath        string `json:"database_path"`
	LocalAPIKey         string `json:"local_api_key"`
	RedirectEnabled     bool   `json:"redirect_enabled"`
	RedirectKeyword     string `json:"redirect_keyword"`
	RedirectTargetModel string `json:"redirect_target_model"`
	RedirectTargetName  string `json:"redirect_target_name"`
	MinimizeToTray      bool   `json:"minimize_to_tray"`
	AutoStart           bool   `json:"auto_start"`
	configPath          string
}

func LoadConfig() *Config {
	configPath := "config.json"

	cfg := &Config{
		Host:                "localhost",
		Port:                8080,
		DatabasePath:        "routes.db",
		LocalAPIKey:         "sk-local-default-key",
		RedirectEnabled:     false,
		RedirectKeyword:     "proxy_auto",
		RedirectTargetModel: "",
		RedirectTargetName:  "",
		MinimizeToTray:      true,
		AutoStart:           false,
		configPath:          configPath,
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
