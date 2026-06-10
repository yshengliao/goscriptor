package redis

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Default pool settings.
const (
	defaultPoolSize     = 10
	defaultMinIdle      = 1
	defaultDialTimeout  = 5 * time.Second
	defaultIdleTimeout  = 5 * time.Minute
	defaultMaxConnAge   = 30 * time.Minute
	defaultReadTimeout  = 3 * time.Second
	defaultWriteTimeout = 3 * time.Second

	// reaperInterval is how often the background reaper runs. Besides reaping
	// stale connections it replenishes idle connections up to MinIdle; on a
	// dial failure it stops that cycle and the next tick acts as the natural
	// backoff.
	reaperInterval = 30 * time.Second
)

// Options configures a Redis client.
type Options struct {
	Addr     string
	Password string
	DB       int

	// PoolSize is the maximum number of connections in the pool.
	// Default: 10.
	PoolSize int

	// MinIdle is the minimum number of idle connections to keep alive.
	// The client pre-warms up to MinIdle connections asynchronously after
	// NewClient returns and the background reaper replenishes idle
	// connections back up to MinIdle (bounded by PoolSize) on every tick.
	// Default: 1.
	MinIdle int

	// DialTimeout is the timeout for establishing new connections.
	// Default: 5s.
	DialTimeout time.Duration

	// ReadTimeout is the per-command read deadline. The effective read
	// deadline is the earlier of (now + ReadTimeout) and the caller ctx
	// deadline, if any. When set to -1 (disabled) only the ctx deadline
	// applies.
	// Default: 3s. Set to -1 to disable.
	ReadTimeout time.Duration

	// WriteTimeout is the per-command write deadline. The effective write
	// deadline is the earlier of (now + WriteTimeout) and the caller ctx
	// deadline, if any. When set to -1 (disabled) only the ctx deadline
	// applies.
	// Default: 3s. Set to -1 to disable.
	WriteTimeout time.Duration

	// IdleTimeout is how long a connection can sit idle before being closed.
	// Default: 5m. Set to -1 to disable.
	IdleTimeout time.Duration

	// MaxConnAge is the maximum lifetime of a connection.
	// Connections older than this are closed when returned to the pool.
	// Default: 30m. Set to -1 to disable.
	MaxConnAge time.Duration
}

func (o *Options) poolSize() int {
	if o.PoolSize > 0 {
		return o.PoolSize
	}
	return defaultPoolSize
}

func (o *Options) minIdle() int {
	if o.MinIdle > 0 {
		return o.MinIdle
	}
	return defaultMinIdle
}

func (o *Options) dialTimeout() time.Duration {
	if o.DialTimeout > 0 {
		return o.DialTimeout
	}
	return defaultDialTimeout
}

func (o *Options) readTimeout() time.Duration {
	if o.ReadTimeout > 0 {
		return o.ReadTimeout
	}
	if o.ReadTimeout < 0 {
		return 0 // disabled
	}
	return defaultReadTimeout
}

func (o *Options) writeTimeout() time.Duration {
	if o.WriteTimeout > 0 {
		return o.WriteTimeout
	}
	if o.WriteTimeout < 0 {
		return 0 // disabled
	}
	return defaultWriteTimeout
}

func (o *Options) idleTimeout() time.Duration {
	if o.IdleTimeout > 0 {
		return o.IdleTimeout
	}
	if o.IdleTimeout < 0 {
		return 0 // disabled
	}
	return defaultIdleTimeout
}

func (o *Options) maxConnAge() time.Duration {
	if o.MaxConnAge > 0 {
		return o.MaxConnAge
	}
	if o.MaxConnAge < 0 {
		return 0 // disabled
	}
	return defaultMaxConnAge
}

// errClosed is returned when an operation is attempted on a closed client.
var errClosed = errors.New("redis: client is closed")

// Client is a minimal Redis client that speaks RESP2.
//
// All mutable pool state (pool, active, waiters) is guarded by mu. The closed
// flag is an atomic.Bool for cheap fast-path checks, but every state
// transition that depends on it re-consults it under mu.
type Client struct {
	opts *Options

	mu      sync.Mutex
	pool    []*conn      // idle connections, used LIFO
	active  int          // total connections (idle + in-use), guarded by mu
	waiters []chan *conn // goroutines waiting for a connection, guarded by mu

	closed     atomic.Bool
	closedCh   chan struct{} // closed by Close to stop the reaper
	reaperDone chan struct{} // closed by the reaper goroutine on exit
}

type conn struct {
	nc        net.Conn
	rd        *bufio.Reader
	createdAt time.Time
	usedAt    time.Time
}

func (cn *conn) isExpired(idleTimeout, maxAge time.Duration) bool {
	now := time.Now()
	if idleTimeout > 0 && now.Sub(cn.usedAt) > idleTimeout {
		return true
	}
	if maxAge > 0 && now.Sub(cn.createdAt) > maxAge {
		return true
	}
	return false
}

// NewClient creates a new Redis client. The constructor is non-blocking: it
// kicks off an asynchronous warm-up that dials up to MinIdle connections and
// starts the background reaper.
func NewClient(opts *Options) *Client {
	c := &Client{
		opts:       opts,
		pool:       make([]*conn, 0, opts.poolSize()),
		closedCh:   make(chan struct{}),
		reaperDone: make(chan struct{}),
	}
	// Pre-warm MinIdle connections without blocking the caller.
	go c.warmup()
	// Start background reaper for idle/expired connections.
	go c.reaper()
	return c
}

// warmup dials up to MinIdle connections to satisfy the idle floor at startup.
func (c *Client) warmup() {
	target := c.opts.minIdle()
	if target > c.opts.poolSize() {
		target = c.opts.poolSize()
	}
	for i := 0; i < target; i++ {
		if !c.dialIdle(context.Background()) {
			return // closed or dial failed; reaper will retry later
		}
	}
}

// dialIdle reserves a slot, dials a fresh connection and parks it in the idle
// pool. It returns false if the client is closed or the dial failed (the slot
// is released on failure). A waiter, if any, is preferred over the idle pool so
// pre-warming also unblocks queued callers.
func (c *Client) dialIdle(ctx context.Context) bool {
	c.mu.Lock()
	if c.closed.Load() || c.active >= c.opts.poolSize() {
		c.mu.Unlock()
		return false
	}
	c.active++
	c.mu.Unlock()

	cn, err := c.dialConn(ctx)
	if err != nil {
		// Release the reserved slot AND wake one waiter: a caller may have
		// enqueued itself while this slot was reserved, and without a wakeup
		// it would sleep until its ctx deadline even though capacity is free.
		c.mu.Lock()
		c.active--
		c.wakeOneWaiterLocked()
		c.mu.Unlock()
		return false
	}

	c.mu.Lock()
	if c.closed.Load() {
		c.active--
		c.mu.Unlock()
		cn.nc.Close()
		return false
	}
	// Prefer handing the fresh conn to a waiter, else park it as idle.
	if w := c.popWaiterLocked(); w != nil {
		w <- cn
		c.mu.Unlock()
		return true
	}
	c.pool = append(c.pool, cn)
	c.mu.Unlock()
	return true
}

// reaper periodically removes idle and expired connections and replenishes the
// idle pool back up to MinIdle.
func (c *Client) reaper() {
	defer close(c.reaperDone)
	ticker := time.NewTicker(reaperInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			c.reapStaleConns()
			c.ensureMinIdle()
		case <-c.closedCh:
			return
		}
	}
}

// reapStaleConns closes every expired idle connection. Unlike a floor-based
// reaper it does not keep stale connections just to satisfy MinIdle; the idle
// floor is restored separately by ensureMinIdle with fresh connections.
func (c *Client) reapStaleConns() {
	idleTimeout := c.opts.idleTimeout()
	maxAge := c.opts.maxConnAge()
	if idleTimeout == 0 && maxAge == 0 {
		return
	}

	c.mu.Lock()
	if c.closed.Load() {
		c.mu.Unlock()
		return
	}
	var stale []*conn
	alive := c.pool[:0]
	for _, cn := range c.pool {
		if cn.isExpired(idleTimeout, maxAge) {
			stale = append(stale, cn)
		} else {
			alive = append(alive, cn)
		}
	}
	c.pool = alive
	c.active -= len(stale)
	c.mu.Unlock()

	for _, cn := range stale {
		cn.nc.Close()
	}
}

// ensureMinIdle replenishes the idle pool up to MinIdle (bounded by PoolSize).
// On the first dial failure it stops; the reaper tick is the natural backoff.
func (c *Client) ensureMinIdle() {
	target := c.opts.minIdle()
	if target > c.opts.poolSize() {
		target = c.opts.poolSize()
	}
	for {
		c.mu.Lock()
		if c.closed.Load() {
			c.mu.Unlock()
			return
		}
		// Replenish based on currently idle connections so we top the pool up
		// rather than over-dialing while connections are in use.
		need := target - len(c.pool)
		full := c.active >= c.opts.poolSize()
		c.mu.Unlock()
		if need <= 0 || full {
			return
		}
		if !c.dialIdle(context.Background()) {
			return
		}
	}
}

func (c *Client) dialConn(ctx context.Context) (*conn, error) {
	dialCtx, cancel := context.WithTimeout(ctx, c.opts.dialTimeout())
	defer cancel()

	var d net.Dialer
	nc, err := d.DialContext(dialCtx, "tcp", c.opts.Addr)
	if err != nil {
		return nil, err
	}
	cn := &conn{
		nc:        nc,
		rd:        bufio.NewReader(nc),
		createdAt: time.Now(),
		usedAt:    time.Now(),
	}

	initCtx, initCancel := context.WithTimeout(ctx, c.opts.dialTimeout())
	defer initCancel()

	if c.opts.Password != "" {
		if _, err := c.execOn(initCtx, cn, "AUTH", c.opts.Password); err != nil {
			nc.Close()
			return nil, err
		}
	}
	if c.opts.DB != 0 {
		if _, err := c.execOn(initCtx, cn, "SELECT", c.opts.DB); err != nil {
			nc.Close()
			return nil, err
		}
	}
	return cn, nil
}

// wakeOneWaiterLocked hands a nil retry-signal to the head waiter, if any,
// telling it a slot was freed so it can re-attempt the acquire loop. Callers
// must hold c.mu.
func (c *Client) wakeOneWaiterLocked() {
	if w := c.popWaiterLocked(); w != nil {
		w <- nil
	}
}

// popWaiterLocked removes and returns the head waiter, or nil if none. Callers
// must hold c.mu. The returned channel has capacity 1 and receives at most one
// send over its lifetime, so sending on it under the lock never blocks.
func (c *Client) popWaiterLocked() chan *conn {
	if len(c.waiters) == 0 {
		return nil
	}
	w := c.waiters[0]
	// Avoid aliasing the backing array so old entries can be GC'd.
	c.waiters = c.waiters[1:]
	if len(c.waiters) == 0 {
		c.waiters = nil
	}
	return w
}

// getConn acquires a connection, blocking (subject to ctx) if the pool is
// saturated.
func (c *Client) getConn(ctx context.Context) (*conn, error) {
	idleTimeout := c.opts.idleTimeout()
	maxAge := c.opts.maxConnAge()

	for {
		if c.closed.Load() {
			return nil, errClosed
		}

		c.mu.Lock()
		if c.closed.Load() {
			c.mu.Unlock()
			return nil, errClosed
		}

		// (a) Reuse an idle connection (LIFO). Collect expired ones to close
		// outside the lock, decrementing active under mu for each.
		var expired []*conn
		var got *conn
		for len(c.pool) > 0 {
			cn := c.pool[len(c.pool)-1]
			c.pool = c.pool[:len(c.pool)-1]
			if cn.isExpired(idleTimeout, maxAge) {
				c.active--
				expired = append(expired, cn)
				continue
			}
			got = cn
			break
		}
		if got != nil {
			c.mu.Unlock()
			closeAll(expired)
			got.usedAt = time.Now()
			return got, nil
		}

		// (b) Open a new connection if there is room.
		if c.active < c.opts.poolSize() {
			c.active++
			c.mu.Unlock()
			closeAll(expired)
			cn, err := c.dialConn(ctx)
			if err != nil {
				// Release the reserved slot and wake one waiter so a freed
				// slot never goes unnoticed (lost-wakeup guard).
				c.mu.Lock()
				c.active--
				c.wakeOneWaiterLocked()
				c.mu.Unlock()
				return nil, err
			}
			return cn, nil
		}

		// (c) Pool is saturated: enqueue and wait.
		ch := make(chan *conn, 1)
		c.waiters = append(c.waiters, ch)
		c.mu.Unlock()
		closeAll(expired)

		select {
		case cn, ok := <-ch:
			if !ok {
				return nil, errClosed // channel closed by Close
			}
			if cn == nil {
				continue // capacity freed; retry the acquire loop
			}
			cn.usedAt = time.Now()
			return cn, nil
		case <-ctx.Done():
			return nil, c.abandonWaiter(ch, ctx.Err())
		}
	}
}

// abandonWaiter handles ctx cancellation while parked as a waiter. If self is
// still queued it is removed and ctxErr returned. Otherwise a sender already
// atomically dequeued+sent (or Close dequeued+closed) before we took mu; a
// non-blocking drain is therefore deterministic and any real connection handed
// to us is returned to the pool before reporting ctxErr.
func (c *Client) abandonWaiter(ch chan *conn, ctxErr error) error {
	c.mu.Lock()
	for i, w := range c.waiters {
		if w == ch {
			c.waiters = append(c.waiters[:i], c.waiters[i+1:]...)
			if len(c.waiters) == 0 {
				c.waiters = nil
			}
			c.mu.Unlock()
			return ctxErr // we removed ourselves; nothing was handed off
		}
	}
	c.mu.Unlock()

	// Not in the queue: a send or close already happened-before this point.
	select {
	case cn, ok := <-ch:
		switch {
		case ok && cn != nil:
			c.putConn(cn) // reclaim the connection we will not use
		case ok && cn == nil:
			// We consumed a retry signal we will not act on. Forward it so
			// the freed slot is never lost on the remaining waiters.
			c.mu.Lock()
			if !c.closed.Load() {
				c.wakeOneWaiterLocked()
			}
			c.mu.Unlock()
		}
		// !ok: channel closed by Close, nothing to reclaim or forward.
	default:
		// Unreachable given the atomic dequeue+send invariant; kept as a
		// safety net so a missed signal can never panic or block.
	}
	return ctxErr
}

// putConn returns a connection to the pool, hands it to a waiter, or closes it.
func (c *Client) putConn(cn *conn) {
	if cn == nil { // defensive: never operate on a nil connection
		return
	}

	c.mu.Lock()
	if c.closed.Load() {
		c.active--
		c.mu.Unlock()
		cn.nc.Close()
		return
	}

	// Hand off to a waiter directly (atomic dequeue+send under mu).
	if w := c.popWaiterLocked(); w != nil {
		cn.usedAt = time.Now()
		w <- cn
		c.mu.Unlock()
		return
	}

	// No waiter exists and we hold mu throughout, so retiring an expired
	// connection here cannot strand a wakeup.
	if cn.isExpired(c.opts.idleTimeout(), c.opts.maxConnAge()) {
		c.active--
		c.mu.Unlock()
		cn.nc.Close()
		return
	}

	c.pool = append(c.pool, cn)
	c.mu.Unlock()
}

// removeConn discards a broken connection, freeing its slot and waking one
// waiter (lost-wakeup guard) so a queued caller can dial a replacement.
func (c *Client) removeConn(cn *conn) {
	if cn == nil {
		return
	}
	c.mu.Lock()
	c.active--
	if !c.closed.Load() {
		c.wakeOneWaiterLocked()
	}
	c.mu.Unlock()
	cn.nc.Close()
}

// closeAll closes every connection in the slice; nil-safe.
func closeAll(conns []*conn) {
	for _, cn := range conns {
		if cn != nil {
			cn.nc.Close()
		}
	}
}

// effectiveDeadline returns the earlier of (now + timeout) and the ctx
// deadline. A zero timeout means "disabled" so only the ctx deadline applies.
// The bool reports whether any deadline should be armed.
func effectiveDeadline(ctx context.Context, timeout time.Duration) (time.Time, bool) {
	var dl time.Time
	if timeout > 0 {
		dl = time.Now().Add(timeout)
	}
	if cd, ok := ctx.Deadline(); ok {
		if dl.IsZero() || cd.Before(dl) {
			dl = cd
		}
	}
	return dl, !dl.IsZero()
}

func (c *Client) execOn(ctx context.Context, cn *conn, args ...any) (any, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Ensure no armed deadline leaks onto a connection that returns to the
	// pool, even on an error path.
	defer cn.nc.SetDeadline(time.Time{})

	if dl, ok := effectiveDeadline(ctx, c.opts.writeTimeout()); ok {
		cn.nc.SetWriteDeadline(dl)
	}
	if err := WriteCommand(cn.nc, args...); err != nil {
		return nil, err
	}

	if dl, ok := effectiveDeadline(ctx, c.opts.readTimeout()); ok {
		cn.nc.SetReadDeadline(dl)
	}
	reply, err := ReadReply(cn.rd)
	if err != nil {
		return nil, err
	}

	if e, ok := reply.(RedisError); ok {
		return nil, e
	}
	return reply, nil
}

// Do executes a raw Redis command and returns the reply.
//
// A server error reply (RedisError, e.g. -ERR/NOSCRIPT/WRONGTYPE) is returned
// as the error but leaves the connection healthy, so it is returned to the
// pool. Only transport/protocol errors discard the connection.
func (c *Client) Do(ctx context.Context, args ...any) (any, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	cn, err := c.getConn(ctx)
	if err != nil {
		return nil, err
	}
	reply, err := c.execOn(ctx, cn, args...)
	if err != nil {
		var rerr RedisError
		if errors.As(err, &rerr) {
			// Server-side error: connection is still usable.
			c.putConn(cn)
		} else {
			// Transport/protocol error: discard the connection.
			c.removeConn(cn)
		}
		return nil, err
	}
	c.putConn(cn)
	return reply, nil
}

// Close releases all pooled connections and stops the background reaper.
func (c *Client) Close() error {
	if !c.closed.CompareAndSwap(false, true) {
		return nil // already closed
	}
	close(c.closedCh)

	// Wait for the reaper to exit before tearing down so it cannot race the
	// teardown (double-close, counter drift) or fight over mu.
	<-c.reaperDone

	c.mu.Lock()
	defer c.mu.Unlock()

	// Wake every waiter; a closed channel signals "client closed".
	for _, ch := range c.waiters {
		close(ch)
	}
	c.waiters = nil

	var last error
	for _, cn := range c.pool {
		c.active--
		if err := cn.nc.Close(); err != nil {
			last = err
		}
	}
	c.pool = nil
	return last
}

// PoolStats returns current pool statistics.
func (c *Client) PoolStats() PoolStats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return PoolStats{
		Active:  c.active,
		Idle:    len(c.pool),
		Waiters: len(c.waiters),
	}
}

// PoolStats contains pool statistics.
type PoolStats struct {
	Active  int // total connections (idle + in-use)
	Idle    int // idle connections in pool
	Waiters int // goroutines waiting for a connection
}

// Ping sends a PING command.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.Do(ctx, "PING")
	return err
}

// Eval executes a Lua script via EVAL.
func (c *Client) Eval(ctx context.Context, script string, keys []string, args ...any) (any, error) {
	cmd := make([]any, 0, 3+len(keys)+len(args))
	cmd = append(cmd, "EVAL", script, len(keys))
	for _, k := range keys {
		cmd = append(cmd, k)
	}
	cmd = append(cmd, args...)
	return c.Do(ctx, cmd...)
}

// EvalSha executes a cached Lua script via EVALSHA.
func (c *Client) EvalSha(ctx context.Context, sha string, keys []string, args ...any) (any, error) {
	cmd := make([]any, 0, 3+len(keys)+len(args))
	cmd = append(cmd, "EVALSHA", sha, len(keys))
	for _, k := range keys {
		cmd = append(cmd, k)
	}
	cmd = append(cmd, args...)
	return c.Do(ctx, cmd...)
}

// ScriptLoad loads a Lua script into the script cache and returns its SHA1.
func (c *Client) ScriptLoad(ctx context.Context, script string) (string, error) {
	reply, err := c.Do(ctx, "SCRIPT", "LOAD", script)
	if err != nil {
		return "", err
	}
	sha, ok := reply.(string)
	if !ok {
		return "", fmt.Errorf("redis: unexpected type %T from SCRIPT LOAD", reply)
	}
	return sha, nil
}

// ScriptExists checks whether a script SHA1 exists in the cache.
func (c *Client) ScriptExists(ctx context.Context, sha string) (bool, error) {
	reply, err := c.Do(ctx, "SCRIPT", "EXISTS", sha)
	if err != nil {
		return false, err
	}
	arr, ok := reply.([]any)
	if !ok || len(arr) == 0 {
		return false, fmt.Errorf("redis: unexpected reply from SCRIPT EXISTS")
	}
	n, ok := arr[0].(int64)
	if !ok {
		return false, fmt.Errorf("redis: unexpected type %T in SCRIPT EXISTS", arr[0])
	}
	return n == 1, nil
}

// FlushAll flushes all keys from all databases.
func (c *Client) FlushAll(ctx context.Context) error {
	_, err := c.Do(ctx, "FLUSHALL")
	return err
}
