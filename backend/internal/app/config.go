// Package app 提供博客配置读取与写入能力。
// Author: Codex
// Created: 2026-02-16
package app

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

func loadConfig(configPath string) (BlogConfig, error) {
	if err := ensureDefaultConfig(configPath); err != nil {
		return BlogConfig{}, err
	}

	var cfg BlogConfig
	if _, err := toml.DecodeFile(configPath, &cfg); err != nil {
		return BlogConfig{}, err
	}
	return cfg, nil
}

func saveConfig(configPath string, cfg BlogConfig) error {
	file, err := os.Create(configPath)
	if err != nil {
		return err
	}
	defer file.Close()
	return toml.NewEncoder(file).Encode(cfg)
}

func ensureDefaultConfig(configPath string) error {
	if _, err := os.Stat(configPath); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return err
	}
	return saveConfig(configPath, defaultConfig())
}

func defaultConfig() BlogConfig {
	return BlogConfig{
		Site: SiteConfig{
			Title:       "Anner's Blog",
			Subtitle:    "技术与生活",
			Description: "专注于 Java 与工程实践的技术博客",
			Icon:        "site/favicon.svg",
		},
		Footer: FooterConfig{
			Copyright: "© 2026 Anner. All rights reserved.",
		},
		Profile: ProfileConfig{
			Name:        "Anner",
			Role:        "Software Engineer @ FineReport",
			Bio:         "热爱技术，专注于 Java 后端开发和系统架构设计。喜欢分享技术心得，记录学习和成长的点滴。",
			Avatar:      "profile/avatar.svg",
			Github:      "https://github.com",
			Email:       "anner@example.com",
			Twitter:     "https://x.com",
			Zhihu:       "https://www.zhihu.com",
			Xiaohongshu: "https://www.xiaohongshu.com",
			Douyin:      "https://www.douyin.com",
		},
		Subscribe: SubscribeConfig{
			Title:       "订阅博客更新",
			Description: "输入您的邮箱地址，第一时间获取最新文章推送。我们承诺不会发送垃圾邮件，您可以随时取消订阅。",
			Placeholder: "请输入您的邮箱地址",
			ButtonText:  "立即订阅",
			Benefits: []string{
				"每周精选技术文章推送",
				"独家技术资料和学习资源",
				"新文章发布即时通知",
			},
			PrivacyNote: "您的邮箱信息将被严格保密",
		},
	}
}
