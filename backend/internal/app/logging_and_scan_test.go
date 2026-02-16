package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultConfigIncludesLoggingLevel(t *testing.T) {
	cfg := defaultConfig()
	if cfg.Logging.Level != "INFO" {
		t.Fatalf("expected default logging level INFO, got %q", cfg.Logging.Level)
	}
}

func TestScanAndSyncWithNotifyTriggersHookForNewMarkdown(t *testing.T) {
	tempDir := t.TempDir()
	noteRoot := filepath.Join(tempDir, "notes", "Agent工程", "测试")
	if err := os.MkdirAll(noteRoot, 0755); err != nil {
		t.Fatalf("mkdir note dir failed: %v", err)
	}
	initialNote := filepath.Join(noteRoot, "初始笔记.md")
	if err := os.WriteFile(initialNote, []byte("# 初始笔记\n\n初始内容\n\n#Agent工程/测试\n"), 0644); err != nil {
		t.Fatalf("write initial note failed: %v", err)
	}

	store, err := openStore(filepath.Join(tempDir, "blog.db"), StoreOptions{
		ScanInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("open store failed: %v", err)
	}
	defer store.db.Close()

	notifyCh := make(chan []Article, 1)
	store.SetOnNewArticles(func(articles []Article) {
		notifyCh <- articles
	})

	newNote := filepath.Join(noteRoot, "新增笔记.md")
	if err = os.WriteFile(newNote, []byte("# 新增笔记\n\n新增内容\n\n#Agent工程/测试\n"), 0644); err != nil {
		t.Fatalf("write new note failed: %v", err)
	}
	if err = store.scanAndSyncWithNotify(true); err != nil {
		t.Fatalf("scan and sync with notify failed: %v", err)
	}

	select {
	case payload := <-notifyCh:
		if len(payload) == 0 {
			t.Fatalf("expected new article payload")
		}
		found := false
		for _, article := range payload {
			if article.Title == "新增笔记" {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected payload contains 新增笔记, got %+v", payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("expected notify hook invoked")
	}
}

func TestInitLoggerCompressesHistoricalLogs(t *testing.T) {
	tempDir := t.TempDir()
	historyLog := filepath.Join(tempDir, "blog-2000-01-01.log")
	if err := os.WriteFile(historyLog, []byte("legacy log"), 0644); err != nil {
		t.Fatalf("write history log failed: %v", err)
	}

	if err := InitLogger(tempDir, "INFO"); err != nil {
		t.Fatalf("init logger failed: %v", err)
	}

	if _, err := os.Stat(historyLog); !os.IsNotExist(err) {
		t.Fatalf("history log should be compressed and removed, err=%v", err)
	}
	if _, err := os.Stat(historyLog + ".gz"); err != nil {
		t.Fatalf("history gzip log should exist, err=%v", err)
	}
}
