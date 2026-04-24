package goscriptor_test

import (
	"context"
	"errors"
	"testing"

	"github.com/yshengliao/goscriptor"
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

func newTestDB(t *testing.T, scr map[string]string) *goscriptor.Scriptor {
	t.Helper()
	addr := redisAddr(t)
	host, port := splitAddr(t, addr)

	opt := &goscriptor.Option{
		Host:     host,
		Port:     port,
		Password: "",
		DB:       0,
		PoolSize: 1,
	}

	// Flush to ensure clean state
	tmp := opt.Create()
	tmp.FlushAll(context.Background())
	tmp.Close()

	s, err := goscriptor.NewDB(opt, 1, scriptDefinition, scr)
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	return s
}

func newTestNew(t *testing.T, scr map[string]string) *goscriptor.Scriptor {
	t.Helper()
	addr := redisAddr(t)
	host, port := splitAddr(t, addr)

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

	s, err := goscriptor.New(client, 1, scriptDefinition, scr)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func assertTestCase(t *testing.T, scriptor *goscriptor.Scriptor) {
	t.Helper()
	ctx := context.Background()

	res, err := scriptor.Exec(ctx, "return 'Hello, World!'", []string{""})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if res.(string) != "Hello, World!" {
		t.Fatalf("expected 'Hello, World!', got %v", res)
	}

	_, err = scriptor.Exec(ctx, "error return 'Hello, World!'", []string{""})
	if err == nil {
		t.Fatal("expected error from bad script")
	}

	res, err = scriptor.ExecSha(ctx, hello, []string{""})
	if err != nil {
		t.Fatalf("ExecSha: %v", err)
	}
	if res.(string) != "Hello, World!" {
		t.Fatalf("expected 'Hello, World!', got %v", res)
	}

	_, err = scriptor.ExecSha(ctx, hello+" not found", []string{""})
	if err == nil {
		t.Fatal("expected error for missing script")
	}
	if err.Error() != "goscriptor: script not found" {
		t.Fatalf("expected 'goscriptor: script not found', got %q", err.Error())
	}
}

func assertTestCaseScriptNil(t *testing.T, scriptor *goscriptor.Scriptor) {
	t.Helper()
	ctx := context.Background()

	res, err := scriptor.Exec(ctx, "return 'Hello, World!'", []string{""})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if res.(string) != "Hello, World!" {
		t.Fatalf("expected 'Hello, World!', got %v", res)
	}

	_, err = scriptor.Exec(ctx, "error return 'Hello, World!'", []string{""})
	if err == nil {
		t.Fatal("expected error from bad script")
	}

	_, err = scriptor.ExecSha(ctx, hello, []string{""})
	if err == nil {
		t.Fatal("expected error for nil scripts")
	}
	if err.Error() != "goscriptor: script not found" {
		t.Fatalf("expected 'goscriptor: script not found', got %q", err.Error())
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
		opt := &goscriptor.Option{Host: host, Port: port, DB: 0, PoolSize: 1}

		// Flush then register
		tmp := opt.Create()
		tmp.FlushAll(context.Background())
		tmp.Close()

		s1, err := goscriptor.NewDB(opt, 1, scriptDefinition, scripts)
		if err != nil {
			t.Fatalf("NewDB register: %v", err)
		}
		assertTestCase(t, s1)

		// Reload from cache (nil scripts, no flush)
		s2, err := goscriptor.NewDB(opt, 1, scriptDefinition, nil)
		if err != nil {
			t.Fatalf("NewDB reload: %v", err)
		}
		assertTestCase(t, s2)
	})

	t.Run("flush and re-register", func(t *testing.T) {
		s := newTestDB(t, scripts)
		err := s.Client.FlushAll(context.Background())
		if err != nil {
			t.Fatalf("FlushAll: %v", err)
		}

		s2 := newTestDB(t, nil)
		assertTestCaseScriptNil(t, s2)

		s3 := newTestDB(t, scripts)
		assertTestCase(t, s3)
	})

	t.Run("nil option", func(t *testing.T) {
		_, err := goscriptor.NewDB(nil, 1, scriptDefinition, nil)
		if !errors.Is(err, goscriptor.ErrNilOption) {
			t.Fatalf("expected ErrNilOption, got %v", err)
		}
	})

	t.Run("nil client", func(t *testing.T) {
		_, err := goscriptor.New(nil, 1, scriptDefinition, nil)
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
		_ = newTestNew(t, scripts)
		s := newTestNew(t, nil)
		assertTestCase(t, s)
	})
}

func TestExecSha_ContextCanceled(t *testing.T) {
	_ = redisAddr(t)

	s := newTestDB(t, scripts)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.ExecSha(ctx, hello, []string{""})
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
}
