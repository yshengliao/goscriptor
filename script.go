package goscriptor

import (
	"context"
	"fmt"

	"github.com/yshengliao/goscriptor/redis"
)

// Lua script templates used to store and retrieve script SHA1 hashes in Redis.
var (
	loadLuaScriptTemplate = `
		redis.pcall('SELECT', ARGV[1])
		return redis.call('HGETALL', KEYS[1])
	`

	getLuaScriptTemplate = `
		redis.pcall('SELECT', ARGV[1])
		return redis.call('HGET', KEYS[1], ARGV[2])
	`

	setLuaScriptTemplate = `
		redis.pcall('SELECT', ARGV[1])
		return redis.call('HSET', KEYS[1], ARGV[2], ARGV[3])
	`

	existsLuaScriptTemplate = `
		redis.pcall('SELECT', ARGV[1])
		return redis.call('EXISTS', KEYS[1])
	`

	hexistsLuaScriptTemplate = `
		redis.pcall('SELECT', ARGV[1])
		return redis.call('HEXISTS', KEYS[1], ARGV[2])
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

	sd := &ScriptDescriptor{}

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
	sd.container = make(map[string]string)

	for name, body := range scripts {
		sha1, err := availableLuaScript(ctx, client, redisScriptDefinition, db, name)
		if err == nil {
			sd.container[name] = sha1
			continue
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

	res, err := client.Eval(ctx, loadLuaScriptTemplate, []string{redisScriptDefinition}, db)
	if err != nil {
		return err
	}

	if v, ok := res.([]any); ok {
		count := len(v)
		if count == 0 {
			return nil
		}
		if count%2 != 0 {
			return fmt.Errorf("goscriptor: HGETALL returned odd number of elements (%d)", count)
		}

		sd.container = make(map[string]string)
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
	}
	return nil
}

// keyExistsLuaScript checks if the script definition key exists.
func keyExistsLuaScript(ctx context.Context, client *redis.Client, redisScriptDefinition string, db int) error {
	exists, err := client.Eval(ctx, existsLuaScriptTemplate, []string{redisScriptDefinition}, db)
	if err != nil {
		return err
	}
	n, ok := exists.(int64)
	if !ok {
		return fmt.Errorf("goscriptor: unexpected type %T from EXISTS", exists)
	}
	if n == 0 {
		return ErrKeyNotFound
	}

	return nil
}

// mkeyExistsLuaScript checks if a script member key exists in the hash.
func mkeyExistsLuaScript(ctx context.Context, client *redis.Client, redisScriptDefinition string, mkey string, db int) error {
	exists, err := client.Eval(ctx, hexistsLuaScriptTemplate, []string{redisScriptDefinition, mkey}, db)
	if err != nil {
		return err
	}
	n, ok := exists.(int64)
	if !ok {
		return fmt.Errorf("goscriptor: unexpected type %T from HEXISTS", exists)
	}
	if n == 0 {
		return ErrKeyNotFound
	}

	return nil
}

// getLuaScript retrieves a script's SHA1 from the Redis hash.
func getLuaScript(ctx context.Context, client *redis.Client, redisScriptDefinition string, name string, db int) (string, error) {
	exists, err := client.Eval(ctx, getLuaScriptTemplate, []string{redisScriptDefinition}, db, name)
	if err != nil {
		return "", err
	}
	str, ok := exists.(string)
	if !ok {
		return "", fmt.Errorf("goscriptor: unexpected type %T from HGET", exists)
	}
	if str == "" {
		return "", ErrScriptNotFound
	}

	return str, nil
}

// setLuaScript stores a script's SHA1 in the Redis hash.
func setLuaScript(ctx context.Context, client *redis.Client, redisScriptDefinition string, name string, sha1 string, db int) error {
	_, err := client.Eval(ctx, setLuaScriptTemplate, []string{redisScriptDefinition}, db, name, sha1)
	return err
}

// availableLuaScript checks that a script exists in both the hash and the script cache.
func availableLuaScript(ctx context.Context, client *redis.Client, redisScriptDefinition string, db int, name string) (string, error) {
	if err := keyExistsLuaScript(ctx, client, redisScriptDefinition, db); err != nil {
		return "", err
	}
	if err := mkeyExistsLuaScript(ctx, client, redisScriptDefinition, name, db); err != nil {
		return "", err
	}
	sha1, err := getLuaScript(ctx, client, redisScriptDefinition, name, db)
	if err != nil {
		return "", err
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
