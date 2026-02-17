// Package app 定义 HTTP Server 并暴露博客 REST 接口。
// Author: Codex
// Created: 2026-02-16
package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type ServerOptions struct {
	ConfigPath         string
	DataDir            string
	WebDir             string
	CommentCreateLimit int
	CommentLikeLimit   int
	RateLimitWindow    time.Duration
}

type Server struct {
	store       *Store
	configPath  string
	config      BlogConfig
	webDir      string
	mailer      *SubscriptionMailer
	mux         *http.ServeMux
	limiter     *ActionLimiter
	createLimit int
	likeLimit   int
	rateWindow  time.Duration
}

var hashedAssetPattern = regexp.MustCompile(`-[0-9A-Za-z_-]{8,}\.`)

func NewServer(options ServerOptions) (*Server, error) {
	config, err := loadConfig(options.ConfigPath)
	if err != nil {
		return nil, err
	}

	dbPath := filepath.Join(options.DataDir, "blog.db")
	scanInterval := resolveScanInterval(config)
	store, err := openStore(dbPath, StoreOptions{
		ScanInterval: scanInterval,
	})
	if err != nil {
		return nil, err
	}

	server := &Server{
		store:       store,
		configPath:  options.ConfigPath,
		config:      config,
		webDir:      options.WebDir,
		mux:         http.NewServeMux(),
		limiter:     NewActionLimiter(),
		createLimit: defaultIfZero(options.CommentCreateLimit, 15),
		likeLimit:   defaultIfZero(options.CommentLikeLimit, 60),
		rateWindow:  defaultDuration(options.RateLimitWindow, time.Minute),
	}
	server.mailer = NewSubscriptionMailer(config, store)
	store.SetOnNewArticles(server.handleNewArticlesDetected)
	LogInfof("server", "server initialized, data_dir=%s, scan_interval=%s", options.DataDir, scanInterval)
	server.registerRoutes()
	return server, nil
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("GET /api/v1/health", s.handleHealth)
	s.mux.HandleFunc("GET /api/v1/config", s.handleGetConfig)
	s.mux.HandleFunc("PUT /api/v1/config", s.handleUpdateConfig)
	s.mux.HandleFunc("GET /api/v1/articles", s.handleListArticles)
	s.mux.HandleFunc("GET /api/v1/articles/recent", s.handleListRecentArticles)
	s.mux.HandleFunc("GET /api/v1/articles/tree", s.handleListArticleTree)
	s.mux.HandleFunc("GET /api/v1/tags/tree", s.handleListTagTree)
	s.mux.HandleFunc("GET /api/v1/articles/search", s.handleSearchArticles)
	s.mux.HandleFunc("GET /api/v1/articles/{id}", s.handleGetArticle)
	s.mux.HandleFunc("GET /api/v1/comments", s.handleListComments)
	s.mux.HandleFunc("POST /api/v1/comments", s.handleCreateComment)
	s.mux.HandleFunc("POST /api/v1/comments/{id}/like", s.handleLikeComment)
	s.mux.HandleFunc("GET /api/v1/data/{path...}", s.handleGetDataAsset)
	s.mux.HandleFunc("POST /api/v1/subscribe", s.handleSubscribe)
	s.mux.HandleFunc("GET /api/v1/profile/stats", s.handleProfileStats)
	s.mux.HandleFunc("/", s.handleSPA)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	writeCachedJSON(w, r, s.config, "public, max-age=300, must-revalidate")
}

func (s *Server) handleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	var payload BlogConfig
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		LogWarnf("server", "update config payload decode failed: %v", err)
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := saveConfig(s.configPath, payload); err != nil {
		LogErrorf("server", "save config failed: %v", err)
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.config = payload
	scanInterval := resolveScanInterval(payload)
	s.store.SetScanInterval(scanInterval)
	SetLogLevel(payload.Logging.Level)
	if s.mailer != nil {
		s.mailer.UpdateConfig(payload)
	}
	LogInfof("server", "config updated, scan_interval=%s, log_level=%s", scanInterval, payload.Logging.Level)
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) handleNewArticlesDetected(articles []Article) {
	if len(articles) == 0 {
		return
	}
	LogInfof("server", "detected %d new articles", len(articles))
	if s.mailer == nil {
		LogWarnf("server", "subscription mailer not initialized")
		return
	}
	s.mailer.EnqueueNewArticles(articles)
}

func (s *Server) handleListArticles(w http.ResponseWriter, r *http.Request) {
	articles, err := s.store.ListArticles()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeCachedJSON(w, r, articles, "no-cache, must-revalidate")
}

func (s *Server) handleListRecentArticles(w http.ResponseWriter, r *http.Request) {
	limit := 20
	limitRaw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if limitRaw != "" {
		parsed, err := strconv.Atoi(limitRaw)
		if err != nil || parsed <= 0 {
			writeErrorMessage(w, http.StatusBadRequest, "invalid limit")
			return
		}
		limit = parsed
	}

	articles, err := s.store.ListRecentArticles(limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeCachedJSON(w, r, articles, "no-cache, must-revalidate")
}

func (s *Server) handleListArticleTree(w http.ResponseWriter, r *http.Request) {
	tree, err := s.store.ListArticleTree()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeCachedJSON(w, r, tree, "no-cache, must-revalidate")
}

func (s *Server) handleListTagTree(w http.ResponseWriter, r *http.Request) {
	tree, err := s.store.ListTagTree()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeCachedJSON(w, r, tree, "no-cache, must-revalidate")
}

func (s *Server) handleSearchArticles(w http.ResponseWriter, r *http.Request) {
	keyword := strings.TrimSpace(r.URL.Query().Get("q"))
	articles, err := s.store.SearchArticles(keyword)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeCachedJSON(w, r, map[string]any{
		"keyword": keyword,
		"items":   articles,
		"total":   len(articles),
	}, "no-cache, must-revalidate")
}

func (s *Server) handleGetArticle(w http.ResponseWriter, r *http.Request) {
	articleID := r.PathValue("id")
	article, err := s.store.GetArticle(articleID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	comments, err := s.store.ListComments(articleID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	etag := buildArticleETag(article, comments)
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache, must-revalidate")
	if etagMatched(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if err = s.store.IncrementView(articleID); err == nil {
		article.Views++
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"article":  article,
		"comments": comments,
	})
}

func (s *Server) handleCreateArticle(w http.ResponseWriter, r *http.Request) {
	var payload Article
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	created, err := s.store.CreateArticle(payload)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleUpdateArticle(w http.ResponseWriter, r *http.Request) {
	articleID := r.PathValue("id")
	var payload Article
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.store.UpdateArticle(articleID, payload); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": articleID, "status": "updated"})
}

func (s *Server) handleMoveArticle(w http.ResponseWriter, r *http.Request) {
	articleID := r.PathValue("id")
	var payload MoveArticleRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.store.MoveArticle(articleID, payload); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": articleID, "status": "moved"})
}

func (s *Server) handleDeleteArticle(w http.ResponseWriter, r *http.Request) {
	articleID := r.PathValue("id")
	if err := s.store.DeleteArticle(articleID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeError(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListComments(w http.ResponseWriter, r *http.Request) {
	articleID := r.URL.Query().Get("article_id")
	comments, err := s.store.ListComments(articleID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeCachedJSON(w, r, comments, "no-cache, must-revalidate")
}

func (s *Server) handleCreateComment(w http.ResponseWriter, r *http.Request) {
	client := clientIP(r)
	if !s.limiter.Allow("comment:"+client, s.createLimit, s.rateWindow) {
		writeErrorMessage(w, http.StatusTooManyRequests, "too many comment requests")
		return
	}

	var payload Comment
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(payload.ArticleID) == "" || strings.TrimSpace(payload.Content) == "" {
		writeErrorMessage(w, http.StatusBadRequest, "article_id and content are required")
		return
	}
	if len([]rune(payload.Content)) > 3000 {
		writeErrorMessage(w, http.StatusBadRequest, "comment content too long")
		return
	}
	comment, err := s.store.AddComment(payload)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, comment)
}

func (s *Server) handleLikeComment(w http.ResponseWriter, r *http.Request) {
	client := clientIP(r)
	if !s.limiter.Allow("like:"+client, s.likeLimit, s.rateWindow) {
		writeErrorMessage(w, http.StatusTooManyRequests, "too many like requests")
		return
	}

	commentID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || commentID <= 0 {
		writeErrorMessage(w, http.StatusBadRequest, "invalid comment id")
		return
	}

	updated, err := s.store.AddCommentLike(commentID, client)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleGetDataAsset(w http.ResponseWriter, r *http.Request) {
	relativePath := strings.TrimSpace(r.PathValue("path"))
	if relativePath == "" {
		writeErrorMessage(w, http.StatusBadRequest, "asset path is required")
		return
	}

	fullPath, err := resolveDataAssetPath(s.store, relativePath)
	if err != nil {
		writeErrorMessage(w, http.StatusBadRequest, err.Error())
		return
	}

	info, err := os.Stat(fullPath)
	if err != nil || info.IsDir() {
		writeErrorMessage(w, http.StatusNotFound, "asset not found")
		return
	}
	etag := buildFileETag(info)
	w.Header().Set("ETag", etag)
	applyDataAssetCacheHeaders(w, fullPath)
	if etagMatched(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	http.ServeFile(w, r, fullPath)
}

func (s *Server) handleSubscribe(w http.ResponseWriter, r *http.Request) {
	var payload SubscribeRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		LogWarnf("server", "subscribe payload decode failed: %v", err)
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.store.AddSubscriber(payload.Email); err != nil {
		LogWarnf("server", "subscribe failed, email=%s, err=%v", payload.Email, err)
		writeError(w, http.StatusBadRequest, err)
		return
	}
	LogInfof("server", "new subscription accepted, email=%s", payload.Email)
	writeJSON(w, http.StatusCreated, map[string]string{"email": payload.Email, "status": "subscribed"})
}

func (s *Server) handleProfileStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.store.ProfileStats()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeCachedJSON(w, r, stats, "no-cache, must-revalidate")
}

func (s *Server) handleSPA(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeErrorMessage(w, http.StatusNotFound, "not found")
		return
	}
	if strings.TrimSpace(s.webDir) == "" {
		writeErrorMessage(w, http.StatusNotFound, "web assets not configured")
		return
	}

	cleanPath := filepath.Clean(r.URL.Path)
	if cleanPath == "." || cleanPath == "/" {
		applyNoCacheHeaders(w)
		http.ServeFile(w, r, filepath.Join(s.webDir, "index.html"))
		return
	}

	target := filepath.Join(s.webDir, cleanPath)
	if stat, err := os.Stat(target); err == nil && !stat.IsDir() {
		applySPACacheHeaders(w, cleanPath)
		if tryServePrecompressedAsset(w, r, cleanPath, target) {
			return
		}
		etag := buildFileETag(stat)
		w.Header().Set("ETag", etag)
		if etagMatched(r.Header.Get("If-None-Match"), etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		http.ServeFile(w, r, target)
		return
	}
	applyNoCacheHeaders(w)
	http.ServeFile(w, r, filepath.Join(s.webDir, "index.html"))
}

func writeJSON(w http.ResponseWriter, code int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeCachedJSON(w http.ResponseWriter, r *http.Request, payload any, cacheControl string) {
	body, err := json.Marshal(payload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	etag := buildBytesETag(body)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if strings.TrimSpace(cacheControl) != "" {
		w.Header().Set("Cache-Control", cacheControl)
	}
	w.Header().Set("ETag", etag)
	if etagMatched(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func writeError(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

func writeErrorMessage(w http.ResponseWriter, code int, message string) {
	writeJSON(w, code, map[string]string{"error": message})
}

func defaultIfZero(value int, defaultValue int) int {
	if value <= 0 {
		return defaultValue
	}
	return value
}

func defaultDuration(value time.Duration, defaultValue time.Duration) time.Duration {
	if value <= 0 {
		return defaultValue
	}
	return value
}

func resolveScanInterval(config BlogConfig) time.Duration {
	seconds := config.System.ScanIntervalSeconds
	if seconds <= 0 {
		seconds = 60
	}
	return time.Duration(seconds) * time.Second
}

func clientIP(r *http.Request) string {
	forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
	if forwarded != "" {
		parts := strings.Split(forwarded, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}
	realIP := strings.TrimSpace(r.Header.Get("X-Real-IP"))
	if realIP != "" {
		return realIP
	}
	remote := strings.TrimSpace(r.RemoteAddr)
	if idx := strings.LastIndex(remote, ":"); idx > 0 {
		return remote[:idx]
	}
	if remote == "" {
		return "unknown"
	}
	return remote
}

func resolveDataAssetPath(store *Store, relativePath string) (string, error) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(relativePath), "/")
	cleaned := filepath.Clean(trimmed)
	if cleaned == "." || cleaned == "" {
		return "", errors.New("invalid asset path")
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(os.PathSeparator)) {
		return "", errors.New("invalid asset path")
	}
	directPath := filepath.Join(store.dataDir, cleaned)
	if stat, err := os.Stat(directPath); err == nil && !stat.IsDir() {
		return directPath, nil
	}

	normalized := filepath.ToSlash(cleaned)
	segments := strings.Split(normalized, "/")
	if len(segments) < 2 {
		return directPath, nil
	}

	noteID := segments[0]
	tailPath := filepath.Join(segments[1:]...)
	noteFile, err := store.locateNoteFile(noteID)
	if err != nil {
		return directPath, nil
	}
	noteDir := filepath.Dir(noteFile)
	fullPath := filepath.Join(noteDir, tailPath)
	cleanNoteDir := filepath.Clean(noteDir)
	cleanFullPath := filepath.Clean(fullPath)
	if cleanFullPath != cleanNoteDir && !strings.HasPrefix(cleanFullPath, cleanNoteDir+string(os.PathSeparator)) {
		return "", errors.New("invalid asset path")
	}
	return fullPath, nil
}

func applyNoCacheHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
}

func applySPACacheHeaders(w http.ResponseWriter, requestPath string) {
	normalized := filepath.ToSlash(strings.TrimSpace(requestPath))
	ext := strings.ToLower(filepath.Ext(normalized))
	if ext == ".html" {
		applyNoCacheHeaders(w)
		return
	}
	if hashedAssetPattern.MatchString(filepath.Base(normalized)) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=3600, must-revalidate")
}

func tryServePrecompressedAsset(w http.ResponseWriter, r *http.Request, requestPath string, targetPath string) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	if strings.TrimSpace(r.Header.Get("Range")) != "" {
		return false
	}
	ext := strings.ToLower(filepath.Ext(requestPath))
	if !isCompressibleAssetExt(ext) {
		return false
	}
	for _, candidate := range []struct {
		encoding string
		suffix   string
	}{
		{encoding: "br", suffix: ".br"},
		{encoding: "gzip", suffix: ".gz"},
	} {
		if !acceptsEncoding(r.Header.Get("Accept-Encoding"), candidate.encoding) {
			continue
		}
		compressedPath := targetPath + candidate.suffix
		info, err := os.Stat(compressedPath)
		if err != nil || info.IsDir() {
			continue
		}
		appendVaryHeader(w, "Accept-Encoding")
		w.Header().Set("Content-Encoding", candidate.encoding)
		if contentType := mime.TypeByExtension(ext); contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		etag := buildFileETag(info)
		w.Header().Set("ETag", etag)
		if etagMatched(r.Header.Get("If-None-Match"), etag) {
			w.WriteHeader(http.StatusNotModified)
			return true
		}
		http.ServeFile(w, r, compressedPath)
		return true
	}
	return false
}

func acceptsEncoding(header string, encoding string) bool {
	if strings.TrimSpace(header) == "" {
		return false
	}
	items := strings.Split(header, ",")
	target := strings.ToLower(strings.TrimSpace(encoding))
	for _, item := range items {
		value := strings.ToLower(strings.TrimSpace(item))
		if value == "" {
			continue
		}
		if semi := strings.Index(value, ";"); semi >= 0 {
			value = strings.TrimSpace(value[:semi])
		}
		if value == target {
			return true
		}
	}
	return false
}

func appendVaryHeader(w http.ResponseWriter, key string) {
	origin := w.Header().Get("Vary")
	if origin == "" {
		w.Header().Set("Vary", key)
		return
	}
	parts := strings.Split(origin, ",")
	needle := strings.ToLower(strings.TrimSpace(key))
	for _, part := range parts {
		if strings.ToLower(strings.TrimSpace(part)) == needle {
			return
		}
	}
	w.Header().Set("Vary", origin+", "+key)
}

func isCompressibleAssetExt(ext string) bool {
	switch ext {
	case ".html", ".js", ".css", ".json", ".svg", ".txt", ".xml", ".map":
		return true
	default:
		return false
	}
}

func applyDataAssetCacheHeaders(w http.ResponseWriter, fullPath string) {
	ext := strings.ToLower(filepath.Ext(fullPath))
	if isDataAssetTextExt(ext) {
		w.Header().Set("Cache-Control", "public, max-age=300, must-revalidate")
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=86400, must-revalidate")
}

func isDataAssetTextExt(ext string) bool {
	switch ext {
	case ".md", ".txt", ".json", ".yaml", ".yml", ".toml":
		return true
	default:
		return false
	}
}

func buildFileETag(info os.FileInfo) string {
	return fmt.Sprintf(`W/"%x-%x"`, info.ModTime().UnixNano(), info.Size())
}

func buildBytesETag(data []byte) string {
	hash := fnv.New64a()
	_, _ = hash.Write(data)
	return fmt.Sprintf(`W/"%x-%x"`, len(data), hash.Sum64())
}

func buildArticleETag(article Article, comments []Comment) string {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(article.ID))
	_, _ = hash.Write([]byte("|"))
	_, _ = hash.Write([]byte(article.ContentHash))
	_, _ = hash.Write([]byte("|"))
	_, _ = hash.Write([]byte(article.UpdatedAt))
	for _, comment := range flattenComments(comments) {
		_, _ = hash.Write([]byte("|"))
		_, _ = hash.Write([]byte(strconv.FormatInt(comment.ID, 10)))
		_, _ = hash.Write([]byte("|"))
		_, _ = hash.Write([]byte(comment.UpdatedAt))
		_, _ = hash.Write([]byte("|"))
		_, _ = hash.Write([]byte(strconv.Itoa(comment.Likes)))
	}
	return fmt.Sprintf(`W/"article-%x"`, hash.Sum64())
}

func flattenComments(comments []Comment) []Comment {
	items := make([]Comment, 0, len(comments))
	for _, comment := range comments {
		items = append(items, comment)
		if len(comment.Replies) > 0 {
			items = append(items, flattenComments(comment.Replies)...)
		}
	}
	return items
}

func etagMatched(ifNoneMatch string, etag string) bool {
	header := strings.TrimSpace(ifNoneMatch)
	if header == "" {
		return false
	}
	if header == "*" {
		return true
	}
	items := strings.Split(header, ",")
	for _, item := range items {
		if strings.TrimSpace(item) == etag {
			return true
		}
	}
	return false
}
