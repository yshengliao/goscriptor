# Migration Guide: go-redis/v9 → Built-in Client

## Why Migrate?

- **Zero supply chain risk** — no transitive dependencies (`xxhash`, `rendezvous`, `atomic`, etc.)
- **Smaller binary** — only what you use gets compiled
- **Full control** — connection pool behaviour is visible and tuneable
- **Same ergonomics** — method signatures are intentionally similar to `go-redis`

## API Mapping

### Client Creation

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

### Command Pattern

go-redis uses a builder pattern (`.Result()`). The built-in client returns values directly:

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

### Script Commands

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

### Nil Handling

```diff
-if err == redis.Nil {
+if val == "" {  // Get returns "" for missing keys
     // key does not exist
 }
```

### Raw Commands

```diff
-client.Do(ctx, "CUSTOM", "ARG1", "ARG2").Result()
+client.Do(ctx, "CUSTOM", "ARG1", "ARG2")
```

## What's Not Supported

The built-in client is deliberately minimal. It does **not** support:

- Redis Cluster / Sentinel failover
- Pub/Sub
- Pipelining / transactions (`MULTI`/`EXEC`)
- Streams (`XADD`, `XREAD`)
- Sorted Sets (`ZADD`, `ZRANGE`)

For these, use `client.Do(ctx, "ZADD", ...)` with raw commands, or keep `go-redis/v9` for that part of your application.
