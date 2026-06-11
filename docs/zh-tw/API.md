# API 參考

## 套件 `goscriptor`

### 型別

#### `Scriptor`

Redis Lua 腳本管理的主要進入點。可安全地由多個 goroutine 並發使用。

```go
type Scriptor struct {
    // Client 是底層 Redis client，已匯出供直接存取。
    // 請勿透過 Client.Do 發送原始 SELECT 指令——這會污染池中連線。
    // 改用 Lua 內部的 SELECT（不會外洩）或透過 Option/Options 的 DB 欄位設定。
    Client *redis.Client
}
```

#### `Option`

使用分離的 host 和 port 的便利設定。

```go
type Option struct {
    Host         string
    Port         int
    Password     string
    DB           int
    PoolSize     int           // 最大連線數（預設：10）
    MinIdle      int           // 最小閒置連線數（預設：1）
    DialTimeout  time.Duration // 預設：5s。0 = 預設，-1 = 停用
    ReadTimeout  time.Duration // 預設：3s。0 = 預設，-1 = 停用
    WriteTimeout time.Duration // 預設：3s。0 = 預設，-1 = 停用
    IdleTimeout  time.Duration // 預設：5m。0 = 預設，-1 = 停用
    MaxConnAge   time.Duration // 預設：30m。0 = 預設，-1 = 停用
}
```

`NewDB` 會驗證 `Host` 非空且 `Port` 在 1..65535 範圍內。

### 建構子

#### `NewDB`

使用 Option 建立新 Redis client 並初始化 Scriptor。傳入的 `ctx` 控制連線 Ping
及初始腳本註冊的 deadline。

```go
func NewDB(ctx context.Context, opt *Option, scriptDB int, redisScriptDefinition string, scripts map[string]string) (*Scriptor, error)
```

**參數：**

| 名稱 | 型別 | 說明 |
|------|------|------|
| `ctx` | `context.Context` | 控制啟動 Ping 及腳本註冊的 deadline |
| `opt` | `*Option` | Redis 連線設定 |
| `scriptDB` | `int` | 用於儲存腳本中繼資料的 Redis DB 編號 |
| `redisScriptDefinition` | `string` | 儲存腳本 SHA1 對應的 Hash key 名稱 |
| `scripts` | `map[string]string` | 腳本名稱 → Lua 原始碼。傳 `nil` 或空 map 從快取載入 |

#### `New`

使用已存在的 Redis client 建立 Scriptor。傳入的 `ctx` 控制連線 Ping
及初始腳本註冊的 deadline。

```go
func New(ctx context.Context, client *redis.Client, scriptDB int, redisScriptDefinition string, scripts map[string]string) (*Scriptor, error)
```

### 方法

#### `Exec`

直接執行 Lua 腳本（不使用快取）。若 `script` 為空字串，回傳 `ErrEmptyScript`。

```go
func (s *Scriptor) Exec(ctx context.Context, script string, keys []string, args ...any) (any, error)
```

#### `ExecSha`

依註冊名稱執行已快取的腳本。若名稱未註冊，回傳 `ErrScriptNotFound`。

```go
func (s *Scriptor) ExecSha(ctx context.Context, scriptname string, keys []string, args ...any) (any, error)
```

#### `Close`

關閉底層 Redis client。

```go
func (s *Scriptor) Close() error
```

### Sentinel Errors

所有 sentinel errors 定義於 `errors.go`：

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

- `ErrEmptyScript` — `Exec("")` 傳入空 script body 時回傳。
- `ErrScriptNotCached` — 由「從快取載入」路徑的 `ExecSha` 回傳：SHA1 已記錄於登錄檔
  但腳本已不在 Redis 腳本快取中（例如 `SCRIPT FLUSH` 後，或 Scriptor 在未提供腳本
  內容的情況下建立後 Redis 重啟）。若 Scriptor 建立時有提供腳本內容，`ExecSha` 會
  自動重新載入腳本並重試一次（self-heal）。

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
    DialTimeout  time.Duration // 預設：5s。0 = 預設，-1 = 停用
    ReadTimeout  time.Duration // 預設：3s。0 = 預設，-1 = 停用
    WriteTimeout time.Duration // 預設：3s。0 = 預設，-1 = 停用
    IdleTimeout  time.Duration // 預設：5m。0 = 預設，-1 = 停用
    MaxConnAge   time.Duration // 預設：30m。0 = 預設，-1 = 停用
}
```

每次指令的 deadline 為 `(now + Read/WriteTimeout)` 與 `ctx` deadline 中較早者。
設為 `-1` 即完全停用（此時 socket I/O 僅由 context deadline 控制）。

#### `Client`

```go
func NewClient(opts *Options) *Client
func (c *Client) Do(ctx context.Context, args ...any) (any, error)
func (c *Client) Close() error
func (c *Client) PoolStats() PoolStats
```

> **`Do` 是嚴格的請求/回覆模式。** SUBSCRIBE/pub-sub、MULTI/EXEC 交易與
> pipelining **無法**透過 `Do` 使用——多訊息協議會使連線狀態失序。
> 單一原始指令（如 `ZADD`）則完全沒問題。

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
func (c *Client) Get(ctx context.Context, key string) (string, error)
func (c *Client) Set(ctx context.Context, key, value string, ttl time.Duration) error
func (c *Client) Del(ctx context.Context, keys ...string) (int64, error)
func (c *Client) Exists(ctx context.Context, keys ...string) (int64, error)
func (c *Client) Incr(ctx context.Context, key string) (int64, error)
func (c *Client) IncrBy(ctx context.Context, key string, delta int64) (int64, error)
```

**說明：**
- `Get`：對不存在的 key **和**值為空字串的 key 都回傳 `("", nil)`——呼叫端無法
  透過回傳值區分這兩種情況。
- `Set`：當 `ttl > 0` 時以毫秒（`PX`）設定過期時間。`ttl` 為 0 則不設定過期。

### Hash 指令

```go
func (c *Client) HGet(ctx context.Context, key, field string) (string, error)
func (c *Client) HGetAll(ctx context.Context, key string) (map[string]string, error)
func (c *Client) HSet(ctx context.Context, key, field, value string) error
func (c *Client) HDel(ctx context.Context, key string, fields ...string) (int64, error)
func (c *Client) HExists(ctx context.Context, key, field string) (bool, error)
```

**說明：**
- `HGet`：對不存在的 field **和**值為空字串的 field 都回傳 `("", nil)`。

### List 指令

```go
func (c *Client) LPush(ctx context.Context, key string, values ...string) (int64, error)
func (c *Client) RPush(ctx context.Context, key string, values ...string) (int64, error)
func (c *Client) LPop(ctx context.Context, key string) (string, error)
func (c *Client) RPop(ctx context.Context, key string) (string, error)
func (c *Client) LLen(ctx context.Context, key string) (int64, error)
func (c *Client) LRange(ctx context.Context, key string, start, stop int64) ([]string, error)
```

**說明：**
- `LPop`/`RPop`：對空 list **和**彈出值為空字串的元素都回傳 `("", nil)`。

### Set 指令

```go
func (c *Client) SAdd(ctx context.Context, key string, members ...string) (int64, error)
func (c *Client) SMembers(ctx context.Context, key string) ([]string, error)
func (c *Client) SRem(ctx context.Context, key string, members ...string) (int64, error)
func (c *Client) SIsMember(ctx context.Context, key, member string) (bool, error)
func (c *Client) SCard(ctx context.Context, key string) (int64, error)
```

### Key 指令

```go
func (c *Client) Expire(ctx context.Context, key string, ttl time.Duration) (bool, error)
func (c *Client) TTL(ctx context.Context, key string) (int64, error)
```

**說明：**
- `Expire`：由於 `EXPIRE` 僅接受整數秒，小於一秒的 TTL 會向上取整到 1 秒。例如傳入
  `900*time.Millisecond` 會以 1 秒送出，保留 key 而不是刪除它。若 `ttl <= 0` 則
  直接傳遞給 Redis（立即刪除 key）。若需毫秒精度，請改用帶 TTL 的 `Set`。

### Script 指令

```go
func (c *Client) Eval(ctx context.Context, script string, keys []string, args ...any) (any, error)
func (c *Client) EvalSha(ctx context.Context, sha string, keys []string, args ...any) (any, error)
func (c *Client) ScriptLoad(ctx context.Context, script string) (string, error)
func (c *Client) ScriptExists(ctx context.Context, sha string) (bool, error)
```

### Server 指令

```go
func (c *Client) Ping(ctx context.Context) error
func (c *Client) FlushAll(ctx context.Context) error
```

### RESP 層

#### `WriteCommand`

以 RESP2 array 格式序列化 Redis 指令。支援的引數型別：`string`、`[]byte`、`int`、
`int32`、`int64`、`float32`、`float64`（使用 `'f'` 格式，無科學記號）、`bool`
（`true` 編碼為 `"1"`，`false` 編碼為 `"0"`）。其他型別（包括 `nil`）回傳錯誤。

```go
func WriteCommand(w io.Writer, args ...any) error
```

#### `ReadReply`

從 `r` 讀取一個 RESP2 回覆並回傳對應的 Go 值：

| RESP 型別 | Go 型別 |
|-----------|---------|
| `+` 簡單字串 | `string` |
| `-` 錯誤 | `RedisError`（實作 `error`） |
| `:` 整數 | `int64` |
| `$` bulk 字串 | `string` |
| `$-1` null bulk | `nil` |
| `*` 陣列 | `[]any` |
| `*-1` null 陣列 | `nil` |

```go
func ReadReply(r *bufio.Reader) (any, error)
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
func (v *RedisReplyValue) AsInt32(defaultVal int32) (int32, error)
func (v *RedisReplyValue) AsInt64(defaultVal int64) (int64, error)
func (v *RedisReplyValue) AsFloat64(defaultVal float64) (float64, error)
func (v *RedisReplyValue) AsString() string
func (v *RedisReplyValue) IsNil() bool
func (v *RedisReplyValue) NullableInt() (*int64, error)
func (v *RedisReplyValue) NullableString() *string
func (v *RedisReplyValue) ToArrayReplyReader() *RedisArrayReplyReader
```
