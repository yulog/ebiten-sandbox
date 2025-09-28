package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// Config holds the application configuration.
type Config struct {
	MisskeyInstance string `json:"misskey_instance"`
	AccessToken     string `json:"access_token"`
}

// loadConfig reads and parses the config.json file.
func loadConfig() (*Config, error) {
	data, err := os.ReadFile("config.json")
	if err != nil {
		return nil, fmt.Errorf("cannot read config.json: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("invalid format in config.json: %w", err)
	}
	if cfg.MisskeyInstance == "" || cfg.MisskeyInstance == "your.misskey.instance.com" || cfg.AccessToken == "" || cfg.AccessToken == "YOUR_MISSKEY_ACCESS_TOKEN" {
		return nil, fmt.Errorf("please update config.json")
	}
	return &cfg, nil
}
