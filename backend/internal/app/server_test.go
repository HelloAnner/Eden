package app

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestGetConfig(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.toml")
	configContent := `
[site]
title = "Anner's Blog"
subtitle = "技术与生活"
description = "A practical engineering blog"

[footer]
copyright = "© 2026 Anner"
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	server, err := NewServer(ServerOptions{
		ConfigPath: configPath,
		DataDir:    tempDir,
		WebDir:     "",
	})
	if err != nil {
		t.Fatalf("create server failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("parse response failed: %v", err)
	}

	siteRaw, ok := payload["site"].(map[string]any)
	if !ok {
		t.Fatalf("site payload missing")
	}
	if siteRaw["title"] != "Anner's Blog" {
		t.Fatalf("unexpected title: %v", siteRaw["title"])
	}
}

func TestLegacyCommentsSchemaMigration(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.toml")
	if err := os.WriteFile(configPath, []byte("[site]\ntitle=\"Blog\"\n"), 0644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	dbPath := filepath.Join(tempDir, "blog.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	_, err = db.Exec(`
CREATE TABLE comments (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    article_id TEXT NOT NULL,
    author TEXT NOT NULL,
    content TEXT NOT NULL,
    likes INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL
);
CREATE INDEX idx_comments_article ON comments(article_id);
`)
	if err != nil {
		db.Close()
		t.Fatalf("create legacy schema failed: %v", err)
	}
	if err = db.Close(); err != nil {
		t.Fatalf("close sqlite failed: %v", err)
	}

	server, err := NewServer(ServerOptions{
		ConfigPath: configPath,
		DataDir:    tempDir,
		WebDir:     "",
	})
	if err != nil {
		t.Fatalf("create server with legacy schema failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/comments?article_id=legacy", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestServeProfileAvatarFromDataDir(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.toml")
	if err := os.WriteFile(configPath, []byte("[site]\ntitle=\"Blog\"\n"), 0644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	avatarDir := filepath.Join(tempDir, "profile")
	if err := os.MkdirAll(avatarDir, 0755); err != nil {
		t.Fatalf("mkdir avatar dir failed: %v", err)
	}
	avatarContent := []byte("avatar-bytes")
	avatarPath := filepath.Join(avatarDir, "avatar.png")
	if err := os.WriteFile(avatarPath, avatarContent, 0644); err != nil {
		t.Fatalf("write avatar failed: %v", err)
	}

	server, err := NewServer(ServerOptions{
		ConfigPath: configPath,
		DataDir:    tempDir,
		WebDir:     "",
	})
	if err != nil {
		t.Fatalf("create server failed: %v", err)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/data/profile/avatar.png", nil)
	getRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", getRec.Code)
	}
	if !bytes.Equal(getRec.Body.Bytes(), avatarContent) {
		t.Fatalf("avatar content mismatch")
	}

	blockReq := httptest.NewRequest(http.MethodGet, "/api/v1/data/%2e%2e/config.toml", nil)
	blockRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(blockRec, blockReq)
	if blockRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", blockRec.Code)
	}
}

func TestSPACacheHeaders(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.toml")
	if err := os.WriteFile(configPath, []byte("[site]\ntitle=\"Blog\"\n"), 0644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}
	webDir := filepath.Join(tempDir, "web")
	if err := os.MkdirAll(filepath.Join(webDir, "assets"), 0755); err != nil {
		t.Fatalf("mkdir web dir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(webDir, "index.html"), []byte("<html><body>index</body></html>"), 0644); err != nil {
		t.Fatalf("write index failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(webDir, "assets", "main-ABCDEF12.js"), []byte("console.log('ok')"), 0644); err != nil {
		t.Fatalf("write asset failed: %v", err)
	}

	server, err := NewServer(ServerOptions{
		ConfigPath: configPath,
		DataDir:    tempDir,
		WebDir:     webDir,
	})
	if err != nil {
		t.Fatalf("create server failed: %v", err)
	}

	indexReq := httptest.NewRequest(http.MethodGet, "/", nil)
	indexRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(indexRec, indexReq)
	if indexRec.Code != http.StatusOK {
		t.Fatalf("index expected 200, got %d", indexRec.Code)
	}
	if cacheControl := indexRec.Header().Get("Cache-Control"); !strings.Contains(cacheControl, "no-cache") {
		t.Fatalf("index cache-control should be no-cache, got %q", cacheControl)
	}

	assetReq := httptest.NewRequest(http.MethodGet, "/assets/main-ABCDEF12.js", nil)
	assetRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(assetRec, assetReq)
	if assetRec.Code != http.StatusOK {
		t.Fatalf("asset expected 200, got %d", assetRec.Code)
	}
	if cacheControl := assetRec.Header().Get("Cache-Control"); !strings.Contains(cacheControl, "immutable") {
		t.Fatalf("hashed asset should be immutable cache, got %q", cacheControl)
	}
}

func TestDataAssetETagCache(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.toml")
	if err := os.WriteFile(configPath, []byte("[site]\ntitle=\"Blog\"\n"), 0644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}
	assetDir := filepath.Join(tempDir, "profile")
	if err := os.MkdirAll(assetDir, 0755); err != nil {
		t.Fatalf("mkdir asset dir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(assetDir, "avatar.png"), []byte("avatar-bytes"), 0644); err != nil {
		t.Fatalf("write avatar failed: %v", err)
	}

	server, err := NewServer(ServerOptions{
		ConfigPath: configPath,
		DataDir:    tempDir,
		WebDir:     "",
	})
	if err != nil {
		t.Fatalf("create server failed: %v", err)
	}

	firstReq := httptest.NewRequest(http.MethodGet, "/api/v1/data/profile/avatar.png", nil)
	firstRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(firstRec, firstReq)
	if firstRec.Code != http.StatusOK {
		t.Fatalf("first request expected 200, got %d", firstRec.Code)
	}
	etag := firstRec.Header().Get("ETag")
	if strings.TrimSpace(etag) == "" {
		t.Fatalf("etag should be set")
	}
	if cacheControl := firstRec.Header().Get("Cache-Control"); !strings.Contains(cacheControl, "max-age=86400") {
		t.Fatalf("unexpected data asset cache-control: %q", cacheControl)
	}

	secondReq := httptest.NewRequest(http.MethodGet, "/api/v1/data/profile/avatar.png", nil)
	secondReq.Header.Set("If-None-Match", etag)
	secondRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(secondRec, secondReq)
	if secondRec.Code != http.StatusNotModified {
		t.Fatalf("second request expected 304, got %d", secondRec.Code)
	}
}

func TestTagTreeETagCache(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.toml")
	if err := os.WriteFile(configPath, []byte("[site]\ntitle=\"Blog\"\n"), 0644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}
	seedMarkdownNote(t, tempDir, "Agent工程/缓存", "缓存测试", "测试缓存")

	server, err := NewServer(ServerOptions{
		ConfigPath: configPath,
		DataDir:    tempDir,
		WebDir:     "",
	})
	if err != nil {
		t.Fatalf("create server failed: %v", err)
	}

	firstReq := httptest.NewRequest(http.MethodGet, "/api/v1/tags/tree", nil)
	firstRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(firstRec, firstReq)
	if firstRec.Code != http.StatusOK {
		t.Fatalf("first request expected 200, got %d", firstRec.Code)
	}
	etag := strings.TrimSpace(firstRec.Header().Get("ETag"))
	if etag == "" {
		t.Fatalf("etag should exist for tags tree")
	}

	secondReq := httptest.NewRequest(http.MethodGet, "/api/v1/tags/tree", nil)
	secondReq.Header.Set("If-None-Match", etag)
	secondRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(secondRec, secondReq)
	if secondRec.Code != http.StatusNotModified {
		t.Fatalf("second request expected 304, got %d", secondRec.Code)
	}
}

func TestServePrecompressedAssetWhenSupported(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.toml")
	if err := os.WriteFile(configPath, []byte("[site]\ntitle=\"Blog\"\n"), 0644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}
	webDir := filepath.Join(tempDir, "web")
	if err := os.MkdirAll(filepath.Join(webDir, "assets"), 0755); err != nil {
		t.Fatalf("mkdir web dir failed: %v", err)
	}
	originPath := filepath.Join(webDir, "assets", "main-ABCDEF12.js")
	brPath := originPath + ".br"
	if err := os.WriteFile(originPath, []byte("origin-data"), 0644); err != nil {
		t.Fatalf("write origin file failed: %v", err)
	}
	if err := os.WriteFile(brPath, []byte("compressed-data"), 0644); err != nil {
		t.Fatalf("write br file failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(webDir, "index.html"), []byte("<html>ok</html>"), 0644); err != nil {
		t.Fatalf("write index failed: %v", err)
	}

	server, err := NewServer(ServerOptions{
		ConfigPath: configPath,
		DataDir:    tempDir,
		WebDir:     webDir,
	})
	if err != nil {
		t.Fatalf("create server failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/assets/main-ABCDEF12.js", nil)
	req.Header.Set("Accept-Encoding", "br, gzip")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if encoding := rec.Header().Get("Content-Encoding"); encoding != "br" {
		t.Fatalf("expected br encoding, got %q", encoding)
	}
	if vary := rec.Header().Get("Vary"); !strings.Contains(vary, "Accept-Encoding") {
		t.Fatalf("expected vary accept-encoding header, got %q", vary)
	}
}

func TestCommentReplyLikeAndRateLimit(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.toml")
	if err := os.WriteFile(configPath, []byte("[site]\ntitle=\"Blog\"\n"), 0644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}
	seedMarkdownNote(t, tempDir, "测试分类/评论", "评论测试文章", "用于评论能力测试。")

	server, err := NewServer(ServerOptions{
		ConfigPath:         configPath,
		DataDir:            tempDir,
		WebDir:             "",
		CommentCreateLimit: 3,
		CommentLikeLimit:   2,
		RateLimitWindow:    time.Hour,
	})
	if err != nil {
		t.Fatalf("create server failed: %v", err)
	}
	articleID := findArticleIDByPathTitle(t, server.store, "测试分类/评论", "评论测试文章")

	create1 := map[string]any{
		"article_id": articleID,
		"author":     "测试用户",
		"content":    "顶级评论",
	}
	topComment := createComment(t, server.Handler(), create1, http.StatusCreated)
	topID := int64(topComment["id"].(float64))

	create2 := map[string]any{
		"article_id": articleID,
		"author":     "测试用户",
		"content":    "这是回复",
		"parent_id":  topID,
	}
	_ = createComment(t, server.Handler(), create2, http.StatusCreated)

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/comments?article_id="+articleID, nil)
	listRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list comments expected 200, got %d", listRec.Code)
	}
	var listPayload []map[string]any
	if err = json.Unmarshal(listRec.Body.Bytes(), &listPayload); err != nil {
		t.Fatalf("list parse failed: %v", err)
	}
	foundReply := false
	for _, item := range listPayload {
		if int64(item["id"].(float64)) != topID {
			continue
		}
		replies, ok := item["replies"].([]any)
		if ok && len(replies) > 0 {
			foundReply = true
		}
	}
	if !foundReply {
		t.Fatalf("reply should be nested under top comment")
	}

	likeReq1 := httptest.NewRequest(http.MethodPost, "/api/v1/comments/"+floatToIntString(topComment["id"])+"/like", nil)
	likeReq1.Header.Set("X-Forwarded-For", "1.2.3.4")
	likeRec1 := httptest.NewRecorder()
	server.Handler().ServeHTTP(likeRec1, likeReq1)
	if likeRec1.Code != http.StatusOK {
		t.Fatalf("first like expected 200, got %d", likeRec1.Code)
	}

	likeReq2 := httptest.NewRequest(http.MethodPost, "/api/v1/comments/"+floatToIntString(topComment["id"])+"/like", nil)
	likeReq2.Header.Set("X-Forwarded-For", "1.2.3.4")
	likeRec2 := httptest.NewRecorder()
	server.Handler().ServeHTTP(likeRec2, likeReq2)
	if likeRec2.Code != http.StatusOK {
		t.Fatalf("second like expected 200, got %d", likeRec2.Code)
	}

	likeReq3 := httptest.NewRequest(http.MethodPost, "/api/v1/comments/"+floatToIntString(topComment["id"])+"/like", nil)
	likeReq3.Header.Set("X-Forwarded-For", "1.2.3.4")
	likeRec3 := httptest.NewRecorder()
	server.Handler().ServeHTTP(likeRec3, likeReq3)
	if likeRec3.Code != http.StatusTooManyRequests {
		t.Fatalf("third like expected 429, got %d", likeRec3.Code)
	}
}

func createComment(t *testing.T, handler http.Handler, body map[string]any, expectedCode int) map[string]any {
	t.Helper()
	payload, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/comments", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != expectedCode {
		t.Fatalf("create comment expected %d, got %d, body=%s", expectedCode, rec.Code, rec.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("comment parse failed: %v", err)
	}
	return response
}

func floatToIntString(value any) string {
	number := int64(value.(float64))
	return strconv.FormatInt(number, 10)
}

func TestParseTagsFromFooterOnly(t *testing.T) {
	content := `
# 文档标题

正文里出现了 #FFB86CA6 和代码颜色 #1e1e1e，不应该进入目录。

` + "```json" + `
{"color":"#BBFABBA6"}
` + "```" + `

#Agent工程/MCP
`

	tags := parseTags(content)
	if len(tags) != 1 {
		t.Fatalf("expected 1 tag, got %d, tags=%v", len(tags), tags)
	}
	if tags[0] != "Agent工程/MCP" {
		t.Fatalf("unexpected tag: %v", tags[0])
	}
}

func TestParseTagsFallbackToDefault(t *testing.T) {
	content := `
# 仅标题

这是没有文末标签的内容。
`
	tags := parseTags(content)
	if len(tags) != 1 {
		t.Fatalf("expected 1 tag, got %d", len(tags))
	}
	if tags[0] != "未分类" {
		t.Fatalf("unexpected fallback tag: %v", tags[0])
	}
}

func TestArticleWriteAPIsDisabled(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.toml")
	if err := os.WriteFile(configPath, []byte("[site]\ntitle=\"Blog\"\n"), 0644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	server, err := NewServer(ServerOptions{
		ConfigPath: configPath,
		DataDir:    tempDir,
		WebDir:     "",
	})
	if err != nil {
		t.Fatalf("create server failed: %v", err)
	}

	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/articles", bytes.NewReader([]byte(`{"title":"x"}`)))
	createRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusNotFound {
		t.Fatalf("create article api should be removed, expected 404, got %d", createRec.Code)
	}

	updateReq := httptest.NewRequest(http.MethodPut, "/api/v1/articles/demo-id", bytes.NewReader([]byte(`{"title":"x"}`)))
	updateRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(updateRec, updateReq)
	if updateRec.Code != http.StatusNotFound {
		t.Fatalf("update article api should be removed, expected 404, got %d", updateRec.Code)
	}

	moveReq := httptest.NewRequest(http.MethodPatch, "/api/v1/articles/demo-id/move", bytes.NewReader([]byte(`{"parent_id":"A/B"}`)))
	moveRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(moveRec, moveReq)
	if moveRec.Code != http.StatusNotFound {
		t.Fatalf("move article api should be removed, expected 404, got %d", moveRec.Code)
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/v1/articles/demo-id", nil)
	deleteRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusNotFound {
		t.Fatalf("delete article api should be removed, expected 404, got %d", deleteRec.Code)
	}
}

func TestFolderSyncBuildsIDAndUpdatesSQLite(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.toml")
	if err := os.WriteFile(configPath, []byte("[site]\ntitle=\"Blog\"\n"), 0644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}
	seedMarkdownNote(t, tempDir, "Agent工程/同步测试", "文件夹新增笔记", "这是文件夹新增的内容")

	server, err := NewServer(ServerOptions{
		ConfigPath: configPath,
		DataDir:    tempDir,
		WebDir:     "",
	})
	if err != nil {
		t.Fatalf("create server failed: %v", err)
	}

	articleID := findArticleIDByPathTitle(t, server.store, "Agent工程/同步测试", "文件夹新增笔记")
	if strings.TrimSpace(articleID) == "" {
		t.Fatalf("article id should not be empty")
	}

	var mappedID string
	err = server.store.db.QueryRow(
		"SELECT id FROM note_identity_map WHERE note_path = ? AND note_title = ?",
		"Agent工程/同步测试",
		"文件夹新增笔记",
	).Scan(&mappedID)
	if err != nil {
		t.Fatalf("query identity map failed: %v", err)
	}
	if mappedID != articleID {
		t.Fatalf("identity map mismatch, expected=%s, actual=%s", articleID, mappedID)
	}

	var metadataCount int
	err = server.store.db.QueryRow(
		"SELECT COUNT(1) FROM note_metadata WHERE id = ? AND path = ? AND title = ?",
		articleID,
		"Agent工程/同步测试",
		"文件夹新增笔记",
	).Scan(&metadataCount)
	if err != nil {
		t.Fatalf("query metadata failed: %v", err)
	}
	if metadataCount != 1 {
		t.Fatalf("metadata should be synced from folder scan")
	}
}

func TestFolderSyncDetectsNewFileAfterStartup(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.toml")
	if err := os.WriteFile(configPath, []byte("[site]\ntitle=\"Blog\"\n"), 0644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}
	seedMarkdownNote(t, tempDir, "Agent工程/启动前", "已有笔记", "启动前已有内容")

	server, err := NewServer(ServerOptions{
		ConfigPath: configPath,
		DataDir:    tempDir,
		WebDir:     "",
	})
	if err != nil {
		t.Fatalf("create server failed: %v", err)
	}

	seedMarkdownNote(t, tempDir, "Agent工程/启动后新增", "新笔记", "这是新增文件")
	if err = server.store.scanAndSync(); err != nil {
		t.Fatalf("scan and sync failed: %v", err)
	}

	newID := findArticleIDByPathTitle(t, server.store, "Agent工程/启动后新增", "新笔记")
	if strings.TrimSpace(newID) == "" {
		t.Fatalf("new note id should not be empty")
	}

	var count int
	if err = server.store.db.QueryRow(
		"SELECT COUNT(1) FROM note_identity_map WHERE id = ? AND note_path = ? AND note_title = ?",
		newID,
		"Agent工程/启动后新增",
		"新笔记",
	).Scan(&count); err != nil {
		t.Fatalf("query identity map failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("new note should sync into identity map")
	}
}

func TestListTagTree(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.toml")
	if err := os.WriteFile(configPath, []byte("[site]\ntitle=\"Blog\"\n"), 0644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}
	seedMarkdownNote(t, tempDir, "Agent工程/后端/Go", "Go 工程实践", "后端文章")
	seedMarkdownNote(t, tempDir, "Agent工程/前端/React", "React 工程实践", "前端文章")

	server, err := NewServer(ServerOptions{
		ConfigPath: configPath,
		DataDir:    tempDir,
		WebDir:     "",
	})
	if err != nil {
		t.Fatalf("create server failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tags/tree", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("tags tree expected 200, got %d", rec.Code)
	}

	var tree []TagTreeNode
	if err = json.Unmarshal(rec.Body.Bytes(), &tree); err != nil {
		t.Fatalf("parse tag tree failed: %v", err)
	}
	if len(tree) != 1 {
		t.Fatalf("expected 1 root tag, got %d", len(tree))
	}
	root := tree[0]
	if root.Path != "Agent工程" || root.Name != "Agent工程" {
		t.Fatalf("unexpected root tag: %+v", root)
	}
	if root.Level != 1 {
		t.Fatalf("root level should be 1, got %d", root.Level)
	}
	if len(root.Children) == 0 {
		t.Fatalf("root children should not be empty")
	}

	hasBackend := false
	hasFrontend := false
	for _, child := range root.Children {
		if child.Path == "Agent工程/后端" && child.Name == "后端" {
			hasBackend = true
		}
		if child.Path == "Agent工程/前端" && child.Name == "前端" {
			hasFrontend = true
		}
	}
	if !hasBackend || !hasFrontend {
		t.Fatalf("expected backend and frontend tags, hasBackend=%v, hasFrontend=%v", hasBackend, hasFrontend)
	}
}

func TestRecentArticlesAndTimestamps(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.toml")
	if err := os.WriteFile(configPath, []byte("[site]\ntitle=\"Blog\"\n"), 0644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}
	seedMarkdownNote(t, tempDir, "技术笔记/最近更新", "最近更新文章", "最近更新测试正文")

	server, err := NewServer(ServerOptions{
		ConfigPath: configPath,
		DataDir:    tempDir,
		WebDir:     "",
	})
	if err != nil {
		t.Fatalf("create server failed: %v", err)
	}
	createdID := findArticleIDByPathTitle(t, server.store, "技术笔记/最近更新", "最近更新文章")

	listReq1 := httptest.NewRequest(http.MethodGet, "/api/v1/articles/recent?limit=5", nil)
	listRec1 := httptest.NewRecorder()
	server.Handler().ServeHTTP(listRec1, listReq1)
	if listRec1.Code != http.StatusOK {
		t.Fatalf("list recent expected 200, got %d", listRec1.Code)
	}

	var recent1 []map[string]any
	if err = json.Unmarshal(listRec1.Body.Bytes(), &recent1); err != nil {
		t.Fatalf("parse recent list failed: %v", err)
	}
	if len(recent1) == 0 {
		t.Fatalf("recent list should not be empty")
	}

	var createdAtBefore string
	var updatedAtBefore string
	for _, item := range recent1 {
		if item["id"] == createdID {
			createdAtBefore, _ = item["created_at"].(string)
			updatedAtBefore, _ = item["updated_at"].(string)
			break
		}
	}
	if createdAtBefore == "" || updatedAtBefore == "" {
		t.Fatalf("created_at and updated_at should exist on recent article")
	}

	time.Sleep(2 * time.Second)

	noteFile := filepath.Join(tempDir, "notes", "技术笔记", "最近更新", "最近更新文章.md")
	updatedContent := "# 最近更新文章\n\n最近更新测试正文-改\n\n#技术笔记/最近更新\n"
	if err = os.WriteFile(noteFile, []byte(updatedContent), 0644); err != nil {
		t.Fatalf("update markdown failed: %v", err)
	}
	if err = server.store.scanAndSync(); err != nil {
		t.Fatalf("scan and sync failed after markdown update: %v", err)
	}

	listReq2 := httptest.NewRequest(http.MethodGet, "/api/v1/articles/recent?limit=5", nil)
	listRec2 := httptest.NewRecorder()
	server.Handler().ServeHTTP(listRec2, listReq2)
	if listRec2.Code != http.StatusOK {
		t.Fatalf("list recent expected 200, got %d", listRec2.Code)
	}

	var recent2 []map[string]any
	if err = json.Unmarshal(listRec2.Body.Bytes(), &recent2); err != nil {
		t.Fatalf("parse recent list failed: %v", err)
	}

	var createdAtAfter string
	var updatedAtAfter string
	for _, item := range recent2 {
		if item["id"] == createdID {
			createdAtAfter, _ = item["created_at"].(string)
			updatedAtAfter, _ = item["updated_at"].(string)
			break
		}
	}
	if createdAtAfter == "" || updatedAtAfter == "" {
		t.Fatalf("created_at and updated_at should exist after update")
	}
	if createdAtBefore != createdAtAfter {
		t.Fatalf("created_at should keep stable, before=%s, after=%s", createdAtBefore, createdAtAfter)
	}
	if updatedAtAfter == updatedAtBefore {
		t.Fatalf("updated_at should change after update")
	}
}

func TestArticleDetailContainsRenderedHTML(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.toml")
	if err := os.WriteFile(configPath, []byte("[site]\ntitle=\"Blog\"\n"), 0644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}
	seedMarkdownNote(t, tempDir, "Agent工程/渲染缓存", "渲染缓存测试", "正文内容\n\n- 列表项")

	server, err := NewServer(ServerOptions{
		ConfigPath: configPath,
		DataDir:    tempDir,
		WebDir:     "",
	})
	if err != nil {
		t.Fatalf("create server failed: %v", err)
	}

	articleID := findArticleIDByPathTitle(t, server.store, "Agent工程/渲染缓存", "渲染缓存测试")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/articles/"+articleID, nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var payload map[string]any
	if err = json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal payload failed: %v", err)
	}
	articleMap, ok := payload["article"].(map[string]any)
	if !ok {
		t.Fatalf("article payload missing")
	}
	rendered, _ := articleMap["rendered_html"].(string)
	if strings.TrimSpace(rendered) == "" {
		t.Fatalf("rendered_html should not be empty")
	}
}

func TestArticleDetailETagInvalidationAfterMarkdownChange(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.toml")
	if err := os.WriteFile(configPath, []byte("[site]\ntitle=\"Blog\"\n"), 0644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}
	tagPath := "Agent工程/失效机制"
	title := "失效机制测试"
	seedMarkdownNote(t, tempDir, tagPath, title, "第一版内容")

	server, err := NewServer(ServerOptions{
		ConfigPath: configPath,
		DataDir:    tempDir,
		WebDir:     "",
	})
	if err != nil {
		t.Fatalf("create server failed: %v", err)
	}
	articleID := findArticleIDByPathTitle(t, server.store, tagPath, title)

	firstReq := httptest.NewRequest(http.MethodGet, "/api/v1/articles/"+articleID, nil)
	firstRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(firstRec, firstReq)
	if firstRec.Code != http.StatusOK {
		t.Fatalf("first request expected 200, got %d", firstRec.Code)
	}
	firstETag := strings.TrimSpace(firstRec.Header().Get("ETag"))
	if firstETag == "" {
		t.Fatalf("first etag should not be empty")
	}

	secondReq := httptest.NewRequest(http.MethodGet, "/api/v1/articles/"+articleID, nil)
	secondReq.Header.Set("If-None-Match", firstETag)
	secondRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(secondRec, secondReq)
	if secondRec.Code != http.StatusNotModified {
		t.Fatalf("second request expected 304, got %d", secondRec.Code)
	}

	noteFile := filepath.Join(tempDir, "notes", filepath.FromSlash(tagPath), title+".md")
	updatedBody := "# " + title + "\n\n第二版内容\n\n#" + tagPath + "\n"
	if err = os.WriteFile(noteFile, []byte(updatedBody), 0644); err != nil {
		t.Fatalf("write updated markdown failed: %v", err)
	}
	if err = server.store.scanAndSync(); err != nil {
		t.Fatalf("scan and sync failed after markdown update: %v", err)
	}

	thirdReq := httptest.NewRequest(http.MethodGet, "/api/v1/articles/"+articleID, nil)
	thirdReq.Header.Set("If-None-Match", firstETag)
	thirdRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(thirdRec, thirdReq)
	if thirdRec.Code != http.StatusOK {
		t.Fatalf("third request expected 200 after update, got %d", thirdRec.Code)
	}
	thirdETag := strings.TrimSpace(thirdRec.Header().Get("ETag"))
	if thirdETag == "" {
		t.Fatalf("third etag should not be empty")
	}
	if thirdETag == firstETag {
		t.Fatalf("etag should change after markdown content update")
	}
}

func TestArticleIDStableWhenContentChanges(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.toml")
	if err := os.WriteFile(configPath, []byte("[site]\ntitle=\"Blog\"\n"), 0644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}
	tagPath := "Agent工程/固定ID"
	title := "固定ID测试"
	seedMarkdownNote(t, tempDir, tagPath, title, "第一版正文内容")

	server, err := NewServer(ServerOptions{
		ConfigPath: configPath,
		DataDir:    tempDir,
		WebDir:     "",
	})
	if err != nil {
		t.Fatalf("create server failed: %v", err)
	}

	oldID := findArticleIDByPathTitle(t, server.store, tagPath, title)
	if strings.TrimSpace(oldID) == "" {
		t.Fatalf("old id should not be empty")
	}

	noteFile := filepath.Join(tempDir, "notes", filepath.FromSlash(tagPath), title+".md")
	updatedBody := "# 这是新的一级标题（文件名不变）\n\n第二版正文内容\n\n#" + tagPath + "\n"
	if err = os.WriteFile(noteFile, []byte(updatedBody), 0644); err != nil {
		t.Fatalf("write updated markdown failed: %v", err)
	}
	if err = server.store.scanAndSync(); err != nil {
		t.Fatalf("scan and sync failed after content update: %v", err)
	}

	newID := findArticleIDByPathTitle(t, server.store, tagPath, title)
	if newID != oldID {
		t.Fatalf("id should stay stable when title unchanged, old=%s, new=%s", oldID, newID)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/articles/"+oldID, nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("old shared link should still work, expected 200 got %d", rec.Code)
	}
}

func TestArticleIDStableWhenMarkdownMovedToAnotherPath(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.toml")
	if err := os.WriteFile(configPath, []byte("[site]\ntitle=\"Blog\"\n"), 0644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}
	oldPath := "Agent工程/原目录"
	newPath := "Agent工程/新目录"
	title := "移动后ID保持"
	seedMarkdownNote(t, tempDir, oldPath, title, "原目录正文")

	server, err := NewServer(ServerOptions{
		ConfigPath: configPath,
		DataDir:    tempDir,
		WebDir:     "",
	})
	if err != nil {
		t.Fatalf("create server failed: %v", err)
	}

	oldID := findArticleIDByPathTitle(t, server.store, oldPath, title)
	if strings.TrimSpace(oldID) == "" {
		t.Fatalf("old id should not be empty")
	}

	oldFile := filepath.Join(tempDir, "notes", filepath.FromSlash(oldPath), title+".md")
	newDir := filepath.Join(tempDir, "notes", filepath.FromSlash(newPath))
	if err = os.MkdirAll(newDir, 0755); err != nil {
		t.Fatalf("mkdir new dir failed: %v", err)
	}
	newFile := filepath.Join(newDir, title+".md")
	if err = os.Rename(oldFile, newFile); err != nil {
		t.Fatalf("move markdown failed: %v", err)
	}
	if err = server.store.scanAndSync(); err != nil {
		t.Fatalf("scan and sync after move failed: %v", err)
	}

	newID := findArticleIDByPathTitle(t, server.store, newPath, title)
	if newID != oldID {
		t.Fatalf("id should stay stable when note moved, old=%s, new=%s", oldID, newID)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/articles/"+oldID, nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("old shared link should still work after move, expected 200 got %d", rec.Code)
	}
}

func TestRecentArticlesSlimPayloadWithoutContent(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.toml")
	if err := os.WriteFile(configPath, []byte("[site]\ntitle=\"Blog\"\n"), 0644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}
	seedMarkdownNote(t, tempDir, "Agent工程/列表瘦身", "瘦身测试", "这是正文，列表不应携带完整内容")

	server, err := NewServer(ServerOptions{
		ConfigPath: configPath,
		DataDir:    tempDir,
		WebDir:     "",
	})
	if err != nil {
		t.Fatalf("create server failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/articles/recent?limit=10", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var items []map[string]any
	if err = json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
		t.Fatalf("unmarshal recent payload failed: %v", err)
	}
	if len(items) == 0 {
		t.Fatalf("recent list should not be empty")
	}
	if _, exists := items[0]["content"]; exists {
		t.Fatalf("recent payload should not include content field")
	}
}

func TestSQLitePragmasApplied(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.toml")
	if err := os.WriteFile(configPath, []byte("[site]\ntitle=\"Blog\"\n"), 0644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	server, err := NewServer(ServerOptions{
		ConfigPath: configPath,
		DataDir:    tempDir,
		WebDir:     "",
	})
	if err != nil {
		t.Fatalf("create server failed: %v", err)
	}

	var journalMode string
	if err = server.store.db.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("query journal_mode failed: %v", err)
	}
	if !strings.EqualFold(strings.TrimSpace(journalMode), "wal") {
		t.Fatalf("journal_mode should be WAL, got %q", journalMode)
	}

	var busyTimeout int
	if err = server.store.db.QueryRow("PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
		t.Fatalf("query busy_timeout failed: %v", err)
	}
	if busyTimeout < 5000 {
		t.Fatalf("busy_timeout should be >= 5000, got %d", busyTimeout)
	}
}

func TestAutoScanRemovesDeletedMarkdownData(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.toml")
	if err := os.WriteFile(configPath, []byte("[site]\ntitle=\"Blog\"\n"), 0644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	noteDir := filepath.Join(tempDir, "notes", "Agent工程", "自动扫描")
	if err := os.MkdirAll(noteDir, 0755); err != nil {
		t.Fatalf("mkdir note dir failed: %v", err)
	}
	noteTitle := "自动扫描测试"
	noteContent := "# 自动扫描测试\n\n这是一篇临时笔记。\n\n#Agent工程/自动扫描\n"
	if err := os.WriteFile(filepath.Join(noteDir, noteTitle+".md"), []byte(noteContent), 0644); err != nil {
		t.Fatalf("write note failed: %v", err)
	}

	server, err := NewServer(ServerOptions{
		ConfigPath: configPath,
		DataDir:    tempDir,
		WebDir:     "",
	})
	if err != nil {
		t.Fatalf("create server failed: %v", err)
	}
	articleID := ""
	articles, err := server.store.ListArticles()
	if err != nil {
		t.Fatalf("list articles failed: %v", err)
	}
	for _, item := range articles {
		if item.Path == "Agent工程/自动扫描" && item.Title == noteTitle {
			articleID = item.ID
			break
		}
	}
	if articleID == "" {
		t.Fatalf("expected article id for %s", noteTitle)
	}

	createCommentPayload := map[string]any{
		"article_id": articleID,
		"author":     "测试用户",
		"content":    "临时评论",
	}
	_ = createComment(t, server.Handler(), createCommentPayload, http.StatusCreated)

	beforeComments, err := server.store.ListComments(articleID)
	if err != nil {
		t.Fatalf("list comments before delete failed: %v", err)
	}
	if len(beforeComments) == 0 {
		t.Fatalf("expected comments before markdown deletion")
	}

	noteFile, err := server.store.locateNoteFile(articleID)
	if err != nil {
		t.Fatalf("locate note file failed: %v", err)
	}
	if err = os.Remove(noteFile); err != nil {
		t.Fatalf("remove markdown failed: %v", err)
	}
	if err = server.store.scanAndSync(); err != nil {
		t.Fatalf("scan and sync failed: %v", err)
	}

	if _, err = server.store.GetArticle(articleID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected article removed after markdown deletion, got err=%v", err)
	}

	afterComments, err := server.store.ListComments(articleID)
	if err != nil {
		t.Fatalf("list comments after delete failed: %v", err)
	}
	if len(afterComments) != 0 {
		t.Fatalf("comments should be removed after markdown deletion, got %d", len(afterComments))
	}

	var commentCount int
	if err = server.store.db.QueryRow("SELECT COUNT(1) FROM comments WHERE article_id = ?", articleID).Scan(&commentCount); err != nil {
		t.Fatalf("query comments count failed: %v", err)
	}
	if commentCount != 0 {
		t.Fatalf("expected comment rows to be deleted, got %d", commentCount)
	}

	tree, err := server.store.ListArticleTree()
	if err != nil {
		t.Fatalf("list tree failed: %v", err)
	}
	if len(tree) != 0 {
		t.Fatalf("expected empty tag tree after markdown deletion, got %d root nodes", len(tree))
	}
	var tagRows int
	if err = server.store.db.QueryRow("SELECT COUNT(1) FROM note_tag_hierarchy").Scan(&tagRows); err != nil {
		t.Fatalf("query tag hierarchy count failed: %v", err)
	}
	if tagRows != 0 {
		t.Fatalf("expected tag hierarchy rows to be deleted, got %d", tagRows)
	}
}

func seedMarkdownNote(t *testing.T, dataDir string, tagPath string, title string, content string) {
	t.Helper()
	dir := filepath.Join(dataDir, "notes", filepath.FromSlash(tagPath))
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir note dir failed: %v", err)
	}
	body := "# " + title + "\n\n" + content + "\n\n#" + tagPath + "\n"
	noteFile := filepath.Join(dir, title+".md")
	if err := os.WriteFile(noteFile, []byte(body), 0644); err != nil {
		t.Fatalf("write note failed: %v", err)
	}
}

func findArticleIDByPathTitle(t *testing.T, store *Store, path string, title string) string {
	t.Helper()
	articles, err := store.ListArticles()
	if err != nil {
		t.Fatalf("list articles failed: %v", err)
	}
	for _, item := range articles {
		if item.Path == path && item.Title == title {
			return item.ID
		}
	}
	t.Fatalf("cannot find article by path=%s title=%s", path, title)
	return ""
}
