# API Reference

## Package `goscriptor`

### Types

#### `Scriptor`

The main entry point for managing Redis Lua scripts. Safe for concurrent use by
multiple goroutines.

```go
type Scriptor struct {
    // Client is the underlying Redis client. Exported for direct access when
    // needed. Do NOT issue a raw SELECT via Client.Do — it will poison pooled
    // connections. Use in-Lua SELECT (which does not leak) or configure DB via
    // Option/Options instead.
    Client *redis.Client
}
```

#### `Option`

Convenience configuration using separate host and port.

```go
type Option struct {
    Host         string
    Port         int
    Password     string
    DB           int
    PoolSize     int           // Max connections (default: 10)
    MinIdle      int           // Min idle connections (default: 1)
    DialTimeout  time.Duration // Default: 5s. 0 = default, -1 = disable
    ReadTimeout  time.Duration // Default: 3s. 0 = default, -1 = disable
    WriteTimeout time.Duration // Default: 3s. 0 = default, -1 = disable
    IdleTimeout  time.Duration // Default: 5m. 0 = default, -1 = disable
    MaxConnAge   time.Duration // Default: 30m. 0 = default, -1 = disable
}
```

`NewDB` validates that `Host` is non-empty and `Port` is in the range 1..65535.

### Constructors

#### `NewDB`

Creates a Scriptor with a new Redis client from Option. The supplied `ctx`
governs the connectivity Ping and the initial script registration.

```go
func NewDB(ctx context.Context, opt *Option, scriptDB int, redisScriptDefinition string, scripts map[string]string) (*Scriptor, error)
```

**Parameters:**

| Name | Type | Description |
|------|------|-------------|
| `ctx` | `context.Context` | Controls the startup Ping and script registration deadline |
| `opt` | `*Option` | Redis connection settings |
| `scriptDB` | `int` | Redis DB number for script metadata storage |
| `redisScriptDefinition` | `string` | Hash key for storing script SHA1 mappings |
| `scripts` | `map[string]string` | Script name → Lua source. Pass `nil` or an empty map to load from cache |

#### `New`

Creates a Scriptor with an existing Redis client. The supplied `ctx` governs
the connectivity Ping and the initial script registration.

```go
func New(ctx context.Context, client *redis.Client, scriptDB int, redisScriptDefinition string, scripts map[string]string) (*Scriptor, error)
```

### Methods

#### `Exec`

Executes a Lua script directly (not cached). Returns `ErrEmptyScript` if
`script` is empty.

```go
func (s *Scriptor) Exec(ctx context.Context, script string, keys []string, args ...any) (any, error)
```

#### `ExecSha`

Executes a cached script by its registered name. Returns `ErrScriptNotFound` if
the name is not registered.

```go
func (s *Scriptor) ExecSha(ctx context.Context, scriptname string, keys []string, args ...any) (any, error)
```

#### `Close`

Closes the underlying Redis client.

```go
func (s *Scriptor) Close() error
```

### Sentinel Errors

All sentinel errors are defined in `errors.go`:

```go
var (
    ErrNilClient       = errors.New("goscriptor: client cannot be nil")
    ErrNilOption       = errors.New("goscriptor: option cannot be nil")
    ErrScriptNotFound  = errors.New("goscriptor: script not found")
    ErrEmptyScript     = errors.New("goscriptor: empty script")
    ErrKeyNotFound     = errors.New("goscriptor: script key does not exist")
    ErrScriptNotCached = errors.New("goscriptor: script not in cache, reload required")
)
```

- `ErrEmptyScript` — returned by `Exec("")` when the script body is empty.
- `ErrScriptNotCached` — returned by `ExecSha` (via the load-from-cache path) when
  the SHA1 is recorded in the registry but the script is no longer in the Redis
  script cache (e.g. after `SCRIPT FLUSH` or a Redis restart when the Scriptor was
  built without providing script bodies). When the Scriptor was built with explicit
  script bodies, `ExecSha` self-heals by reloading the script and retrying once.

---

## Package `goscriptor/redis`

### Types

#### `Options`

```go
type Options struct {
    Addr         string        // "host:port"
    Password     string
    DB           int
    PoolSize     int           // Max connections (default: 10)
    MinIdle      int           // Min idle connections (default: 1)
    DialTimeout  time.Duration // Default: 5s. 0 = default, -1 = disable
    ReadTimeout  time.Duration // Default: 3s. 0 = default, -1 = disable
    WriteTimeout time.Duration // Default: 3s. 0 = default, -1 = disable
    IdleTimeout  time.Duration // Default: 5m. 0 = default, -1 = disable
    MaxConnAge   time.Duration // Default: 30m. 0 = default, -1 = disable
}
```

Per-command deadlines are computed as the earlier of `(now + Read/WriteTimeout)`
and the `ctx` deadline. Set a timeout to `-1` to disable it entirely (the context
deadline then governs socket I/O).

#### `Client`

```go
func NewClient(opts *Options) *Client
func (c *Client) Do(ctx context.Context, args ...any) (any, error)
func (c *Client) Close() error
func (c *Client) PoolStats() PoolStats
```

> **`Do` is strict request/reply.** SUBSCRIBE/pub-sub, MULTI/EXEC transactions, and
> pipelining are **not** possible via `Do` — multi-message protocols will desync the
> connection. Single raw commands such as `ZADD` work fine.

#### `PoolStats`

```go
type PoolStats struct {
    Active  int // Total connections (idle + in-use)
    Idle    int // Idle connections in pool
    Waiters int // Goroutines waiting for a connection
}
```

### String Commands

```go
func (c *Client) Get(ctx context.Context, key string) (string, error)
func (c *Client) Set(ctx context.Context, key, value string, ttl time.Duration) error
func (c *Client) Del(ctx context.Context, keys ...string) (int64, error)
func (c *Client) Exists(ctx context.Context, keys ...string) (int64, error)
func (c *Client) Incr(ctx context.Context, key string) (int64, error)
func (c *Client) IncrBy(ctx context.Context, key string, delta int64) (int64, error)
```

**Notes:**
- `Get`: returns `("", nil)` for both a missing key **and** a key whose value is the
  empty string — callers cannot distinguish these two cases via the return value.
- `Set`: when `ttl > 0` the expiry is set in milliseconds (`PX`). A `ttl` of 0 sets
  the key without expiry.

### Hash Commands

```go
func (c *Client) HGet(ctx context.Context, key, field string) (string, error)
func (c *Client) HGetAll(ctx context.Context, key string) (map[string]string, error)
func (c *Client) HSet(ctx context.Context, key, field, value string) error
func (c *Client) HDel(ctx context.Context, key string, fields ...string) (int64, error)
func (c *Client) HExists(ctx context.Context, key, field string) (bool, error)
```

**Notes:**
- `HGet`: returns `("", nil)` for both a missing field **and** a field whose value is
  the empty string.

### List Commands

```go
func (c *Client) LPush(ctx context.Context, key string, values ...string) (int64, error)
func (c *Client) RPush(ctx context.Context, key string, values ...string) (int64, error)
func (c *Client) LPop(ctx context.Context, key string) (string, error)
func (c *Client) RPop(ctx context.Context, key string) (string, error)
func (c *Client) LLen(ctx context.Context, key string) (int64, error)
func (c *Client) LRange(ctx context.Context, key string, start, stop int64) ([]string, error)
```

**Notes:**
- `LPop`/`RPop`: return `("", nil)` for both an empty list **and** a popped element
  whose value is the empty string.

### Set Commands

```go
func (c *Client) SAdd(ctx context.Context, key string, members ...string) (int64, error)
func (c *Client) SMembers(ctx context.Context, key string) ([]string, error)
func (c *Client) SRem(ctx context.Context, key string, members ...string) (int64, error)
func (c *Client) SIsMember(ctx context.Context, key, member string) (bool, error)
func (c *Client) SCard(ctx context.Context, key string) (int64, error)
```

### Key Commands

```go
func (c *Client) Expire(ctx context.Context, key string, ttl time.Duration) (bool, error)
func (c *Client) TTL(ctx context.Context, key string) (int64, error)
```

**Notes:**
- `Expire`: because `EXPIRE` accepts whole seconds, sub-second TTLs are rounded
  **up** to 1 second. For example, `900*time.Millisecond` is sent as 1 second,
  preserving the key rather than deleting it. If `ttl <= 0` the value is passed
  through to Redis unchanged (which immediately deletes the key). For sub-second
  precision use `Set` with a millisecond TTL instead.

### Script Commands

```go
func (c *Client) Eval(ctx context.Context, script string, keys []string, args ...any) (any, error)
func (c *Client) EvalSha(ctx context.Context, sha string, keys []string, args ...any) (any, error)
func (c *Client) ScriptLoad(ctx context.Context, script string) (string, error)
func (c *Client) ScriptExists(ctx context.Context, sha string) (bool, error)
```

### Server Commands

```go
func (c *Client) Ping(ctx context.Context) error
func (c *Client) FlushAll(ctx context.Context) error
```

### RESP Layer

#### `WriteCommand`

Serialises a Redis command in RESP2 array format. Supported argument types:
`string`, `[]byte`, `int`, `int64`. All other types are serialised via `fmt.Sprint`.

```go
func WriteCommand(w io.Writer, args ...any) error
```

#### `ReadReply`

Reads one RESP2 reply from `r` and returns the Go value:

| RESP type | Go type |
|-----------|---------|
| `+` simple string | `string` |
| `-` error | `RedisError` (implements `error`) |
| `:` integer | `int64` |
| `$` bulk string | `string` |
| `$-1` null bulk | `nil` |
| `*` array | `[]any` |
| `*-1` null array | `nil` |

```go
func ReadReply(r *bufio.Reader) (any, error)
```

---

## Package `goscriptor` — Reply Reader

### `RedisArrayReplyReader`

Sequential cursor for parsing Lua script array replies.

```go
r := goscriptor.NewRedisArrayReplyReader(reply)
for r.HasNext() {
    name := r.ReadString()
    score, _ := r.ReadInt64(0)
    fmt.Printf("%s: %d\n", name, score)
}
```

### `RedisReplyValue`

Type-safe wrapper for individual reply values.

```go
func (v *RedisReplyValue) AsInt32(defaultVal int32) (int32, error)
func (v *RedisReplyValue) AsInt64(defaultVal int64) (int64, error)
func (v *RedisReplyValue) AsFloat64(defaultVal float64) (float64, error)
func (v *RedisReplyValue) AsString() string
func (v *RedisReplyValue) IsNil() bool
func (v *RedisReplyValue) NullableInt() (*int64, error)
func (v *RedisReplyValue) NullableString() *string
func (v *RedisReplyValue) ToArrayReplyReader() *RedisArrayReplyReader
```
