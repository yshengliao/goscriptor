# Connection Pool Guide

## Overview

Goscriptor's built-in Redis client includes a production-grade connection pool with no external dependencies. The pool manages TCP connections to Redis, handling connection reuse, health monitoring, and automatic cleanup.

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

## How It Works

### Connection Lifecycle

1. **Checkout**: `getConn` tries the idle pool first. If empty and under `PoolSize`, dials a new connection. If at capacity, the goroutine enters a **waiter queue**.
2. **Use**: The connection is exclusively owned by one goroutine. Read/write deadlines are set per-command.
3. **Return**: `putConn` checks for waiters first (direct handoff). Otherwise returns to idle pool. Expired connections are closed instead.
4. **Error**: On any I/O error, the connection is discarded (not returned to pool).

### Background Reaper

A goroutine runs every 30 seconds to evict connections that exceed `IdleTimeout` or `MaxConnAge`, while respecting `MinIdle`.

### Waiter Queue

When all connections are in use:
- New requests wait in a FIFO channel queue
- When a connection is returned, it goes directly to the first waiter
- If the waiter's context expires, it removes itself from the queue

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

## Graceful Shutdown

```go
// Close stops the background reaper and closes all pooled connections.
// Safe to call multiple times.
if err := client.Close(); err != nil {
    log.Printf("pool close error: %v", err)
}
```
