package goscriptor

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
)

const (
	scriptDefinitionTest = "scriptKey|0.0.0"
	hello                = "hello"
	helloScript          = `return 'Hello, World!'`
)

func mockRedisServer() *miniredis.Miniredis {
	s, err := miniredis.Run()
	if err != nil {
		panic(err)
	}
	s.FlushAll()
	return s
}

func newRedisClient(addr string) redis.UniversalClient {
	opt := &UniversalOptions{Addrs: []string{addr}, DB: 0, PoolSize: 1}
	return opt.CreateAddrs()
}

func TestScriptDescriptor_Register(t *testing.T) {
	assert := assert.New(t)
	s := mockRedisServer()
	defer s.Close()

	client := newRedisClient(s.Addr())
	ctx := context.Background()

	scripts := map[string]string{hello: helloScript}
	sd := &ScriptDescriptor{}
	err := sd.Register(ctx, client, &scripts, scriptDefinitionTest, 1)
	assert.Nil(err)
	sha := sd.container[hello]
	assert.NotEmpty(sha)

	// verify the script was loaded
	exists, err := client.(*redis.Client).ScriptExists(ctx, sha).Result()
	assert.Nil(err)
	assert.True(exists[0])
}

func TestScriptDescriptor_LoadScripts(t *testing.T) {
	assert := assert.New(t)
	s := mockRedisServer()
	defer s.Close()

	client := newRedisClient(s.Addr())
	ctx := context.Background()

	scripts := map[string]string{hello: helloScript}
	sd := &ScriptDescriptor{}
	err := sd.Register(ctx, client, &scripts, scriptDefinitionTest, 1)
	assert.Nil(err)
	sha := sd.container[hello]

	sd2 := &ScriptDescriptor{}
	err = sd2.LoadScripts(ctx, client, scriptDefinitionTest, 1)
	assert.Nil(err)
	assert.Equal(sha, sd2.container[hello])
}

func TestScriptDescriptor_LoadScripts_NoKey(t *testing.T) {
	assert := assert.New(t)
	s := mockRedisServer()
	defer s.Close()

	client := newRedisClient(s.Addr())
	ctx := context.Background()

	sd := &ScriptDescriptor{}
	err := sd.LoadScripts(ctx, client, scriptDefinitionTest, 1)
	assert.Nil(err)
	assert.Nil(sd.container)
}

func TestScriptDescriptor_LoadScripts_MissingScript(t *testing.T) {
	assert := assert.New(t)
	s := mockRedisServer()
	defer s.Close()

	client := newRedisClient(s.Addr())
	ctx := context.Background()

	client.Do(ctx, "SELECT", 1)
	client.(*redis.Client).HSet(ctx, scriptDefinitionTest, hello, "deadbeef")

	sd := &ScriptDescriptor{}
	err := sd.LoadScripts(ctx, client, scriptDefinitionTest, 1)
	assert.NotNil(err)
}
