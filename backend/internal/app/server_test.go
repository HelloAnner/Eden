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
