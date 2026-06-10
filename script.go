package goscriptor

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/yshengliao/goscriptor/redis"
)

// Lua script templates used to store and retrieve script SHA1 hashes in Redis.
var (
	loadLuaScriptTemplate = `
		redis.call('SELECT', ARGV[1])
		return redis.call('HGETALL', KEYS[1])
	`

	setLuaScriptTemplate = `
		redis.call('SELECT', ARGV[1])
		return redis.call('HSET', KEYS[1], ARGV[2], ARGV[3])
	`

	// availableLuaScriptTemplate combines EXISTS + HEXISTS + HGET in a single
	// round-trip. Returns the SHA1 string if the key, field, and script cache
	// all exist; otherwise returns a sentinel string that cannot collide with a
	// 40-hex-char SHA1 value.
	availableLuaScriptTemplate = `
		redis.call('SELECT', ARGV[1])
		if redis.call('EXISTS', KEYS[1]) == 0 then
			return '__GOSCRIPTOR_KEY_NOT_FOUND__'
		end
		if redis.call('HEXISTS', KEYS[1], ARGV[2]) == 0 then
			return '__GOSCRIPTOR_FIELD_NOT_FOUND__'
		end
		return redis.call('HGET', KEYS[1], ARGV[2])
	`
)

// scriptEntry pairs a script's SHA1 hash with its source body. The body is
// retained so a Scriptor can re-load a script into the Redis script cache and
// self-heal after a NOSCRIPT failure (e.g. a Redis restart or SCRIPT FLUSH).
// When the body is empty (the load-from-cache path), self-healing is not
// possible.
type scriptEntry struct {
	sha  string
	body string
}

// scriptDescriptor manages script registration and loading.
type scriptDescriptor struct {
	container map[string]scriptEntry
}

// newScriptDescriptor creates a new script descriptor.
func newScriptDescriptor(ctx context.Context, client *redis.Client, scripts map[string]string, redisScriptDefinition string, db int) (*scriptDescriptor, error) {
	if client == nil {
		return nil, ErrNilClient
	}

	sd := &scriptDescriptor{
		container: make(map[string]scriptEntry),
	}

	if len(scripts) == 0 {
		err := sd.loadScripts(ctx, client, redisScriptDefinition, db)
		if err != nil {
			return nil, err
		}
		return sd, nil
	}

	err := sd.register(ctx, client, scripts, redisScriptDefinition, db)
	if err != nil {
		return nil, err
	}

	return sd, nil
}

// register loads scripts into Redis and records their SHA1 hashes alongside
// their source bodies. A cached SHA1 is reused only when it matches the SHA1
// of the current body; on a mismatch the new body is loaded and persisted so
// that a changed script body under the same name always wins.
func (sd *scriptDescriptor) register(ctx context.Context, client *redis.Client, scripts map[string]string, redisScriptDefinition string, db int) error {
	if client == nil {
		return ErrNilClient
	}

	sd.container = make(map[string]scriptEntry)

	for name, body := range scripts {
		sha1hash, err := availableLuaScript(ctx, client, redisScriptDefinition, db, name)
		if err == nil && sha1hash == sha1Hex(body) {
			// The cached SHA matches the current body: reuse it.
			sd.container[name] = scriptEntry{sha: sha1hash, body: body}
			continue
		}

		// A real error (network failure, auth error, etc.) must surface. The
		// expected "not registered yet" cases — ErrKeyNotFound,
		// ErrScriptNotFound, ErrScriptNotCached — fall through to reload, as
		// does a stale-SHA mismatch (err == nil but the digest differs).
		if err != nil && !errors.Is(err, ErrKeyNotFound) && !errors.Is(err, ErrScriptNotFound) && !errors.Is(err, ErrScriptNotCached) {
			return fmt.Errorf("goscriptor: checking script %q: %w", name, err)
		}

		sha1hash, err = client.ScriptLoad(ctx, body)
		if err != nil {
			return err
		}

		err = setLuaScript(ctx, client, redisScriptDefinition, name, sha1hash, db)
		if err != nil {
			return err
		}

		sd.container[name] = scriptEntry{sha: sha1hash, body: body}
	}

	return nil
}

// loadScripts loads previously registered script SHA1 hashes from Redis.
//
// Because the source bodies are unknown when loading from the registry, the
// resulting entries carry empty bodies. A Scriptor built this way cannot
// self-heal after the Redis script cache is flushed (e.g. SCRIPT FLUSH or a
// Redis restart): ExecSha will return ErrScriptNotCached in that case. Build
// the Scriptor with explicit script bodies to enable self-healing.
func (sd *scriptDescriptor) loadScripts(ctx context.Context, client *redis.Client, redisScriptDefinition string, db int) error {
	if client == nil {
		return ErrNilClient
	}

	sd.container = make(map[string]scriptEntry)

	res, err := client.Eval(ctx, loadLuaScriptTemplate, []string{redisScriptDefinition}, db)
	if err != nil {
		return err
	}

	// A nil result means the key is absent — success with empty container.
	if res == nil {
		return nil
	}

	v, ok := res.([]any)
	if !ok {
		return fmt.Errorf("goscriptor: unexpected type %T from script registry load", res)
	}

	count := len(v)
	if count == 0 {
		return nil
	}
	if count%2 != 0 {
		return fmt.Errorf("goscriptor: HGETALL returned odd number of elements (%d)", count)
	}

	for i := 0; i < count; i = i + 2 {
		key, value := v[i], v[i+1]

		keyStr, ok1 := key.(string)
		valueStr, ok2 := value.(string)
		if !ok1 || !ok2 {
			return fmt.Errorf("goscriptor: unexpected type %T or %T from HGETALL", key, value)
		}

		exists, err := client.ScriptExists(ctx, valueStr)
		if err != nil {
			return err
		}
		if !exists {
			return ErrScriptNotCached
		}
		sd.container[keyStr] = scriptEntry{sha: valueStr}
	}

	return nil
}

// setLuaScript stores a script's SHA1 in the Redis hash.
func setLuaScript(ctx context.Context, client *redis.Client, redisScriptDefinition string, name string, sha1 string, db int) error {
	_, err := client.Eval(ctx, setLuaScriptTemplate, []string{redisScriptDefinition}, db, name, sha1)
	return err
}

// sha1Hex returns the SHA1 hex digest of body, matching the SHA1 that Redis
// computes for SCRIPT LOAD.
func sha1Hex(body string) string {
	sum := sha1.Sum([]byte(body))
	return hex.EncodeToString(sum[:])
}

// availableLuaScript checks that a script exists in both the hash and the
// Redis script cache, using a single EVAL round-trip for the hash lookup.
// String sentinels (not error replies) are used so that the connection is
// never discarded on the "not found" paths.
func availableLuaScript(ctx context.Context, client *redis.Client, redisScriptDefinition string, db int, name string) (string, error) {
	res, err := client.Eval(ctx, availableLuaScriptTemplate, []string{redisScriptDefinition}, db, name)
	if err != nil {
		return "", err
	}

	sha1hash, ok := res.(string)
	if !ok {
		// Nil or non-string reply — treat as not found.
		return "", ErrScriptNotFound
	}

	switch sha1hash {
	case "__GOSCRIPTOR_KEY_NOT_FOUND__":
		return "", ErrKeyNotFound
	case "__GOSCRIPTOR_FIELD_NOT_FOUND__":
		return "", ErrScriptNotFound
	case "":
		return "", ErrScriptNotFound
	}

	exists, err := client.ScriptExists(ctx, sha1hash)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", ErrScriptNotCached
	}

	return sha1hash, nil
}
