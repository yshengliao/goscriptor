# Connection Pool Guide

## Overview

Goscriptor's built-in Redis client includes a production-grade connection pool with
no external dependencies. The pool manages TCP connections to Redis, handling
connection reuse, health monitoring, and automatic cleanup.

## Configuration

```go
client := redis.NewClient(&redis.Options{
    Addr:         "127.0.0.1:6379",
    Password:     "secret",
    DB:           0,
    PoolSize:     20,              // max 20 connections
    MinIdle:      3,               // keep at least 3 idle
    DialTimeout:  5 * time.Second,
    ReadTimeout:  3 * time.Second,
    WriteTimeout: 3 * time.Second,
    IdleTimeout:  5 * time.Minute, // close idle conns after 5m
    MaxConnAge:   30 * time.Minute,// retire conns after 30m
})
```

All timeout fields follow the same convention: `0` uses the built-in default,
`-1` disables the timeout entirely (the `ctx` deadline then governs socket I/O).
Per-command deadlines are set to the earlier of `(now + Read/WriteTimeout)` and
the caller's `ctx` deadline.

## How It Works

### Connection Lifecycle

1. **Checkout**: `getConn` tries the idle pool first. If empty and under `PoolSize`,
   dials a new connection. If at capacity, the goroutine enters a **waiter queue**.
2. **Use**: The connection is exclusively owned by one goroutine. Read/write deadlines
   are set per-command.
3. **Return**: `putConn` checks for waiters first (direct handoff). Otherwise returns
   to idle pool. Expired connections are closed instead.
4. **Error**: On transport (I/O) errors the connection is discarded. Server error
   replies (`-ERR`, `NOSCRIPT`, `WRONGTYPE`) do **not** discard the connection —
   the connection remains healthy and is returned to the pool.

### MinIdle Pre-warm

At `NewClient` time the pool asynchronously dials `MinIdle` connections in the
background so the pool is warmed before the first real request arrives. The
background reaper also replenishes idle connections back to `MinIdle` after each
30-second cycle, ensuring the minimum floor is maintained even after a Redis restart.

### Background Reaper

A goroutine runs every 30 seconds to:
1. Evict all connections that exceed `IdleTimeout` or `MaxConnAge` (while keeping at
   least `MinIdle` connections alive).
2. Replenish idle connections back up to `MinIdle` with fresh dials.

### Waiter Queue

When all connections are in use:
- New requests wait in a FIFO channel queue.
- When a connection is returned it goes directly to the first waiter (direct handoff,
  no lock contention for the waiter).
- If the waiter's context expires it removes itself from the queue and returns
  `ctx.Err()`. Any connection that arrives in the channel after removal is handed
  back to the pool immediately.

### Graceful Shutdown

`Close` atomically marks the client closed, signals the background reaper to stop,
wakes all waiters (they receive a nil connection and return an error), and closes
every idle connection in the pool.

```go
// Close stops the background reaper and closes all pooled connections.
// Safe to call multiple times (idempotent).
if err := client.Close(); err != nil {
    log.Printf("pool close error: %v", err)
}
```

## Monitoring

```go
stats := client.PoolStats()
fmt.Printf("Active: %d, Idle: %d, Waiters: %d\n",
    stats.Active, stats.Idle, stats.Waiters)
```

| Field | Meaning |
|-------|---------|
| `Active` | Total connections (idle + in-use) |
| `Idle` | Connections sitting in the pool |
| `Waiters` | Goroutines blocked waiting for a connection |

**Health indicators:**

- `Waiters > 0` sustained → increase `PoolSize`
- `Idle == PoolSize` sustained → decrease `PoolSize` to save resources
- `Active` climbing without returning → possible connection leak

## Tuning Guidelines

| Scenario | Recommendation |
|----------|---------------|
| Low-traffic API | `PoolSize: 5`, `MinIdle: 1` |
| High-throughput worker | `PoolSize: 50`, `MinIdle: 10` |
| Cloud / NAT environment | `IdleTimeout: 2m`, `MaxConnAge: 10m` |
| Long-running Lua scripts | `ReadTimeout: 30s` or `-1` |
| Local development | `PoolSize: 1`, timeouts at defaults |

> **Redis Cluster:** Not supported. This library issues `SELECT` internally for DB
> isolation, which is incompatible with Redis Cluster mode.
