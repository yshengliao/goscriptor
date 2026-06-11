package redis

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// --- in-process fake RESP server -------------------------------------------

// fakeServer is a tiny TCP server that speaks just enough RESP2 for the pool
// tests. Each accepted connection is handed to handler, which owns that
// connection's read/reply loop. No real Redis is involved.
type fakeServer struct {
	ln      net.Listener
	accepts int64 // total accepted connections (atomic)
	handler func(fs *fakeServer, idx int, c net.Conn)
	wg      sync.WaitGroup
	done    chan struct{} // closed by close() so blocked handlers can exit
}

// newFakeServer starts a server on 127.0.0.1:0. If handler is nil the default
// "read command, reply +OK" loop is used.
func newFakeServer(t *testing.T, handler func(fs *fakeServer, idx int, c net.Conn)) *fakeServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	fs := &fakeServer{
		ln:      ln,
		handler: handler,
		done:    make(chan struct{}),
	}
	if fs.handler == nil {
		fs.handler = handleOK
	}
	fs.wg.Add(1)
	go fs.serve()
	return fs
}

func (fs *fakeServer) addr() string { return fs.ln.Addr().String() }

func (fs *fakeServer) acceptCount() int { return int(atomic.LoadInt64(&fs.accepts)) }

func (fs *fakeServer) serve() {
	defer fs.wg.Done()
	for {
		c, err := fs.ln.Accept()
		if err != nil {
			return // listener closed
		}
		idx := int(atomic.AddInt64(&fs.accepts, 1)) - 1
		fs.wg.Add(1)
		go func() {
			defer fs.wg.Done()
			defer c.Close()
			fs.handler(fs, idx, c)
		}()
	}
}

// close stops the listener, releases any blocked handlers and waits for all
// connection goroutines to drain.
func (fs *fakeServer) close() {
	fs.ln.Close()
	close(fs.done)
	fs.wg.Wait()
}

// block parks a handler until the server is torn down. Use it for "never
// reply" handlers so they exit cleanly at close() instead of leaking.
func (fs *fakeServer) block() { <-fs.done }

// handleOK reads RESP command arrays and replies +OK to each until the peer
// closes or errors.
func handleOK(fs *fakeServer, idx int, c net.Conn) {
	r := bufio.NewReader(c)
	for {
		if _, err := readCommand(r); err != nil {
			return
		}
		if _, err := c.Write([]byte("+OK\r\n")); err != nil {
			return
		}
	}
}

// readCommand consumes one RESP2 command array (*N then N bulk strings) and
// returns the argument strings.
func readCommand(r *bufio.Reader) ([]string, error) {
	line, err := readCRLFLine(r)
	if err != nil {
		return nil, err
	}
	if len(line) == 0 || line[0] != '*' {
		return nil, fmt.Errorf("fake: expected array, got %q", line)
	}
	n, err := strconv.Atoi(line[1:])
	if err != nil {
		return nil, err
	}
	args := make([]string, 0, n)
	for i := 0; i < n; i++ {
		hdr, err := readCRLFLine(r)
		if err != nil {
			return nil, err
		}
		if len(hdr) == 0 || hdr[0] != '$' {
			return nil, fmt.Errorf("fake: expected bulk, got %q", hdr)
		}
		blen, err := strconv.Atoi(hdr[1:])
		if err != nil {
			return nil, err
		}
		buf := make([]byte, blen+2) // include trailing CRLF
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		args = append(args, string(buf[:blen]))
	}
	return args, nil
}

func readCRLFLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	line = line[:len(line)-1] // drop \n
	if len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}
	return line, nil
}

// --- test helpers -----------------------------------------------------------

// waitFor polls cond up to timeout, returning false on timeout.
func waitFor(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return cond()
}

// waitIdle blocks until the pool reports want idle connections.
func waitIdle(t *testing.T, c *Client, want int) {
	t.Helper()
	if !waitFor(2*time.Second, func() bool { return c.PoolStats().Idle == want }) {
		t.Fatalf("timed out waiting for Idle==%d, stats=%+v", want, c.PoolStats())
	}
}

// --- tests ------------------------------------------------------------------

// Concurrent stress: many goroutines mix Do, short-ctx cancellations and
// PoolStats. Afterwards no slot must leak and active must never exceed
// PoolSize.
func TestPool_ConcurrentStress(t *testing.T) {
	fs := newFakeServer(t, nil)
	defer fs.close()

	const poolSize = 3
	c := NewClient(&Options{Addr: fs.addr(), PoolSize: poolSize})
	defer c.Close()

	const goroutines = 60
	const opsEach = 25

	var maxActive int64
	observe := func() {
		a := int64(c.PoolStats().Active)
		for {
			m := atomic.LoadInt64(&maxActive)
			if a <= m || atomic.CompareAndSwapInt64(&maxActive, m, a) {
				break
			}
		}
	}

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			for i := 0; i < opsEach; i++ {
				switch (seed + i) % 3 {
				case 0:
					ctx, cancel := context.WithTimeout(context.Background(), time.Duration(seed%3)*time.Millisecond)
					c.Do(ctx, "PING")
					cancel()
				case 1:
					c.Do(context.Background(), "PING")
				default:
					c.PoolStats()
				}
				observe()
			}
		}(g)
	}
	wg.Wait()

	if got := int(atomic.LoadInt64(&maxActive)); got > poolSize {
		t.Fatalf("active exceeded PoolSize: max observed %d > %d", got, poolSize)
	}

	// After draining, every connection must be idle (no leaked slots).
	if !waitFor(2*time.Second, func() bool {
		s := c.PoolStats()
		return s.Active == s.Idle && s.Waiters == 0
	}) {
		t.Fatalf("slots leaked after drain: %+v", c.PoolStats())
	}
	if s := c.PoolStats(); s.Active > poolSize {
		t.Fatalf("active %d > PoolSize %d after drain", s.Active, poolSize)
	}
}

// Close while goroutines are blocked waiting for a connection: all waiters must
// return promptly with the closed error and nothing must panic.
func TestPool_CloseWhileWaiting(t *testing.T) {
	// Server reads the first command on each conn but never replies, so the
	// in-use connections stay occupied and later callers queue as waiters.
	fs := newFakeServer(t, func(fs *fakeServer, idx int, c net.Conn) {
		r := bufio.NewReader(c)
		readCommand(r)
		fs.block() // hold the connection until teardown
	})
	defer fs.close()

	const poolSize = 2
	c := NewClient(&Options{Addr: fs.addr(), PoolSize: poolSize})

	ctx := context.Background()
	// Saturate the pool: these calls block in ReadReply holding a conn each.
	for i := 0; i < poolSize; i++ {
		go func() { c.Do(ctx, "PING") }()
	}
	if !waitFor(2*time.Second, func() bool { return c.PoolStats().Active == poolSize }) {
		t.Fatalf("pool never saturated: %+v", c.PoolStats())
	}

	// Queue several waiters.
	const nWaiters = 4
	errs := make(chan error, nWaiters)
	for i := 0; i < nWaiters; i++ {
		go func() {
			_, err := c.Do(ctx, "PING")
			errs <- err
		}()
	}
	if !waitFor(2*time.Second, func() bool { return c.PoolStats().Waiters == nWaiters }) {
		t.Fatalf("waiters never queued: %+v", c.PoolStats())
	}

	c.Close()

	for i := 0; i < nWaiters; i++ {
		select {
		case err := <-errs:
			if !errors.Is(err, errClosed) {
				t.Fatalf("waiter got %v, want errClosed", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("waiter did not return after Close")
		}
	}
}

// A2+A3 regression: a waiter whose ctx cancels exactly while a putConn hands
// off and/or Close runs must never panic and must never leak a slot.
func TestPool_CancelHandoffRace(t *testing.T) {
	for iter := 0; iter < 250; iter++ {
		fs := newFakeServer(t, nil)
		const poolSize = 1
		c := NewClient(&Options{Addr: fs.addr(), PoolSize: poolSize})
		waitIdle(t, c, 1) // warm-up settled

		// Occupy the single connection.
		cn, err := c.getConn(context.Background())
		if err != nil {
			t.Fatalf("getConn: %v", err)
		}

		// Waiter with a ctx that fires very soon. A fixed 1ms deadline tends to
		// fire before the handoff is even attempted under load, so most
		// iterations miss the target interleave; widen it to a random 5-10ms so
		// the cancellation and the handoff actually overlap more often.
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(5+iter%6)*time.Millisecond)
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, gerr := c.getConn(ctx)
			if gerr == nil {
				c.putConn(got) // we briefly won the race; give it back
			}
		}()

		// Race the handoff against the cancellation.
		time.Sleep(time.Duration(iter%2) * time.Millisecond)
		c.putConn(cn)

		wg.Wait()
		cancel()

		// No slot may have leaked: after draining, Active == Idle. Use a
		// generous 5s budget so an extreme-load scheduler hiccup cannot trip a
		// false positive while still bounding a genuine leak.
		if !waitFor(5*time.Second, func() bool {
			s := c.PoolStats()
			return s.Active == s.Idle && s.Waiters == 0
		}) {
			t.Fatalf("iter %d: slot leak: %+v", iter, c.PoolStats())
		}
		c.Close()
		fs.close()
	}
}

// A4 regression: PoolSize 1, the in-use connection dies (server closes it →
// I/O error → removeConn). A queued waiter must wake and dial a fresh
// connection well before its ctx deadline instead of hanging.
func TestPool_LostWakeupOnRemove(t *testing.T) {
	var release sync.Once
	releaseCh := make(chan struct{})
	fs := newFakeServer(t, func(fs *fakeServer, idx int, c net.Conn) {
		r := bufio.NewReader(c)
		for {
			args, err := readCommand(r)
			if err != nil {
				return
			}
			// Dispatch the killer behaviour by command content, NOT by accept
			// order: ephemeral-port reuse can let a zombie warm-up dial from a
			// previous test's already-closed client steal an accept slot, so
			// idx is not a reliable identity.
			if len(args) > 0 && args[0] == "DIEAFTERREAD" {
				// Read the command, then die without replying to force an I/O
				// error in the holder's ReadReply.
				select {
				case <-releaseCh:
				case <-fs.done: // safety net if the test fails before releasing
				}
				c.Close()
				return
			}
			if _, err := c.Write([]byte("+OK\r\n")); err != nil {
				return
			}
		}
	})
	defer fs.close()

	c := NewClient(&Options{Addr: fs.addr(), PoolSize: 1})
	defer c.Close()
	ctx := context.Background()

	// Settle the async MinIdle warm-up first: an in-flight warm-up dial also
	// reads as Active==1/Idle==0, which would let the polls below pass before
	// A actually owns the connection.
	if _, err := c.Do(ctx, "PING"); err != nil {
		t.Fatalf("settle ping: %v", err)
	}
	if !waitFor(5*time.Second, func() bool {
		s := c.PoolStats()
		return s.Active == 1 && s.Idle == 1 && s.Waiters == 0
	}) {
		t.Fatalf("pool never settled after warm-up: %+v", c.PoolStats())
	}

	// Goroutine A grabs the only conn and blocks in ReadReply.
	aDone := make(chan error, 1)
	go func() { _, err := c.Do(ctx, "DIEAFTERREAD"); aDone <- err }()
	if !waitFor(5*time.Second, func() bool { return c.PoolStats().Active == 1 && c.PoolStats().Idle == 0 }) {
		t.Fatalf("A never took the conn: %+v", c.PoolStats())
	}

	// Goroutine B queues as a waiter with a generous deadline.
	bCtx, bCancel := context.WithTimeout(ctx, 10*time.Second)
	defer bCancel()
	bDone := make(chan error, 1)
	go func() { _, err := c.Do(bCtx, "PING"); bDone <- err }()
	if !waitFor(5*time.Second, func() bool { return c.PoolStats().Waiters == 1 }) {
		t.Fatalf("B never queued: %+v", c.PoolStats())
	}

	// Kill A's connection; removeConn must wake B.
	start := time.Now()
	release.Do(func() { close(releaseCh) })

	if err := <-aDone; err == nil {
		t.Fatalf("A should have failed with an I/O error (accepts=%d, stats=%+v)", fs.acceptCount(), c.PoolStats())
	}
	select {
	case err := <-bDone:
		if err != nil {
			t.Fatalf("B failed: %v", err)
		}
		if elapsed := time.Since(start); elapsed > 5*time.Second {
			t.Fatalf("B woke too slowly (%v); lost wakeup?", elapsed)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("B hung: lost wakeup")
	}
}

// A5 regression: a server error reply (-ERR) is surfaced as an error but the
// connection stays healthy and is reused.
func TestPool_ServerErrorKeepsConn(t *testing.T) {
	// Each connection embeds its identity (accept index) in the error reply so
	// reuse can be asserted by identity rather than by global accept counts —
	// ephemeral-port reuse can let a zombie warm-up dial from a previous test's
	// closed client inflate the accept counter.
	fs := newFakeServer(t, func(fs *fakeServer, idx int, c net.Conn) {
		r := bufio.NewReader(c)
		reply := []byte(fmt.Sprintf("-ERR boom %d\r\n", idx))
		for {
			if _, err := readCommand(r); err != nil {
				return
			}
			if _, err := c.Write(reply); err != nil {
				return
			}
		}
	})
	defer fs.close()

	c := NewClient(&Options{Addr: fs.addr(), PoolSize: 2, MinIdle: 1})
	defer c.Close()
	waitIdle(t, c, 1) // warm-up created one idle conn

	ctx := context.Background()
	_, err := c.Do(ctx, "PING")
	var rerr RedisError
	if !errors.As(err, &rerr) {
		t.Fatalf("want RedisError, got %T %v", err, err)
	}
	first := rerr.Error()
	if !strings.HasPrefix(first, "ERR boom") {
		t.Fatalf("want 'ERR boom <id>', got %q", first)
	}

	// Connection must be back in the pool, healthy.
	if !waitFor(time.Second, func() bool {
		s := c.PoolStats()
		return s.Active == 1 && s.Idle == 1
	}) {
		t.Fatalf("conn not returned after server error: %+v", c.PoolStats())
	}

	// Second Do must reuse the same connection (same embedded identity).
	_, err = c.Do(ctx, "PING")
	if !errors.As(err, &rerr) {
		t.Fatalf("second Do: want RedisError, got %T %v", err, err)
	}
	if second := rerr.Error(); second != first {
		t.Fatalf("connection not reused: identity went %q -> %q", first, second)
	}
}

// F1 regression: a server error reply followed by EXTRA bytes (a misbehaving
// server/proxy that breaks the single-reply framing assumption) must surface
// the RedisError AND discard the connection, never pool it. If the leftover
// "+SNEAKY" were pooled, the next reader on that conn would mis-frame it as its
// own reply. Each connection replies "-ERR boom <id>\r\n+SNEAKY\r\n" on its
// FIRST command, so a discarded conn is provable by a fresh identity on the
// next Do — and the next Do must read its own "-ERR boom", never "+SNEAKY".
func TestPool_ServerErrorWithTrailingBytesDiscardsConn(t *testing.T) {
	fs := newFakeServer(t, func(fs *fakeServer, idx int, c net.Conn) {
		r := bufio.NewReader(c)
		first := true
		for {
			if _, err := readCommand(r); err != nil {
				return
			}
			if first {
				first = false
				// Single write so the trailing "+SNEAKY" lands in the same TCP
				// segment as the error reply and is therefore already buffered
				// by the client's bufio reader when it parses the error.
				if _, err := c.Write([]byte(fmt.Sprintf("-ERR boom %d\r\n+SNEAKY\r\n", idx))); err != nil {
					return
				}
				continue
			}
			// Reached only if the conn were wrongly reused: emit a marker the
			// assertions can catch instead of another "-ERR boom".
			if _, err := c.Write([]byte("+REUSED\r\n")); err != nil {
				return
			}
		}
	})
	defer fs.close()

	c := NewClient(&Options{Addr: fs.addr(), PoolSize: 2, MinIdle: 1})
	defer c.Close()
	waitIdle(t, c, 1) // warm-up parked one idle conn (no command sent yet)

	ctx := context.Background()

	// First Do: the reply carries trailing bytes, so the conn must be dropped.
	reply, err := c.Do(ctx, "PING")
	var rerr RedisError
	if !errors.As(err, &rerr) {
		t.Fatalf("first Do: want RedisError, got reply=%v err=%T %v", reply, err, err)
	}
	first := rerr.Error()
	if !strings.HasPrefix(first, "ERR boom") {
		t.Fatalf("first Do: want 'ERR boom <id>', got %q", first)
	}

	// The poisoned conn must be discarded, not pooled: had it been pooled it
	// would show as Active==1/Idle==1 here. The lone warm-up conn was the one we
	// just poisoned, so a correct removeConn leaves the pool empty.
	if !waitFor(2*time.Second, func() bool {
		s := c.PoolStats()
		return s.Active == 0 && s.Idle == 0 && s.Waiters == 0
	}) {
		t.Fatalf("poisoned conn not discarded (want empty pool): %+v", c.PoolStats())
	}

	// Second Do: if the poisoned conn had been pooled, this would either read
	// the leftover "+SNEAKY" as a successful status reply (err==nil) or, on a
	// reused conn, hit our "+REUSED" marker. A correct discard means we land on
	// a different connection that emits its own first-command "-ERR boom <id>".
	reply, err = c.Do(ctx, "PING")
	if !errors.As(err, &rerr) {
		t.Fatalf("second Do: leftover bytes leaked (reply=%v err=%v); conn was not discarded", reply, err)
	}
	second := rerr.Error()
	if second == "SNEAKY" || strings.Contains(second, "SNEAKY") {
		t.Fatalf("second Do mis-framed the trailing bytes: got %q", second)
	}
	if !strings.HasPrefix(second, "ERR boom") {
		t.Fatalf("second Do: want a fresh 'ERR boom <id>', got %q", second)
	}
	if second == first {
		t.Fatalf("poisoned conn was reused: identity stayed %q", first)
	}
}

// F2 regression: putConn must retire an EXPIRED connection before any waiter
// handoff, symmetric with the getConn reuse path. A queued waiter must receive
// a nil retry-signal (never the stale conn), the freed slot must be accounted
// for (Active drops), and a follow-up acquire must dial a fresh connection.
func TestPool_PutExpiredConnWakesWaiterWithRetry(t *testing.T) {
	fs := newFakeServer(t, nil)
	defer fs.close()

	c := NewClient(&Options{Addr: fs.addr(), PoolSize: 1, MinIdle: 1})
	defer c.Close()
	waitIdle(t, c, 1) // warm-up parked the single idle conn

	// Take the only connection so we own a concrete *conn to expire.
	cn, err := c.getConn(context.Background())
	if err != nil {
		t.Fatalf("getConn: %v", err)
	}
	if s := c.PoolStats(); s.Active != 1 || s.Idle != 0 {
		t.Fatalf("after getConn want Active 1/Idle 0, got %+v", s)
	}

	// Force expiry by rewinding both clocks well past idleTimeout/maxConnAge.
	old := time.Now().Add(-time.Hour)
	c.mu.Lock()
	cn.createdAt = old
	cn.usedAt = old
	c.mu.Unlock()

	// Queue a waiter directly so we can observe exactly what putConn hands it.
	w := make(chan *conn, 1)
	c.mu.Lock()
	c.waiters = []chan *conn{w}
	c.mu.Unlock()

	// Return the expired conn. putConn must NOT hand it to w; it must drop it,
	// free the slot and wake w with a nil retry-signal.
	c.putConn(cn)

	select {
	case got, ok := <-w:
		if !ok {
			t.Fatal("waiter channel closed; want nil retry signal")
		}
		if got != nil {
			if got == cn {
				t.Fatal("waiter received the EXPIRED connection instead of a nil retry signal")
			}
			t.Fatalf("waiter received a connection (%p) instead of a nil retry signal", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("putConn did not wake the waiter after retiring the expired conn")
	}

	// The expired conn's slot must be released (signal conservation).
	if !waitFor(2*time.Second, func() bool { return c.PoolStats().Active == 0 }) {
		t.Fatalf("expired conn slot not freed: %+v", c.PoolStats())
	}

	// The retry path must now be able to dial a brand-new connection.
	fresh, err := c.getConn(context.Background())
	if err != nil {
		t.Fatalf("post-retry getConn: %v", err)
	}
	if fresh == cn {
		t.Fatal("retry reused the expired connection")
	}
	if !fresh.createdAt.After(old) {
		t.Fatalf("retry did not dial a fresh conn: createdAt=%v not after %v", fresh.createdAt, old)
	}
	if s := c.PoolStats(); s.Active != 1 {
		t.Fatalf("after fresh dial want Active 1, got %+v", s)
	}
	c.putConn(fresh)
}

// MinIdle pre-warm and replenish via the reaper helpers.
func TestPool_MinIdlePrewarmAndReplenish(t *testing.T) {
	fs := newFakeServer(t, nil)
	defer fs.close()

	c := NewClient(&Options{
		Addr:        fs.addr(),
		PoolSize:    4,
		MinIdle:     2,
		IdleTimeout: time.Minute, // enabled so reapStaleConns is active
	})
	defer c.Close()

	// Pre-warm should bring Idle up to 2.
	waitIdle(t, c, 2)
	if s := c.PoolStats(); s.Active != 2 {
		t.Fatalf("after pre-warm want Active 2, got %+v", s)
	}

	// Expire the idle conns by rewinding their clocks, then reap them.
	c.mu.Lock()
	old := time.Now().Add(-time.Hour)
	for _, cn := range c.pool {
		cn.usedAt = old
		cn.createdAt = old
	}
	c.mu.Unlock()

	c.reapStaleConns()
	if s := c.PoolStats(); s.Idle != 0 || s.Active != 0 {
		t.Fatalf("after reap want empty pool, got %+v", s)
	}

	// ensureMinIdle must restore the floor with fresh connections.
	c.ensureMinIdle()
	if !waitFor(2*time.Second, func() bool { return c.PoolStats().Idle == 2 }) {
		t.Fatalf("ensureMinIdle did not restore floor: %+v", c.PoolStats())
	}
	if s := c.PoolStats(); s.Active != 2 {
		t.Fatalf("after replenish want Active 2, got %+v", s)
	}
}

// A7: ctx deadline must bound socket I/O even when ReadTimeout is far larger.
func TestPool_CtxDeadlineOnSocket(t *testing.T) {
	// Server accepts and reads the command but never replies (until teardown).
	fs := newFakeServer(t, func(fs *fakeServer, idx int, c net.Conn) {
		r := bufio.NewReader(c)
		readCommand(r)
		fs.block() // never reply; released cleanly at close()
	})
	defer fs.close()

	c := NewClient(&Options{
		Addr:        fs.addr(),
		PoolSize:    1,
		ReadTimeout: 3 * time.Second, // far longer than the ctx deadline
	})
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := c.Do(ctx, "PING")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected a deadline error")
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("Do took %v, ctx deadline (100ms) not applied to socket", elapsed)
	}
}

// TestPool_AbandonForwardsRetrySignal pins the rule that a cancelling waiter
// which consumed a nil retry-signal must forward it to the next waiter:
// otherwise the freed-slot notification dies with the canceller and the
// remaining waiters sleep until their own deadlines.
func TestPool_AbandonForwardsRetrySignal(t *testing.T) {
	fs := newFakeServer(t, nil)
	defer fs.close()

	c := NewClient(&Options{Addr: fs.addr(), PoolSize: 1, MinIdle: 1})
	defer c.Close()

	// Let the async warm-up settle so it cannot interact with the manual
	// waiter queue below.
	if !waitFor(time.Second, func() bool { return c.PoolStats().Active == 1 }) {
		t.Fatal("warm-up did not settle")
	}

	w1 := make(chan *conn, 1)
	w2 := make(chan *conn, 1)
	c.mu.Lock()
	c.waiters = []chan *conn{w1, w2}
	// Simulate a freed slot waking the head waiter (w1).
	c.wakeOneWaiterLocked()
	c.mu.Unlock()

	// w1 "cancelled" after it was dequeued: abandonWaiter must drain the
	// retry signal and forward it to w2.
	if err := c.abandonWaiter(w1, context.Canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("abandonWaiter returned %v, want context.Canceled", err)
	}

	select {
	case cn, ok := <-w2:
		if !ok || cn != nil {
			t.Fatalf("w2 received (cn=%v, ok=%v), want forwarded nil retry signal", cn, ok)
		}
	case <-time.After(time.Second):
		t.Fatal("retry signal was not forwarded to the next waiter")
	}
}

// TestPool_DialIdleFailureWakesWaiter pins the rule that a failed warm-up /
// replenish dial must wake a waiter: the reserved slot made a concurrent
// caller enqueue itself, and without a wakeup it would sleep until its ctx
// deadline even though capacity is free again.
func TestPool_DialIdleFailureWakesWaiter(t *testing.T) {
	// An address that refuses connections immediately: listen, grab the
	// port, close the listener.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	// The whole test hinges on this address refusing connections. Ephemeral
	// ports can be re-bound by an unrelated process between Close above and now;
	// if a probe dial unexpectedly succeeds the premise is invalid, so skip
	// (an environment artefact) rather than report a false failure.
	if probe, err := net.DialTimeout("tcp", addr, 200*time.Millisecond); err == nil {
		probe.Close()
		t.Skipf("address %s unexpectedly accepts connections; "+
			"refused-port premise unavailable in this environment", addr)
	}

	c := NewClient(&Options{Addr: addr, PoolSize: 1, MinIdle: 1})
	defer c.Close()

	// Let the (failing) warm-up settle before installing the manual waiter.
	// Poll instead of sleeping a fixed interval: under load the warm-up dial
	// can still be in flight after an arbitrary fixed delay (which would leave
	// Active==1 and corrupt the manual waiter setup below). The warm-up has
	// settled once Active==0 holds across several consecutive samples — a single
	// Active==0 reading could merely be the gap before the dial reserves its
	// slot, so require stability.
	if !waitFor(5*time.Second, func() bool {
		stable := 0
		for i := 0; i < 5; i++ {
			if c.PoolStats().Active != 0 {
				return false
			}
			stable++
			time.Sleep(2 * time.Millisecond)
		}
		return stable == 5
	}) {
		t.Fatalf("failing warm-up never settled: %+v", c.PoolStats())
	}

	w := make(chan *conn, 1)
	c.mu.Lock()
	c.waiters = []chan *conn{w}
	c.mu.Unlock()

	if ok := c.dialIdle(context.Background()); ok {
		// dialIdle reported success: the address started accepting mid-test, so
		// the premise no longer holds. It will have handed the fresh conn to our
		// manual waiter; drain and close it so nothing leaks, then skip.
		select {
		case cn := <-w:
			if cn != nil {
				cn.nc.Close()
			}
		default:
		}
		t.Skipf("address %s began accepting connections mid-test; "+
			"refused-port premise unavailable in this environment", addr)
	}

	select {
	case cn, ok := <-w:
		if !ok || cn != nil {
			t.Fatalf("waiter received (cn=%v, ok=%v), want nil retry signal", cn, ok)
		}
	case <-time.After(time.Second):
		t.Fatal("failed dialIdle did not wake the queued waiter")
	}

	if got := c.PoolStats().Active; got != 0 {
		t.Fatalf("active = %d after failed dial, want 0", got)
	}
}
