package goscriptor

import (
	"strconv"

	"github.com/go-redis/redis/v8"
)

// Option - Redis Option
type Option struct {
	Host     string
	Port     int
	Password string
	DB       int
	PoolSize int
}

// Create - create a new redis descriptor
func (opt *Option) Create() redis.UniversalClient {
	return redis.NewUniversalClient(&redis.UniversalOptions{
		Addrs:         []string{opt.Host + ":" + strconv.Itoa(opt.Port)},
		Password:      opt.Password,
		DB:            opt.DB,
		PoolSize:      opt.PoolSize,
		RouteRandomly: false,
	})
}

// UniversalOptions - Redis Option
type UniversalOptions redis.UniversalOptions

// CreateAddrs - create a new redis descriptor
//
//	https://redis.uptrace.dev/guide/universal.html
func (opt *UniversalOptions) CreateAddrs() redis.UniversalClient {
	return redis.NewUniversalClient((*redis.UniversalOptions)(opt))
}
