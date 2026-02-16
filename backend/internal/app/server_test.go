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

func TestArticleCRUDAndTree(t *testing.T) {
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

	createBody := map[string]any{
		"parent_id":    "",
		"title":        "新文章",
		"slug":         "new-article",
		"category":     "技术笔记",
		"path":         "技术笔记/自动化",
		"excerpt":      "摘要",
		"content":      "正文",
		"published_at": "2026-02-16",
		"read_minutes": 5,
		"views":        10,
		"order_index":  1,
	}
	createPayload, _ := json.Marshal(createBody)
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/articles", bytes.NewReader(createPayload))
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create article expected 201, got %d", createRec.Code)
	}
	var created Article
	if err = json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("parse create response failed: %v", err)
	}
	if created.ID == "" {
		t.Fatalf("created article id should not be empty")
	}

	var mappedID string
	err = server.store.db.QueryRow(
		"SELECT id FROM note_identity_map WHERE note_path = ? AND note_title = ?",
		"技术笔记/自动化",
		"新文章",
	).Scan(&mappedID)
	if err != nil {
		t.Fatalf("query identity map failed: %v", err)
	}
	if mappedID != created.ID {
		t.Fatalf("identity map id mismatch, expected=%s, actual=%s", created.ID, mappedID)
	}
	var tagCount int
	err = server.store.db.QueryRow("SELECT COUNT(1) FROM note_tag_hierarchy WHERE tag_path = ?", "技术笔记/自动化").Scan(&tagCount)
	if err != nil {
		t.Fatalf("query tag hierarchy failed: %v", err)
	}
	if tagCount != 1 {
		t.Fatalf("expected tag hierarchy to contain 技术笔记/自动化")
	}

	treeReq := httptest.NewRequest(http.MethodGet, "/api/v1/articles/tree", nil)
	treeRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(treeRec, treeReq)
	if treeRec.Code != http.StatusOK {
		t.Fatalf("tree expected 200, got %d", treeRec.Code)
	}

	var treeData []map[string]any
	if err := json.Unmarshal(treeRec.Body.Bytes(), &treeData); err != nil {
		t.Fatalf("tree parse failed: %v", err)
	}
	if len(treeData) == 0 {
		t.Fatalf("tree should contain at least one article")
	}

	updateBody := map[string]any{
		"title":        "新文章-已更新",
		"slug":         "new-article",
		"category":     "技术笔记",
		"excerpt":      "更新摘要",
		"content":      "更新正文",
		"published_at": "2026-02-16",
		"read_minutes": 6,
		"views":        11,
		"order_index":  2,
	}
	updatePayload, _ := json.Marshal(updateBody)
	updateReq := httptest.NewRequest(http.MethodPut, "/api/v1/articles/"+created.ID, bytes.NewReader(updatePayload))
	updateReq.Header.Set("Content-Type", "application/json")
	updateRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(updateRec, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update article expected 200, got %d", updateRec.Code)
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/v1/articles/"+created.ID, nil)
	deleteRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusNoContent {
		t.Fatalf("delete article expected 204, got %d", deleteRec.Code)
	}
}

func TestRecentArticlesAndTimestamps(t *testing.T) {
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

	createBody := map[string]any{
		"title":       "最近更新文章",
		"category":    "技术笔记",
		"path":        "技术笔记/最近更新",
		"excerpt":     "最近更新测试摘要",
		"content":     "最近更新测试正文",
		"order_index": 1,
	}
	createPayload, _ := json.Marshal(createBody)
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/articles", bytes.NewReader(createPayload))
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create article expected 201, got %d", createRec.Code)
	}
	var created Article
	if err = json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("parse create response failed: %v", err)
	}
	if created.ID == "" {
		t.Fatalf("created article id should not be empty")
	}

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
		if item["id"] == created.ID {
			createdAtBefore, _ = item["created_at"].(string)
			updatedAtBefore, _ = item["updated_at"].(string)
			break
		}
	}
	if createdAtBefore == "" || updatedAtBefore == "" {
		t.Fatalf("created_at and updated_at should exist on recent article")
	}

	time.Sleep(2 * time.Second)

	updateBody := map[string]any{
		"title":       "最近更新文章-改",
		"category":    "技术笔记",
		"path":        "技术笔记/最近更新",
		"excerpt":     "最近更新测试摘要-改",
		"content":     "最近更新测试正文-改",
		"order_index": 1,
	}
	updatePayload, _ := json.Marshal(updateBody)
	updateReq := httptest.NewRequest(http.MethodPut, "/api/v1/articles/"+created.ID, bytes.NewReader(updatePayload))
	updateReq.Header.Set("Content-Type", "application/json")
	updateRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(updateRec, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update article expected 200, got %d", updateRec.Code)
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
		if item["id"] == created.ID {
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
