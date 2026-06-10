# 連線池指南

## 概覽

Goscriptor 的內建 Redis client 包含生產級連線池，無外部依賴。連線池管理與 Redis 的
TCP 連線，處理連線重用、健康監控和自動清理。

## 設定

```go
client := redis.NewClient(&redis.Options{
    Addr:         "127.0.0.1:6379",
    Password:     "secret",
    DB:           0,
    PoolSize:     20,              // 最多 20 個連線
    MinIdle:      3,               // 至少保持 3 個閒置
    DialTimeout:  5 * time.Second,
    ReadTimeout:  3 * time.Second,
    WriteTimeout: 3 * time.Second,
    IdleTimeout:  5 * time.Minute, // 閒置超過 5 分鐘自動關閉
    MaxConnAge:   30 * time.Minute,// 連線存活超過 30 分鐘後淘汰
})
```

所有超時欄位遵循相同規則：`0` 使用內建預設值，`-1` 完全停用（此時 socket I/O 僅由
`ctx` deadline 控制）。每次指令的 deadline 設為 `(now + Read/WriteTimeout)` 與
呼叫端 `ctx` deadline 中的較早者。

## 運作機制

### 連線生命週期

1. **取出**：`getConn` 先嘗試閒置池。若為空且未達 `PoolSize`，撥接新連線。若已達
   上限，goroutine 進入**等待佇列**。
2. **使用**：連線由單一 goroutine 獨佔。每次指令獨立設定讀寫 deadline。
3. **歸還**：`putConn` 先檢查等待者（直接交接）。否則放回閒置池。過期連線直接關閉。
4. **錯誤**：傳輸（I/O）錯誤時連線被丟棄。伺服器錯誤回覆（`-ERR`、`NOSCRIPT`、
   `WRONGTYPE`）**不會**丟棄連線——連線保持健康並歸還至池中。

### MinIdle 預熱

`NewClient` 建立時，pool 在背景中非同步撥接 `MinIdle` 個連線，使 pool 在第一個真實
請求到來前就已預熱。背景 reaper 每次 30 秒週期結束後也會補充閒置連線至 `MinIdle`，
確保即使 Redis 重啟後最低水位仍能維持。

### 背景清理器（Reaper）

每 30 秒執行一次：
1. 清除所有超過 `IdleTimeout` 或 `MaxConnAge` 的連線（同時保留至少 `MinIdle` 個
   連線存活）。
2. 重新補充閒置連線至 `MinIdle`。

### 等待佇列

當所有連線都在使用中：
- 新請求在 FIFO channel 佇列中等待。
- 連線歸還時直接交給第一個等待者（直接交接，等待者無需競爭鎖）。
- 若等待者的 context 逾期，它自行從佇列中移除並回傳 `ctx.Err()`。如果連線在移除後
  才抵達 channel，立即歸還至池中。

### 優雅關閉

`Close` 以原子操作將 client 標記為已關閉，通知背景 reaper 停止，喚醒所有等待者
（它們收到 nil 連線並回傳錯誤），並關閉池中所有閒置連線。

```go
// Close 會停止背景清理器並關閉所有池中連線。
// 可安全重複呼叫（冪等）。
if err := client.Close(); err != nil {
    log.Printf("pool close error: %v", err)
}
```

## 監控

```go
stats := client.PoolStats()
fmt.Printf("Active: %d, Idle: %d, Waiters: %d\n",
    stats.Active, stats.Idle, stats.Waiters)
```

| 欄位 | 意義 |
|------|------|
| `Active` | 全部連線（閒置 + 使用中） |
| `Idle` | 池中閒置連線 |
| `Waiters` | 等待連線的 goroutine 數量 |

**健康指標：**

- `Waiters > 0` 持續 → 增加 `PoolSize`
- `Idle == PoolSize` 持續 → 減少 `PoolSize` 以節省資源
- `Active` 持續攀升不回降 → 可能有連線洩漏

## 調校建議

| 情境 | 建議 |
|------|------|
| 低流量 API | `PoolSize: 5`、`MinIdle: 1` |
| 高吞吐量 Worker | `PoolSize: 50`、`MinIdle: 10` |
| Cloud / NAT 環境 | `IdleTimeout: 2m`、`MaxConnAge: 10m` |
| 長時間 Lua 腳本 | `ReadTimeout: 30s` 或 `-1` |
| 本地開發 | `PoolSize: 1`，超時使用預設值 |

> **Redis Cluster：** 不支援。此函式庫內部使用 `SELECT` 進行 DB 隔離，與
> Redis Cluster 模式不相容。
