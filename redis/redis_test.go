// Package redis_test contains integration tests for the redis client.
//
// Integration tests skip automatically when REDIS_ADDR is not set. All data
// keys used by these tests carry the "goscriptor_test:" prefix. The suite must
// run serially against a shared Redis instance (use -p 1 when running the full
// module). No FlushAll is performed; cleanup is targeted per test.
package redis_test

import (
	"bufio"
	"bytes"
	"context"
	"errors"
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

// waitPool polls the client's pool stats until cond holds or timeout elapses,
// returning the final evaluation of cond.
func waitPool(c *redis.Client, timeout time.Duration, cond func(redis.PoolStats) bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond(c.PoolStats()) {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return cond(c.PoolStats())
}

// newTestClient creates a test client. When key names are provided they are
// prefixed with "goscriptor_test:" and deleted both immediately (to ensure a
// clean starting state) and again on cleanup (to leave Redis tidy).
func newTestClient(t *testing.T, keys ...string) *redis.Client {
	t.Helper()
	c := redis.NewClient(&redis.Options{
		Addr:     redisAddr(t),
		PoolSize: 2,
	})
	if len(keys) > 0 {
		prefixed := make([]string, len(keys))
		for i, k := range keys {
			prefixed[i] = "goscriptor_test:" + k
		}
		ctx := context.Background()
		// Delete at start to ensure clean state even after a previous failure.
		c.Del(ctx, prefixed...)
		t.Cleanup(func() {
			c.Del(context.Background(), prefixed...)
		})
	}
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
	c := newTestClient(t, "k1")
	defer c.Close()
	ctx := context.Background()

	err := c.Set(ctx, "goscriptor_test:k1", "v1", 0)
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	val, err := c.Get(ctx, "goscriptor_test:k1")
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

	val, err := c.Get(context.Background(), "goscriptor_test:nonexistent")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if val != "" {
		t.Fatalf("expected empty string for missing key, got %q", val)
	}
}

func TestClient_SetWithTTL(t *testing.T) {
	c := newTestClient(t, "ttlkey")
	defer c.Close()
	ctx := context.Background()

	c.Set(ctx, "goscriptor_test:ttlkey", "val", 10*time.Second)
	ttl, _ := c.TTL(ctx, "goscriptor_test:ttlkey")
	if ttl <= 0 || ttl > 10 {
		t.Fatalf("expected TTL in (0, 10], got %d", ttl)
	}
}

func TestClient_Del(t *testing.T) {
	c := newTestClient(t, "d1", "d2")
	defer c.Close()
	ctx := context.Background()

	c.Set(ctx, "goscriptor_test:d1", "v", 0)
	c.Set(ctx, "goscriptor_test:d2", "v", 0)
	n, err := c.Del(ctx, "goscriptor_test:d1", "goscriptor_test:d2", "goscriptor_test:d3")
	if err != nil {
		t.Fatalf("Del: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 deleted, got %d", n)
	}
}

func TestClient_Exists(t *testing.T) {
	c := newTestClient(t, "e1")
	defer c.Close()
	ctx := context.Background()

	c.Set(ctx, "goscriptor_test:e1", "v", 0)
	n, _ := c.Exists(ctx, "goscriptor_test:e1", "goscriptor_test:e2")
	if n != 1 {
		t.Fatalf("expected 1, got %d", n)
	}
}

func TestClient_IncrIncrBy(t *testing.T) {
	c := newTestClient(t, "counter")
	defer c.Close()
	ctx := context.Background()

	v1, _ := c.Incr(ctx, "goscriptor_test:counter")
	if v1 != 1 {
		t.Fatalf("expected 1, got %d", v1)
	}
	v2, _ := c.IncrBy(ctx, "goscriptor_test:counter", 9)
	if v2 != 10 {
		t.Fatalf("expected 10, got %d", v2)
	}
}

// --- Key commands ---

func TestClient_ExpireTTL(t *testing.T) {
	c := newTestClient(t, "ek")
	defer c.Close()
	ctx := context.Background()

	c.Set(ctx, "goscriptor_test:ek", "v", 0)
	ok, _ := c.Expire(ctx, "goscriptor_test:ek", 60*time.Second)
	if !ok {
		t.Fatal("expected Expire to return true")
	}
	ttl, _ := c.TTL(ctx, "goscriptor_test:ek")
	if ttl <= 0 || ttl > 60 {
		t.Fatalf("expected TTL in (0, 60], got %d", ttl)
	}

	ttl2, _ := c.TTL(ctx, "goscriptor_test:nonexistent_key_xxx")
	if ttl2 != -2 {
		t.Fatalf("expected -2 for missing key, got %d", ttl2)
	}
}

// --- Hash commands ---

func TestClient_Hash(t *testing.T) {
	c := newTestClient(t, "h1")
	defer c.Close()
	ctx := context.Background()

	c.HSet(ctx, "goscriptor_test:h1", "f1", "v1")
	c.HSet(ctx, "goscriptor_test:h1", "f2", "v2")

	val, _ := c.HGet(ctx, "goscriptor_test:h1", "f1")
	if val != "v1" {
		t.Fatalf("HGet: expected v1, got %q", val)
	}

	missing, _ := c.HGet(ctx, "goscriptor_test:h1", "f_missing")
	if missing != "" {
		t.Fatalf("HGet missing: expected empty, got %q", missing)
	}

	all, _ := c.HGetAll(ctx, "goscriptor_test:h1")
	if len(all) != 2 || all["f1"] != "v1" || all["f2"] != "v2" {
		t.Fatalf("HGetAll: unexpected %v", all)
	}

	exists, _ := c.HExists(ctx, "goscriptor_test:h1", "f1")
	if !exists {
		t.Fatal("HExists: expected true")
	}
	notExists, _ := c.HExists(ctx, "goscriptor_test:h1", "f_missing")
	if notExists {
		t.Fatal("HExists: expected false")
	}

	n, _ := c.HDel(ctx, "goscriptor_test:h1", "f1", "f_missing")
	if n != 1 {
		t.Fatalf("HDel: expected 1, got %d", n)
	}
}

// --- List commands ---

func TestClient_List(t *testing.T) {
	c := newTestClient(t, "list", "emptylist")
	defer c.Close()
	ctx := context.Background()

	n, _ := c.RPush(ctx, "goscriptor_test:list", "a", "b", "c")
	if n != 3 {
		t.Fatalf("RPush: expected 3, got %d", n)
	}
	n, _ = c.LPush(ctx, "goscriptor_test:list", "z")
	if n != 4 {
		t.Fatalf("LPush: expected 4, got %d", n)
	}

	length, _ := c.LLen(ctx, "goscriptor_test:list")
	if length != 4 {
		t.Fatalf("LLen: expected 4, got %d", length)
	}

	head, _ := c.LPop(ctx, "goscriptor_test:list")
	if head != "z" {
		t.Fatalf("LPop: expected z, got %q", head)
	}
	tail, _ := c.RPop(ctx, "goscriptor_test:list")
	if tail != "c" {
		t.Fatalf("RPop: expected c, got %q", tail)
	}

	items, _ := c.LRange(ctx, "goscriptor_test:list", 0, -1)
	if len(items) != 2 || items[0] != "a" || items[1] != "b" {
		t.Fatalf("LRange: expected [a b], got %v", items)
	}

	// Pop from empty
	c.Del(ctx, "goscriptor_test:list")
	empty, _ := c.LPop(ctx, "goscriptor_test:emptylist")
	if empty != "" {
		t.Fatalf("LPop empty: expected empty, got %q", empty)
	}
	emptyR, _ := c.RPop(ctx, "goscriptor_test:emptylist")
	if emptyR != "" {
		t.Fatalf("RPop empty: expected empty, got %q", emptyR)
	}
}

// --- Set commands ---

func TestClient_Set_Commands(t *testing.T) {
	c := newTestClient(t, "s1")
	defer c.Close()
	ctx := context.Background()

	n, _ := c.SAdd(ctx, "goscriptor_test:s1", "a", "b", "c")
	if n != 3 {
		t.Fatalf("SAdd: expected 3, got %d", n)
	}

	card, _ := c.SCard(ctx, "goscriptor_test:s1")
	if card != 3 {
		t.Fatalf("SCard: expected 3, got %d", card)
	}

	isMember, _ := c.SIsMember(ctx, "goscriptor_test:s1", "a")
	if !isMember {
		t.Fatal("SIsMember: expected true for 'a'")
	}
	isMember2, _ := c.SIsMember(ctx, "goscriptor_test:s1", "z")
	if isMember2 {
		t.Fatal("SIsMember: expected false for 'z'")
	}

	members, _ := c.SMembers(ctx, "goscriptor_test:s1")
	if len(members) != 3 {
		t.Fatalf("SMembers: expected 3 members, got %d", len(members))
	}

	removed, _ := c.SRem(ctx, "goscriptor_test:s1", "a", "z")
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
	// Pool size 1: the holder takes the only connection via BLPOP.
	c := redis.NewClient(&redis.Options{
		Addr:     addr,
		PoolSize: 1,
	})
	defer c.Close()
	ctx := context.Background()

	// A second helper client is used to unblock the BLPOP deterministically.
	helper := redis.NewClient(&redis.Options{Addr: addr, PoolSize: 1})
	defer helper.Close()

	holderKey := "goscriptor_test:pool-exhaust:holder"
	// Delete upfront in case a previous run left an item in the list.
	helper.Del(context.Background(), holderKey)
	defer helper.Del(context.Background(), holderKey)

	// Settle the async MinIdle warm-up first: with PoolSize 1, polling
	// Active==1 && Idle==0 alone can be satisfied by an in-flight warm-up
	// dial (slot reserved, conn not yet parked), in which case the holder
	// does not actually own the connection yet.
	if err := c.Ping(ctx); err != nil {
		t.Fatalf("settle ping: %v", err)
	}
	if !waitPool(c, 2*time.Second, func(s redis.PoolStats) bool {
		return s.Active == 1 && s.Idle == 1 && s.Waiters == 0
	}) {
		t.Fatalf("pool never settled after warm-up: %+v", c.PoolStats())
	}

	// Hold the only connection with a blocking BLPOP. The holder is now the
	// only actor that can pop the parked conn, so Idle==0 means it owns it.
	holderDone := make(chan error, 1)
	go func() {
		_, err := c.Do(ctx, "BLPOP", holderKey, "5")
		holderDone <- err
	}()
	if !waitPool(c, 2*time.Second, func(s redis.PoolStats) bool {
		return s.Active == 1 && s.Idle == 0
	}) {
		t.Fatalf("holder never took the connection: %+v", c.PoolStats())
	}

	// Second concurrent request should queue as a waiter.
	waitDone := make(chan error, 1)
	go func() {
		_, err := c.Do(ctx, "PING")
		waitDone <- err
	}()
	if !waitPool(c, 2*time.Second, func(s redis.PoolStats) bool {
		return s.Waiters == 1
	}) {
		t.Fatalf("waiter never queued: %+v", c.PoolStats())
	}

	// Unblock the holder by pushing an item onto the list.
	if _, err := helper.LPush(ctx, holderKey, "go"); err != nil {
		t.Fatalf("LPush to unblock holder: %v", err)
	}

	if err := <-holderDone; err != nil {
		t.Fatalf("holder BLPOP failed: %v", err)
	}

	if err := <-waitDone; err != nil {
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

	// A helper client is used to unblock the BLPOP holder after the test so
	// we can use a generous timeout (5s) and avoid races.
	helper := redis.NewClient(&redis.Options{Addr: addr, PoolSize: 1})
	defer helper.Close()

	holderKey := "goscriptor_test:pool-waiter-cancel:holder"
	// Delete upfront in case a previous run left an item in the list.
	helper.Del(context.Background(), holderKey)
	defer helper.Del(context.Background(), holderKey)

	// Settle the async MinIdle warm-up first so that Active==1 && Idle==0
	// below can only mean "the holder owns the connection" (an in-flight
	// warm-up dial also reads as Active==1/Idle==0).
	if err := c.Ping(ctx); err != nil {
		t.Fatalf("settle ping: %v", err)
	}
	if !waitPool(c, 2*time.Second, func(s redis.PoolStats) bool {
		return s.Active == 1 && s.Idle == 1 && s.Waiters == 0
	}) {
		t.Fatalf("pool never settled after warm-up: %+v", c.PoolStats())
	}

	// Hold the only connection with a server-side blocking command. BLPOP keeps
	// this connection occupied without busying the Redis event loop.
	hold := make(chan struct{})
	go func() {
		c.Do(ctx, "BLPOP", holderKey, "5")
		close(hold)
	}()
	if !waitPool(c, 2*time.Second, func(s redis.PoolStats) bool {
		return s.Active == 1 && s.Idle == 0
	}) {
		t.Fatalf("holder never took the connection: %+v", c.PoolStats())
	}

	shortCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()

	_, err := c.Do(shortCtx, "PING")
	if err == nil {
		t.Fatal("expected timeout error")
	}

	// Unblock the holder so it exits cleanly.
	helper.LPush(context.Background(), holderKey, "done")
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
		IdleTimeout:  300 * time.Millisecond,
		MaxConnAge:   300 * time.Millisecond,
	})
	defer c.Close()

	ctx := context.Background()
	if err := c.Ping(ctx); err != nil {
		t.Fatalf("Ping with custom timeouts: %v", err)
	}

	// Wait for connections to expire, then ping again (forces new connection).
	// The pool replenishes MinIdle via the background reaper every 30s, so the
	// expiry-then-ping path exercises lazy re-dial — assertions stay as-is.
	time.Sleep(500 * time.Millisecond)
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
	// The failure must be a server reply (RedisError), not a dial/transport
	// error. Against a no-auth Redis, the server replies with an ERR message
	// about no password being set — still a RedisError.
	var rerr redis.RedisError
	if !errors.As(err, &rerr) {
		t.Fatalf("expected a RedisError (server reply), got %T: %v", err, err)
	}
}
