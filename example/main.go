package main

import (
	"context"

	"github.com/yshengliao/goscriptor"
)

var (
	scriptDefinition = "scriptKey|0.0.0"

	hello               = "hello"
	_HelloworldTemplate = `
    return 'Hello, World!'
    `
)

type MyScriptor struct {
	Scriptor *goscriptor.Scriptor
}

// hello function
func (s *MyScriptor) hello(ctx context.Context) (string, error) {
	res, err := s.Scriptor.ExecSha(ctx, hello, []string{})
	if err != nil {
		return "", err
	}

	return res.(string), nil
}

func main() {
	opt := &goscriptor.Option{
		Host:     "127.0.0.1",
		Port:     6379,
		Password: "",
		DB:       0,
		PoolSize: 10,
	}

	scripts := map[string]string{
		hello: _HelloworldTemplate,
	}

	scriptor, err := goscriptor.NewDB(opt, 1, scriptDefinition, scripts)
	if err != nil {
		panic(err)
	}

	myscript := &MyScriptor{
		Scriptor: scriptor,
	}
	ctx := context.Background()

	for range 2 {
		res, err := myscript.hello(ctx)
		if err != nil {
			panic(err)
		}
		println(res)
	}
}
