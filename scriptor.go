// Package goscriptor provides a zero-dependency Redis Lua script manager for Go.
//
// It handles script registration, SHA1 caching, and atomic execution via EVALSHA,
// backed by a built-in RESP2 Redis client with production-grade connection pooling.
//
// For standalone Redis client usage, import the redis sub-package:
//
//	import "github.com/yshengliao/goscriptor/redis"
//
// Note: This library uses the SELECT command internally. Redis Cluster is not supported.
package goscriptor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/yshengliao/goscriptor/redis"
)

const scriptDefinition = "scriptor_v.0.0.0"

// Scriptor manages Redis Lua scripts.
//
// A Scriptor is safe for concurrent use by multiple goroutines: its script
// registry is guarded by an internal RWMutex.
type Scriptor struct {
	// Client is the underlying pooled Redis client. It is exported so callers
	// can issue general-purpose commands against the same pool. Note: sending a
	// raw SELECT through Client.Do poisons the pooled connection's database
	// state for whichever connection serviced it, since that connection then
	// returns to the pool selected onto a different DB. The SELECTs that
	// goscriptor issues from inside its Lua scripts are scoped to the script's
	// execution and do NOT leak, but a raw SELECT does — avoid it.
	Client *redis.Client

	mu                    sync.RWMutex
	scripts               map[string]scriptEntry
	redisScriptDB         int
	redisScriptDefinition string
}

// New creates a new Scriptor with the given Redis client.
//
// The supplied ctx governs the connectivity Ping and the initial script
// registration (or load-from-cache). The caller controls the deadline: pass a
// ctx with a timeout to bound startup. Note: goscriptor does not support Redis
// Cluster because it uses the SELECT command internally.
func New(ctx context.Context, client *redis.Client, scriptDB int, redisScriptDefinition string, scripts map[string]string) (*Scriptor, error) {
	if client == nil {
		return nil, ErrNilClient
	}

	s := &Scriptor{
		Client:        client,
		scripts:       make(map[string]scriptEntry),
		redisScriptDB: scriptDB,
	}

	if redisScriptDefinition != "" {
		s.redisScriptDefinition = redisScriptDefinition
	} else {
		s.redisScriptDefinition = scriptDefinition
	}

	if err := s.Client.Ping(ctx); err != nil {
		return nil, err
	}

	sd, err := newScriptDescriptor(ctx, s.Client, scripts, s.redisScriptDefinition, s.redisScriptDB)
	if err != nil {
		return nil, err
	}
	s.scripts = sd.container

	return s, nil
}

// NewDB creates a new Scriptor with a new Redis client built from Option.
//
// The supplied ctx governs the connectivity Ping and the initial script
// registration; the caller controls the deadline.
func NewDB(ctx context.Context, opt *Option, scriptDB int, redisScriptDefinition string, scripts map[string]string) (*Scriptor, error) {
	if opt == nil {
		return nil, ErrNilOption
	}

	if opt.Host == "" {
		return nil, fmt.Errorf("goscriptor: option Host must not be empty")
	}
	if opt.Port < 1 || opt.Port > 65535 {
		return nil, fmt.Errorf("goscriptor: option Port %d is out of range (1..65535)", opt.Port)
	}

	return New(ctx, opt.Create(), scriptDB, redisScriptDefinition, scripts)
}

// Exec executes a Lua script directly.
func (s *Scriptor) Exec(ctx context.Context, script string, keys []string, args ...any) (any, error) {
	if script == "" {
		return nil, ErrEmptyScript
	}
	return s.Client.Eval(ctx, script, keys, args...)
}

// ExecSha executes a cached Lua script by name.
//
// If the script is missing from the Redis script cache (a NOSCRIPT reply, e.g.
// after a Redis restart or SCRIPT FLUSH) and its source body was retained,
// ExecSha transparently re-loads the script, persists the new SHA, and retries
// the execution exactly once. If the body is unavailable (the Scriptor was
// built via the load-from-cache path), ExecSha returns an error satisfying
// errors.Is(err, ErrScriptNotCached).
func (s *Scriptor) ExecSha(ctx context.Context, scriptname string, keys []string, args ...any) (any, error) {
	s.mu.RLock()
	entry, ok := s.scripts[scriptname]
	s.mu.RUnlock()
	if !ok || entry.sha == "" {
		return nil, ErrScriptNotFound
	}

	res, err := s.Client.EvalSha(ctx, entry.sha, keys, args...)
	if err == nil {
		return res, nil
	}
	if !isNoScript(err) {
		return nil, err
	}

	// NOSCRIPT: the script is no longer in the Redis cache. Self-heal only if
	// the source body was retained.
	if entry.body == "" {
		return nil, fmt.Errorf("goscriptor: cannot reload script %q: %w: %w", scriptname, ErrScriptNotCached, err)
	}

	newSha, loadErr := s.Client.ScriptLoad(ctx, entry.body)
	if loadErr != nil {
		return nil, loadErr
	}

	// Update the cached SHA under the write lock. The body is unchanged.
	s.mu.Lock()
	if cur, stillThere := s.scripts[scriptname]; stillThere {
		cur.sha = newSha
		s.scripts[scriptname] = cur
	}
	s.mu.Unlock()

	// Best-effort persist of the new SHA to the registry hash. A persist
	// failure must not fail the execution after a successful reload.
	_ = setLuaScript(ctx, s.Client, s.redisScriptDefinition, scriptname, newSha, s.redisScriptDB)

	// Retry exactly once with the freshly loaded SHA.
	return s.Client.EvalSha(ctx, newSha, keys, args...)
}

// isNoScript reports whether err is a Redis NOSCRIPT server error.
func isNoScript(err error) bool {
	var rerr redis.RedisError
	if !errors.As(err, &rerr) {
		return false
	}
	return strings.HasPrefix(string(rerr), "NOSCRIPT")
}

// Close closes the underlying Redis client.
func (s *Scriptor) Close() error {
	return s.Client.Close()
}
