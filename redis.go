package goscriptor

import (
	"strconv"

	"github.com/go-redis/redis/v8"
)

// Option - Redis Option
type Option struct {
	Host     string
	Addrs    []string
	Port     int
	Password string
	DB       int
	PoolSize int
}

// Create - create a new redis descriptor
func (opt *Option) Create() redis.UniversalClient {
	return redis.NewUniversalClient(&redis.UniversalOptions{
		Addrs:    []string{opt.Host + ":" + strconv.Itoa(opt.Port)},
		Password: opt.Password,
		DB:       opt.DB,
		PoolSize: opt.PoolSize,
	})
}

// CreateAddrs - create a new redis descriptor
func (opt *Option) CreateAddrs() redis.UniversalClient {
	return redis.NewUniversalClient(&redis.UniversalOptions{
		Addrs:    opt.Addrs,
		Password: opt.Password,
		DB:       opt.DB,
		PoolSize: opt.PoolSize,
	})
}
