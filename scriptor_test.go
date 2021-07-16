package goscriptor_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yshengliao/goscriptor"
)

const (
	scriptDefinition    = "scriptKey|0.0.0"
	hello               = "hello"
	_HelloworldTemplate = `
	return 'Hello, World!'
	`
)

var (
	scripts = map[string]string{
		hello: _HelloworldTemplate,
	}
)

func goscriptor_NewDB(host string, port int, scr map[string]string) (*goscriptor.Scriptor, error) {
	opt := &goscriptor.Option{
		Host:     host,
		Port:     port,
		Password: "",
		DB:       0,
		PoolSize: 1,
	}

	// opt := &goscriptor.Option{
	// 	Host:     "127.0.0.1",
	// 	Port:     6379,
	// 	Password: "",
	// 	DB:       0,
	// 	PoolSize: 1,
	// }

	scriptor, err := goscriptor.NewDB(opt, 1, scriptDefinition, &scr)
	return scriptor, err
}

func goscriptor_New(host string, port int, scr map[string]string, assert *assert.Assertions) (*goscriptor.Scriptor, error) {
	opt := &goscriptor.Option{
		Host:     host,
		Port:     port,
		Password: "",
		DB:       0,
		PoolSize: 1,
	}
	// opt := &goscriptor.Option{
	// 	Host:     "127.0.0.1",
	// 	Port:     6379,
	// 	Password: "",
	// 	DB:       0,
	// 	PoolSize: 1,
	// }

	redis := opt.Create()

	assert.NotNil(redis)

	scriptor, err := goscriptor.New(redis, 1, scriptDefinition, &scr)
	return scriptor, err
}

func goscriptor_TestCase(scriptor *goscriptor.Scriptor, assert *assert.Assertions) {
	var res interface{}
	var err error

	res, err = scriptor.Exec("return 'Hello, World!'", []string{""})
	assert.Nil(err)
	assert.Equal("Hello, World!", res.(string), "they should be equal")

	res, err = scriptor.Exec("error return 'Hello, World!'", []string{""})
	assert.NotNil(err)

	res, err = scriptor.ExecSha(hello, []string{""})
	assert.Nil(err)
	assert.Equal("Hello, World!", res.(string), "they should be equal")

	res, err = scriptor.ExecSha(hello+" not found", []string{""})
	assert.NotNil(err)
	assert.Equal("script not found.", err.Error(), "they should be equal")
}

func goscriptor_TestCaseScriptNil(scriptor *goscriptor.Scriptor, assert *assert.Assertions) {
	var res interface{}
	var err error

	res, err = scriptor.Exec("return 'Hello, World!'", []string{""})
	assert.Nil(err)
	assert.Equal("Hello, World!", res.(string), "they should be equal")

	res, err = scriptor.Exec("error return 'Hello, World!'", []string{""})
	assert.NotNil(err)

	res, err = scriptor.ExecSha(hello, []string{""})
	assert.NotNil(err)
	assert.Equal("script not found.", err.Error(), "they should be equal")
}

func Test_goscriptor_NewDB(t *testing.T) {
	var scriptor *goscriptor.Scriptor
	var err error

	assert := assert.New(t)

	// Mock Redis
	s := MockRedisServer()
	assert.NotNil(s)
	defer s.Close()

	// scripts does not exist
	scriptor, err = goscriptor_NewDB(s.Host(), s.Server().Addr().Port, nil)
	assert.Nil(err)
	goscriptor_TestCaseScriptNil(scriptor, assert)

	// scripts is empty
	scriptor, err = goscriptor_NewDB(s.Host(), s.Server().Addr().Port, map[string]string{})
	assert.Nil(err)
	goscriptor_TestCaseScriptNil(scriptor, assert)

	// register scripts
	scriptor, err = goscriptor_NewDB(s.Host(), s.Server().Addr().Port, scripts)
	assert.Nil(err)
	// run test cases
	goscriptor_TestCase(scriptor, assert)

	// scripts does not exist, and reload redis cache scripts
	scriptor, err = goscriptor_NewDB(s.Host(), s.Server().Addr().Port, nil)
	assert.Nil(err)
	// run test cases
	goscriptor_TestCase(scriptor, assert)

	// scripts is empty, and reload redis cache scripts
	scriptor, err = goscriptor_NewDB(s.Host(), s.Server().Addr().Port, map[string]string{})
	assert.Nil(err)
	// run test cases
	goscriptor_TestCase(scriptor, assert)

	// flushAll redis
	ok, err := scriptor.Client.FlushAll(scriptor.CTX).Result()
	assert.Nil(err)
	assert.Equal("OK", ok, "they should be equal")

	// scripts does not exist
	scriptor, err = goscriptor_NewDB(s.Host(), s.Server().Addr().Port, nil)
	assert.Nil(err)
	goscriptor_TestCaseScriptNil(scriptor, assert)

	// scripts is empty
	scriptor, err = goscriptor_NewDB(s.Host(), s.Server().Addr().Port, map[string]string{})
	assert.Nil(err)
	goscriptor_TestCaseScriptNil(scriptor, assert)

	// can re-register scripts
	scriptor, err = goscriptor_NewDB(s.Host(), s.Server().Addr().Port, scripts)
	assert.Nil(err)
	// run test cases
	goscriptor_TestCase(scriptor, assert)
}

func Test_goscriptor_New(t *testing.T) {
	var scriptor *goscriptor.Scriptor
	var err error

	assert := assert.New(t)

	// Mock Redis
	s := MockRedisServer()
	assert.NotNil(s)
	defer s.Close()

	// scripts does not exist
	scriptor, err = goscriptor_New(s.Host(), s.Server().Addr().Port, nil, assert)
	assert.Nil(err)
	goscriptor_TestCaseScriptNil(scriptor, assert)

	// scripts is empty
	scriptor, err = goscriptor_New(s.Host(), s.Server().Addr().Port, map[string]string{}, assert)
	assert.Nil(err)
	goscriptor_TestCaseScriptNil(scriptor, assert)

	// register scripts
	scriptor, err = goscriptor_New(s.Host(), s.Server().Addr().Port, scripts, assert)
	assert.Nil(err)
	// run test cases
	goscriptor_TestCase(scriptor, assert)

	// scripts does not exist, and reload redis cache scripts
	scriptor, err = goscriptor_New(s.Host(), s.Server().Addr().Port, nil, assert)
	assert.Nil(err)
	// run test cases
	goscriptor_TestCase(scriptor, assert)

	// scripts is empty, and reload redis cache scripts
	scriptor, err = goscriptor_New(s.Host(), s.Server().Addr().Port, map[string]string{}, assert)
	assert.Nil(err)
	// run test cases
	goscriptor_TestCase(scriptor, assert)

	// flushAll redis
	ok, err := scriptor.Client.FlushAll(scriptor.CTX).Result()
	assert.Nil(err)
	assert.Equal("OK", ok, "they should be equal")

	// scripts does not exist
	scriptor, err = goscriptor_NewDB(s.Host(), s.Server().Addr().Port, nil)
	assert.Nil(err)
	goscriptor_TestCaseScriptNil(scriptor, assert)

	// scripts is empty
	scriptor, err = goscriptor_NewDB(s.Host(), s.Server().Addr().Port, map[string]string{})
	assert.Nil(err)
	goscriptor_TestCaseScriptNil(scriptor, assert)

	// can re-register scripts
	scriptor, err = goscriptor_New(s.Host(), s.Server().Addr().Port, scripts, assert)
	assert.Nil(err)
	// run test cases
	goscriptor_TestCase(scriptor, assert)
}
