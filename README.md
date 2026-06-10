# Goscriptor — Zero-Dependency Redis Script Manager for Go

[![Go Version](https://img.shields.io/badge/go-1.25+-blue.svg)](https://go.dev/)
![Status](https://img.shields.io/badge/status-v1.0.0-brightgreen.svg)
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
- **24 built-in data commands** — String, Hash, List, Set, Key operations; plus Ping, FlushAll, Do, Eval, EvalSha, ScriptLoad, ScriptExists in the client

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
    "log"

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

    ctx := context.Background()
    s, err := goscriptor.NewDB(ctx, opt, 1, "myapp|v1.0", scripts)
    if err != nil {
        log.Fatal(err)
    }
    defer s.Close()

    res, err := s.ExecSha(ctx, "hello", []string{})
    if err != nil {
        log.Fatal(err)
    }
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
├── scriptor.go      Scriptor — main API (Exec, ExecSha, Close)
├── script.go        internal script registration and SHA1 caching helpers
├── option.go        Option — convenience constructor for NewDB
├── errors.go        Sentinel errors
├── redis/           Standalone Redis client (public sub-package)
│   ├── client.go    Client, connection pool, Ping, FlushAll, Do,
│   │                Eval, EvalSha, ScriptLoad, ScriptExists
│   ├── resp.go      RESP2 protocol encoder/decoder
│   └── commands.go  24 data commands (String, Hash, List, Set, Key)
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

Set any timeout to `-1` to disable it (context deadline then governs I/O).

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

> **Concurrency note:** `Scriptor` is safe for concurrent use by multiple goroutines.
> Issuing a raw `SELECT` via `s.Client.Do(ctx, "SELECT", n)` will poison pooled
> connections. Use the `DB` field in `Option`/`Options` instead.

## Testing

Unit tests run without a Redis server. Integration tests are skipped unless
`REDIS_ADDR` is set. CI runs both with `-race` and a `redis:7` service container,
and uploads a coverage artifact.

```bash
# Unit tests (no Redis required)
go test ./...

# Integration tests (requires running Redis)
REDIS_ADDR=127.0.0.1:6379 go test -v ./...

# Race detector (CI also runs this)
REDIS_ADDR=127.0.0.1:6379 go test -race ./...
```

## Documentation

- 📖 **[English Documentation](docs/en/)** — API reference, connection pool guide
- 📖 **[繁體中文文件](docs/zh-tw/)** — API 參考、連線池指南

## Changelog

### v1.0.0

This is the first stable release. It contains breaking API changes from v0.5.x
(ctx-aware constructors, `ErrEmptyScript`, narrowed exported surface) — hence the
major version bump per Semantic Versioning.

- **Connection pool overhaul**: Fixed data race on waiter list, lost wakeups, and
  `Close`/reaper synchronisation. `MinIdle` connections are now pre-warmed
  asynchronously at client creation and replenished by the background reaper every
  30 s. Server error replies (`-ERR`, `NOSCRIPT`, `WRONGTYPE`) no longer discard the
  connection; only transport errors do.
- **NOSCRIPT self-healing**: `ExecSha` retains the original script body. On Redis
  restart or `SCRIPT FLUSH` it reloads the script, re-persists the SHA1, and retries
  once automatically. Scriptors created from the load-from-cache path (nil/empty
  scripts map) cannot self-heal and return `ErrScriptNotCached`.
- **Breaking change**: `New` and `NewDB` now accept a leading `context.Context` so
  the caller controls the startup deadline. The old internal 5 s timeout is removed.
- **`Option` pool tuning fields**: `MinIdle`, `DialTimeout`, `ReadTimeout`,
  `WriteTimeout`, `IdleTimeout`, `MaxConnAge` are now part of `goscriptor.Option`
  (0 = default, -1 = disable). `NewDB` validates `Host`/`Port`.
- **`ErrEmptyScript`**: `Exec("")` now returns the new sentinel `ErrEmptyScript`
  (errors.go now has 6 sentinels).
- **RESP hardening**: `ReadReply` enforces 512 MB bulk / 16 M array length caps and
  integer-overflow checks. `WriteCommand` accepts `int32`, `int64`, `float32` (`f`
  notation), `float64`, `bool` (`"1"`/`"0"`); unsupported types return an error.
- **TTL fixes**: `Set` floors sub-millisecond TTLs to 1 ms (PX); `Expire` rounds
  sub-second durations up to 1 s to avoid the truncation-to-zero that would silently
  delete the key.
- **script.go cleanup**: dead code removed, value sentinels added, real error
  propagation. SHA body verification: a changed body under the same name wins.
- **Test de-flaking** and **GitHub Actions CI**: `gofmt`+`vet`+`build`+`go test -race`
  unit job, plus `redis:7` service integration job with coverage artifact.

### v0.5.2-alpha
- **Performance**: Achieved near zero-allocation for RESP2 serialization using `sync.Pool` (PING: 20 B/op, 2 allocs/op).
- **Performance**: Optimized `ReadReply` to avoid string allocations during integer parsing.
- **Bug Fix**: Fixed a critical nil pointer dereference issue when waking waiting goroutines in the connection pool via `Close()`.
- **Tests**: Increased test coverage (pool exhaustion, waiter cancellation, robust RESP parsing).

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

Run the benchmarks locally (requires a running Redis instance):

```bash
REDIS_ADDR=127.0.0.1:6379 go test -bench=. -benchmem -run='^$' ./redis/
```

**Benchmark Results (Linux container, Intel Xeon @ 2.80 GHz):**

```text
goos: linux
goarch: amd64
pkg: github.com/yshengliao/goscriptor/redis
cpu: Intel(R) Xeon(R) Processor @ 2.80GHz
BenchmarkPing-4   	   16210	     74665 ns/op	      20 B/op	       2 allocs/op
BenchmarkGet-4    	   15558	     86597 ns/op	      96 B/op	       4 allocs/op
PASS
ok  	github.com/yshengliao/goscriptor/redis	4.095s
```

*Absolute numbers vary by hardware; the allocation counts are the meaningful metric.*

- **Zero-allocation formatting**: Writing RESP2 commands leverages `sync.Pool`, eliminating dynamic memory allocation during normal request lifecycles.
- **Minimal parsing allocation**: `ReadReply` uses `bufio.Reader.ReadLine()` and custom byte parsing instead of strings, bringing `PING` down to just `2 allocs/op` (20 Bytes/op).
