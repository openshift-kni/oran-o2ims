/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package cache

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Entry is a thread-safe, lazily populated cache entry with optional TTL
// expiration and explicit invalidation support.
type Entry[T any] struct {
	mu        sync.RWMutex
	data      T
	valid     bool
	expiresAt time.Time
	maxAge    time.Duration
	loader    func(ctx context.Context) (T, error)
	name      string
}

// NewEntry creates a cache entry that loads data using the provided function.
// The maxAge parameter controls TTL-based expiration (0 means no TTL — only
// explicit invalidation triggers a reload). The name is used for logging.
func NewEntry[T any](name string, maxAge time.Duration, loader func(ctx context.Context) (T, error)) *Entry[T] {
	return &Entry[T]{
		name:   name,
		maxAge: maxAge,
		loader: loader,
	}
}

// Get returns the cached data, loading it if necessary.
// The returned value is shared across all callers and must not be modified.
func (c *Entry[T]) Get(ctx context.Context) (T, error) {
	c.mu.RLock()
	if c.valid && (c.maxAge == 0 || time.Now().Before(c.expiresAt)) {
		defer c.mu.RUnlock()
		return c.data, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.valid && (c.maxAge == 0 || time.Now().Before(c.expiresAt)) {
		return c.data, nil
	}

	data, err := c.loader(ctx)
	if err != nil {
		var zero T
		return zero, fmt.Errorf("failed to load %s cache: %w", c.name, err)
	}

	c.data = data
	c.valid = true
	if c.maxAge > 0 {
		c.expiresAt = time.Now().Add(c.maxAge)
	}

	slog.Info("Cache populated", "cache", c.name)
	return c.data, nil
}

// Invalidate clears the cache so the next Get call reloads from the source.
func (c *Entry[T]) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.valid = false
	slog.Info("Cache invalidated", "cache", c.name)
}
