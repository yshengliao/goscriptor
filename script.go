package goscriptor

import (
	"context"
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

// ScriptDescriptor manages script registration and loading.
type ScriptDescriptor struct {
	container map[string]string
}

// NewScriptDescriptor creates a new script descriptor.
func NewScriptDescriptor(ctx context.Context, client *redis.Client, scripts map[string]string, redisScriptDefinition string, db int) (*ScriptDescriptor, error) {
	if client == nil {
		return nil, ErrNilClient
	}

	sd := &ScriptDescriptor{
		container: make(map[string]string),
	}

	if len(scripts) == 0 {
		err := sd.LoadScripts(ctx, client, redisScriptDefinition, db)
		if err != nil {
			return nil, err
		}
		return sd, nil
	}

	err := sd.Register(ctx, client, scripts, redisScriptDefinition, db)
	if err != nil {
		return nil, err
	}

	return sd, nil
}

// Register loads scripts into Redis and records their SHA1 hashes.
func (sd *ScriptDescriptor) Register(ctx context.Context, client *redis.Client, scripts map[string]string, redisScriptDefinition string, db int) error {
	if client == nil {
		return ErrNilClient
	}

	sd.container = make(map[string]string)

	for name, body := range scripts {
		sha1, err := availableLuaScript(ctx, client, redisScriptDefinition, db, name)
		if err == nil {
			sd.container[name] = sha1
			continue
		}

		// ErrKeyNotFound, ErrScriptNotFound, and ErrScriptNotCached are all
		// expected "not registered yet" cases — fall through to reload.
		// Any other error (network failure, auth error, etc.) is a real problem.
		if !errors.Is(err, ErrKeyNotFound) && !errors.Is(err, ErrScriptNotFound) && !errors.Is(err, ErrScriptNotCached) {
			return fmt.Errorf("goscriptor: checking script %q: %w", name, err)
		}

		sha1, err = client.ScriptLoad(ctx, body)
		if err != nil {
			return err
		}

		err = setLuaScript(ctx, client, redisScriptDefinition, name, sha1, db)
		if err != nil {
			return err
		}

		sd.container[name] = sha1
	}

	return nil
}

// LoadScripts loads previously registered script SHA1 hashes from Redis.
func (sd *ScriptDescriptor) LoadScripts(ctx context.Context, client *redis.Client, redisScriptDefinition string, db int) error {
	if client == nil {
		return ErrNilClient
	}

	sd.container = make(map[string]string)

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
		sd.container[keyStr] = valueStr
	}

	return nil
}

// setLuaScript stores a script's SHA1 in the Redis hash.
func setLuaScript(ctx context.Context, client *redis.Client, redisScriptDefinition string, name string, sha1 string, db int) error {
	_, err := client.Eval(ctx, setLuaScriptTemplate, []string{redisScriptDefinition}, db, name, sha1)
	return err
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

	sha1, ok := res.(string)
	if !ok {
		// Nil or non-string reply — treat as not found.
		return "", ErrScriptNotFound
	}

	switch sha1 {
	case "__GOSCRIPTOR_KEY_NOT_FOUND__":
		return "", ErrKeyNotFound
	case "__GOSCRIPTOR_FIELD_NOT_FOUND__":
		return "", ErrScriptNotFound
	case "":
		return "", ErrScriptNotFound
	}

	exists, err := client.ScriptExists(ctx, sha1)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", ErrScriptNotCached
	}

	return sha1, nil
}
