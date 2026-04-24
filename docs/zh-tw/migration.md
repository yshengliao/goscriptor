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

```diff
-if err == redis.Nil {
+if val == "" {  // Get 對不存在的 key 回傳 ""
     // key 不存在
 }
```

### 原始指令

```diff
-client.Do(ctx, "CUSTOM", "ARG1", "ARG2").Result()
+client.Do(ctx, "CUSTOM", "ARG1", "ARG2")
```

## 不支援的功能

內建 client 刻意保持精簡，**不支援**以下功能：

- Redis Cluster / Sentinel 容錯切換
- Pub/Sub
- Pipelining / 交易（`MULTI`/`EXEC`）
- Streams（`XADD`、`XREAD`）
- Sorted Sets（`ZADD`、`ZRANGE`）

對於這些需求，可使用 `client.Do(ctx, "ZADD", ...)` 發送原始指令，或在應用程式的該部分繼續使用 `go-redis/v9`。
