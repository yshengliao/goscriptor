package goscriptor_test

import (
	"net"
	"os"
	"strconv"
	"testing"
)

// redisAddr returns the Redis address from REDIS_ADDR env var.
// Tests that need Redis will skip if not set.
func redisAddr(t *testing.T) string {
	t.Helper()
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		t.Skip("REDIS_ADDR not set, skipping integration test")
	}
	return addr
}

// splitAddr splits "host:port" into (host, port).
func splitAddr(t *testing.T, addr string) (string, int) {
	t.Helper()
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("invalid REDIS_ADDR %q: %v", addr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("invalid port %q: %v", portStr, err)
	}
	return host, port
}
