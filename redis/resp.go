// Package redis provides a minimal, zero-dependency Redis client using the RESP2 protocol.
package redis

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"strconv"
	"sync"
)

// maxBulkLen is Redis's proto-max-bulk-len default (512 MB).
const maxBulkLen = 512 << 20

// maxArrayLen caps RESP array allocations to prevent malicious or corrupt length
// headers from triggering huge allocations.
const maxArrayLen = 1 << 24

// RedisError represents an error reply from Redis.
type RedisError string

func (e RedisError) Error() string { return string(e) }

var bufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 0, 512)
		return &b
	},
}

// WriteCommand serialises a Redis command in RESP2 array format and writes it to w.
//
// Supported argument types: string, []byte, int, int64, int32, float64, float32, bool.
//   - float64 and float32 are formatted with 'f' notation (no scientific notation) because
//     Redis cannot parse scientific notation.
//   - bool is encoded as "1" (true) or "0" (false).
//   - Any other type, including nil, returns an error rather than silently sending
//     Go-formatted garbage to the server.
//
// WriteCommand also guards against partial writes: if the underlying io.Writer accepts
// fewer bytes than requested without returning an error, io.ErrShortWrite is returned.
func WriteCommand(w io.Writer, args ...any) error {
	ptr := bufPool.Get().(*[]byte)
	buf := (*ptr)[:0] // reset length

	buf = append(buf, '*')
	buf = strconv.AppendInt(buf, int64(len(args)), 10)
	buf = append(buf, '\r', '\n')

	for _, arg := range args {
		var s string
		switch v := arg.(type) {
		case string:
			s = v
		case []byte:
			s = string(v) // safe: transient use for length/append
		case int:
			s = strconv.Itoa(v)
		case int32:
			s = strconv.FormatInt(int64(v), 10)
		case int64:
			s = strconv.FormatInt(v, 10)
		case float64:
			s = strconv.FormatFloat(v, 'f', -1, 64)
		case float32:
			s = strconv.FormatFloat(float64(v), 'f', -1, 32)
		case bool:
			if v {
				s = "1"
			} else {
				s = "0"
			}
		default:
			// Return the buffer to the pool before returning the error.
			if cap(buf) <= 4096 {
				*ptr = buf
				bufPool.Put(ptr)
			}
			return fmt.Errorf("redis: unsupported argument type %T", arg)
		}

		buf = append(buf, '$')
		buf = strconv.AppendInt(buf, int64(len(s)), 10)
		buf = append(buf, '\r', '\n')
		buf = append(buf, s...)
		buf = append(buf, '\r', '\n')
	}

	n, err := w.Write(buf)
	if err == nil && n < len(buf) {
		err = io.ErrShortWrite
	}

	// Put back only if it hasn't grown outrageously large.
	if cap(buf) <= 4096 {
		*ptr = buf
		bufPool.Put(ptr)
	}

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
		n, err := parseAsciiInt(line[1:])
		if err != nil {
			return nil, fmt.Errorf("redis: invalid integer %q", line[1:])
		}
		return n, nil
	case '$':
		n, err := parseAsciiInt(line[1:])
		if err != nil {
			return nil, fmt.Errorf("redis: invalid bulk length %q", line[1:])
		}
		if n == -1 {
			return nil, nil // RESP null bulk string
		}
		if n < -1 || n > maxBulkLen {
			return nil, fmt.Errorf("redis: bulk length out of range: %d", n)
		}
		// Read exact bulk string size + \r\n
		buf := make([]byte, n+2)
		if _, err = io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		return string(buf[:n]), nil
	case '*':
		n, err := parseAsciiInt(line[1:])
		if err != nil {
			return nil, fmt.Errorf("redis: invalid array length %q", line[1:])
		}
		if n == -1 {
			return nil, nil // RESP null array
		}
		if n < -1 || n > maxArrayLen {
			return nil, fmt.Errorf("redis: array length out of range: %d", n)
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

// readLine reads a line up to \r\n without allocating if it fits in bufio buffer.
func readLine(r *bufio.Reader) ([]byte, error) {
	line, isPrefix, err := r.ReadLine()
	if err != nil {
		return nil, err
	}
	if isPrefix {
		// Rare case: line is too long for bufio.Reader's buffer
		full := append([]byte(nil), line...)
		for isPrefix && err == nil {
			line, isPrefix, err = r.ReadLine()
			full = append(full, line...)
		}
		return full, err
	}
	// Note: r.ReadLine strips \r\n and returns a slice into its internal buffer.
	// This is safe because we only parse it before the next read.
	return line, nil
}

// parseAsciiInt is a fast path for ASCII integer parsing to avoid string allocations.
// It detects overflow and rejects bare sign characters with no digits.
func parseAsciiInt(b []byte) (int64, error) {
	if len(b) == 0 {
		return 0, fmt.Errorf("empty int")
	}
	var n int64
	var sign int64 = 1
	var start int
	if b[0] == '-' {
		sign = -1
		start = 1
	} else if b[0] == '+' {
		start = 1
	}
	// Reject bare "-" or "+" with no digits.
	if start == len(b) {
		return 0, fmt.Errorf("no digits after sign")
	}
	for i := start; i < len(b); i++ {
		if b[i] < '0' || b[i] > '9' {
			return 0, fmt.Errorf("invalid char")
		}
		digit := int64(b[i] - '0')
		// Overflow guard: n*10 + digit > math.MaxInt64
		if n > (math.MaxInt64-digit)/10 {
			return 0, fmt.Errorf("integer overflow")
		}
		n = n*10 + digit
	}
	return n * sign, nil
}
