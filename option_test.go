package goscriptor_test

import (
	"context"
	"testing"

	"github.com/yshengliao/goscriptor"
)

func TestOption_Create_Ping(t *testing.T) {
	addr := redisAddr(t)
	host, port := splitAddr(t, addr)

	opt := &goscriptor.Option{
		Host:     host,
		Port:     port,
		Password: "",
		DB:       0,
		PoolSize: 1,
	}

	client := opt.Create()
	if client == nil {
		t.Fatal("expected non-nil client")
	}

	err := client.Ping(context.Background())
	if err != nil {
		t.Fatalf("Ping failed: %v", err)
	}
}
