# 遷移指南：go-redis/v9 → 內建 Client

## 為什麼要遷移？

- **零供應鏈風險** — 沒有間接依賴（`xxhash`、`rendezvous`、`atomic` 等）
- **更小的二進位** — 只編譯實際使用的程式碼
- **完全掌控** — 連線池行為透明可調
- **相似的人體工學** — 方法簽名刻意接近 `go-redis`

## API 對應

### Client 建立

```diff
-import "github.com/redis/go-redis/v9"
+import "github.com/yshengliao/goscriptor/redis"

-client := redis.NewClient(&redis.Options{
+client := redis.NewClient(&redis.Options{
     Addr:     "localhost:6379",
     Password: "",
     DB:       0,
+    PoolSize: 10,
 })
```

### 指令模式

go-redis 使用 builder pattern（`.Result()`）。內建 client 直接回傳值：

```diff
-val, err := client.Get(ctx, "key").Result()
+val, err := client.Get(ctx, "key")

-err := client.Set(ctx, "key", "value", 5*time.Minute).Err()
+err := client.Set(ctx, "key", "value", 5*time.Minute)

-n, err := client.Del(ctx, "key1", "key2").Result()
+n, err := client.Del(ctx, "key1", "key2")

-exists, err := client.Exists(ctx, "key").Result()
+exists, err := client.Exists(ctx, "key")
```

### Script 指令

```diff
-sha, err := client.ScriptLoad(ctx, script).Result()
+sha, err := client.ScriptLoad(ctx, script)

-result, err := client.EvalSha(ctx, sha, keys, args...).Result()
+result, err := client.EvalSha(ctx, sha, keys, args...)

-exists, err := client.ScriptExists(ctx, sha).Result()
-if !exists[0] { ... }
+exists, err := client.ScriptExists(ctx, sha)
+if !exists { ... }
```

### Nil 處理

內建 client 不使用 `redis.Nil` sentinel error。不存在的 key 以空字串搭配 nil 錯誤回傳：

```diff
-if err == redis.Nil {
+if val == "" {  // Get 對不存在的 key 回傳 ""
     // key 不存在
 }
```

> **注意：** `Get`、`HGet`、`LPop`、`RPop` 對**不存在的 key/field** 和**儲存值為
> 空字串的 key/field** 都回傳 `("", nil)`。需要區分兩種情況的呼叫端，應先使用
> `Exists`/`HExists` 確認存在，或改以 sentinel 值代替空字串儲存。

### 原始指令

```diff
-client.Do(ctx, "CUSTOM", "ARG1", "ARG2").Result()
+client.Do(ctx, "CUSTOM", "ARG1", "ARG2")
```

> **重要：** `Do` 是嚴格的單一請求/回覆交換。以下多訊息模式會**使連線狀態失序**，
> **不受支援**：
>
> - `SUBSCRIBE` / `PSUBSCRIBE`（pub-sub）
> - `MULTI` / `EXEC`（交易）
> - Pipelining（在讀取回覆前連續發送多個指令）
>
> 單一原始指令（`ZADD`、`ZRANGE`、`XADD` 等）則完全沒問題。

## 不支援的功能

內建 client 刻意保持精簡，**不支援**以下功能：

- Redis Cluster / Sentinel 容錯切換
- Pub/Sub
- Pipelining / 交易（`MULTI`/`EXEC`）
- Streams（`XADD`、`XREAD`）
- Sorted Sets（`ZADD`、`ZRANGE`）

對於這些需求，偶爾使用時可用 `client.Do(ctx, "ZADD", ...)` 發送原始指令；
若大量使用，請在應用程式的該部分繼續使用 `go-redis/v9`。
