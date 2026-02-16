// Package app 提供简单的接口防爆破限流能力。
// Author: Codex
// Created: 2026-02-16
package app

import (
	"sync"
	"time"
)

type ActionLimiter struct {
	mu      sync.Mutex
	records map[string][]time.Time
}

func NewActionLimiter() *ActionLimiter {
	return &ActionLimiter{
		records: map[string][]time.Time{},
	}
}

func (l *ActionLimiter) Allow(key string, limit int, window time.Duration) bool {
	if limit <= 0 {
		return true
	}

	now := time.Now()
	cutoff := now.Add(-window)

	l.mu.Lock()
	defer l.mu.Unlock()

	history := l.records[key]
	filtered := make([]time.Time, 0, len(history)+1)
	for _, item := range history {
		if item.After(cutoff) {
			filtered = append(filtered, item)
		}
	}

	if len(filtered) >= limit {
		l.records[key] = filtered
		return false
	}

	filtered = append(filtered, now)
	l.records[key] = filtered
	return true
}
