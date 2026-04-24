# API Reference

## Package `goscriptor`

### Types

#### `Scriptor`

The main entry point for managing Redis Lua scripts.

```go
type Scriptor struct {
    Client *redis.Client  // Underlying Redis client (exported for direct access)
}
```

#### `Option`

Convenience configuration using separate host and port.

```go
type Option struct {
    Host     string
    Port     int
    Password string
    DB       int
    PoolSize int
}
```

### Constructors

#### `NewDB`

Creates a Scriptor with a new Redis client from Option.

```go
func NewDB(opt *Option, scriptDB int, redisScriptDefinition string, scripts map[string]string) (*Scriptor, error)
```

**Parameters:**

| Name | Type | Description |
|------|------|-------------|
| `opt` | `*Option` | Redis connection settings |
| `scriptDB` | `int` | Redis DB number for script metadata storage |
| `redisScriptDefinition` | `string` | Hash key for storing script SHA1 mappings |
| `scripts` | `map[string]string` | Script name → Lua source. Pass `nil` to load from cache |

#### `New`

Creates a Scriptor with an existing Redis client.

```go
func New(client *redis.Client, scriptDB int, redisScriptDefinition string, scripts map[string]string) (*Scriptor, error)
```

### Methods

#### `Exec`

Executes a Lua script directly (not cached).

```go
func (s *Scriptor) Exec(ctx context.Context, script string, keys []string, args ...any) (any, error)
```

#### `ExecSha`

Executes a cached script by its registered name.

```go
func (s *Scriptor) ExecSha(ctx context.Context, scriptname string, keys []string, args ...any) (any, error)
```

#### `Close`

Closes the underlying Redis client.

```go
func (s *Scriptor) Close() error
```

### Sentinel Errors

```go
var (
    ErrNilClient      // Client is nil
    ErrNilOption      // Option is nil
    ErrScriptNotFound // Script name not registered
    ErrKeyNotFound    // Script definition key missing in Redis
    ErrScriptNotCached // SHA1 recorded but script not in Redis cache
)
```

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
    DialTimeout  time.Duration // Default: 5s
    ReadTimeout  time.Duration // Default: 3s, -1 to disable
    WriteTimeout time.Duration // Default: 3s, -1 to disable
    IdleTimeout  time.Duration // Default: 5m, -1 to disable
    MaxConnAge   time.Duration // Default: 30m, -1 to disable
}
```

#### `Client`

```go
func NewClient(opts *Options) *Client
func (c *Client) Do(ctx context.Context, args ...any) (any, error)
func (c *Client) Close() error
func (c *Client) PoolStats() PoolStats
```

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
func (c *Client) Get(ctx, key) (string, error)
func (c *Client) Set(ctx, key, value, ttl) error
func (c *Client) Del(ctx, keys...) (int64, error)
func (c *Client) Exists(ctx, keys...) (int64, error)
func (c *Client) Incr(ctx, key) (int64, error)
func (c *Client) IncrBy(ctx, key, delta) (int64, error)
```

### Hash Commands

```go
func (c *Client) HGet(ctx, key, field) (string, error)
func (c *Client) HGetAll(ctx, key) (map[string]string, error)
func (c *Client) HSet(ctx, key, field, value) error
func (c *Client) HDel(ctx, key, fields...) (int64, error)
func (c *Client) HExists(ctx, key, field) (bool, error)
```

### List Commands

```go
func (c *Client) LPush(ctx, key, values...) (int64, error)
func (c *Client) RPush(ctx, key, values...) (int64, error)
func (c *Client) LPop(ctx, key) (string, error)
func (c *Client) RPop(ctx, key) (string, error)
func (c *Client) LLen(ctx, key) (int64, error)
func (c *Client) LRange(ctx, key, start, stop) ([]string, error)
```

### Set Commands

```go
func (c *Client) SAdd(ctx, key, members...) (int64, error)
func (c *Client) SMembers(ctx, key) ([]string, error)
func (c *Client) SRem(ctx, key, members...) (int64, error)
func (c *Client) SIsMember(ctx, key, member) (bool, error)
func (c *Client) SCard(ctx, key) (int64, error)
```

### Key Commands

```go
func (c *Client) Expire(ctx, key, ttl) (bool, error)
func (c *Client) TTL(ctx, key) (int64, error)
```

### Script Commands

```go
func (c *Client) Eval(ctx, script, keys, args...) (any, error)
func (c *Client) EvalSha(ctx, sha, keys, args...) (any, error)
func (c *Client) ScriptLoad(ctx, script) (string, error)
func (c *Client) ScriptExists(ctx, sha) (bool, error)
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
func (v *RedisReplyValue) AsInt32(default) (int32, error)
func (v *RedisReplyValue) AsInt64(default) (int64, error)
func (v *RedisReplyValue) AsFloat64(default) (float64, error)
func (v *RedisReplyValue) AsString() string
func (v *RedisReplyValue) IsNil() bool
func (v *RedisReplyValue) NullableInt() (*int64, error)
func (v *RedisReplyValue) NullableString() *string
func (v *RedisReplyValue) ToArrayReplyReader() *RedisArrayReplyReader
```
