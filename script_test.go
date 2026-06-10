package goscriptor

import (
	"context"
	"os"
	"testing"

	"github.com/yshengliao/goscriptor/redis"
)

const (
	scriptDefinitionTest = "scriptKey|0.0.0"
	hello                = "hello"
	helloScript          = `return 'Hello, World!'`
)

func testRedisClient(t *testing.T) *redis.Client {
	t.Helper()
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		t.Skip("REDIS_ADDR not set, skipping integration test")
	}
	client := redis.NewClient(&redis.Options{Addr: addr, DB: 0, PoolSize: 1})
	client.FlushAll(context.Background())
	return client
}

func TestScriptDescriptor_Register(t *testing.T) {
	client := testRedisClient(t)
	ctx := context.Background()

	scripts := map[string]string{hello: helloScript}
	sd := &scriptDescriptor{}
	err := sd.register(ctx, client, scripts, scriptDefinitionTest, 1)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	sha := sd.container[hello].sha
	if sha == "" {
		t.Fatal("expected non-empty SHA")
	}

	exists, err := client.ScriptExists(ctx, sha)
	if err != nil {
		t.Fatalf("ScriptExists: %v", err)
	}
	if !exists {
		t.Fatal("script should exist in cache")
	}
}

func TestScriptDescriptor_LoadScripts(t *testing.T) {
	client := testRedisClient(t)
	ctx := context.Background()

	scripts := map[string]string{hello: helloScript}
	sd := &scriptDescriptor{}
	err := sd.register(ctx, client, scripts, scriptDefinitionTest, 1)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	sha := sd.container[hello].sha

	sd2 := &scriptDescriptor{}
	err = sd2.loadScripts(ctx, client, scriptDefinitionTest, 1)
	if err != nil {
		t.Fatalf("loadScripts: %v", err)
	}
	if sd2.container[hello].sha != sha {
		t.Fatalf("expected SHA %q, got %q", sha, sd2.container[hello].sha)
	}
}

func TestScriptDescriptor_LoadScripts_NoKey(t *testing.T) {
	client := testRedisClient(t)
	ctx := context.Background()

	sd := &scriptDescriptor{}
	err := sd.loadScripts(ctx, client, scriptDefinitionTest, 1)
	if err != nil {
		t.Fatalf("loadScripts should not error on missing key: %v", err)
	}
	if len(sd.container) != 0 {
		t.Fatalf("expected empty container, got %v", sd.container)
	}
}

func TestScriptDescriptor_LoadScripts_MissingScript(t *testing.T) {
	client := testRedisClient(t)
	ctx := context.Background()

	client.Do(ctx, "SELECT", 1)
	client.HSet(ctx, scriptDefinitionTest, hello, "deadbeef")

	sd := &scriptDescriptor{}
	err := sd.loadScripts(ctx, client, scriptDefinitionTest, 1)
	if err == nil {
		t.Fatal("expected error for missing script in cache")
	}
}
