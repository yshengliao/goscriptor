package goscriptor_test

import (
	"context"
	"os"
	"testing"
	"github.com/yshengliao/goscriptor/redis"
)

func BenchmarkPing(b *testing.B) {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		b.Skip("REDIS_ADDR not set")
	}
	c := redis.NewClient(&redis.Options{Addr: addr, PoolSize: 1})
	defer c.Close()
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Ping(ctx)
	}
}

func BenchmarkGet(b *testing.B) {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		b.Skip("REDIS_ADDR not set")
	}
	c := redis.NewClient(&redis.Options{Addr: addr, PoolSize: 1})
	defer c.Close()
	ctx := context.Background()
	c.Set(ctx, "mybenchkey", "hello_world_zero_copy_test", 0)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Get(ctx, "mybenchkey")
	}
}
