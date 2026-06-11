package redis

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"
)

// shortWriter is an io.Writer that always reports writing one fewer byte than it
// actually wrote, without returning an error — simulating a legal short-write.
type shortWriter struct {
	buf bytes.Buffer
}

func (sw *shortWriter) Write(p []byte) (int, error) {
	n, err := sw.buf.Write(p)
	if err != nil {
		return n, err
	}
	if n > 0 {
		// Report one byte less than actually written.
		return n - 1, nil
	}
	return 0, nil
}

// --- ReadReply malformed-input tests ---

func makeReader(s string) *bufio.Reader {
	return bufio.NewReader(strings.NewReader(s))
}

func TestReadReply_MalformedInputs(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "overflow length bulk",
			input: "$99999999999999999999\r\nhello\r\n",
		},
		{
			name:  "over-cap bulk length",
			input: fmt.Sprintf("$%d\r\nhello\r\n", int64(maxBulkLen)+1),
		},
		{
			name:  "negative-but-not-minus-one bulk",
			input: "$-5\r\n",
		},
		{
			name:  "bad length chars bulk",
			input: "$abc\r\n",
		},
		{
			name:  "empty RESP line",
			input: "\r\n",
		},
		{
			name:  "truncated bulk payload",
			input: "$10\r\nabc", // declares 10 bytes, only 3 + EOF
		},
		{
			name:  "truncated array",
			input: "*3\r\n+a\r\n", // says 3 elements, only 1 supplied
		},
		{
			name:  "unknown type byte",
			input: "!unknown\r\n",
		},
		{
			name:  "over-cap array length",
			input: fmt.Sprintf("*%d\r\n", int64(maxArrayLen)+1),
		},
		{
			name:  "negative-but-not-minus-one array",
			input: "*-5\r\n",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			r := makeReader(tt.input)
			val, err := ReadReply(r)
			if err == nil {
				t.Fatalf("expected error, got value %v", val)
			}
		})
	}
}

// --- parseAsciiInt unit tests ---

func TestParseAsciiInt(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int64
		wantErr bool
	}{
		{"empty", "", 0, true},
		{"bare minus", "-", 0, true},
		{"bare plus", "+", 0, true},
		{"zero", "0", 0, false},
		{"positive", "12345", 12345, false},
		{"negative", "-42", -42, false},
		{"max int64", "9223372036854775807", 9223372036854775807, false},
		{"overflow", "9223372036854775808", 0, true},
		{"overflow large", "99999999999999999999", 0, true},
		{"invalid char", "12a3", 0, true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseAsciiInt([]byte(tt.input))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for input %q, got %d", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for input %q: %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("input %q: expected %d, got %d", tt.input, tt.want, got)
			}
		})
	}
}

// --- WriteCommand tests ---

func respDecode(t *testing.T, data []byte) []string {
	t.Helper()
	r := bufio.NewReader(bytes.NewReader(data))
	raw, err := ReadReply(r)
	if err != nil {
		t.Fatalf("ReadReply: %v", err)
	}
	arr, ok := raw.([]any)
	if !ok {
		t.Fatalf("expected array, got %T", raw)
	}
	result := make([]string, len(arr))
	for i, v := range arr {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("element %d is not a string: %T", i, v)
		}
		result[i] = s
	}
	return result
}

func TestWriteCommand_ByteSlice(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteCommand(&buf, "SET", []byte("mykey"), []byte("myval")); err != nil {
		t.Fatal(err)
	}
	args := respDecode(t, buf.Bytes())
	if args[1] != "mykey" || args[2] != "myval" {
		t.Fatalf("unexpected args: %v", args)
	}
}

func TestWriteCommand_Int64(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteCommand(&buf, "INCRBY", "key", int64(42)); err != nil {
		t.Fatal(err)
	}
	args := respDecode(t, buf.Bytes())
	if args[2] != "42" {
		t.Fatalf("expected \"42\", got %q", args[2])
	}
}

func TestWriteCommand_Float64_NoScientific(t *testing.T) {
	var buf bytes.Buffer
	// 1e20 must be sent as "100000000000000000000", not "1e+20".
	if err := WriteCommand(&buf, "SET", "k", float64(1e20)); err != nil {
		t.Fatal(err)
	}
	args := respDecode(t, buf.Bytes())
	if args[2] != "100000000000000000000" {
		t.Fatalf("expected decimal notation, got %q", args[2])
	}
}

func TestWriteCommand_Float32(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteCommand(&buf, "SET", "k", float32(3.14)); err != nil {
		t.Fatal(err)
	}
	args := respDecode(t, buf.Bytes())
	if args[2] == "" {
		t.Fatal("float32 argument should produce a non-empty string")
	}
	// Must not contain 'e' or 'E' (no scientific notation).
	for _, c := range args[2] {
		if c == 'e' || c == 'E' {
			t.Fatalf("float32 arg must not use scientific notation, got %q", args[2])
		}
	}
}

func TestWriteCommand_BoolTrue(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteCommand(&buf, "SET", "k", true); err != nil {
		t.Fatal(err)
	}
	args := respDecode(t, buf.Bytes())
	if args[2] != "1" {
		t.Fatalf("bool true should encode as \"1\", got %q", args[2])
	}
}

func TestWriteCommand_BoolFalse(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteCommand(&buf, "SET", "k", false); err != nil {
		t.Fatal(err)
	}
	args := respDecode(t, buf.Bytes())
	if args[2] != "0" {
		t.Fatalf("bool false should encode as \"0\", got %q", args[2])
	}
}

func TestWriteCommand_UnsupportedType_Error(t *testing.T) {
	var buf bytes.Buffer
	err := WriteCommand(&buf, "SET", "k", struct{}{})
	if err == nil {
		t.Fatal("expected error for unsupported type struct{}{}")
	}
}

func TestWriteCommand_Nil_Error(t *testing.T) {
	var buf bytes.Buffer
	err := WriteCommand(&buf, "SET", "k", nil)
	if err == nil {
		t.Fatal("expected error for nil argument")
	}
}

func TestWriteCommand_ShortWriter(t *testing.T) {
	sw := &shortWriter{}
	err := WriteCommand(sw, "PING")
	if err != io.ErrShortWrite {
		t.Fatalf("expected io.ErrShortWrite, got %v", err)
	}
}
