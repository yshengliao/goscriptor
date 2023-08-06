package main

import (
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
func (s *MyScriptor) hello() (string, error) {
	res, err := s.Scriptor.ExecSha(hello, []string{})
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

	scriptor, err := goscriptor.NewDB(opt, 1, scriptDefinition, &scripts)
	if err != nil {
		panic(err)
	}

	myscript := &MyScriptor{
		Scriptor: scriptor,
	}
	res, err := myscript.hello()
	if err != nil {
		panic(err)
	}
	println(res)

	res, err = myscript.hello()
	if err != nil {
		panic(err)
	}
	println(res)

	res, err = myscript.hello()
	if err != nil {
		panic(err)
	}
	println(res)
}
