package config

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"sync"
	"time"
)

// Holder keeps the active configuration and reloads it when the file changes.
// An invalid file never becomes active: the old config stays and the error is
// kept for the admin banner.
type Holder struct {
	path string

	mu      sync.RWMutex
	cfg     *Config
	raw     []byte
	lastErr error
}

func NewHolder(path string) (*Holder, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg, err := Parse(raw)
	if err != nil {
		return nil, err
	}
	return &Holder{path: path, cfg: cfg, raw: raw}, nil
}

// NewStaticHolder wraps a fixed config, mainly for tests.
func NewStaticHolder(c *Config) *Holder { return &Holder{cfg: c} }

func (h *Holder) Get() *Config {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.cfg
}

// LastError returns the error of the most recent failed reload, or nil.
func (h *Holder) LastError() error {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.lastErr
}

// Reload reads the file again. It compares content instead of mtime because
// ConfigMap updates swap symlinks.
func (h *Holder) Reload() {
	raw, err := os.ReadFile(h.path)
	h.mu.Lock()
	defer h.mu.Unlock()
	if err != nil {
		h.lastErr = err
		return
	}
	if bytes.Equal(raw, h.raw) {
		return
	}
	cfg, err := Parse(raw)
	if err != nil {
		h.lastErr = err
		h.raw = raw // don't log the same broken file every tick
		slog.Error("config reload failed, keeping previous config", "err", err)
		return
	}
	h.cfg, h.raw, h.lastErr = cfg, raw, nil
	slog.Info("config reloaded", "targets", len(cfg.Targets))
}

// Watch polls the file until ctx ends.
func (h *Holder) Watch(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			h.Reload()
		}
	}
}
