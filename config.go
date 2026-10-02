package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type databaseConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	Database string `json:"database"`
	SSLMode  string `json:"sslmode"`
}

func loadDatabaseConfig(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}

	var config databaseConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return "", fmt.Errorf("parse %s: %w", path, err)
	}
	if strings.TrimSpace(config.Host) == "" || strings.TrimSpace(config.User) == "" ||
		config.Password == "" || strings.TrimSpace(config.Database) == "" {
		return "", fmt.Errorf("host, user, password, and database must be filled in %s", path)
	}
	if config.Port < 1 || config.Port > 65535 {
		return "", fmt.Errorf("port in %s must be between 1 and 65535", path)
	}
	if config.SSLMode == "" {
		config.SSLMode = "disable"
	}

	databaseURL := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(config.User, config.Password),
		Host:   net.JoinHostPort(config.Host, strconv.Itoa(config.Port)),
		Path:   config.Database,
	}
	query := databaseURL.Query()
	query.Set("sslmode", config.SSLMode)
	databaseURL.RawQuery = query.Encode()

	return databaseURL.String(), nil
}
