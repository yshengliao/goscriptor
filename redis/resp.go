// Package redis provides a minimal Redis client using the RESP2 protocol.
// This is an internal package — not intended for external consumption.
package redis

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
)

// RedisError represents an error reply from Redis.
type RedisError string

func (e RedisError) Error() string { return string(e) }

// WriteCommand serialises a Redis command in RESP2 array format.
func WriteCommand(w io.Writer, args ...any) error {
	buf := make([]byte, 0, 256)
	buf = append(buf, '*')
	buf = strconv.AppendInt(buf, int64(len(args)), 10)
	buf = append(buf, '\r', '\n')

	for _, arg := range args {
		s := fmt.Sprint(arg)
		buf = append(buf, '$')
		buf = strconv.AppendInt(buf, int64(len(s)), 10)
		buf = append(buf, '\r', '\n')
		buf = append(buf, s...)
		buf = append(buf, '\r', '\n')
	}

	_, err := w.Write(buf)
	return err
}

// ReadReply reads one RESP2 reply from r.
func ReadReply(r *bufio.Reader) (any, error) {
	line, err := readLine(r)
	if err != nil {
		return nil, err
	}
	if len(line) == 0 {
		return nil, fmt.Errorf("redis: empty RESP line")
	}

	switch line[0] {
	case '+':
		return string(line[1:]), nil
	case '-':
		return RedisError(line[1:]), nil
	case ':':
		n, err := strconv.ParseInt(string(line[1:]), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("redis: invalid integer %q", line[1:])
		}
		return n, nil
	case '$':
		n, err := strconv.ParseInt(string(line[1:]), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("redis: invalid bulk length %q", line[1:])
		}
		if n < 0 {
			return nil, nil
		}
		buf := make([]byte, n+2)
		if _, err = io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		return string(buf[:n]), nil
	case '*':
		n, err := strconv.ParseInt(string(line[1:]), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("redis: invalid array length %q", line[1:])
		}
		if n < 0 {
			return nil, nil
		}
		arr := make([]any, n)
		for i := range arr {
			arr[i], err = ReadReply(r)
			if err != nil {
				return nil, err
			}
		}
		return arr, nil
	default:
		return nil, fmt.Errorf("redis: unknown RESP type %q", line[0])
	}
}

func readLine(r *bufio.Reader) ([]byte, error) {
	line, err := r.ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	if len(line) < 2 || line[len(line)-2] != '\r' {
		return nil, fmt.Errorf("redis: invalid RESP line ending")
	}
	return line[:len(line)-2], nil
}
