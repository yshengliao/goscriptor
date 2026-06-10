package goscriptor

import (
	"strconv"
	"time"

	"github.com/yshengliao/goscriptor/redis"
)

// Option provides a simple way to configure a Redis connection by host and port.
type Option struct {
	Host     string
	Port     int
	Password string
	DB       int

	// PoolSize is the maximum number of connections in the pool.
	// Default: 10.
	PoolSize int

	// MinIdle is the minimum number of idle connections to keep alive.
	// The client pre-warms up to MinIdle connections asynchronously after the
	// client is created and the background reaper replenishes idle connections
	// back up to MinIdle (bounded by PoolSize) on every tick.
	// Default: 1.
	MinIdle int

	// DialTimeout is the timeout for establishing new connections.
	// Default: 5s.
	DialTimeout time.Duration

	// ReadTimeout is the per-command read deadline. The effective read deadline
	// is the earlier of (now + ReadTimeout) and the caller ctx deadline, if any.
	// When set to -1 (disabled) only the ctx deadline applies.
	// Default: 3s. Set to -1 to disable.
	ReadTimeout time.Duration

	// WriteTimeout is the per-command write deadline. The effective write
	// deadline is the earlier of (now + WriteTimeout) and the caller ctx
	// deadline, if any. When set to -1 (disabled) only the ctx deadline applies.
	// Default: 3s. Set to -1 to disable.
	WriteTimeout time.Duration

	// IdleTimeout is how long a connection can sit idle before being closed.
	// Default: 5m. Set to -1 to disable.
	IdleTimeout time.Duration

	// MaxConnAge is the maximum lifetime of a connection. Connections older than
	// this are closed when returned to the pool.
	// Default: 30m. Set to -1 to disable.
	MaxConnAge time.Duration
}

// Create creates a new Redis client from this option.
func (opt *Option) Create() *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:         opt.Host + ":" + strconv.Itoa(opt.Port),
		Password:     opt.Password,
		DB:           opt.DB,
		PoolSize:     opt.PoolSize,
		MinIdle:      opt.MinIdle,
		DialTimeout:  opt.DialTimeout,
		ReadTimeout:  opt.ReadTimeout,
		WriteTimeout: opt.WriteTimeout,
		IdleTimeout:  opt.IdleTimeout,
		MaxConnAge:   opt.MaxConnAge,
	})
}
