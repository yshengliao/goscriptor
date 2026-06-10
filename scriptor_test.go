package goscriptor_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/yshengliao/goscriptor"
	"github.com/yshengliao/goscriptor/redis"
)

const (
	scriptDefinition    = "scriptKey|0.0.0"
	hello               = "hello"
	_HelloworldTemplate = `
	return 'Hello, World!'
	`
)

var (
	scripts = map[string]string{
		hello: _HelloworldTemplate,
	}
)

// cleanScriptDB deletes the script definition hash key(s) on DB 1 using a
// dedicated client so no raw SELECT is ever sent through a pooled connection.
func cleanScriptDB(t *testing.T, addr string, keys ...string) {
	t.Helper()
	c := redis.NewClient(&redis.Options{Addr: addr, DB: 1, PoolSize: 1})
	defer c.Close()
	ctx := context.Background()
	for _, k := range keys {
		if _, err := c.Del(ctx, k); err != nil {
			t.Logf("cleanScriptDB Del %q: %v", k, err)
		}
	}
}

func newTestDB(t *testing.T, scr map[string]string) *goscriptor.Scriptor {
	t.Helper()
	addr := redisAddr(t)
	host, port := splitAddr(t, addr)

	cleanScriptDB(t, addr, scriptDefinition)

	opt := &goscriptor.Option{
		Host:     host,
		Port:     port,
		Password: "",
		DB:       0,
		PoolSize: 1,
	}

	s, err := goscriptor.NewDB(context.Background(), opt, 1, scriptDefinition, scr)
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	return s
}

func newTestNew(t *testing.T, scr map[string]string) *goscriptor.Scriptor {
	t.Helper()
	addr := redisAddr(t)
	host, port := splitAddr(t, addr)

	cleanScriptDB(t, addr, scriptDefinition)

	opt := &goscriptor.Option{
		Host:     host,
		Port:     port,
		Password: "",
		DB:       0,
		PoolSize: 1,
	}

	client := opt.Create()
	if client == nil {
		t.Fatal("expected non-nil client")
	}

	s, err := goscriptor.New(context.Background(), client, 1, scriptDefinition, scr)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func assertTestCase(t *testing.T, scriptor *goscriptor.Scriptor) {
	t.Helper()
	ctx := context.Background()

	res, err := scriptor.Exec(ctx, "return 'Hello, World!'", nil)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if res.(string) != "Hello, World!" {
		t.Fatalf("expected 'Hello, World!', got %v", res)
	}

	_, err = scriptor.Exec(ctx, "error return 'Hello, World!'", nil)
	if err == nil {
		t.Fatal("expected error from bad script")
	}

	res, err = scriptor.ExecSha(ctx, hello, nil)
	if err != nil {
		t.Fatalf("ExecSha: %v", err)
	}
	if res.(string) != "Hello, World!" {
		t.Fatalf("expected 'Hello, World!', got %v", res)
	}

	_, err = scriptor.ExecSha(ctx, hello+" not found", nil)
	if err == nil {
		t.Fatal("expected error for missing script")
	}
	if !errors.Is(err, goscriptor.ErrScriptNotFound) {
		t.Fatalf("expected ErrScriptNotFound, got %v", err)
	}
}

func assertTestCaseScriptNil(t *testing.T, scriptor *goscriptor.Scriptor) {
	t.Helper()
	ctx := context.Background()

	res, err := scriptor.Exec(ctx, "return 'Hello, World!'", nil)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if res.(string) != "Hello, World!" {
		t.Fatalf("expected 'Hello, World!', got %v", res)
	}

	_, err = scriptor.Exec(ctx, "error return 'Hello, World!'", nil)
	if err == nil {
		t.Fatal("expected error from bad script")
	}

	_, err = scriptor.ExecSha(ctx, hello, nil)
	if err == nil {
		t.Fatal("expected error for nil scripts")
	}
	if !errors.Is(err, goscriptor.ErrScriptNotFound) {
		t.Fatalf("expected ErrScriptNotFound, got %v", err)
	}
}

func TestNewDB(t *testing.T) {
	_ = redisAddr(t)

	t.Run("nil scripts", func(t *testing.T) {
		s := newTestDB(t, nil)
		assertTestCaseScriptNil(t, s)
	})

	t.Run("empty scripts", func(t *testing.T) {
		s := newTestDB(t, map[string]string{})
		assertTestCaseScriptNil(t, s)
	})

	t.Run("register and exec", func(t *testing.T) {
		s := newTestDB(t, scripts)
		assertTestCase(t, s)
	})

	t.Run("reload from cache", func(t *testing.T) {
		addr := redisAddr(t)
		host, port := splitAddr(t, addr)

		// Clean then register.
		cleanScriptDB(t, addr, scriptDefinition)

		opt := &goscriptor.Option{Host: host, Port: port, DB: 0, PoolSize: 1}

		s1, err := goscriptor.NewDB(context.Background(), opt, 1, scriptDefinition, scripts)
		if err != nil {
			t.Fatalf("NewDB register: %v", err)
		}
		assertTestCase(t, s1)

		// Reload from cache (nil scripts, no clean).
		s2, err := goscriptor.NewDB(context.Background(), opt, 1, scriptDefinition, nil)
		if err != nil {
			t.Fatalf("NewDB reload: %v", err)
		}
		assertTestCase(t, s2)
	})

	t.Run("flush and re-register", func(t *testing.T) {
		addr := redisAddr(t)
		s := newTestDB(t, scripts)
		// Flush script cache only for this sub-test.
		if _, err := s.Client.Do(context.Background(), "SCRIPT", "FLUSH"); err != nil {
			t.Fatalf("SCRIPT FLUSH: %v", err)
		}
		// Also remove the definition key so s2 starts clean.
		cleanScriptDB(t, addr, scriptDefinition)

		s2 := newTestDB(t, nil)
		assertTestCaseScriptNil(t, s2)

		s3 := newTestDB(t, scripts)
		assertTestCase(t, s3)
	})

	t.Run("nil option", func(t *testing.T) {
		_, err := goscriptor.NewDB(context.Background(), nil, 1, scriptDefinition, nil)
		if !errors.Is(err, goscriptor.ErrNilOption) {
			t.Fatalf("expected ErrNilOption, got %v", err)
		}
	})

	t.Run("nil client", func(t *testing.T) {
		_, err := goscriptor.New(context.Background(), nil, 1, scriptDefinition, nil)
		if !errors.Is(err, goscriptor.ErrNilClient) {
			t.Fatalf("expected ErrNilClient, got %v", err)
		}
	})
}

func TestNew(t *testing.T) {
	_ = redisAddr(t)

	t.Run("register and exec", func(t *testing.T) {
		s := newTestNew(t, scripts)
		assertTestCase(t, s)
	})

	t.Run("reload from cache", func(t *testing.T) {
		addr := redisAddr(t)
		host, port := splitAddr(t, addr)

		// Clean once, register, then reload without cleaning again.
		cleanScriptDB(t, addr, scriptDefinition)
		opt := &goscriptor.Option{Host: host, Port: port, DB: 0, PoolSize: 1}

		client1 := opt.Create()
		s1, err := goscriptor.New(context.Background(), client1, 1, scriptDefinition, scripts)
		if err != nil {
			t.Fatalf("New (register): %v", err)
		}
		assertTestCase(t, s1)

		client2 := opt.Create()
		s2, err := goscriptor.New(context.Background(), client2, 1, scriptDefinition, nil)
		if err != nil {
			t.Fatalf("New (reload from cache): %v", err)
		}
		assertTestCase(t, s2)
	})
}

func TestExecSha_ContextCanceled(t *testing.T) {
	_ = redisAddr(t)

	s := newTestDB(t, scripts)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.ExecSha(ctx, hello, nil)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestClose(t *testing.T) {
	_ = redisAddr(t)

	s := newTestDB(t, scripts)
	err := s.Close()
	if err != nil {
		t.Fatalf("Close: %v", err)
	}

	// After Close, commands must fail.
	_, pingErr := s.Client.Do(context.Background(), "PING")
	if pingErr == nil {
		t.Fatal("expected error after Close")
	}

	// Active connections must drain to zero within 2 seconds.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s.Client.PoolStats().Active == 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	if s.Client.PoolStats().Active != 0 {
		t.Fatalf("pool Active did not reach 0 after Close: %+v", s.Client.PoolStats())
	}
}

// TestExecSha_NoScriptSelfHeal verifies that after the Redis script cache is
// flushed, ExecSha transparently reloads the retained script body and succeeds,
// and that a subsequent ExecSha also succeeds against the refreshed cache.
func TestExecSha_NoScriptSelfHeal(t *testing.T) {
	_ = redisAddr(t)

	s := newTestDB(t, scripts)
	defer func() { _ = s.Close() }()
	ctx := context.Background()

	// Sanity: first execution works against the freshly registered script.
	if _, err := s.ExecSha(ctx, hello, nil); err != nil {
		t.Fatalf("initial ExecSha: %v", err)
	}

	// Wipe the Redis script cache out from under the Scriptor.
	if _, err := s.Client.Do(ctx, "SCRIPT", "FLUSH"); err != nil {
		t.Fatalf("SCRIPT FLUSH: %v", err)
	}

	// Self-heal: the NOSCRIPT path must reload the body and succeed.
	res, err := s.ExecSha(ctx, hello, nil)
	if err != nil {
		t.Fatalf("ExecSha after flush (self-heal): %v", err)
	}
	if res.(string) != "Hello, World!" {
		t.Fatalf("expected 'Hello, World!', got %v", res)
	}

	// The cache is now refreshed; a second call must succeed without reloading.
	res, err = s.ExecSha(ctx, hello, nil)
	if err != nil {
		t.Fatalf("second ExecSha after self-heal: %v", err)
	}
	if res.(string) != "Hello, World!" {
		t.Fatalf("expected 'Hello, World!', got %v", res)
	}
}

// TestExecSha_ChangedBodyReRegister verifies that registering a different body
// under an existing name (same definition) supersedes the stale cached SHA, so
// ExecSha returns the new body's result.
func TestExecSha_ChangedBodyReRegister(t *testing.T) {
	addr := redisAddr(t)
	host, port := splitAddr(t, addr)

	const def = "changed_body|def"
	const name = "swap"

	// Clean the definition key before starting.
	cleanScriptDB(t, addr, def)

	opt := &goscriptor.Option{Host: host, Port: port, DB: 0, PoolSize: 1}
	ctx := context.Background()

	// First Scriptor registers body A under name "swap".
	s1, err := goscriptor.NewDB(ctx, opt, 1, def, map[string]string{name: "return 'A'"})
	if err != nil {
		t.Fatalf("NewDB s1: %v", err)
	}
	defer func() { _ = s1.Close() }()

	resA, err := s1.ExecSha(ctx, name, nil)
	if err != nil {
		t.Fatalf("s1 ExecSha: %v", err)
	}
	if resA.(string) != "A" {
		t.Fatalf("expected 'A', got %v", resA)
	}

	// Second Scriptor, same definition + name, but a changed body B. The
	// stale-SHA verification must cause B to be loaded and persisted.
	s2, err := goscriptor.NewDB(ctx, opt, 1, def, map[string]string{name: "return 'B'"})
	if err != nil {
		t.Fatalf("NewDB s2: %v", err)
	}
	defer func() { _ = s2.Close() }()

	resB, err := s2.ExecSha(ctx, name, nil)
	if err != nil {
		t.Fatalf("s2 ExecSha: %v", err)
	}
	if resB.(string) != "B" {
		t.Fatalf("expected 'B' from changed body, got %v", resB)
	}
}

// TestExecSha_LoadFromCacheNoSelfHeal verifies that a Scriptor built via the
// load-from-cache path (nil scripts) carries no script bodies and therefore
// cannot self-heal: after a SCRIPT FLUSH, ExecSha returns an error satisfying
// errors.Is(err, ErrScriptNotCached).
func TestExecSha_LoadFromCacheNoSelfHeal(t *testing.T) {
	addr := redisAddr(t)
	host, port := splitAddr(t, addr)

	// Clean then register via a first Scriptor.
	cleanScriptDB(t, addr, scriptDefinition)

	opt := &goscriptor.Option{Host: host, Port: port, DB: 0, PoolSize: 1}
	ctx := context.Background()

	s1, err := goscriptor.NewDB(ctx, opt, 1, scriptDefinition, scripts)
	if err != nil {
		t.Fatalf("NewDB s1: %v", err)
	}
	defer func() { _ = s1.Close() }()

	// Second Scriptor loads from cache (nil scripts) — no bodies retained.
	s2, err := goscriptor.NewDB(ctx, opt, 1, scriptDefinition, nil)
	if err != nil {
		t.Fatalf("NewDB s2 (load from cache): %v", err)
	}
	defer func() { _ = s2.Close() }()

	// Flush the script cache; s2 has no body to reload from.
	if _, err := s2.Client.Do(ctx, "SCRIPT", "FLUSH"); err != nil {
		t.Fatalf("SCRIPT FLUSH: %v", err)
	}

	_, err = s2.ExecSha(ctx, hello, nil)
	if err == nil {
		t.Fatal("expected error from load-from-cache Scriptor after flush")
	}
	if !errors.Is(err, goscriptor.ErrScriptNotCached) {
		t.Fatalf("expected ErrScriptNotCached, got %v", err)
	}
}
