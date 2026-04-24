# 連線池指南

## 概覽

Goscriptor 的內建 Redis client 包含生產級連線池，無外部依賴。連線池管理與 Redis 的 TCP 連線，處理連線重用、健康監控和自動清理。

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

## 運作機制

### 連線生命週期

1. **取出**：`getConn` 先嘗試閒置池。若為空且未達 `PoolSize`，撥接新連線。若已達上限，goroutine 進入**等待佇列**。
2. **使用**：連線由單一 goroutine 獨佔。每次指令獨立設定讀寫 deadline。
3. **歸還**：`putConn` 先檢查等待者（直接交接）。否則放回閒置池。過期連線直接關閉。
4. **錯誤**：任何 I/O 錯誤時，連線被丟棄（不放回池中）。

### 背景清理器（Reaper）

每 30 秒執行一次，清除超過 `IdleTimeout` 或 `MaxConnAge` 的連線，同時維持 `MinIdle` 最低水位。

### 等待佇列

當所有連線都在使用中：
- 新請求在 FIFO channel 佇列中等待
- 當連線被歸還時，直接交給第一個等待者
- 若等待者的 context 逾期，它會自行從佇列中移除

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

## 優雅關閉

```go
// Close 會停止背景清理器並關閉所有池中連線。
// 可安全重複呼叫。
if err := client.Close(); err != nil {
    log.Printf("pool close error: %v", err)
}
```
