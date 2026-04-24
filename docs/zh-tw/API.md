# API 參考

## 套件 `goscriptor`

### 型別

#### `Scriptor`

Redis Lua 腳本管理的主要進入點。

```go
type Scriptor struct {
    Client *redis.Client  // 底層 Redis client（已匯出，可直接存取）
}
```

#### `Option`

使用分離的 host 和 port 的便利設定。

```go
type Option struct {
    Host     string
    Port     int
    Password string
    DB       int
    PoolSize int
}
```

### 建構子

#### `NewDB`

使用 Option 建立新 Redis client 並初始化 Scriptor。

```go
func NewDB(opt *Option, scriptDB int, redisScriptDefinition string, scripts map[string]string) (*Scriptor, error)
```

**參數：**

| 名稱 | 型別 | 說明 |
|------|------|------|
| `opt` | `*Option` | Redis 連線設定 |
| `scriptDB` | `int` | 用於儲存腳本中繼資料的 Redis DB 編號 |
| `redisScriptDefinition` | `string` | 儲存腳本 SHA1 對應的 Hash key 名稱 |
| `scripts` | `map[string]string` | 腳本名稱 → Lua 原始碼。傳 `nil` 從快取載入 |

#### `New`

使用已存在的 Redis client 建立 Scriptor。

```go
func New(client *redis.Client, scriptDB int, redisScriptDefinition string, scripts map[string]string) (*Scriptor, error)
```

### 方法

#### `Exec`

直接執行 Lua 腳本（不使用快取）。

```go
func (s *Scriptor) Exec(ctx context.Context, script string, keys []string, args ...any) (any, error)
```

#### `ExecSha`

依註冊名稱執行已快取的腳本。

```go
func (s *Scriptor) ExecSha(ctx context.Context, scriptname string, keys []string, args ...any) (any, error)
```

#### `Close`

關閉底層 Redis client。

```go
func (s *Scriptor) Close() error
```

### Sentinel Errors

```go
var (
    ErrNilClient      // Client 為 nil
    ErrNilOption      // Option 為 nil
    ErrScriptNotFound // 腳本名稱未註冊
    ErrKeyNotFound    // Redis 中缺少腳本定義 key
    ErrScriptNotCached // SHA1 已記錄但腳本不在 Redis 快取中
)
```

---

## 套件 `goscriptor/redis`

### 型別

#### `Options`

```go
type Options struct {
    Addr         string        // "host:port"
    Password     string
    DB           int
    PoolSize     int           // 最大連線數（預設：10）
    MinIdle      int           // 最小閒置連線數（預設：1）
    DialTimeout  time.Duration // 預設：5s
    ReadTimeout  time.Duration // 預設：3s，-1 停用
    WriteTimeout time.Duration // 預設：3s，-1 停用
    IdleTimeout  time.Duration // 預設：5m，-1 停用
    MaxConnAge   time.Duration // 預設：30m，-1 停用
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
    Active  int // 全部連線（閒置 + 使用中）
    Idle    int // 池中閒置連線
    Waiters int // 等待連線的 goroutine 數量
}
```

### String 指令

```go
func (c *Client) Get(ctx, key) (string, error)
func (c *Client) Set(ctx, key, value, ttl) error
func (c *Client) Del(ctx, keys...) (int64, error)
func (c *Client) Exists(ctx, keys...) (int64, error)
func (c *Client) Incr(ctx, key) (int64, error)
func (c *Client) IncrBy(ctx, key, delta) (int64, error)
```

### Hash 指令

```go
func (c *Client) HGet(ctx, key, field) (string, error)
func (c *Client) HGetAll(ctx, key) (map[string]string, error)
func (c *Client) HSet(ctx, key, field, value) error
func (c *Client) HDel(ctx, key, fields...) (int64, error)
func (c *Client) HExists(ctx, key, field) (bool, error)
```

### List 指令

```go
func (c *Client) LPush(ctx, key, values...) (int64, error)
func (c *Client) RPush(ctx, key, values...) (int64, error)
func (c *Client) LPop(ctx, key) (string, error)
func (c *Client) RPop(ctx, key) (string, error)
func (c *Client) LLen(ctx, key) (int64, error)
func (c *Client) LRange(ctx, key, start, stop) ([]string, error)
```

### Set 指令

```go
func (c *Client) SAdd(ctx, key, members...) (int64, error)
func (c *Client) SMembers(ctx, key) ([]string, error)
func (c *Client) SRem(ctx, key, members...) (int64, error)
func (c *Client) SIsMember(ctx, key, member) (bool, error)
func (c *Client) SCard(ctx, key) (int64, error)
```

### Key 指令

```go
func (c *Client) Expire(ctx, key, ttl) (bool, error)
func (c *Client) TTL(ctx, key) (int64, error)
```

### Script 指令

```go
func (c *Client) Eval(ctx, script, keys, args...) (any, error)
func (c *Client) EvalSha(ctx, sha, keys, args...) (any, error)
func (c *Client) ScriptLoad(ctx, script) (string, error)
func (c *Client) ScriptExists(ctx, sha) (bool, error)
```

---

## 套件 `goscriptor` — Reply Reader

### `RedisArrayReplyReader`

用於循序解析 Lua 腳本陣列回覆的游標。

```go
r := goscriptor.NewRedisArrayReplyReader(reply)
for r.HasNext() {
    name := r.ReadString()
    score, _ := r.ReadInt64(0)
    fmt.Printf("%s: %d\n", name, score)
}
```

### `RedisReplyValue`

個別回覆值的型別安全包裝。

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
