package goscriptor_test

import (
    "context"
    "testing"

    "github.com/go-redis/redis/v8"
    "github.com/stretchr/testify/assert"
    "github.com/yshengliao/goscriptor"
)

const (
    scriptDefinition = "scriptKey|0.0.0"
    hello            = "hello"
    helloScript      = `return 'Hello, World!'`
)

func newRedisClient(addr string) redis.UniversalClient {
    opt := &goscriptor.UniversalOptions{Addrs: []string{addr}, DB: 0, PoolSize: 1}
    return opt.CreateAddrs()
}

func TestScriptDescriptor_Register(t *testing.T) {
    assert := assert.New(t)
    s := MockRedisServer()
    defer s.Close()

    client := newRedisClient(s.Addr())
    ctx := context.Background()

    scripts := map[string]string{hello: helloScript}

    sd := &goscriptor.ScriptDescriptor{}
    err := sd.Register(ctx, client, &scripts, scriptDefinition, 1)
    assert.Nil(err)
    sha := sd.container[hello]
    assert.NotEmpty(sha)

    // verify value stored in redis
    client.Do(ctx, "SELECT", 1)
    val, err := client.(*redis.Client).HGet(ctx, scriptDefinition, hello).Result()
    assert.Nil(err)
    assert.Equal(sha, val)

    exists, err := client.(*redis.Client).ScriptExists(ctx, sha).Result()
    assert.Nil(err)
    assert.True(exists[0])
}

func TestScriptDescriptor_LoadScripts(t *testing.T) {
    assert := assert.New(t)
    s := MockRedisServer()
    defer s.Close()

    client := newRedisClient(s.Addr())
    ctx := context.Background()

    scripts := map[string]string{hello: helloScript}
    sd := &goscriptor.ScriptDescriptor{}
    err := sd.Register(ctx, client, &scripts, scriptDefinition, 1)
    assert.Nil(err)
    sha := sd.container[hello]

    sd2 := &goscriptor.ScriptDescriptor{}
    err = sd2.LoadScripts(ctx, client, scriptDefinition, 1)
    assert.Nil(err)
    assert.Equal(sha, sd2.container[hello])
}

func TestScriptDescriptor_LoadScripts_NoKey(t *testing.T) {
    assert := assert.New(t)
    s := MockRedisServer()
    defer s.Close()

    client := newRedisClient(s.Addr())
    ctx := context.Background()

    sd := &goscriptor.ScriptDescriptor{}
    err := sd.LoadScripts(ctx, client, scriptDefinition, 1)
    assert.Nil(err)
    assert.Nil(sd.container)
}

func TestScriptDescriptor_LoadScripts_MissingScript(t *testing.T) {
    assert := assert.New(t)
    s := MockRedisServer()
    defer s.Close()

    client := newRedisClient(s.Addr())
    ctx := context.Background()

    client.Do(ctx, "SELECT", 1)
    client.(*redis.Client).HSet(ctx, scriptDefinition, hello, "deadbeef")

    sd := &goscriptor.ScriptDescriptor{}
    err := sd.LoadScripts(ctx, client, scriptDefinition, 1)
    assert.NotNil(err)
}


