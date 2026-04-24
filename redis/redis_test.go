package redis_test

import (
	"bytes"
	"bufio"
	"context"
	"os"
	"testing"
	"time"

	"github.com/yshengliao/goscriptor/redis"
)

func redisAddr(t *testing.T) string {
	t.Helper()
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		t.Skip("REDIS_ADDR not set, skipping integration test")
	}
	return addr
}

func newTestClient(t *testing.T) *redis.Client {
	t.Helper()
	c := redis.NewClient(&redis.Options{
		Addr:     redisAddr(t),
		PoolSize: 2,
	})
	c.FlushAll(context.Background())
	return c
}

// --- RESP protocol tests (unit, no Redis needed) ---

func TestWriteCommand(t *testing.T) {
	var buf bytes.Buffer
	err := redis.WriteCommand(&buf, "SET", "key", "value")
	if err != nil {
		t.Fatal(err)
	}
	expected := "*3\r\n$3\r\nSET\r\n$3\r\nkey\r\n$5\r\nvalue\r\n"
	if buf.String() != expected {
		t.Fatalf("expected %q, got %q", expected, buf.String())
	}
}

func TestWriteCommand_Int(t *testing.T) {
	var buf bytes.Buffer
	err := redis.WriteCommand(&buf, "EXPIRE", "key", 60)
	if err != nil {
		t.Fatal(err)
	}
	expected := "*3\r\n$6\r\nEXPIRE\r\n$3\r\nkey\r\n$2\r\n60\r\n"
	if buf.String() != expected {
		t.Fatalf("expected %q, got %q", expected, buf.String())
	}
}

func TestReadReply_SimpleString(t *testing.T) {
	r := bufio.NewReader(bytes.NewReader([]byte("+OK\r\n")))
	reply, err := redis.ReadReply(r)
	if err != nil {
		t.Fatal(err)
	}
	if reply.(string) != "OK" {
		t.Fatalf("expected OK, got %v", reply)
	}
}

func TestReadReply_Error(t *testing.T) {
	r := bufio.NewReader(bytes.NewReader([]byte("-ERR bad\r\n")))
	reply, err := redis.ReadReply(r)
	if err != nil {
		t.Fatal(err)
	}
	e, ok := reply.(redis.RedisError)
	if !ok {
		t.Fatalf("expected RedisError, got %T", reply)
	}
	if e.Error() != "ERR bad" {
		t.Fatalf("expected 'ERR bad', got %q", e.Error())
	}
}

func TestReadReply_Integer(t *testing.T) {
	r := bufio.NewReader(bytes.NewReader([]byte(":42\r\n")))
	reply, err := redis.ReadReply(r)
	if err != nil {
		t.Fatal(err)
	}
	if reply.(int64) != 42 {
		t.Fatalf("expected 42, got %v", reply)
	}
}

func TestReadReply_BulkString(t *testing.T) {
	r := bufio.NewReader(bytes.NewReader([]byte("$5\r\nhello\r\n")))
	reply, err := redis.ReadReply(r)
	if err != nil {
		t.Fatal(err)
	}
	if reply.(string) != "hello" {
		t.Fatalf("expected hello, got %v", reply)
	}
}

func TestReadReply_NilBulkString(t *testing.T) {
	r := bufio.NewReader(bytes.NewReader([]byte("$-1\r\n")))
	reply, err := redis.ReadReply(r)
	if err != nil {
		t.Fatal(err)
	}
	if reply != nil {
		t.Fatalf("expected nil, got %v", reply)
	}
}

func TestReadReply_Array(t *testing.T) {
	data := "*2\r\n$3\r\nfoo\r\n$3\r\nbar\r\n"
	r := bufio.NewReader(bytes.NewReader([]byte(data)))
	reply, err := redis.ReadReply(r)
	if err != nil {
		t.Fatal(err)
	}
	arr := reply.([]any)
	if len(arr) != 2 || arr[0].(string) != "foo" || arr[1].(string) != "bar" {
		t.Fatalf("unexpected array: %v", arr)
	}
}

func TestReadReply_NilArray(t *testing.T) {
	r := bufio.NewReader(bytes.NewReader([]byte("*-1\r\n")))
	reply, err := redis.ReadReply(r)
	if err != nil {
		t.Fatal(err)
	}
	if reply != nil {
		t.Fatalf("expected nil, got %v", reply)
	}
}

func TestReadReply_EmptyArray(t *testing.T) {
	r := bufio.NewReader(bytes.NewReader([]byte("*0\r\n")))
	reply, err := redis.ReadReply(r)
	if err != nil {
		t.Fatal(err)
	}
	arr := reply.([]any)
	if len(arr) != 0 {
		t.Fatalf("expected empty array, got %v", arr)
	}
}

func TestReadReply_InvalidType(t *testing.T) {
	r := bufio.NewReader(bytes.NewReader([]byte("~invalid\r\n")))
	_, err := redis.ReadReply(r)
	if err == nil {
		t.Fatal("expected error for unknown RESP type")
	}
}

// --- Integration tests (need Redis) ---

func TestClient_PingClose(t *testing.T) {
	c := newTestClient(t)
	defer c.Close()

	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestClient_DoAfterClose(t *testing.T) {
	c := newTestClient(t)
	c.Close()

	_, err := c.Do(context.Background(), "PING")
	if err == nil {
		t.Fatal("expected error after close")
	}
}

func TestClient_DoubleClose(t *testing.T) {
	c := newTestClient(t)
	if err := c.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("second close should be nil: %v", err)
	}
}

func TestClient_ContextCanceled(t *testing.T) {
	c := newTestClient(t)
	defer c.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := c.Do(ctx, "PING")
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestClient_PoolStats(t *testing.T) {
	c := newTestClient(t)
	defer c.Close()

	c.Ping(context.Background())

	stats := c.PoolStats()
	if stats.Active < 1 {
		t.Fatalf("expected Active >= 1, got %d", stats.Active)
	}
	if stats.Idle < 1 {
		t.Fatalf("expected Idle >= 1, got %d", stats.Idle)
	}
}

// --- String commands ---

func TestClient_GetSet(t *testing.T) {
	c := newTestClient(t)
	defer c.Close()
	ctx := context.Background()

	err := c.Set(ctx, "k1", "v1", 0)
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	val, err := c.Get(ctx, "k1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if val != "v1" {
		t.Fatalf("expected v1, got %q", val)
	}
}

func TestClient_GetMissing(t *testing.T) {
	c := newTestClient(t)
	defer c.Close()

	val, err := c.Get(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if val != "" {
		t.Fatalf("expected empty string for missing key, got %q", val)
	}
}

func TestClient_SetWithTTL(t *testing.T) {
	c := newTestClient(t)
	defer c.Close()
	ctx := context.Background()

	c.Set(ctx, "ttlkey", "val", 10*time.Second)
	ttl, _ := c.TTL(ctx, "ttlkey")
	if ttl <= 0 || ttl > 10 {
		t.Fatalf("expected TTL in (0, 10], got %d", ttl)
	}
}

func TestClient_Del(t *testing.T) {
	c := newTestClient(t)
	defer c.Close()
	ctx := context.Background()

	c.Set(ctx, "d1", "v", 0)
	c.Set(ctx, "d2", "v", 0)
	n, err := c.Del(ctx, "d1", "d2", "d3")
	if err != nil {
		t.Fatalf("Del: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 deleted, got %d", n)
	}
}

func TestClient_Exists(t *testing.T) {
	c := newTestClient(t)
	defer c.Close()
	ctx := context.Background()

	c.Set(ctx, "e1", "v", 0)
	n, _ := c.Exists(ctx, "e1", "e2")
	if n != 1 {
		t.Fatalf("expected 1, got %d", n)
	}
}

func TestClient_IncrIncrBy(t *testing.T) {
	c := newTestClient(t)
	defer c.Close()
	ctx := context.Background()

	v1, _ := c.Incr(ctx, "counter")
	if v1 != 1 {
		t.Fatalf("expected 1, got %d", v1)
	}
	v2, _ := c.IncrBy(ctx, "counter", 9)
	if v2 != 10 {
		t.Fatalf("expected 10, got %d", v2)
	}
}

// --- Key commands ---

func TestClient_ExpireTTL(t *testing.T) {
	c := newTestClient(t)
	defer c.Close()
	ctx := context.Background()

	c.Set(ctx, "ek", "v", 0)
	ok, _ := c.Expire(ctx, "ek", 60*time.Second)
	if !ok {
		t.Fatal("expected Expire to return true")
	}
	ttl, _ := c.TTL(ctx, "ek")
	if ttl <= 0 || ttl > 60 {
		t.Fatalf("expected TTL in (0, 60], got %d", ttl)
	}

	ttl2, _ := c.TTL(ctx, "nonexistent_key_xxx")
	if ttl2 != -2 {
		t.Fatalf("expected -2 for missing key, got %d", ttl2)
	}
}

// --- Hash commands ---

func TestClient_Hash(t *testing.T) {
	c := newTestClient(t)
	defer c.Close()
	ctx := context.Background()

	c.HSet(ctx, "h1", "f1", "v1")
	c.HSet(ctx, "h1", "f2", "v2")

	val, _ := c.HGet(ctx, "h1", "f1")
	if val != "v1" {
		t.Fatalf("HGet: expected v1, got %q", val)
	}

	missing, _ := c.HGet(ctx, "h1", "f_missing")
	if missing != "" {
		t.Fatalf("HGet missing: expected empty, got %q", missing)
	}

	all, _ := c.HGetAll(ctx, "h1")
	if len(all) != 2 || all["f1"] != "v1" || all["f2"] != "v2" {
		t.Fatalf("HGetAll: unexpected %v", all)
	}

	exists, _ := c.HExists(ctx, "h1", "f1")
	if !exists {
		t.Fatal("HExists: expected true")
	}
	notExists, _ := c.HExists(ctx, "h1", "f_missing")
	if notExists {
		t.Fatal("HExists: expected false")
	}

	n, _ := c.HDel(ctx, "h1", "f1", "f_missing")
	if n != 1 {
		t.Fatalf("HDel: expected 1, got %d", n)
	}
}

// --- List commands ---

func TestClient_List(t *testing.T) {
	c := newTestClient(t)
	defer c.Close()
	ctx := context.Background()

	n, _ := c.RPush(ctx, "list", "a", "b", "c")
	if n != 3 {
		t.Fatalf("RPush: expected 3, got %d", n)
	}
	n, _ = c.LPush(ctx, "list", "z")
	if n != 4 {
		t.Fatalf("LPush: expected 4, got %d", n)
	}

	length, _ := c.LLen(ctx, "list")
	if length != 4 {
		t.Fatalf("LLen: expected 4, got %d", length)
	}

	head, _ := c.LPop(ctx, "list")
	if head != "z" {
		t.Fatalf("LPop: expected z, got %q", head)
	}
	tail, _ := c.RPop(ctx, "list")
	if tail != "c" {
		t.Fatalf("RPop: expected c, got %q", tail)
	}

	items, _ := c.LRange(ctx, "list", 0, -1)
	if len(items) != 2 || items[0] != "a" || items[1] != "b" {
		t.Fatalf("LRange: expected [a b], got %v", items)
	}

	// Pop from empty
	c.Del(ctx, "list")
	empty, _ := c.LPop(ctx, "emptylist")
	if empty != "" {
		t.Fatalf("LPop empty: expected empty, got %q", empty)
	}
	emptyR, _ := c.RPop(ctx, "emptylist")
	if emptyR != "" {
		t.Fatalf("RPop empty: expected empty, got %q", emptyR)
	}
}

// --- Set commands ---

func TestClient_Set_Commands(t *testing.T) {
	c := newTestClient(t)
	defer c.Close()
	ctx := context.Background()

	n, _ := c.SAdd(ctx, "s1", "a", "b", "c")
	if n != 3 {
		t.Fatalf("SAdd: expected 3, got %d", n)
	}

	card, _ := c.SCard(ctx, "s1")
	if card != 3 {
		t.Fatalf("SCard: expected 3, got %d", card)
	}

	isMember, _ := c.SIsMember(ctx, "s1", "a")
	if !isMember {
		t.Fatal("SIsMember: expected true for 'a'")
	}
	isMember2, _ := c.SIsMember(ctx, "s1", "z")
	if isMember2 {
		t.Fatal("SIsMember: expected false for 'z'")
	}

	members, _ := c.SMembers(ctx, "s1")
	if len(members) != 3 {
		t.Fatalf("SMembers: expected 3 members, got %d", len(members))
	}

	removed, _ := c.SRem(ctx, "s1", "a", "z")
	if removed != 1 {
		t.Fatalf("SRem: expected 1, got %d", removed)
	}
}

// --- Script commands ---

func TestClient_ScriptLoadExists(t *testing.T) {
	c := newTestClient(t)
	defer c.Close()
	ctx := context.Background()

	sha, err := c.ScriptLoad(ctx, "return 1")
	if err != nil {
		t.Fatalf("ScriptLoad: %v", err)
	}
	if sha == "" {
		t.Fatal("expected non-empty SHA")
	}

	exists, _ := c.ScriptExists(ctx, sha)
	if !exists {
		t.Fatal("script should exist")
	}

	notExists, _ := c.ScriptExists(ctx, "0000000000000000000000000000000000000000")
	if notExists {
		t.Fatal("nonexistent script should not exist")
	}
}

func TestClient_EvalEvalSha(t *testing.T) {
	c := newTestClient(t)
	defer c.Close()
	ctx := context.Background()

	res, err := c.Eval(ctx, "return 'hi'", nil)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if res.(string) != "hi" {
		t.Fatalf("expected hi, got %v", res)
	}

	sha, _ := c.ScriptLoad(ctx, "return KEYS[1]")
	res2, _ := c.EvalSha(ctx, sha, []string{"mykey"})
	if res2.(string) != "mykey" {
		t.Fatalf("expected mykey, got %v", res2)
	}
}

func TestClient_PoolExhaustion(t *testing.T) {
	addr := redisAddr(t)
	c := redis.NewClient(&redis.Options{
		Addr:     addr,
		PoolSize: 1,
	})
	defer c.Close()
	ctx := context.Background()

	// Fill pool with one connection
	c.Ping(ctx)

	// Second concurrent request should wait, then succeed once first returns
	done := make(chan error, 1)
	go func() {
		_, err := c.Do(ctx, "PING")
		done <- err
	}()

	// Small sleep to let goroutine start waiting
	time.Sleep(10 * time.Millisecond)

	// This should release the connection
	c.Ping(ctx)

	err := <-done
	if err != nil {
		t.Fatalf("concurrent Ping failed: %v", err)
	}
}

func TestClient_PoolWaiterContextCancel(t *testing.T) {
	addr := redisAddr(t)
	c := redis.NewClient(&redis.Options{
		Addr:     addr,
		PoolSize: 1,
	})
	defer c.Close()

	ctx := context.Background()

	// Hold the only connection with a slow Lua script
	hold := make(chan struct{})
	go func() {
		// This Lua busy-waits for ~200ms, keeping the connection occupied
		c.Eval(ctx, `
			local t = redis.call('TIME')
			local start = tonumber(t[1]) * 1000000 + tonumber(t[2])
			while true do
				local now = redis.call('TIME')
				local cur = tonumber(now[1]) * 1000000 + tonumber(now[2])
				if cur - start > 200000 then break end
			end
			return 'done'
		`, nil)
		close(hold)
	}()

	time.Sleep(20 * time.Millisecond) // let goroutine take the connection

	shortCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()

	_, err := c.Do(shortCtx, "PING")
	if err == nil {
		t.Fatal("expected timeout error")
	}
	<-hold
}

func TestClient_CustomTimeouts(t *testing.T) {
	addr := redisAddr(t)
	c := redis.NewClient(&redis.Options{
		Addr:         addr,
		PoolSize:     2,
		MinIdle:      2,
		DialTimeout:  2 * time.Second,
		ReadTimeout:  1 * time.Second,
		WriteTimeout: 1 * time.Second,
		IdleTimeout:  1 * time.Second,
		MaxConnAge:   1 * time.Second,
	})
	defer c.Close()

	ctx := context.Background()
	if err := c.Ping(ctx); err != nil {
		t.Fatalf("Ping with custom timeouts: %v", err)
	}

	// Wait for connections to expire, then ping again (forces new connection)
	time.Sleep(1200 * time.Millisecond)
	if err := c.Ping(ctx); err != nil {
		t.Fatalf("Ping after expiry: %v", err)
	}
}

func TestClient_DisabledTimeouts(t *testing.T) {
	addr := redisAddr(t)
	c := redis.NewClient(&redis.Options{
		Addr:         addr,
		PoolSize:     1,
		ReadTimeout:  -1,
		WriteTimeout: -1,
		IdleTimeout:  -1,
		MaxConnAge:   -1,
	})
	defer c.Close()

	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("Ping with disabled timeouts: %v", err)
	}
}

func TestClient_WrongPassword(t *testing.T) {
	addr := redisAddr(t)
	c := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: "definitely_wrong_password_12345",
		PoolSize: 1,
	})
	defer c.Close()

	err := c.Ping(context.Background())
	if err == nil {
		t.Fatal("expected auth error")
	}
}

