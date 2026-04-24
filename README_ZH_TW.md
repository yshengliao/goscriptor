# Goscriptor — 零依賴 Redis 腳本管理器

[![Go Version](https://img.shields.io/badge/go-1.25+-blue.svg)](https://go.dev/)
![Status](https://img.shields.io/badge/status-v0.5.1--alpha-orange.svg)
[![License](https://img.shields.io/badge/license-MIT-brightgreen.svg)](LICENSE)
![Dependencies](https://img.shields.io/badge/dependencies-0-brightgreen.svg)
![AI Generated](https://img.shields.io/badge/AI_Generated-Antigravity-blueviolet.svg)

> 輕量級 Go 函式庫，提供 Redis Lua 腳本的原子執行、SHA1 快取管理，以及內建零依賴 Redis client。
>
> [English](README.md)

## 特色

- **零外部依賴** — 內建 RESP2 client，不需要 `go-redis`
- **Lua 腳本生命週期** — 註冊、快取（SHA1）、原子執行
- **生產級連線池** — 最大連線數、閒置超時、連線壽命、等待佇列
- **獨立 Redis client** — 透過 `goscriptor/redis` 子套件獨立使用
- **20+ 內建指令** — String、Hash、List、Set、Key 操作

> **注意：** 此函式庫內部使用 `SELECT` 指令進行 DB 隔離，**不支援 Redis Cluster**。

## 快速開始

```bash
go get github.com/yshengliao/goscriptor
```

### Lua 腳本管理

```go
package main

import (
    "context"
    "fmt"

    "github.com/yshengliao/goscriptor"
)

func main() {
    opt := &goscriptor.Option{
        Host: "127.0.0.1", Port: 6379,
        DB: 0, PoolSize: 10,
    }

    scripts := map[string]string{
        "hello": `return 'Hello, World!'`,
    }

    s, err := goscriptor.NewDB(opt, 1, "myapp|v1.0", scripts)
    if err != nil {
        panic(err)
    }
    defer s.Close()

    ctx := context.Background()
    res, _ := s.ExecSha(ctx, "hello", []string{})
    fmt.Println(res) // Hello, World!
}
```

### 獨立 Redis Client

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/yshengliao/goscriptor/redis"
)

func main() {
    c := redis.NewClient(&redis.Options{
        Addr:     "127.0.0.1:6379",
        PoolSize: 10,
    })
    defer c.Close()

    ctx := context.Background()

    c.Set(ctx, "name", "goscriptor", 5*time.Minute)
    val, _ := c.Get(ctx, "name")
    fmt.Println(val) // goscriptor

    c.LPush(ctx, "queue", "task1", "task2")
    item, _ := c.RPop(ctx, "queue")
    fmt.Println(item) // task1
}
```

## 架構

```
goscriptor/
├── scriptor.go      Scriptor — 主 API（Exec、ExecSha）
├── script.go        ScriptDescriptor — 註冊、快取、載入
├── option.go        Option — 便利建構子
├── reply.go         RedisArrayReplyReader — 型別安全回覆解析
├── errors.go        Sentinel errors
├── redis/           獨立 Redis client（公開子套件）
│   ├── client.go    Client、連線池、統計
│   ├── resp.go      RESP2 協議編解碼
│   └── commands.go  20+ 內建 Redis 指令
└── example/
    └── main.go      使用範例
```

## 連線池

| 設定 | 預設值 | 說明 |
|------|--------|------|
| `PoolSize` | 10 | 最大活躍連線數 |
| `MinIdle` | 1 | 最小閒置連線數 |
| `IdleTimeout` | 5m | 閒置超過此時間的連線自動關閉 |
| `MaxConnAge` | 30m | 連線存活超過此時間後淘汰 |
| `ReadTimeout` | 3s | 每次指令的讀取超時 |
| `WriteTimeout` | 3s | 每次指令的寫入超時 |
| `DialTimeout` | 5s | TCP 建連超時 |

設為 `-1` 可關閉對應功能。

```go
stats := client.PoolStats()
fmt.Printf("Active: %d, Idle: %d, Waiters: %d\n",
    stats.Active, stats.Idle, stats.Waiters)
```

## 可用指令

| 類別 | 指令 |
|------|------|
| **String** | `Get`、`Set`（含 TTL）、`Del`、`Exists`、`Incr`、`IncrBy` |
| **Hash** | `HGet`、`HGetAll`、`HSet`、`HDel`、`HExists` |
| **List** | `LPush`、`RPush`、`LPop`、`RPop`、`LLen`、`LRange` |
| **Set** | `SAdd`、`SMembers`、`SRem`、`SIsMember`、`SCard` |
| **Key** | `Expire`、`TTL` |
| **Script** | `Eval`、`EvalSha`、`ScriptLoad`、`ScriptExists` |
| **Server** | `Ping`、`FlushAll`、`Do`（原始指令） |

## 測試

```bash
# 單元測試（不需要 Redis）
go test ./...

# 整合測試（需要 Redis）
REDIS_ADDR=127.0.0.1:6379 go test -v ./...
```

## 技術文件

- 📖 **[繁體中文文件](docs/zh-tw/)** — API 參考、連線池指南
- 📖 **[English Documentation](docs/en/)** — API reference, connection pool guide

## 變更紀錄

### v0.5.1-alpha (2026-04-24)

- 以內建 RESP2 client 取代 `go-redis/v9`——**零外部依賴**。
- 生產級連線池（最大活躍數、閒置超時、連線壽命、等待佇列、背景清理器）。
- 公開 `redis/` 子套件，內含 20+ 內建指令（String、Hash、List、Set、Key）。
- `PoolStats()` 執行期監控（Active / Idle / Waiters）。
- 專案重整：`internal/redis/` → 公開 `redis/`、`main/` → `example/`、檔案重新命名。
- 雙語文件（`docs/en/`、`docs/zh-tw/`）含 API 參考、連線池指南、遷移指南。
- 移除死碼（`ScriptDescriptor.Scripts` 欄位），新增 HGETALL 奇數長度防衛。
- 修正 `RedisReplyValue` type-switch 二次斷言。
- 清除 LLM 殘留物（`doc.go`、過時 README、`code-review.md`）。

### v0.4.0-alpha (2026-04-24)

- 升級至 Go 1.25、`go-redis/v9`。
- 移除 `gopkg.in/guregu/null.v3`——改用原生指標型別。
- 移除 `testify`、`miniredis`——全部測試使用標準庫 `testing` + 真實 Redis。
- 引入 sentinel errors、顯式 `context.Context` 傳遞、comma-ok assertions。
- 移除 `sync.Once`、map 指標傳遞、`UniversalClient`。
- 黑箱測試（`package goscriptor_test`）、`REDIS_ADDR` 環境變數開關。

## 授權

MIT License — 見 [LICENSE](LICENSE)。
