package goscriptor

import (
	"strconv"

	"github.com/yshengliao/goscriptor/redis"
)

// Option provides a simple way to configure a Redis connection by host and port.
type Option struct {
	Host     string
	Port     int
	Password string
	DB       int
	PoolSize int
}

// Create creates a new Redis client from this option.
func (opt *Option) Create() *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:     opt.Host + ":" + strconv.Itoa(opt.Port),
		Password: opt.Password,
		DB:       opt.DB,
		PoolSize: opt.PoolSize,
	})
}
