# Goscriptor — Zero-Dependency Redis Script Manager for Go

[![Go Version](https://img.shields.io/badge/go-1.25+-blue.svg)](https://go.dev/)
![Status](https://img.shields.io/badge/status-v0.5.1--alpha-orange.svg)
[![License](https://img.shields.io/badge/license-MIT-brightgreen.svg)](LICENSE)
![Dependencies](https://img.shields.io/badge/dependencies-0-brightgreen.svg)
![AI Generated](https://img.shields.io/badge/AI_Generated-Antigravity-blueviolet.svg)

> A lightweight Go library for managing Redis Lua scripts with atomic execution, SHA1 caching, and a built-in zero-dependency Redis client.
>
> [繁體中文](README_ZH_TW.md)

## Features

- **Zero external dependencies** — built-in RESP2 client, no `go-redis` required
- **Lua script lifecycle** — register, cache (SHA1), and execute atomically
- **Production-grade connection pool** — max connections, idle timeout, connection age, waiter queue
- **Standalone Redis client** — usable independently via `goscriptor/redis` sub-package
- **20+ built-in commands** — String, Hash, List, Set, Key operations

> **Note:** This library uses `SELECT` internally for DB isolation. **Redis Cluster is not supported.**

## Quick Start

```bash
go get github.com/yshengliao/goscriptor
```

### Lua Script Management

```go
package main

import (
    "context"
    "fmt"

    "github.com/yshengliao/goscriptor"
)

func main() {
    opt := &goscriptor.Option{
        Host: "127.0.0.1", Port: 6379,
        DB: 0, PoolSize: 10,
    }

    scripts := map[string]string{
        "hello": `return 'Hello, World!'`,
    }

    s, err := goscriptor.NewDB(opt, 1, "myapp|v1.0", scripts)
    if err != nil {
        panic(err)
    }
    defer s.Close()

    ctx := context.Background()
    res, _ := s.ExecSha(ctx, "hello", []string{})
    fmt.Println(res) // Hello, World!
}
```

### Standalone Redis Client

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/yshengliao/goscriptor/redis"
)

func main() {
    c := redis.NewClient(&redis.Options{
        Addr:     "127.0.0.1:6379",
        PoolSize: 10,
    })
    defer c.Close()

    ctx := context.Background()

    c.Set(ctx, "name", "goscriptor", 5*time.Minute)
    val, _ := c.Get(ctx, "name")
    fmt.Println(val) // goscriptor

    c.LPush(ctx, "queue", "task1", "task2")
    item, _ := c.RPop(ctx, "queue")
    fmt.Println(item) // task1
}
```

## Architecture

```
goscriptor/
├── scriptor.go      Scriptor — main API (Exec, ExecSha)
├── script.go        ScriptDescriptor — register, cache, load
├── option.go        Option — convenience constructor
├── reply.go         RedisArrayReplyReader — type-safe reply parsing
├── errors.go        Sentinel errors
├── redis/           Standalone Redis client (public sub-package)
│   ├── client.go    Client, connection pool, pool stats
│   ├── resp.go      RESP2 protocol encoder/decoder
│   └── commands.go  20+ built-in Redis commands
└── example/
    └── main.go      Usage example
```

## Connection Pool

| Setting | Default | Description |
|---------|---------|-------------|
| `PoolSize` | 10 | Maximum active connections |
| `MinIdle` | 1 | Minimum idle connections kept alive |
| `IdleTimeout` | 5m | Idle connections closed after this duration |
| `MaxConnAge` | 30m | Connections retired after this lifetime |
| `ReadTimeout` | 3s | Per-command read deadline |
| `WriteTimeout` | 3s | Per-command write deadline |
| `DialTimeout` | 5s | Timeout for new TCP connections |

Set any timeout to `-1` to disable it.

```go
stats := client.PoolStats()
fmt.Printf("Active: %d, Idle: %d, Waiters: %d\n",
    stats.Active, stats.Idle, stats.Waiters)
```

## Available Commands

| Category | Commands |
|----------|----------|
| **String** | `Get`, `Set` (with TTL), `Del`, `Exists`, `Incr`, `IncrBy` |
| **Hash** | `HGet`, `HGetAll`, `HSet`, `HDel`, `HExists` |
| **List** | `LPush`, `RPush`, `LPop`, `RPop`, `LLen`, `LRange` |
| **Set** | `SAdd`, `SMembers`, `SRem`, `SIsMember`, `SCard` |
| **Key** | `Expire`, `TTL` |
| **Script** | `Eval`, `EvalSha`, `ScriptLoad`, `ScriptExists` |
| **Server** | `Ping`, `FlushAll`, `Do` (raw command) |

## Testing

```bash
# Unit tests (no Redis required)
go test ./...

# Integration tests (requires running Redis)
REDIS_ADDR=127.0.0.1:6379 go test -v ./...
```

## Documentation

- 📖 **[English Documentation](docs/en/)** — API reference, connection pool guide
- 📖 **[繁體中文文件](docs/zh-tw/)** — API 參考、連線池指南

## Changelog

### v0.5.1-alpha (2026-04-24)

- Replaced `go-redis/v9` with built-in RESP2 client — **zero external dependencies**.
- Production-grade connection pool (max active, idle timeout, max age, waiter queue, background reaper).
- Public `redis/` sub-package with 20+ built-in commands (String, Hash, List, Set, Key).
- `PoolStats()` for runtime monitoring (Active / Idle / Waiters).
- Reorganised project: `internal/redis/` → public `redis/`, `main/` → `example/`, file renames.
- Bilingual documentation (`docs/en/`, `docs/zh-tw/`) with API reference, connection pool guide, migration guide.
- Removed dead code (`ScriptDescriptor.Scripts` field), added HGETALL odd-count guard.
- Fixed type-switch double assertions in `RedisReplyValue`.
- Cleaned LLM artefacts (`doc.go`, stale README, code-review.md).

### v0.4.0-alpha (2026-04-24)

- Migrated to Go 1.25, `go-redis/v9`.
- Removed `gopkg.in/guregu/null.v3` — replaced with native pointer types.
- Removed `testify`, `miniredis` — all tests use stdlib `testing` + real Redis.
- Introduced sentinel errors, explicit `context.Context` passing, comma-ok assertions.
- Removed `sync.Once`, map pointer passing, `UniversalClient`.
- Black-box tests (`package goscriptor_test`), `REDIS_ADDR` env-var gating.

## License

MIT License — see [LICENSE](LICENSE).

---

## Testing & Performance

This project relies on real Redis for integration tests to ensure RESP2 correctness and connection pool reliability. The underlying custom client has been rigorously optimized for zero-allocation command formatting and bulk string parsing.

Run the tests and benchmarks locally (requires a running Redis instance at `127.0.0.1:6379`):

```bash
$ REDIS_ADDR=127.0.0.1:6379 go test -bench=. -benchmem ./...
```

**Benchmark Results (Apple M3 Pro):**

```text
goos: darwin
goarch: arm64
pkg: github.com/yshengliao/goscriptor
cpu: Apple M3 Pro
BenchmarkPing-12           13921             85492 ns/op              20 B/op          2 allocs/op
BenchmarkGet-12            14032             84385 ns/op              96 B/op          4 allocs/op
PASS
ok      github.com/yshengliao/goscriptor        4.150s
```

*   **Zero-Allocation formatting**: Writing RESP2 commands leverages `sync.Pool`, eliminating dynamic memory allocation during normal request lifecycles.
*   **Minimal Parsing Allocation**: `ReadReply` uses `bufio.Reader.ReadLine()` and custom byte parsing instead of strings, bringing `PING` down to just `2 allocs/op` (20 Bytes/op).
