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
	"time"

	"github.com/yshengliao/goscriptor/redis"
)

// redis script definition
// the hash key definition that is used to store the script
var (
	scriptDefinition = "scriptor_v.0.0.0"
)

// Scriptor manages Redis Lua scripts.
type Scriptor struct {
	Client                *redis.Client
	scripts               map[string]string
	redisScriptDB         int
	redisScriptDefinition string
}

// New creates a new scriptor with the given redis client.
// Note: goscriptor does not support Redis Cluster because it uses the SELECT command internally.
func New(client *redis.Client, scriptDB int, redisScriptDefinition string, scripts map[string]string) (*Scriptor, error) {
	if client == nil {
		return nil, ErrNilClient
	}

	s := &Scriptor{
		Client:        client,
		scripts:       make(map[string]string),
		redisScriptDB: scriptDB,
	}

	if redisScriptDefinition != "" {
		s.redisScriptDefinition = redisScriptDefinition
	} else {
		s.redisScriptDefinition = scriptDefinition
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.Client.Ping(ctx); err != nil {
		return nil, err
	}

	scriptDescriptor, err := NewScriptDescriptor(ctx, s.Client, scripts, s.redisScriptDefinition, s.redisScriptDB)
	if err != nil {
		return nil, err
	}
	s.scripts = scriptDescriptor.container

	return s, nil
}

// NewDB creates a new Scriptor with a new redis client from Option.
func NewDB(opt *Option, scriptDB int, redisScriptDefinition string, scripts map[string]string) (*Scriptor, error) {
	if opt == nil {
		return nil, ErrNilOption
	}

	return New(opt.Create(), scriptDB, redisScriptDefinition, scripts)
}

// Exec executes a Lua script directly.
func (s *Scriptor) Exec(ctx context.Context, script string, keys []string, args ...any) (any, error) {
	if script == "" {
		return nil, ErrScriptNotFound
	}
	return s.Client.Eval(ctx, script, keys, args...)
}

// ExecSha executes a cached Lua script by name.
func (s *Scriptor) ExecSha(ctx context.Context, scriptname string, keys []string, args ...any) (any, error) {
	sha, ok := s.scripts[scriptname]
	if !ok || sha == "" {
		return nil, ErrScriptNotFound
	}
	return s.Client.EvalSha(ctx, sha, keys, args...)
}

// Close closes the underlying Redis client.
func (s *Scriptor) Close() error {
	return s.Client.Close()
}
