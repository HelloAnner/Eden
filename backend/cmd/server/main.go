// 博客服务启动入口，负责加载配置、初始化数据目录并启动 HTTP 服务。
// Author: Codex
// Created: 2026-02-16
package main

import (
	"log"
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
	port := envOrDefault("PORT", "20260")

	server, err := app.NewServer(app.ServerOptions{
		ConfigPath: configPath,
		DataDir:    dataDir,
		WebDir:     webDir,
	})
	if err != nil {
		log.Fatalf("initialize server failed: %v", err)
	}

	log.Printf("blog server listening on :%s", port)
	if err = http.ListenAndServe(":"+port, server.Handler()); err != nil {
		log.Fatalf("start server failed: %v", err)
	}
}

func configureTimezone() {
	zone := envOrDefault("TZ", "Asia/Shanghai")
	location, err := time.LoadLocation(zone)
	if err != nil {
		log.Printf("load timezone failed: %v, fallback to Asia/Shanghai", err)
		location, err = time.LoadLocation("Asia/Shanghai")
		if err != nil {
			log.Printf("fallback timezone failed: %v", err)
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
