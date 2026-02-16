// Package app 定义博客后端 API 的核心数据结构。
// Author: Codex
// Created: 2026-02-16
package app

type SiteConfig struct {
	Title       string `json:"title" toml:"title"`
	Subtitle    string `json:"subtitle" toml:"subtitle"`
	Description string `json:"description" toml:"description"`
	Icon        string `json:"icon" toml:"icon"`
}

type FooterConfig struct {
	Copyright string `json:"copyright" toml:"copyright"`
}

type ProfileConfig struct {
	Name        string `json:"name" toml:"name"`
	Role        string `json:"role" toml:"role"`
	Bio         string `json:"bio" toml:"bio"`
	Avatar      string `json:"avatar" toml:"avatar"`
	Github      string `json:"github" toml:"github"`
	Email       string `json:"email" toml:"email"`
	Twitter     string `json:"twitter" toml:"twitter"`
	Zhihu       string `json:"zhihu" toml:"zhihu"`
	Xiaohongshu string `json:"xiaohongshu" toml:"xiaohongshu"`
	Douyin      string `json:"douyin" toml:"douyin"`
}

type SubscribeConfig struct {
	Title       string   `json:"title" toml:"title"`
	Description string   `json:"description" toml:"description"`
	Placeholder string   `json:"placeholder" toml:"placeholder"`
	ButtonText  string   `json:"button_text" toml:"button_text"`
	Benefits    []string `json:"benefits" toml:"benefits"`
	PrivacyNote string   `json:"privacy_note" toml:"privacy_note"`
}

type BlogConfig struct {
	Site      SiteConfig      `json:"site" toml:"site"`
	Footer    FooterConfig    `json:"footer" toml:"footer"`
	Profile   ProfileConfig   `json:"profile" toml:"profile"`
	Subscribe SubscribeConfig `json:"subscribe" toml:"subscribe"`
}

type Article struct {
	ID          string   `json:"id"`
	ParentID    string   `json:"parent_id"`
	Title       string   `json:"title"`
	Slug        string   `json:"slug"`
	Category    string   `json:"category"`
	Path        string   `json:"path"`
	Tags        []string `json:"tags"`
	Excerpt     string   `json:"excerpt"`
	Content     string   `json:"content"`
	PublishedAt string   `json:"published_at"`
	ReadMinutes int      `json:"read_minutes"`
	Views       int      `json:"views"`
	OrderIndex  int      `json:"order_index"`
	CreatedAt   string   `json:"created_at,omitempty"`
	UpdatedAt   string   `json:"updated_at,omitempty"`
	SourceFile  string   `json:"-"`
}

type ArticleTreeNode struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Type      string            `json:"type"`
	ArticleID string            `json:"article_id,omitempty"`
	Icon      string            `json:"icon,omitempty"`
	Children  []ArticleTreeNode `json:"children,omitempty"`
}

type MoveArticleRequest struct {
	ParentID   string `json:"parent_id"`
	OrderIndex int    `json:"order_index"`
}

type Comment struct {
	ID        int64     `json:"id"`
	ArticleID string    `json:"article_id"`
	ParentID  int64     `json:"parent_id,omitempty"`
	Author    string    `json:"author"`
	Content   string    `json:"content"`
	Likes     int       `json:"likes"`
	CreatedAt string    `json:"created_at"`
	UpdatedAt string    `json:"updated_at,omitempty"`
	Replies   []Comment `json:"replies,omitempty"`
}

type SubscribeRequest struct {
	Email string `json:"email"`
}

type ProfileStats struct {
	Articles int `json:"articles"`
	Views    int `json:"views"`
	Years    int `json:"years"`
}
