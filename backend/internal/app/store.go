// Package app 提供 Markdown + SQLite 混合存储，实现笔记扫描、聚合与搜索。
// Author: Codex
// Created: 2026-02-16
package app

import (
	"bufio"
	"bytes"
	cryptorand "crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io/fs"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	_ "modernc.org/sqlite"
)

const defaultScanInterval = 60 * time.Second

type StoreOptions struct {
	ScanInterval  time.Duration
	OnNewArticles func([]Article)
}

type Store struct {
	db      *sql.DB
	dataDir string

	mu            sync.RWMutex
	notes         map[string]Article
	noteTree      []ArticleTreeNode
	scanInterval  time.Duration
	onNewArticles func([]Article)
}

func (s *Store) SetOnNewArticles(handler func([]Article)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onNewArticles = handler
}

func (s *Store) SetScanInterval(interval time.Duration) {
	if interval <= 0 {
		interval = defaultScanInterval
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.scanInterval = interval
	LogInfof("store", "scan interval updated to %s", interval)
}

func openStore(dbPath string, options StoreOptions) (*Store, error) {
	dataDir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	if err = applySQLitePragmas(db); err != nil {
		return nil, err
	}

	scanInterval := options.ScanInterval
	if scanInterval <= 0 {
		scanInterval = defaultScanInterval
	}

	store := &Store{
		db:            db,
		dataDir:       dataDir,
		notes:         map[string]Article{},
		scanInterval:  scanInterval,
		onNewArticles: options.OnNewArticles,
	}
	if err = store.initSchema(); err != nil {
		return nil, err
	}
	if err = store.ensureDefaultNotes(); err != nil {
		return nil, err
	}
	if err = store.scanAndSync(); err != nil {
		return nil, err
	}
	if err = store.ensureDefaultComments(); err != nil {
		return nil, err
	}
	store.startAutoScan()
	return store, nil
}

func (s *Store) initSchema() error {
	schema := `
CREATE TABLE IF NOT EXISTS note_metadata (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    slug TEXT NOT NULL,
    category TEXT NOT NULL,
    path TEXT NOT NULL,
    tags_json TEXT NOT NULL,
    excerpt TEXT NOT NULL,
    published_at TEXT NOT NULL,
    read_minutes INTEGER NOT NULL DEFAULT 1,
    views INTEGER NOT NULL DEFAULT 0,
    order_index INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    content_hash TEXT NOT NULL DEFAULT '',
    rendered_html TEXT NOT NULL DEFAULT '',
    source_file TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_note_metadata_category ON note_metadata(category);
CREATE INDEX IF NOT EXISTS idx_note_metadata_path ON note_metadata(path);
CREATE INDEX IF NOT EXISTS idx_note_metadata_published_at ON note_metadata(published_at);
CREATE INDEX IF NOT EXISTS idx_note_metadata_updated_created_order ON note_metadata(updated_at DESC, created_at DESC, order_index ASC);
CREATE INDEX IF NOT EXISTS idx_note_metadata_path_title ON note_metadata(path, title);

CREATE TABLE IF NOT EXISTS note_identity_map (
    id TEXT PRIMARY KEY,
    note_path TEXT NOT NULL,
    note_title TEXT NOT NULL,
    source_file TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(note_path, note_title)
);

CREATE INDEX IF NOT EXISTS idx_note_identity_map_path_title ON note_identity_map(note_path, note_title);
CREATE INDEX IF NOT EXISTS idx_note_identity_map_title ON note_identity_map(note_title);

CREATE TABLE IF NOT EXISTS note_tag_hierarchy (
    tag_path TEXT PRIMARY KEY,
    tag_name TEXT NOT NULL,
    parent_path TEXT NOT NULL,
    level INTEGER NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_note_tag_hierarchy_parent ON note_tag_hierarchy(parent_path);

CREATE TABLE IF NOT EXISTS note_runtime_metadata (
    id TEXT PRIMARY KEY,
    views INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS note_visual_metadata (
    node_id TEXT PRIMARY KEY,
    node_type TEXT NOT NULL,
    icon TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS comments (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    article_id TEXT NOT NULL,
    parent_id INTEGER NOT NULL DEFAULT 0,
    author TEXT NOT NULL,
    content TEXT NOT NULL,
    likes INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_comments_article ON comments(article_id);
CREATE INDEX IF NOT EXISTS idx_comments_article_created ON comments(article_id, created_at);

CREATE TABLE IF NOT EXISTS comment_like_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    comment_id INTEGER NOT NULL,
    source_ip TEXT NOT NULL,
    created_at TEXT NOT NULL,
    FOREIGN KEY(comment_id) REFERENCES comments(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS subscribers (
    email TEXT PRIMARY KEY,
    active INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    last_sent_at TEXT NOT NULL DEFAULT '',
    last_error TEXT NOT NULL DEFAULT ''
);
`
	if _, err := s.db.Exec(schema); err != nil {
		return err
	}
	if err := s.migrateNoteMetadataSchema(); err != nil {
		return err
	}
	if err := s.migrateCommentsSchema(); err != nil {
		return err
	}
	return s.migrateSubscribersSchema()
}

func applySQLitePragmas(db *sql.DB) error {
	var journalMode string
	if err := db.QueryRow("PRAGMA journal_mode = WAL").Scan(&journalMode); err != nil {
		return err
	}
	if _, err := db.Exec("PRAGMA synchronous = NORMAL"); err != nil {
		return err
	}
	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		return err
	}
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		return err
	}
	if _, err := db.Exec("PRAGMA temp_store = MEMORY"); err != nil {
		return err
	}
	return nil
}

func (s *Store) migrateNoteMetadataSchema() error {
	contentHashExists, err := s.tableColumnExists("note_metadata", "content_hash")
	if err != nil {
		return err
	}
	if !contentHashExists {
		if _, err = s.db.Exec("ALTER TABLE note_metadata ADD COLUMN content_hash TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
	}
	renderedHTMLExists, err := s.tableColumnExists("note_metadata", "rendered_html")
	if err != nil {
		return err
	}
	if !renderedHTMLExists {
		if _, err = s.db.Exec("ALTER TABLE note_metadata ADD COLUMN rendered_html TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
	}
	if _, err = s.db.Exec("CREATE INDEX IF NOT EXISTS idx_note_metadata_updated_created_order ON note_metadata(updated_at DESC, created_at DESC, order_index ASC)"); err != nil {
		return err
	}
	if _, err = s.db.Exec("CREATE INDEX IF NOT EXISTS idx_note_metadata_path_title ON note_metadata(path, title)"); err != nil {
		return err
	}
	if _, err = s.db.Exec("CREATE INDEX IF NOT EXISTS idx_note_identity_map_title ON note_identity_map(note_title)"); err != nil {
		return err
	}
	if _, err = s.db.Exec("CREATE INDEX IF NOT EXISTS idx_comments_article_created ON comments(article_id, created_at)"); err != nil {
		return err
	}
	return nil
}

func (s *Store) migrateCommentsSchema() error {
	parentExists, err := s.commentColumnExists("parent_id")
	if err != nil {
		return err
	}
	if !parentExists {
		if _, err = s.db.Exec("ALTER TABLE comments ADD COLUMN parent_id INTEGER NOT NULL DEFAULT 0"); err != nil {
			return err
		}
	}

	updatedExists, err := s.commentColumnExists("updated_at")
	if err != nil {
		return err
	}
	if !updatedExists {
		if _, err = s.db.Exec("ALTER TABLE comments ADD COLUMN updated_at TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
	}

	if _, err = s.db.Exec("UPDATE comments SET parent_id = 0 WHERE parent_id IS NULL"); err != nil {
		return err
	}
	if _, err = s.db.Exec("UPDATE comments SET updated_at = created_at WHERE updated_at IS NULL OR updated_at = ''"); err != nil {
		return err
	}
	if _, err = s.db.Exec("CREATE INDEX IF NOT EXISTS idx_comments_parent ON comments(parent_id)"); err != nil {
		return err
	}
	return nil
}

func (s *Store) commentColumnExists(columnName string) (bool, error) {
	return s.tableColumnExists("comments", columnName)
}

func (s *Store) tableColumnExists(tableName string, columnName string) (bool, error) {
	rows, err := s.db.Query(fmt.Sprintf("PRAGMA table_info(%s)", tableName))
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name string
		var typeName string
		var notNull int
		var defaultValue sql.NullString
		var primaryKey int
		if err = rows.Scan(&cid, &name, &typeName, &notNull, &defaultValue, &primaryKey); err != nil {
			return false, err
		}
		if name == columnName {
			return true, nil
		}
	}
	return false, nil
}

func (s *Store) migrateSubscribersSchema() error {
	activeExists, err := s.tableColumnExists("subscribers", "active")
	if err != nil {
		return err
	}
	if !activeExists {
		if _, err = s.db.Exec("ALTER TABLE subscribers ADD COLUMN active INTEGER NOT NULL DEFAULT 1"); err != nil {
			return err
		}
	}

	updatedExists, err := s.tableColumnExists("subscribers", "updated_at")
	if err != nil {
		return err
	}
	if !updatedExists {
		if _, err = s.db.Exec("ALTER TABLE subscribers ADD COLUMN updated_at TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
	}

	lastSentExists, err := s.tableColumnExists("subscribers", "last_sent_at")
	if err != nil {
		return err
	}
	if !lastSentExists {
		if _, err = s.db.Exec("ALTER TABLE subscribers ADD COLUMN last_sent_at TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
	}

	lastErrorExists, err := s.tableColumnExists("subscribers", "last_error")
	if err != nil {
		return err
	}
	if !lastErrorExists {
		if _, err = s.db.Exec("ALTER TABLE subscribers ADD COLUMN last_error TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
	}

	now := time.Now().Format(time.RFC3339)
	if _, err = s.db.Exec("UPDATE subscribers SET active = 1 WHERE active IS NULL"); err != nil {
		return err
	}
	if _, err = s.db.Exec("UPDATE subscribers SET updated_at = created_at WHERE updated_at IS NULL OR updated_at = ''"); err != nil {
		return err
	}
	if _, err = s.db.Exec("UPDATE subscribers SET created_at = ? WHERE created_at IS NULL OR created_at = ''", now); err != nil {
		return err
	}
	if _, err = s.db.Exec("CREATE INDEX IF NOT EXISTS idx_subscribers_active ON subscribers(active)"); err != nil {
		return err
	}
	return nil
}

func (s *Store) startAutoScan() {
	go func() {
		for {
			s.mu.RLock()
			interval := s.scanInterval
			s.mu.RUnlock()
			if interval <= 0 {
				interval = defaultScanInterval
			}
			timer := time.NewTimer(interval)
			<-timer.C
			if err := s.scanAndSyncWithNotify(true); err != nil {
				LogErrorf("store", "auto scan failed: %v", err)
			}
		}
	}()
}

func (s *Store) scanAndSync() error {
	return s.scanAndSyncWithNotify(false)
}

func (s *Store) scanAndSyncWithNotify(notifyNew bool) error {
	existingIDs := map[string]struct{}{}
	if notifyNew {
		var err error
		existingIDs, err = s.loadCurrentMetadataIDs()
		if err != nil {
			return err
		}
	}

	articles, err := s.scanNotesFromDisk()
	if err != nil {
		return err
	}
	if err = s.syncMetadata(articles); err != nil {
		return err
	}
	tree := buildNoteTree(articles)
	if err = s.attachNodeIcons(tree); err != nil {
		return err
	}

	next := make(map[string]Article, len(articles))
	for _, article := range articles {
		next[article.ID] = article
	}
	s.mu.Lock()
	s.notes = next
	s.noteTree = tree
	s.mu.Unlock()

	if notifyNew {
		s.mu.RLock()
		hook := s.onNewArticles
		s.mu.RUnlock()
		if hook == nil {
			return nil
		}
		newArticles := make([]Article, 0)
		for _, article := range articles {
			if _, exists := existingIDs[article.ID]; exists {
				continue
			}
			newArticles = append(newArticles, article)
		}
		if len(newArticles) > 0 {
			preview := make([]string, 0, 3)
			for _, item := range newArticles {
				preview = append(preview, item.ID)
				if len(preview) >= 3 {
					break
				}
			}
			LogInfof("store", "detected %d new articles, preview_ids=%s", len(newArticles), strings.Join(preview, ","))
			payload := make([]Article, len(newArticles))
			copy(payload, newArticles)
			go hook(payload)
		}
	}
	return nil
}

func (s *Store) scanNotesFromDisk() ([]Article, error) {
	root := s.notesRootDir()
	if err := os.MkdirAll(root, 0755); err != nil {
		return nil, err
	}
	identityMap, err := s.loadPathTitleIDMap()
	if err != nil {
		return nil, err
	}
	articles := make([]Article, 0)
	titleSourceMap := map[string]string{}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() {
			name := entry.Name()
			if name == "attachments" || name == "comments" {
				return filepath.SkipDir
			}
			return nil
		}
		if !isMarkdownFile(path) {
			return nil
		}
		relFile, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		relFile = filepath.ToSlash(relFile)
		if strings.TrimSpace(relFile) == "" || strings.HasPrefix(relFile, "../") {
			return nil
		}
		notePath := filepath.ToSlash(filepath.Dir(relFile))
		if notePath == "." {
			notePath = "未分类"
		}
		title := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		title = strings.TrimSpace(title)
		if title == "" {
			return nil
		}
		existingSource, exists := titleSourceMap[title]
		duplicateTitle := exists && existingSource != path
		if !exists {
			titleSourceMap[title] = path
		}
		key := buildPathTitleKey(notePath, title)
		if duplicateTitle {
			key = buildPathTitleLegacyKey(notePath, title)
			LogWarnf("store", "duplicate title detected, fallback to path+title key, title=%s, source=%s, duplicate=%s", title, existingSource, path)
		}
		noteID := strings.TrimSpace(identityMap[key])
		if noteID == "" {
			noteID = generateNoteID(key)
		}
		article, ok, parseErr := s.readArticleFromFile(noteID, path, notePath, title)
		if parseErr != nil {
			return nil
		}
		if ok {
			articles = append(articles, article)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(articles, func(i int, j int) bool {
		if articles[i].Path != articles[j].Path {
			return articles[i].Path < articles[j].Path
		}
		if articles[i].PublishedAt != articles[j].PublishedAt {
			return articles[i].PublishedAt > articles[j].PublishedAt
		}
		return articles[i].Title < articles[j].Title
	})
	for index := range articles {
		articles[index].OrderIndex = index + 1
	}
	return articles, nil
}

func (s *Store) readArticleFromFile(noteID string, mdPath string, notePath string, titleFromFile string) (Article, bool, error) {
	bytes, err := os.ReadFile(mdPath)
	if err != nil {
		return Article{}, false, err
	}
	content := string(bytes)
	info, err := os.Stat(mdPath)
	if err != nil {
		return Article{}, false, err
	}

	segments := splitPath(notePath)
	if len(segments) == 0 {
		segments = []string{"未分类"}
	}
	path := strings.Join(segments, "/")
	tags := parseTags(content)
	if !containsTag(tags, path) {
		tags = append([]string{path}, tags...)
	}
	title := strings.TrimSpace(titleFromFile)
	if title == "" {
		title = parseTitle(content, noteID)
	}
	article := Article{
		ID:          noteID,
		ParentID:    "",
		Title:       title,
		Slug:        noteID,
		Category:    firstOrDefault(segments, "未分类"),
		Path:        path,
		Tags:        tags,
		Excerpt:     parseExcerpt(content),
		Content:     content,
		PublishedAt: info.ModTime().Format("2006-01-02"),
		ReadMinutes: calcReadMinutes(content),
		Views:       0,
		OrderIndex:  0,
		CreatedAt:   info.ModTime().Format(time.RFC3339),
		UpdatedAt:   info.ModTime().Format(time.RFC3339),
		SourceFile:  mdPath,
	}
	return article, true, nil
}

func (s *Store) syncMetadata(articles []Article) error {
	viewMap, err := s.loadViewMap()
	if err != nil {
		return err
	}
	createdAtMap, err := s.loadCreatedAtMap()
	if err != nil {
		return err
	}
	renderCacheMap, err := s.loadRenderCacheMap()
	if err != nil {
		return err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err = tx.Exec("DELETE FROM note_metadata"); err != nil {
		return err
	}

	statement, err := tx.Prepare(`
INSERT INTO note_metadata(id, title, slug, category, path, tags_json, excerpt, published_at, read_minutes, views, order_index, created_at, updated_at, content_hash, rendered_html, source_file)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`)
	if err != nil {
		return err
	}
	defer statement.Close()

	for index := range articles {
		article := articles[index]
		tagsJSON, marshalErr := json.Marshal(article.Tags)
		if marshalErr != nil {
			return marshalErr
		}
		createdAt := article.CreatedAt
		if existing, ok := createdAtMap[article.ID]; ok && strings.TrimSpace(existing) != "" {
			createdAt = existing
		}
		articles[index].CreatedAt = createdAt
		viewCount := viewMap[article.ID]
		contentHash := calcContentHash(article.Content)
		renderedHTML := renderCacheMap[article.ID].HTML
		if renderedHTML == "" || renderCacheMap[article.ID].Hash != contentHash {
			renderedHTML = renderArticleHTMLForCache(article)
		}
		articles[index].ContentHash = contentHash
		articles[index].RenderedHTML = renderedHTML
		sourceFile := strings.TrimSpace(article.SourceFile)
		if sourceFile == "" {
			sourceFile = s.buildNoteFilePath(article.Path, article.Title)
		}
		if _, err = statement.Exec(
			article.ID,
			article.Title,
			article.Slug,
			article.Category,
			article.Path,
			string(tagsJSON),
			article.Excerpt,
			article.PublishedAt,
			article.ReadMinutes,
			viewCount,
			article.OrderIndex,
			createdAt,
			article.UpdatedAt,
			contentHash,
			renderedHTML,
			sourceFile,
		); err != nil {
			return err
		}
	}
	if err = s.syncRuntimeMetadataTx(tx, articles); err != nil {
		return err
	}
	if err = s.syncIdentityMapTx(tx, articles); err != nil {
		return err
	}
	if err = s.syncTagHierarchyTx(tx, articles); err != nil {
		return err
	}
	if err = s.cleanupCommentDataTx(tx, articles); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) syncIdentityMapTx(tx *sql.Tx, articles []Article) error {
	if _, err := tx.Exec("DELETE FROM note_identity_map"); err != nil {
		return err
	}
	now := time.Now().Format(time.RFC3339)
	for _, article := range articles {
		sourceFile := strings.TrimSpace(article.SourceFile)
		if sourceFile == "" {
			sourceFile = s.buildNoteFilePath(article.Path, article.Title)
		}
		if _, err := tx.Exec(`
INSERT INTO note_identity_map(id, note_path, note_title, source_file, updated_at)
VALUES(?, ?, ?, ?, ?)
`, article.ID, article.Path, article.Title, sourceFile, now); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) cleanupCommentDataTx(tx *sql.Tx, articles []Article) error {
	alive := map[string]struct{}{}
	for _, article := range articles {
		alive[article.ID] = struct{}{}
	}

	rows, err := tx.Query("SELECT DISTINCT article_id FROM comments")
	if err != nil {
		return err
	}
	defer rows.Close()

	staleArticleIDs := make([]string, 0)
	for rows.Next() {
		var articleID string
		if err = rows.Scan(&articleID); err != nil {
			return err
		}
		if _, exists := alive[articleID]; !exists {
			staleArticleIDs = append(staleArticleIDs, articleID)
		}
	}
	for _, articleID := range staleArticleIDs {
		if _, err = tx.Exec("DELETE FROM comments WHERE article_id = ?", articleID); err != nil {
			return err
		}
	}

	if _, err = tx.Exec("DELETE FROM comment_like_events WHERE comment_id NOT IN (SELECT id FROM comments)"); err != nil {
		return err
	}
	return nil
}

func (s *Store) syncTagHierarchyTx(tx *sql.Tx, articles []Article) error {
	type tagNode struct {
		Path   string
		Name   string
		Parent string
		Level  int
	}
	nodes := map[string]tagNode{}
	for _, article := range articles {
		segments := splitPath(article.Path)
		prefix := make([]string, 0, len(segments))
		for _, segment := range segments {
			prefix = append(prefix, segment)
			tagPath := strings.Join(prefix, "/")
			parentPath := ""
			if len(prefix) > 1 {
				parentPath = strings.Join(prefix[:len(prefix)-1], "/")
			}
			nodes[tagPath] = tagNode{
				Path:   tagPath,
				Name:   segment,
				Parent: parentPath,
				Level:  len(prefix),
			}
		}
	}
	if _, err := tx.Exec("DELETE FROM note_tag_hierarchy"); err != nil {
		return err
	}
	now := time.Now().Format(time.RFC3339)
	paths := make([]string, 0, len(nodes))
	for path := range nodes {
		paths = append(paths, path)
	}
	sort.Slice(paths, func(i int, j int) bool {
		left := nodes[paths[i]]
		right := nodes[paths[j]]
		if left.Level != right.Level {
			return left.Level < right.Level
		}
		return left.Path < right.Path
	})
	for _, path := range paths {
		node := nodes[path]
		if _, err := tx.Exec(`
INSERT INTO note_tag_hierarchy(tag_path, tag_name, parent_path, level, updated_at)
VALUES(?, ?, ?, ?, ?)
`, node.Path, node.Name, node.Parent, node.Level, now); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ListArticles() ([]Article, error) {
	viewMap, err := s.loadViewMap()
	if err != nil {
		return nil, err
	}

	rows, err := s.db.Query(`
SELECT id, title, slug, category, path, tags_json, excerpt, published_at, read_minutes, views, order_index, created_at, updated_at
FROM note_metadata
ORDER BY order_index ASC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	s.mu.RLock()
	defer s.mu.RUnlock()

	articles := make([]Article, 0)
	for rows.Next() {
		var article Article
		var tagsJSON string
		if err = rows.Scan(
			&article.ID,
			&article.Title,
			&article.Slug,
			&article.Category,
			&article.Path,
			&tagsJSON,
			&article.Excerpt,
			&article.PublishedAt,
			&article.ReadMinutes,
			&article.Views,
			&article.OrderIndex,
			&article.CreatedAt,
			&article.UpdatedAt,
		); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(tagsJSON), &article.Tags)
		article.Views = viewMap[article.ID]
		articles = append(articles, article)
	}
	return articles, nil
}

func (s *Store) ListArticleTree() ([]ArticleTreeNode, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneTree(s.noteTree), nil
}

func (s *Store) ListTagTree() ([]TagTreeNode, error) {
	rows, err := s.db.Query(`
SELECT h.tag_path, h.tag_name, h.parent_path, h.level, COALESCE(v.icon, '')
FROM note_tag_hierarchy h
LEFT JOIN note_visual_metadata v ON v.node_id = ('folder:' || h.tag_path)
ORDER BY h.level ASC, h.tag_path ASC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type rowData struct {
		Path   string
		Name   string
		Parent string
		Level  int
		Icon   string
	}
	items := make([]rowData, 0)
	for rows.Next() {
		var item rowData
		if err = rows.Scan(&item.Path, &item.Name, &item.Parent, &item.Level, &item.Icon); err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	nodeMap := map[string]*tagTreeBuilder{}
	for _, item := range items {
		nodeMap[item.Path] = &tagTreeBuilder{
			ID:       "folder:" + item.Path,
			Name:     item.Name,
			Path:     item.Path,
			Level:    item.Level,
			Icon:     item.Icon,
			Children: []*tagTreeBuilder{},
		}
	}

	roots := make([]*tagTreeBuilder, 0)
	for _, item := range items {
		node := nodeMap[item.Path]
		if strings.TrimSpace(item.Parent) == "" {
			roots = append(roots, node)
			continue
		}
		parent, ok := nodeMap[item.Parent]
		if !ok {
			roots = append(roots, node)
			continue
		}
		parent.Children = append(parent.Children, node)
	}

	result := make([]TagTreeNode, 0, len(roots))
	for _, root := range roots {
		result = append(result, buildTagTreeNode(root))
	}
	sortTagTreeNodes(result)
	return result, nil
}

func (s *Store) ListRecentArticles(limit int) ([]Article, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}

	viewMap, err := s.loadViewMap()
	if err != nil {
		return nil, err
	}

	rows, err := s.db.Query(`
SELECT id, title, slug, category, path, tags_json, excerpt, published_at, read_minutes, views, order_index, created_at, updated_at
FROM note_metadata
ORDER BY updated_at DESC, created_at DESC, order_index ASC
LIMIT ?
`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	s.mu.RLock()
	defer s.mu.RUnlock()

	items := make([]Article, 0)
	for rows.Next() {
		var article Article
		var tagsJSON string
		if err = rows.Scan(
			&article.ID,
			&article.Title,
			&article.Slug,
			&article.Category,
			&article.Path,
			&tagsJSON,
			&article.Excerpt,
			&article.PublishedAt,
			&article.ReadMinutes,
			&article.Views,
			&article.OrderIndex,
			&article.CreatedAt,
			&article.UpdatedAt,
		); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(tagsJSON), &article.Tags)
		article.Views = viewMap[article.ID]
		items = append(items, article)
	}
	return items, nil
}

func (s *Store) SearchArticles(keyword string) ([]Article, error) {
	viewMap, err := s.loadViewMap()
	if err != nil {
		return nil, err
	}

	trimmed := strings.TrimSpace(keyword)
	like := "%" + trimmed + "%"
	rows, err := s.db.Query(`
SELECT id, title, slug, category, path, tags_json, excerpt, published_at, read_minutes, views, order_index, created_at, updated_at
FROM note_metadata
WHERE ? = '' OR title LIKE ? OR excerpt LIKE ? OR path LIKE ? OR tags_json LIKE ?
ORDER BY order_index ASC
`, trimmed, like, like, like, like)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	s.mu.RLock()
	defer s.mu.RUnlock()

	results := make([]Article, 0)
	for rows.Next() {
		var article Article
		var tagsJSON string
		if err = rows.Scan(
			&article.ID,
			&article.Title,
			&article.Slug,
			&article.Category,
			&article.Path,
			&tagsJSON,
			&article.Excerpt,
			&article.PublishedAt,
			&article.ReadMinutes,
			&article.Views,
			&article.OrderIndex,
			&article.CreatedAt,
			&article.UpdatedAt,
		); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(tagsJSON), &article.Tags)
		article.Views = viewMap[article.ID]
		results = append(results, article)
	}
	return results, nil
}

func (s *Store) GetArticle(id string) (Article, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	article, ok := s.notes[id]
	if !ok {
		return Article{}, sql.ErrNoRows
	}
	viewCount, err := s.getViewCount(id)
	if err == nil {
		article.Views = viewCount
	}
	return article, nil
}

func (s *Store) CreateArticle(article Article) (Article, error) {
	title := strings.TrimSpace(article.Title)
	if title == "" {
		return Article{}, errors.New("title is required")
	}
	if err := validateNoteTitle(title); err != nil {
		return Article{}, err
	}
	path := normalizeArticlePath(article.Path, article.Category)
	key := buildPathTitleKey(path, title)
	if strings.TrimSpace(article.ID) == "" {
		article.ID = generateNoteID(key)
	}
	article.Path = path
	segments := splitPath(path)
	article.Category = firstOrDefault(segments, "未分类")
	article.Title = title
	noteDir := s.buildNoteDir(path)
	if err := os.MkdirAll(noteDir, 0755); err != nil {
		return Article{}, err
	}
	notePath := s.buildNoteFilePath(path, title)
	if _, err := os.Stat(notePath); err == nil {
		return Article{}, errors.New("note already exists at same path and title")
	}
	content := buildMarkdown(article)
	if err := os.WriteFile(notePath, []byte(content), 0644); err != nil {
		return Article{}, err
	}
	if err := s.upsertIdentityMap(article.ID, article.Path, article.Title, notePath); err != nil {
		return Article{}, err
	}
	if err := s.scanAndSync(); err != nil {
		return Article{}, err
	}
	return s.GetArticle(article.ID)
}

func (s *Store) UpdateArticle(id string, article Article) error {
	noteFile, err := s.locateNoteFile(id)
	if err != nil {
		return sql.ErrNoRows
	}
	existing, getErr := s.GetArticle(id)
	if getErr != nil {
		return getErr
	}
	if strings.TrimSpace(article.ID) == "" {
		article.ID = id
	}
	if strings.TrimSpace(article.Path) == "" {
		article.Path = existing.Path
	}
	if strings.TrimSpace(article.Category) == "" {
		article.Category = existing.Category
	}
	normalizedPath := normalizeArticlePath(article.Path, article.Category)
	title := strings.TrimSpace(article.Title)
	if title == "" {
		title = existing.Title
		article.Title = title
	}
	if err = validateNoteTitle(title); err != nil {
		return err
	}
	article.ID = id
	article.Title = title
	article.Path = normalizedPath
	pathSegments := splitPath(normalizedPath)
	article.Category = firstOrDefault(pathSegments, "未分类")
	targetDir := s.buildNoteDir(normalizedPath)
	if err = os.MkdirAll(targetDir, 0755); err != nil {
		return err
	}
	targetFile := s.buildNoteFilePath(normalizedPath, title)
	if filepath.Clean(noteFile) != filepath.Clean(targetFile) {
		if _, statErr := os.Stat(targetFile); statErr == nil {
			return errors.New("note already exists at same path and title")
		}
		if err = os.Rename(noteFile, targetFile); err != nil {
			return err
		}
	}
	content := buildMarkdown(article)
	if err := os.WriteFile(targetFile, []byte(content), 0644); err != nil {
		return err
	}
	if err = s.upsertIdentityMap(id, normalizedPath, title, targetFile); err != nil {
		return err
	}
	return s.scanAndSync()
}

func (s *Store) MoveArticle(id string, req MoveArticleRequest) error {
	article, err := s.GetArticle(id)
	if err != nil {
		return err
	}
	newPath := strings.TrimSpace(req.ParentID)
	if newPath == "" {
		newPath = article.Path
	}
	parts := strings.Split(strings.Trim(newPath, "/"), "/")
	if len(parts) == 0 {
		parts = []string{"未分类"}
	}
	article.Path = strings.Join(parts, "/")
	article.Category = parts[0]
	article.Tags = []string{article.Path}
	return s.UpdateArticle(id, article)
}

func (s *Store) DeleteArticle(id string) error {
	noteFile, err := s.locateNoteFile(id)
	if err != nil {
		return sql.ErrNoRows
	}
	if err := os.Remove(noteFile); err != nil {
		return err
	}
	s.cleanupEmptyNoteDirs(filepath.Dir(noteFile))
	return s.scanAndSync()
}

func (s *Store) ListComments(articleID string) ([]Comment, error) {
	rows, err := s.db.Query(`
SELECT id, article_id, parent_id, author, content, likes, created_at, updated_at
FROM comments
WHERE article_id = ?
ORDER BY created_at ASC, id ASC
`, articleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	all := make([]Comment, 0)
	for rows.Next() {
		var item Comment
		if err = rows.Scan(
			&item.ID,
			&item.ArticleID,
			&item.ParentID,
			&item.Author,
			&item.Content,
			&item.Likes,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		item.Replies = []Comment{}
		all = append(all, item)
	}
	return buildCommentTree(all), nil
}

func (s *Store) AddComment(comment Comment) (Comment, error) {
	if comment.ParentID > 0 {
		var parentArticleID string
		err := s.db.QueryRow("SELECT article_id FROM comments WHERE id = ?", comment.ParentID).Scan(&parentArticleID)
		if errors.Is(err, sql.ErrNoRows) {
			return Comment{}, errors.New("parent comment not found")
		}
		if err != nil {
			return Comment{}, err
		}
		if parentArticleID != comment.ArticleID {
			return Comment{}, errors.New("parent comment article mismatch")
		}
	}

	if strings.TrimSpace(comment.Author) == "" {
		comment.Author = "访客"
	}
	now := time.Now().Format("2006-01-02 15:04")
	comment.CreatedAt = now
	comment.UpdatedAt = now
	comment.Likes = 0
	result, err := s.db.Exec(`
INSERT INTO comments(article_id, parent_id, author, content, likes, created_at, updated_at)
VALUES(?, ?, ?, ?, ?, ?, ?)
`, comment.ArticleID, comment.ParentID, comment.Author, strings.TrimSpace(comment.Content), comment.Likes, comment.CreatedAt, comment.UpdatedAt)
	if err != nil {
		return Comment{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Comment{}, err
	}
	comment.ID = id
	comment.Replies = []Comment{}
	return comment, nil
}

func (s *Store) AddCommentLike(commentID int64, sourceIP string) (Comment, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Comment{}, err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`
UPDATE comments SET likes = likes + 1, updated_at = ? WHERE id = ?
`, time.Now().Format("2006-01-02 15:04"), commentID)
	if err != nil {
		return Comment{}, err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return Comment{}, sql.ErrNoRows
	}

	_, err = tx.Exec(`
INSERT INTO comment_like_events(comment_id, source_ip, created_at)
VALUES(?, ?, ?)
`, commentID, sourceIP, time.Now().Format(time.RFC3339))
	if err != nil {
		return Comment{}, err
	}

	comment, err := queryCommentByID(tx, commentID)
	if err != nil {
		return Comment{}, err
	}
	if err = tx.Commit(); err != nil {
		return Comment{}, err
	}
	return comment, nil
}

func (s *Store) AddSubscriber(email string) error {
	email = strings.TrimSpace(email)
	if email == "" {
		return errors.New("email is required")
	}
	now := time.Now().Format(time.RFC3339)
	_, err := s.db.Exec(
		`INSERT INTO subscribers(email, active, created_at, updated_at)
VALUES(?, 1, ?, ?)
ON CONFLICT(email) DO UPDATE SET active = 1, updated_at = excluded.updated_at`,
		email,
		now,
		now,
	)
	if err != nil {
		LogErrorf("store", "add subscriber failed, email=%s, err=%v", email, err)
		return err
	}
	LogInfof("store", "subscriber upsert success, email=%s", email)
	return err
}

func (s *Store) ListActiveSubscribers() ([]Subscriber, error) {
	rows, err := s.db.Query(`
SELECT email, active, created_at, updated_at, last_sent_at, last_error
FROM subscribers
WHERE active = 1
ORDER BY created_at ASC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Subscriber, 0)
	for rows.Next() {
		var item Subscriber
		var activeInt int
		if err = rows.Scan(
			&item.Email,
			&activeInt,
			&item.CreatedAt,
			&item.UpdatedAt,
			&item.LastSentAt,
			&item.LastError,
		); err != nil {
			return nil, err
		}
		item.Active = activeInt == 1
		items = append(items, item)
	}
	return items, nil
}

func (s *Store) UpdateSubscriberDelivery(email string, sendAt string, sendErr string) error {
	email = strings.TrimSpace(email)
	if email == "" {
		return errors.New("email is required")
	}
	_, err := s.db.Exec(`
UPDATE subscribers
SET updated_at = ?, last_sent_at = ?, last_error = ?
WHERE email = ?
`, time.Now().Format(time.RFC3339), sendAt, sendErr, email)
	if err != nil {
		LogErrorf("store", "update subscriber delivery failed, email=%s, err=%v", email, err)
	}
	return err
}

func (s *Store) ProfileStats() (ProfileStats, error) {
	var stats ProfileStats
	if err := s.db.QueryRow("SELECT COUNT(1) FROM note_metadata").Scan(&stats.Articles); err != nil {
		return ProfileStats{}, err
	}
	if err := s.db.QueryRow("SELECT COALESCE(SUM(views), 0) FROM note_runtime_metadata").Scan(&stats.Views); err != nil {
		return ProfileStats{}, err
	}
	stats.Years = 3
	return stats, nil
}

func (s *Store) ensureDefaultNotes() error {
	if has, err := hasMarkdownNotes(s.notesRootDir()); err != nil {
		return err
	} else if has {
		return nil
	}

	defaultNotes := []struct {
		ID       string
		Title    string
		Content  string
		TagPath  string
		Comments []string
	}{
		{
			ID:      "agent-mcp-quick-start",
			Title:   "Agent 工程快速入门：MCP 与工具编排",
			TagPath: "Agent工程/MCP",
			Content: "MCP（Model Context Protocol）是 Agent 与外部工具协作的标准接口。实践里，建议先定义最小工具集合，再逐步补齐鉴权、限流与幂等。\n\n1. 先把核心链路跑通\n\n2. 再做可观测和重试\n\n3. 最后做工具权限分级",
			Comments: []string{
				"author: 张三\nlikes: 5\ncreated_at: 2026-02-16 10:30\n\n这篇把 MCP 的落地步骤讲清楚了，尤其是最小工具集合的建议。\n",
				"author: 李四\nlikes: 3\ncreated_at: 2026-02-16 18:45\n\n希望后续补一篇关于工具失败重试和熔断策略的实战案例。\n",
			},
		},
		{
			ID:      "agent-observability-practice",
			Title:   "Agent 可观测性实践：日志、轨迹与评估",
			TagPath: "Agent工程/架构",
			Content: "Agent 线上质量稳定的前提是可观测。建议统一 request_id、trace_id，并把每一步工具调用的输入摘要、输出摘要、耗时、重试次数写入日志。\n\n同时维护离线评估集，定期回放关键场景，避免提示词迭代引入回归。",
		},
		{
			ID:      "agent-note-workflow",
			Title:   "知识笔记自动化：Obsidian 到博客发布流",
			TagPath: "Agent工程/工作流",
			Content: "将 Obsidian 笔记目录作为单一事实来源，后端定时扫描 Markdown 并同步 SQLite 聚合索引，前端全部从 API 渲染。\n\n这样既保留本地写作体验，又能获得可搜索、可统计、可编排的博客系统。",
		},
	}

	for _, note := range defaultNotes {
		noteDir := s.buildNoteDir(note.TagPath)
		if err := os.MkdirAll(noteDir, 0755); err != nil {
			return err
		}
		commentDir := filepath.Join(noteDir, "comments")
		if err := os.MkdirAll(commentDir, 0755); err != nil {
			return err
		}

		body := fmt.Sprintf("# %s\n\n%s\n\n#%s\n", note.Title, note.Content, note.TagPath)
		noteFile := s.buildNoteFilePath(note.TagPath, note.Title)
		if err := os.WriteFile(noteFile, []byte(body), 0644); err != nil {
			return err
		}
		if _, err := s.db.Exec(`
INSERT INTO note_identity_map(id, note_path, note_title, source_file, updated_at)
VALUES(?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET note_path = excluded.note_path, note_title = excluded.note_title, source_file = excluded.source_file, updated_at = excluded.updated_at
`, note.ID, note.TagPath, note.Title, noteFile, time.Now().Format(time.RFC3339)); err != nil {
			return err
		}

		for index, comment := range note.Comments {
			fileName := fmt.Sprintf("%02d.md", index+1)
			if err := os.WriteFile(filepath.Join(commentDir, fileName), []byte(comment), 0644); err != nil {
				return err
			}
		}
	}
	return nil
}

func hasMarkdownNotes(dataDir string) (bool, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return false, err
	}
	found := false
	err := filepath.WalkDir(dataDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() {
			name := entry.Name()
			if name == "attachments" || name == "comments" {
				return filepath.SkipDir
			}
			return nil
		}
		if isMarkdownFile(path) {
			found = true
			return fs.SkipAll
		}
		return nil
	})
	if err != nil && !errors.Is(err, fs.SkipAll) {
		return false, err
	}
	return found, nil
}

func (s *Store) notesRootDir() string {
	return filepath.Join(s.dataDir, "notes")
}

func normalizeArticlePath(path string, category string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		trimmed = strings.TrimSpace(category)
	}
	segments := splitPath(trimmed)
	return strings.Join(segments, "/")
}

func (s *Store) buildNoteDir(path string) string {
	segments := splitPath(path)
	root := s.notesRootDir()
	all := append([]string{root}, segments...)
	return filepath.Join(all...)
}

func containsTag(tags []string, target string) bool {
	for _, tag := range tags {
		if strings.TrimSpace(tag) == strings.TrimSpace(target) {
			return true
		}
	}
	return false
}

func (s *Store) buildNoteFilePath(path string, title string) string {
	return filepath.Join(s.buildNoteDir(path), noteFileNameFromTitle(title))
}

func noteFileNameFromTitle(title string) string {
	return strings.TrimSpace(title) + ".md"
}

func validateNoteTitle(title string) error {
	trimmed := strings.TrimSpace(title)
	if trimmed == "" {
		return errors.New("title is required")
	}
	if strings.Contains(trimmed, "/") || strings.Contains(trimmed, "\\") {
		return errors.New("title cannot contain path separator")
	}
	if strings.HasPrefix(trimmed, ".") {
		return errors.New("title cannot start with dot")
	}
	return nil
}

func isMarkdownFile(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".md")
}

func buildPathTitleKey(path string, title string) string {
	return strings.TrimSpace(title)
}

func buildPathTitleLegacyKey(path string, title string) string {
	return strings.TrimSpace(path) + "\n" + strings.TrimSpace(title)
}

func generateNoteID(key string) string {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(key))
	return fmt.Sprintf("note-%x", hash.Sum64())
}

func (s *Store) loadPathTitleIDMap() (map[string]string, error) {
	result := map[string]string{}

	rows, err := s.db.Query("SELECT note_path, note_title, id FROM note_identity_map")
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var notePath string
			var noteTitle string
			var id string
			if scanErr := rows.Scan(&notePath, &noteTitle, &id); scanErr != nil {
				return nil, scanErr
			}
			titleKey := buildPathTitleKey(notePath, noteTitle)
			if _, exists := result[titleKey]; !exists {
				result[titleKey] = id
			}
			result[buildPathTitleLegacyKey(notePath, noteTitle)] = id
		}
	}

	rows, err = s.db.Query("SELECT path, title, id FROM note_metadata")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var notePath string
		var noteTitle string
		var id string
		if scanErr := rows.Scan(&notePath, &noteTitle, &id); scanErr != nil {
			return nil, scanErr
		}
		titleKey := buildPathTitleKey(notePath, noteTitle)
		if _, exists := result[titleKey]; !exists {
			result[titleKey] = id
		}
		legacyKey := buildPathTitleLegacyKey(notePath, noteTitle)
		if _, exists := result[legacyKey]; !exists {
			result[legacyKey] = id
		}
	}
	return result, nil
}

func (s *Store) upsertIdentityMap(noteID string, notePath string, noteTitle string, sourceFile string) error {
	if strings.TrimSpace(noteID) == "" {
		return errors.New("note id is required")
	}
	now := time.Now().Format(time.RFC3339)
	_, err := s.db.Exec(`
INSERT OR REPLACE INTO note_identity_map(id, note_path, note_title, source_file, updated_at)
VALUES(?, ?, ?, ?, ?)
`, noteID, notePath, noteTitle, sourceFile, now)
	return err
}

func (s *Store) locateNoteFile(articleID string) (string, error) {
	var sourceFile string
	err := s.db.QueryRow("SELECT source_file FROM note_identity_map WHERE id = ?", articleID).Scan(&sourceFile)
	if errors.Is(err, sql.ErrNoRows) {
		err = s.db.QueryRow("SELECT source_file FROM note_metadata WHERE id = ?", articleID).Scan(&sourceFile)
	}
	if err == nil && strings.TrimSpace(sourceFile) != "" {
		if stat, statErr := os.Stat(sourceFile); statErr == nil && !stat.IsDir() {
			return sourceFile, nil
		}
	}
	return "", sql.ErrNoRows
}

func (s *Store) cleanupEmptyNoteDirs(start string) {
	root := filepath.Clean(s.notesRootDir())
	current := filepath.Clean(start)
	for current != root && strings.HasPrefix(current, root+string(os.PathSeparator)) {
		entries, err := os.ReadDir(current)
		if err != nil || len(entries) > 0 {
			return
		}
		if removeErr := os.Remove(current); removeErr != nil {
			return
		}
		current = filepath.Dir(current)
	}
}

func buildMarkdown(article Article) string {
	title := strings.TrimSpace(article.Title)
	if title == "" {
		title = article.ID
	}
	content := strings.TrimSpace(article.Content)
	if strings.HasPrefix(content, "# ") {
		return content
	}
	path := strings.TrimSpace(article.Path)
	if path == "" {
		if strings.TrimSpace(article.Category) == "" {
			path = "未分类"
		} else {
			path = article.Category
		}
	}
	tagLine := "#" + path
	if strings.Contains(content, "\n#") || strings.HasSuffix(content, tagLine) {
		return fmt.Sprintf("# %s\n\n%s\n", title, content)
	}
	return fmt.Sprintf("# %s\n\n%s\n\n%s\n", title, content, tagLine)
}

func parseTitle(content string, fallback string) string {
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "# ") {
			title := strings.TrimSpace(strings.TrimPrefix(line, "# "))
			if title != "" {
				return title
			}
		}
	}
	return fallback
}

func parseExcerpt(content string) string {
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "```") {
			continue
		}
		return line
	}
	return "暂无摘要"
}

func parseTags(content string) []string {
	lines := strings.Split(content, "\n")
	tagSet := map[string]struct{}{}
	tags := make([]string, 0)

	start := -1
	for index := len(lines) - 1; index >= 0; index-- {
		line := strings.TrimSpace(lines[index])
		if line == "" {
			continue
		}
		lineTags := parseTagLine(line)
		if len(lineTags) == 0 {
			if start == -1 {
				break
			}
			break
		}
		start = index
	}

	if start != -1 {
		for index := start; index < len(lines); index++ {
			line := strings.TrimSpace(lines[index])
			if line == "" {
				continue
			}
			lineTags := parseTagLine(line)
			if len(lineTags) == 0 {
				break
			}
			for _, rawTag := range lineTags {
				tag := strings.TrimSpace(strings.Trim(rawTag, "/"))
				if tag == "" {
					continue
				}
				if _, exists := tagSet[tag]; exists {
					continue
				}
				tagSet[tag] = struct{}{}
				tags = append(tags, tag)
			}
		}
	}

	if len(tags) == 0 {
		return []string{"未分类"}
	}
	return tags
}

func parseTagLine(line string) []string {
	if !strings.HasPrefix(line, "#") {
		return nil
	}
	if strings.HasPrefix(line, "# ") {
		return nil
	}
	tag := strings.TrimSpace(strings.TrimPrefix(line, "#"))
	if tag == "" {
		return nil
	}
	return []string{tag}
}

func derivePathSegments(tags []string) []string {
	for _, tag := range tags {
		if strings.Contains(tag, "/") {
			return splitPath(tag)
		}
	}
	return splitPath(firstOrDefault(tags, "未分类"))
}

func splitPath(path string) []string {
	parts := strings.Split(path, "/")
	segments := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			segments = append(segments, trimmed)
		}
	}
	if len(segments) == 0 {
		return []string{"未分类"}
	}
	return segments
}

func calcReadMinutes(content string) int {
	runes := len([]rune(strings.ReplaceAll(content, " ", "")))
	if runes <= 0 {
		return 1
	}
	minutes := runes / 400
	if runes%400 != 0 {
		minutes++
	}
	if minutes < 1 {
		return 1
	}
	return minutes
}

func buildNoteTree(articles []Article) []ArticleTreeNode {
	root := &treeBuilder{
		ID:       "root",
		Name:     "root",
		Type:     "folder",
		Children: map[string]*treeBuilder{},
	}

	for _, article := range articles {
		segments := splitPath(article.Path)
		current := root
		prefix := make([]string, 0, len(segments))
		for _, segment := range segments {
			prefix = append(prefix, segment)
			key := strings.Join(prefix, "/")
			child, exists := current.Children[key]
			if !exists {
				child = &treeBuilder{
					ID:       "folder:" + key,
					Name:     segment,
					Type:     "folder",
					Children: map[string]*treeBuilder{},
				}
				current.Children[key] = child
			}
			current = child
		}
		noteKey := "note:" + article.ID
		current.Children[noteKey] = &treeBuilder{
			ID:        noteKey,
			Name:      article.Title,
			Type:      "note",
			ArticleID: article.ID,
			Children:  map[string]*treeBuilder{},
		}
	}

	return toTreeNodes(root.Children)
}

type treeBuilder struct {
	ID        string
	Name      string
	Type      string
	ArticleID string
	Children  map[string]*treeBuilder
}

type tagTreeBuilder struct {
	ID       string
	Name     string
	Path     string
	Level    int
	Icon     string
	Children []*tagTreeBuilder
}

func toTreeNodes(children map[string]*treeBuilder) []ArticleTreeNode {
	keys := make([]string, 0, len(children))
	for key := range children {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i int, j int) bool {
		left := children[keys[i]]
		right := children[keys[j]]
		if left.Type != right.Type {
			return left.Type == "folder"
		}
		return left.Name < right.Name
	})

	nodes := make([]ArticleTreeNode, 0, len(keys))
	for _, key := range keys {
		child := children[key]
		nodes = append(nodes, ArticleTreeNode{
			ID:        child.ID,
			Name:      child.Name,
			Type:      child.Type,
			ArticleID: child.ArticleID,
			Children:  toTreeNodes(child.Children),
		})
	}
	return nodes
}

func cloneTree(nodes []ArticleTreeNode) []ArticleTreeNode {
	result := make([]ArticleTreeNode, 0, len(nodes))
	for _, node := range nodes {
		result = append(result, ArticleTreeNode{
			ID:        node.ID,
			Name:      node.Name,
			Type:      node.Type,
			ArticleID: node.ArticleID,
			Icon:      node.Icon,
			Children:  cloneTree(node.Children),
		})
	}
	return result
}

func buildTagTreeNode(source *tagTreeBuilder) TagTreeNode {
	node := TagTreeNode{
		ID:       source.ID,
		Name:     source.Name,
		Path:     source.Path,
		Level:    source.Level,
		Icon:     source.Icon,
		Children: []TagTreeNode{},
	}
	for _, child := range source.Children {
		node.Children = append(node.Children, buildTagTreeNode(child))
	}
	return node
}

func sortTagTreeNodes(nodes []TagTreeNode) {
	sort.Slice(nodes, func(i int, j int) bool {
		left := nodes[i]
		right := nodes[j]
		if left.Level != right.Level {
			return left.Level < right.Level
		}
		return left.Name < right.Name
	})
	for index := range nodes {
		sortTagTreeNodes(nodes[index].Children)
	}
}

func (s *Store) loadViewMap() (map[string]int, error) {
	rows, err := s.db.Query("SELECT id, views FROM note_runtime_metadata")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	viewMap := map[string]int{}
	for rows.Next() {
		var id string
		var views int
		if err = rows.Scan(&id, &views); err != nil {
			return nil, err
		}
		viewMap[id] = views
	}
	return viewMap, nil
}

func (s *Store) loadCreatedAtMap() (map[string]string, error) {
	rows, err := s.db.Query("SELECT id, created_at FROM note_metadata")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	createdAtMap := map[string]string{}
	for rows.Next() {
		var id string
		var createdAt string
		if err = rows.Scan(&id, &createdAt); err != nil {
			return nil, err
		}
		createdAtMap[id] = createdAt
	}
	return createdAtMap, nil
}

type renderCacheEntry struct {
	Hash string
	HTML string
}

func (s *Store) loadRenderCacheMap() (map[string]renderCacheEntry, error) {
	rows, err := s.db.Query("SELECT id, content_hash, rendered_html FROM note_metadata")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cache := map[string]renderCacheEntry{}
	for rows.Next() {
		var id string
		var hash string
		var html string
		if err = rows.Scan(&id, &hash, &html); err != nil {
			return nil, err
		}
		cache[id] = renderCacheEntry{Hash: hash, HTML: html}
	}
	return cache, nil
}

func calcContentHash(content string) string {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(content))
	return fmt.Sprintf("%x", hash.Sum64())
}

func renderArticleHTMLForCache(article Article) string {
	preprocessed := preprocessObsidianForEmail(article.Content, article.Title, article.ID, "")
	var rendered bytes.Buffer
	md := goldmark.New(goldmark.WithExtensions(extension.GFM))
	if err := md.Convert([]byte(preprocessed), &rendered); err != nil {
		LogWarnf("store", "render html failed, article=%s, err=%v", article.ID, err)
		return ""
	}
	return rendered.String()
}

func (s *Store) loadCurrentMetadataIDs() (map[string]struct{}, error) {
	rows, err := s.db.Query("SELECT id FROM note_metadata")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := map[string]struct{}{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		result[id] = struct{}{}
	}
	return result, nil
}

func (s *Store) syncRuntimeMetadataTx(tx *sql.Tx, articles []Article) error {
	now := time.Now().Format(time.RFC3339)
	alive := map[string]struct{}{}
	for _, article := range articles {
		alive[article.ID] = struct{}{}
		if _, err := tx.Exec(
			"INSERT INTO note_runtime_metadata(id, views, updated_at) VALUES(?, ?, ?) ON CONFLICT(id) DO NOTHING",
			article.ID, 0, now,
		); err != nil {
			return err
		}
	}

	rows, err := tx.Query("SELECT id FROM note_runtime_metadata")
	if err != nil {
		return err
	}
	defer rows.Close()

	stale := make([]string, 0)
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return err
		}
		if _, ok := alive[id]; !ok {
			stale = append(stale, id)
		}
	}
	for _, id := range stale {
		if _, err = tx.Exec("DELETE FROM note_runtime_metadata WHERE id = ?", id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) getViewCount(articleID string) (int, error) {
	var views int
	err := s.db.QueryRow("SELECT views FROM note_runtime_metadata WHERE id = ?", articleID).Scan(&views)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return views, nil
}

func (s *Store) IncrementView(articleID string) error {
	now := time.Now().Format(time.RFC3339)
	_, err := s.db.Exec(`
INSERT INTO note_runtime_metadata(id, views, updated_at)
VALUES(?, 1, ?)
ON CONFLICT(id) DO UPDATE SET views = views + 1, updated_at = excluded.updated_at
`, articleID, now)
	return err
}

func (s *Store) attachNodeIcons(nodes []ArticleTreeNode) error {
	nodeTypeMap := map[string]string{}
	collectNodeTypes(nodes, nodeTypeMap)

	rows, err := s.db.Query("SELECT node_id, icon FROM note_visual_metadata")
	if err != nil {
		return err
	}
	iconMap := map[string]string{}
	for rows.Next() {
		var nodeID string
		var icon string
		if err = rows.Scan(&nodeID, &icon); err != nil {
			rows.Close()
			return err
		}
		iconMap[nodeID] = icon
	}
	rows.Close()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now().Format(time.RFC3339)
	for nodeID, nodeType := range nodeTypeMap {
		icon, ok := iconMap[nodeID]
		if !ok || strings.TrimSpace(icon) == "" {
			icon = randomNotionIcon()
		}
		iconMap[nodeID] = icon
		if _, err = tx.Exec(`
INSERT INTO note_visual_metadata(node_id, node_type, icon, updated_at)
VALUES(?, ?, ?, ?)
ON CONFLICT(node_id) DO UPDATE SET node_type = excluded.node_type, icon = excluded.icon, updated_at = excluded.updated_at
`, nodeID, nodeType, icon, now); err != nil {
			return err
		}
	}

	staleNodeIDs := make([]string, 0)
	for nodeID := range iconMap {
		if _, exists := nodeTypeMap[nodeID]; !exists {
			staleNodeIDs = append(staleNodeIDs, nodeID)
		}
	}
	for _, nodeID := range staleNodeIDs {
		if _, err = tx.Exec("DELETE FROM note_visual_metadata WHERE node_id = ?", nodeID); err != nil {
			return err
		}
		delete(iconMap, nodeID)
	}

	if err = tx.Commit(); err != nil {
		return err
	}
	applyNodeIcons(nodes, iconMap)
	return nil
}

func collectNodeTypes(nodes []ArticleTreeNode, bucket map[string]string) {
	for _, node := range nodes {
		bucket[node.ID] = node.Type
		if len(node.Children) > 0 {
			collectNodeTypes(node.Children, bucket)
		}
	}
}

func applyNodeIcons(nodes []ArticleTreeNode, iconMap map[string]string) {
	for i := range nodes {
		if icon, ok := iconMap[nodes[i].ID]; ok {
			nodes[i].Icon = icon
		}
		if len(nodes[i].Children) > 0 {
			applyNodeIcons(nodes[i].Children, iconMap)
		}
	}
}

func randomNotionIcon() string {
	max := big.NewInt(int64(len(notionIconPool)))
	randomIndex, err := cryptorand.Int(cryptorand.Reader, max)
	if err != nil {
		return notionIconPool[0]
	}
	return notionIconPool[randomIndex.Int64()]
}

var notionIconPool = []string{
	"📄", "📝", "📌", "📚", "📖", "📘", "📙", "📗", "📕", "📓", "📒", "📔", "🗂️", "📂", "📁", "🗃️", "🧾", "📎", "📐", "📏",
	"✏️", "🖊️", "🖋️", "🖌️", "🧠", "💡", "🔖", "🏷️", "📋", "🧭", "🧩", "🧪", "⚙️", "🔧", "🛠️", "📈", "📊", "📉", "🗒️", "📇",
	"📅", "🗓️", "⏰", "⏳", "⌛", "🕒", "🕘", "🕛", "🌟", "✨", "🔥", "💫", "🌈", "☀️", "🌤️", "⛅", "🌙", "⭐", "☁️", "🌧️",
	"🌱", "🌿", "🍀", "🌵", "🌳", "🌲", "🌸", "🌼", "🌻", "🌺", "🍁", "🍂", "🍃", "🪴", "🌾", "🌍", "🌎", "🌏", "🗺️", "🧳",
	"🏠", "🏡", "🏢", "🏛️", "🏫", "🏭", "🏗️", "🛤️", "🚀", "🛰️", "🛸", "🚂", "🚲", "🛵", "🚗", "🚕", "🛶", "⛵", "✈️", "🛫",
	"🧵", "🪡", "🧶", "🧥", "👕", "👖", "🧣", "🧤", "👟", "👞", "🎒", "💼", "👜", "👓", "🕶️", "🧢", "👑", "🎓", "🪖", "📿",
	"💻", "⌨️", "🖥️", "🖨️", "🖱️", "💾", "💿", "📱", "☎️", "📡", "🔌", "💡", "🔋", "📶", "🧮", "🧷", "🔐", "🔒", "🔓", "🛡️",
	"🔍", "🔎", "🧭", "🧱", "⚗️", "🔬", "🔭", "🧬", "🦠", "🧫", "🧲", "🪛", "🔨", "🪚", "🪓", "⛏️", "🧯", "🪜", "🧰", "⚖️",
	"🍎", "🍊", "🍋", "🍌", "🍉", "🍇", "🍓", "🫐", "🍒", "🥭", "🍍", "🥝", "🥑", "🥦", "🥕", "🌽", "🥔", "🍞", "🥐", "🥨",
	"☕", "🍵", "🧋", "🥤", "🍶", "🍷", "🍺", "🍫", "🍪", "🍰", "🧁", "🍨", "🍜", "🍛", "🍣", "🍱", "🥟", "🌮", "🍔", "🍕",
	"🎯", "🎲", "🧩", "♟️", "🎮", "🕹️", "🎸", "🎹", "🎻", "🥁", "🎤", "🎧", "📻", "🎬", "🎭", "🎨", "🖼️", "📷", "🎞️", "🎟️",
	"💬", "🗨️", "🧾", "📢", "📣", "📯", "🔔", "🔕", "📮", "📬", "📪", "✉️", "📨", "📩", "📤", "📥", "🧷", "📌", "🧵", "🪢",
	"😀", "😄", "😁", "😎", "🤓", "🧐", "🤖", "👾", "🦄", "🐳", "🦊", "🐼", "🐧", "🦉", "🦋", "🐝", "🐙", "🐢", "🦖", "🐉",
}

func firstOrDefault(values []string, fallback string) string {
	if len(values) == 0 {
		return fallback
	}
	return values[0]
}

func buildCommentTree(all []Comment) []Comment {
	childrenMap := map[int64][]Comment{}
	roots := make([]Comment, 0)

	for _, item := range all {
		if item.ParentID <= 0 {
			roots = append(roots, item)
			continue
		}
		childrenMap[item.ParentID] = append(childrenMap[item.ParentID], item)
	}

	var build func(Comment) Comment
	build = func(comment Comment) Comment {
		children := childrenMap[comment.ID]
		for index := range children {
			children[index] = build(children[index])
		}
		comment.Replies = children
		return comment
	}

	result := make([]Comment, 0, len(roots))
	for _, root := range roots {
		result = append(result, build(root))
	}
	return result
}

func queryCommentByID(queryer interface {
	QueryRow(query string, args ...any) *sql.Row
}, commentID int64) (Comment, error) {
	var comment Comment
	err := queryer.QueryRow(`
SELECT id, article_id, parent_id, author, content, likes, created_at, updated_at
FROM comments WHERE id = ?
`, commentID).Scan(
		&comment.ID,
		&comment.ArticleID,
		&comment.ParentID,
		&comment.Author,
		&comment.Content,
		&comment.Likes,
		&comment.CreatedAt,
		&comment.UpdatedAt,
	)
	if err != nil {
		return Comment{}, err
	}
	comment.Replies = []Comment{}
	return comment, nil
}

func (s *Store) ensureDefaultComments() error {
	var count int
	if err := s.db.QueryRow("SELECT COUNT(1) FROM comments").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	now := time.Now().Format("2006-01-02 15:04")
	_, err := s.db.Exec(`
INSERT INTO comments(article_id, parent_id, author, content, likes, created_at, updated_at)
VALUES
  (?, 0, ?, ?, ?, ?, ?),
  (?, 0, ?, ?, ?, ?, ?)
`,
		"java-generics-deep-dive", "张三", "写得很详细，对泛型的理解更深入了！特别是类型擦除那部分，之前一直不太明白。", 5, "2024-01-16 10:30", now,
		"java-generics-deep-dive", "李四", "请问泛型在实际项目中有什么最佳实践吗？希望能出一篇实战文章。", 3, "2024-01-15 18:45", now,
	)
	return err
}
