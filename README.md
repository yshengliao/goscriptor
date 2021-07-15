# goscriptor

Packet GoScriptor implements a way to use redis script more easily

## Install

```console
go get -u -v github.com/yshengliao/goscriptor
```

## Usage

Let's start with a trivial example:

```go
package main

import (
    "github.com/yshengliao/goscriptor"
)

var (
    scriptDefinition = "scriptor_v.0.0.0"

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
}
```

----------

### Dependency

- testify - github.com/stretchr/testify
- go-redis - github.com/go-redis/redis/v8
- miniredis - github.com/alicebob/miniredis/v2


## TODO

1. [ ] Add test cases using "testify".
2. [ ] Add redis script test method.
3. [ ] Fix code comments.
4. [ ] Improve or remove useless code
5. [ ] Check code formatting
