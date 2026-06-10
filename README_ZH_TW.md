# Goscriptor — 零依賴 Redis 腳本管理器

[![Go Version](https://img.shields.io/badge/go-1.25+-blue.svg)](https://go.dev/)
![Status](https://img.shields.io/badge/status-v1.0.0-brightgreen.svg)
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
- **24 個內建資料指令** — String、Hash、List、Set、Key 操作；Ping、FlushAll、Do、Eval、EvalSha、ScriptLoad、ScriptExists 位於 client 中

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
    "log"

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

    ctx := context.Background()
    s, err := goscriptor.NewDB(ctx, opt, 1, "myapp|v1.0", scripts)
    if err != nil {
        log.Fatal(err)
    }
    defer s.Close()

    res, err := s.ExecSha(ctx, "hello", []string{})
    if err != nil {
        log.Fatal(err)
    }
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
├── scriptor.go      Scriptor — 主 API（Exec、ExecSha、Close）
├── script.go        內部腳本註冊與 SHA1 快取輔助函式
├── option.go        Option — NewDB 的便利建構子
├── errors.go        Sentinel errors
├── redis/           獨立 Redis client（公開子套件）
│   ├── client.go    Client、連線池、Ping、FlushAll、Do、
│   │                Eval、EvalSha、ScriptLoad、ScriptExists
│   ├── resp.go      RESP2 協議編解碼
│   └── commands.go  24 個資料指令（String、Hash、List、Set、Key）
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

設為 `-1` 可關閉對應功能（此時僅由 context deadline 控制 I/O）。

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

> **並發安全說明：** `Scriptor` 可安全地由多個 goroutine 並發使用。
> 透過 `s.Client.Do(ctx, "SELECT", n)` 發送原始 `SELECT` 指令會污染池中連線。
> 請改用 `Option`/`Options` 的 `DB` 欄位。

## 測試

單元測試無需 Redis 即可執行。整合測試需設定 `REDIS_ADDR` 環境變數，否則跳過。
CI 以 `-race` 執行所有測試，並搭配 `redis:7` 服務容器，測試完成後上傳覆蓋率產物。

```bash
# 單元測試（不需要 Redis）
go test ./...

# 整合測試（需要 Redis）
REDIS_ADDR=127.0.0.1:6379 go test -v ./...

# Race detector（CI 也會執行此項）
REDIS_ADDR=127.0.0.1:6379 go test -race ./...
```

## 技術文件

- 📖 **[繁體中文文件](docs/zh-tw/)** — API 參考、連線池指南
- 📖 **[English Documentation](docs/en/)** — API reference, connection pool guide

## 變更紀錄

### v1.0.0

這是第一個穩定版本。此版本相較於 v0.5.x 包含破壞性 API 變更（ctx-aware 建構子、
`ErrEmptyScript`、縮小匯出介面）——依語意化版本規範因此升級主版本號。

- **連線池全面翻修**：修正 waiter 清單的資料競爭、喚醒遺漏問題，以及 `Close`/reaper
  的同步問題。`MinIdle` 連線現在在 client 建立時非同步預熱，並由背景 reaper 每 30 秒
  補充。伺服器錯誤回覆（`-ERR`、`NOSCRIPT`、`WRONGTYPE`）不再丟棄連線，只有傳輸
  錯誤才會。
- **NOSCRIPT 自動修復**：`ExecSha` 保留原始腳本內容。Redis 重啟或 `SCRIPT FLUSH` 後，
  自動重新載入腳本、重新持久化 SHA1，並自動重試一次。透過「從快取載入」路徑
  （nil/空 scripts map）建立的 Scriptor 無法自動修復，會回傳 `ErrScriptNotCached`。
- **破壞性變更**：`New` 與 `NewDB` 現在接受前置的 `context.Context`，由呼叫端控制
  啟動 deadline。原本內部的 5 秒超時已移除。
- **`Option` 連線池調校欄位**：`MinIdle`、`DialTimeout`、`ReadTimeout`、
  `WriteTimeout`、`IdleTimeout`、`MaxConnAge` 現在是 `goscriptor.Option` 的欄位
  （0 = 預設值，-1 = 停用）。`NewDB` 會驗證 `Host`/`Port`。
- **`ErrEmptyScript`**：`Exec("")` 現在回傳新的 sentinel `ErrEmptyScript`
  （errors.go 現有 6 個 sentinel）。
- **RESP 強化**：`ReadReply` 強制執行 512 MB bulk / 16 M 陣列長度上限與整數溢出檢查。
  `WriteCommand` 支援 `int32`、`int64`、`float32`（`f` 記法）、`float64`、
  `bool`（`"1"`/`"0"`）；不支援的型別回傳錯誤。
- **TTL 修復**：`Set` 將小於毫秒的 TTL 向上取整到 1 ms（PX）；`Expire` 將小於秒的
  duration 向上取整到 1 s，以避免截斷為零導致靜默刪除 key。
- **script.go 清理**：移除死碼、加入值 sentinel、修正錯誤傳遞。SHA body 驗證：
  相同名稱下若 body 已變更，以新 body 為準。
- **測試去除抖動**及 **GitHub Actions CI**：`gofmt`+`vet`+`build`+`go test -race`
  單元測試工作，加上搭配 `redis:7` 服務的整合測試工作，並上傳覆蓋率產物。

### v0.5.2-alpha
- **效能優化**：透過 `sync.Pool` 實現 RESP2 指令序列化的近乎零記憶體分配，PING 操作降至 20 B/op, 2 allocs/op。
- **效能優化**：優化 `ReadReply` 解析，避免整數解析時的字串轉換分配。
- **錯誤修復**：修復了當 `Close()` 喚醒等待中的連線池 goroutine 時導致的 nil pointer dereference 嚴重錯誤。
- **測試擴充**：測試覆蓋率提升（包含連線池耗盡、Waiter 上下文取消、協議解析的極端測試）。

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

---

## 測試與效能 (Testing & Performance)

本專案依賴真實的 Redis 進行整合測試，以確保 RESP2 協議的正確性與連線池的穩定性。底層自建的 Client 已針對 Zero-allocation 與字串解析進行極限優化。

請在本地執行效能基準測試（需要執行中的 Redis 實例）：

```bash
REDIS_ADDR=127.0.0.1:6379 go test -bench=. -benchmem -run='^$' ./redis/
```

**基準測試結果（Linux 容器，Intel Xeon @ 2.80 GHz）：**

```text
goos: linux
goarch: amd64
pkg: github.com/yshengliao/goscriptor/redis
cpu: Intel(R) Xeon(R) Processor @ 2.80GHz
BenchmarkPing-4   	   16210	     74665 ns/op	      20 B/op	       2 allocs/op
BenchmarkGet-4    	   15558	     86597 ns/op	      96 B/op	       4 allocs/op
PASS
ok  	github.com/yshengliao/goscriptor/redis	4.095s
```

*絕對數字因硬體而異；分配次數才是關鍵指標。*

- **零分配指令構造**：寫入 RESP2 指令時利用 `sync.Pool`，在正常的請求週期內消除動態記憶體分配。
- **極低解析分配**：`ReadReply` 改用 `bufio.Reader.ReadLine()` 搭配自訂的 `[]byte` 整數解析，將 `PING` 操作降至極低的 `2 allocs/op` (20 Bytes/op)。
