// 博客服务启动入口，负责加载配置、初始化数据目录并启动 HTTP 服务。
// Author: Codex
// Created: 2026-02-16
package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"blog/backend/internal/app"
)

func main() {
	configureTimezone()

	rootDir := envOrDefault("APP_ROOT", ".")
	configPath := envOrDefault("CONFIG_PATH", filepath.Join(rootDir, "config.toml"))
	dataDir := envOrDefault("DATA_DIR", filepath.Join(rootDir, "data"))
	webDir := envOrDefault("WEB_DIR", filepath.Join(rootDir, "web"))
	logDir := envOrDefault("LOG_DIR", filepath.Join(rootDir, "logs"))
	port := envOrDefault("PORT", "20260")
	config, err := app.LoadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config failed: %v\n", err)
		os.Exit(1)
	}
	if err = app.InitLogger(logDir, config.Logging.Level); err != nil {
		fmt.Fprintf(os.Stderr, "init logger failed: %v\n", err)
		os.Exit(1)
	}
	app.LogInfof("bootstrap", "logger initialized, level=%s, dir=%s", config.Logging.Level, logDir)

	server, err := app.NewServer(app.ServerOptions{
		ConfigPath: configPath,
		DataDir:    dataDir,
		WebDir:     webDir,
	})
	if err != nil {
		app.LogErrorf("bootstrap", "initialize server failed: %v", err)
		os.Exit(1)
	}

	app.LogInfof("bootstrap", "blog server listening on :%s", port)
	if err = http.ListenAndServe(":"+port, server.Handler()); err != nil {
		app.LogErrorf("bootstrap", "start server failed: %v", err)
		os.Exit(1)
	}
}

func configureTimezone() {
	zone := envOrDefault("TZ", "Asia/Shanghai")
	location, err := time.LoadLocation(zone)
	if err != nil {
		app.LogWarnf("bootstrap", "load timezone failed: %v, fallback to Asia/Shanghai", err)
		location, err = time.LoadLocation("Asia/Shanghai")
		if err != nil {
			app.LogWarnf("bootstrap", "fallback timezone failed: %v", err)
			return
		}
	}
	time.Local = location
}

func envOrDefault(key string, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}
