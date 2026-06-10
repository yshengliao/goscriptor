package main

import (
	"context"
	"fmt"

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

// hello executes the cached hello script.
func (s *MyScriptor) hello(ctx context.Context) (string, error) {
	res, err := s.Scriptor.ExecSha(ctx, hello, []string{})
	if err != nil {
		return "", err
	}
	str, ok := res.(string)
	if !ok {
		return "", fmt.Errorf("unexpected type %T", res)
	}
	return str, nil
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

	ctx := context.Background()

	scriptor, err := goscriptor.NewDB(ctx, opt, 1, scriptDefinition, scripts)
	if err != nil {
		panic(err)
	}
	defer func() { _ = scriptor.Close() }()

	myscript := &MyScriptor{
		Scriptor: scriptor,
	}

	for range 2 {
		res, err := myscript.hello(ctx)
		if err != nil {
			panic(err)
		}
		fmt.Println(res)
	}
}
