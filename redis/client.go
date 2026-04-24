package redis

import (
	"bufio"
	"context"
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
	// Default: 1.
	MinIdle int

	// DialTimeout is the timeout for establishing new connections.
	// Default: 5s.
	DialTimeout time.Duration

	// ReadTimeout is the per-command read deadline.
	// Default: 3s. Set to -1 to disable.
	ReadTimeout time.Duration

	// WriteTimeout is the per-command write deadline.
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

// Client is a minimal Redis client that speaks RESP2.
type Client struct {
	opts *Options

	mu       sync.Mutex
	pool     []*conn
	active   int32 // total connections (pooled + in-use)
	closed   atomic.Bool
	waiters  []chan *conn // goroutines waiting for a connection
	closedCh chan struct{}
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

// NewClient creates a new Redis client.
func NewClient(opts *Options) *Client {
	c := &Client{
		opts:     opts,
		pool:     make([]*conn, 0, opts.poolSize()),
		closedCh: make(chan struct{}),
	}
	// Start background reaper for idle/expired connections
	go c.reaper()
	return c
}

// reaper periodically removes idle and expired connections.
func (c *Client) reaper() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			c.reapStaleConns()
		case <-c.closedCh:
			return
		}
	}
}

func (c *Client) reapStaleConns() {
	idleTimeout := c.opts.idleTimeout()
	maxAge := c.opts.maxConnAge()
	if idleTimeout == 0 && maxAge == 0 {
		return
	}

	c.mu.Lock()
	minIdle := c.opts.minIdle()
	var stale []*conn
	alive := c.pool[:0]
	for _, cn := range c.pool {
		if cn.isExpired(idleTimeout, maxAge) && len(alive) >= minIdle {
			stale = append(stale, cn)
		} else {
			alive = append(alive, cn)
		}
	}
	c.pool = alive
	c.mu.Unlock()

	for _, cn := range stale {
		atomic.AddInt32(&c.active, -1)
		cn.nc.Close()
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

func (c *Client) getConn(ctx context.Context) (*conn, error) {
	if c.closed.Load() {
		return nil, fmt.Errorf("redis: client is closed")
	}

	c.mu.Lock()

	// Try to get an idle connection from pool
	idleTimeout := c.opts.idleTimeout()
	maxAge := c.opts.maxConnAge()
	for len(c.pool) > 0 {
		cn := c.pool[len(c.pool)-1]
		c.pool = c.pool[:len(c.pool)-1]

		if cn.isExpired(idleTimeout, maxAge) {
			c.mu.Unlock()
			atomic.AddInt32(&c.active, -1)
			cn.nc.Close()
			c.mu.Lock()
			continue
		}
		c.mu.Unlock()
		cn.usedAt = time.Now()
		return cn, nil
	}

	// Can we create a new connection?
	if int(c.active) < c.opts.poolSize() {
		c.active++
		c.mu.Unlock()
		cn, err := c.dialConn(ctx)
		if err != nil {
			atomic.AddInt32(&c.active, -1)
			return nil, err
		}
		return cn, nil
	}

	// Pool is full — wait for a connection to be returned
	ch := make(chan *conn, 1)
	c.waiters = append(c.waiters, ch)
	c.mu.Unlock()

	select {
	case cn := <-ch:
		if cn == nil {
			return nil, fmt.Errorf("redis: client is closed")
		}
		cn.usedAt = time.Now()
		return cn, nil
	case <-ctx.Done():
		// Remove ourselves from waiters
		c.mu.Lock()
		for i, w := range c.waiters {
			if w == ch {
				c.waiters = append(c.waiters[:i], c.waiters[i+1:]...)
				break
			}
		}
		c.mu.Unlock()
		// Drain channel in case a connection arrived
		select {
		case cn := <-ch:
			c.putConn(cn)
		default:
		}
		return nil, ctx.Err()
	}
}

func (c *Client) putConn(cn *conn) {
	if c.closed.Load() {
		atomic.AddInt32(&c.active, -1)
		cn.nc.Close()
		return
	}

	c.mu.Lock()

	// If someone is waiting, hand the connection directly
	if len(c.waiters) > 0 {
		ch := c.waiters[0]
		c.waiters = c.waiters[1:]
		c.mu.Unlock()
		ch <- cn
		return
	}

	// Check if connection should be retired
	if cn.isExpired(c.opts.idleTimeout(), c.opts.maxConnAge()) {
		c.mu.Unlock()
		atomic.AddInt32(&c.active, -1)
		cn.nc.Close()
		return
	}

	c.pool = append(c.pool, cn)
	c.mu.Unlock()
}

func (c *Client) removeConn(cn *conn) {
	atomic.AddInt32(&c.active, -1)
	cn.nc.Close()
}

func (c *Client) execOn(ctx context.Context, cn *conn, args ...any) (any, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	wt := c.opts.writeTimeout()
	if wt > 0 {
		cn.nc.SetWriteDeadline(time.Now().Add(wt))
	}
	if err := WriteCommand(cn.nc, args...); err != nil {
		return nil, err
	}

	rt := c.opts.readTimeout()
	if rt > 0 {
		cn.nc.SetReadDeadline(time.Now().Add(rt))
	}
	reply, err := ReadReply(cn.rd)
	if err != nil {
		return nil, err
	}

	// Reset deadlines
	cn.nc.SetDeadline(time.Time{})

	if e, ok := reply.(RedisError); ok {
		return nil, e
	}
	return reply, nil
}

// Do executes a raw Redis command and returns the reply.
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
		c.removeConn(cn) // discard broken connection
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

	c.mu.Lock()
	defer c.mu.Unlock()

	// Wake up all waiters
	for _, ch := range c.waiters {
		close(ch)
	}
	c.waiters = nil

	var last error
	for _, cn := range c.pool {
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
		Active:  int(atomic.LoadInt32(&c.active)),
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
