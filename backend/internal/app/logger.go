// Package app 提供应用级日志能力，支持按天切分与压缩。
// Author: Codex
// Created: 2026-02-16
package app

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type LogLevel int

const (
	LogLevelDebug LogLevel = iota
	LogLevelInfo
	LogLevelWarn
	LogLevelError
)

type dailyRotateWriter struct {
	dir         string
	prefix      string
	currentDate string
	currentFile *os.File
	mu          sync.Mutex
}

type AppLogger struct {
	level  LogLevel
	writer *dailyRotateWriter
}

var (
	defaultLoggerMu sync.RWMutex
	defaultLogger   *AppLogger
)

func InitLogger(logDir string, level string) error {
	writer, err := newDailyRotateWriter(logDir, "blog")
	if err != nil {
		return err
	}
	logger := &AppLogger{
		level:  parseLogLevel(level),
		writer: writer,
	}

	defaultLoggerMu.Lock()
	defaultLogger = logger
	defaultLoggerMu.Unlock()
	return nil
}

func SetLogLevel(level string) {
	defaultLoggerMu.Lock()
	defer defaultLoggerMu.Unlock()
	if defaultLogger == nil {
		return
	}
	defaultLogger.level = parseLogLevel(level)
}

func LogDebugf(component string, format string, args ...any) {
	logf(LogLevelDebug, component, format, args...)
}

func LogInfof(component string, format string, args ...any) {
	logf(LogLevelInfo, component, format, args...)
}

func LogWarnf(component string, format string, args ...any) {
	logf(LogLevelWarn, component, format, args...)
}

func LogErrorf(component string, format string, args ...any) {
	logf(LogLevelError, component, format, args...)
}

func logf(level LogLevel, component string, format string, args ...any) {
	defaultLoggerMu.RLock()
	logger := defaultLogger
	defaultLoggerMu.RUnlock()

	message := fmt.Sprintf(format, args...)
	levelText := levelToText(level)
	if logger == nil {
		fmt.Printf("%s [%s] [%s] %s\n", time.Now().Format("2006-01-02 15:04:05.000"), levelText, component, message)
		return
	}
	if level < logger.level {
		return
	}
	line := fmt.Sprintf("%s [%s] [%s] %s\n", time.Now().Format("2006-01-02 15:04:05.000"), levelText, component, message)
	_ = logger.writer.WriteLine(line)
}

func parseLogLevel(level string) LogLevel {
	switch strings.ToUpper(strings.TrimSpace(level)) {
	case "DEBUG":
		return LogLevelDebug
	case "WARN":
		return LogLevelWarn
	case "ERROR":
		return LogLevelError
	default:
		return LogLevelInfo
	}
}

func levelToText(level LogLevel) string {
	switch level {
	case LogLevelDebug:
		return "DEBUG"
	case LogLevelWarn:
		return "WARN"
	case LogLevelError:
		return "ERROR"
	default:
		return "INFO"
	}
}

func newDailyRotateWriter(dir string, prefix string) (*dailyRotateWriter, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	writer := &dailyRotateWriter{
		dir:    dir,
		prefix: prefix,
	}
	if err := writer.compressHistoricalLogs(); err != nil {
		return nil, err
	}
	if err := writer.ensureCurrentFile(); err != nil {
		return nil, err
	}
	return writer, nil
}

func (w *dailyRotateWriter) WriteLine(line string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.ensureCurrentFile(); err != nil {
		return err
	}
	if _, err := w.currentFile.WriteString(line); err != nil {
		return err
	}
	_, _ = os.Stdout.WriteString(line)
	return nil
}

func (w *dailyRotateWriter) ensureCurrentFile() error {
	nowDate := time.Now().Format("2006-01-02")
	if w.currentFile != nil && w.currentDate == nowDate {
		return nil
	}

	var oldPath string
	if w.currentFile != nil {
		oldPath = filepath.Join(w.dir, fmt.Sprintf("%s-%s.log", w.prefix, w.currentDate))
		_ = w.currentFile.Close()
		w.currentFile = nil
	}

	logPath := filepath.Join(w.dir, fmt.Sprintf("%s-%s.log", w.prefix, nowDate))
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	w.currentDate = nowDate
	w.currentFile = file

	if oldPath != "" {
		_ = compressLogFile(oldPath)
	}
	return nil
}

func (w *dailyRotateWriter) compressHistoricalLogs() error {
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return err
	}
	today := time.Now().Format("2006-01-02")
	todayFile := fmt.Sprintf("%s-%s.log", w.prefix, today)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, w.prefix+"-") || !strings.HasSuffix(name, ".log") {
			continue
		}
		if name == todayFile {
			continue
		}
		if err := compressLogFile(filepath.Join(w.dir, name)); err != nil {
			return err
		}
	}
	return nil
}

func compressLogFile(logPath string) error {
	if strings.HasSuffix(logPath, ".gz") {
		return nil
	}
	info, err := os.Stat(logPath)
	if err != nil {
		return nil
	}
	if info.Size() == 0 {
		_ = os.Remove(logPath)
		return nil
	}

	source, err := os.Open(logPath)
	if err != nil {
		return err
	}
	defer source.Close()

	targetPath := logPath + ".gz"
	target, err := os.Create(targetPath)
	if err != nil {
		return err
	}
	gzWriter := gzip.NewWriter(target)
	if _, err = io.Copy(gzWriter, source); err != nil {
		_ = gzWriter.Close()
		_ = target.Close()
		return err
	}
	if err = gzWriter.Close(); err != nil {
		_ = target.Close()
		return err
	}
	if err = target.Close(); err != nil {
		return err
	}
	return os.Remove(logPath)
}
