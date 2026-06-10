package goscriptor_test

import (
	"testing"

	"github.com/yshengliao/goscriptor"
)

func TestRedisReplyValue_AsInt32(t *testing.T) {
	tests := []struct {
		name    string
		input   any
		want    int32
		wantErr bool
	}{
		{"string int", "42", 42, false},
		{"string float", "3.7", 3, false},
		{"int", int(10), 10, false},
		{"int32", int32(20), 20, false},
		{"int64", int64(30), 30, false},
		{"nil", nil, 99, false},
		{"bad string", "abc", 99, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := goscriptor.NewRedisReplyValue(tt.input)
			got, err := v.AsInt32(99)
			if tt.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("expected %d, got %d", tt.want, got)
			}
		})
	}
}

func TestRedisReplyValue_AsInt64(t *testing.T) {
	tests := []struct {
		name    string
		input   any
		want    int64
		wantErr bool
	}{
		{"string", "100", 100, false},
		{"int", int(10), 10, false},
		{"int32", int32(20), 20, false},
		{"int64", int64(30), 30, false},
		{"nil", nil, -1, false},
		{"bad", "xyz", -1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := goscriptor.NewRedisReplyValue(tt.input)
			got, err := v.AsInt64(-1)
			if tt.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("expected %d, got %d", tt.want, got)
			}
		})
	}
}

func TestRedisReplyValue_AsFloat64(t *testing.T) {
	tests := []struct {
		name    string
		input   any
		want    float64
		wantErr bool
	}{
		{"string", "3.14", 3.14, false},
		{"int", int(5), 5.0, false},
		{"int32", int32(6), 6.0, false},
		{"int64", int64(7), 7.0, false},
		{"float32", float32(1.5), 1.5, false},
		{"float64", float64(2.5), 2.5, false},
		{"nil", nil, 0.0, false},
		{"bad", "abc", 0.0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := goscriptor.NewRedisReplyValue(tt.input)
			got, err := v.AsFloat64(0.0)
			if tt.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			// float32 loses precision, use tolerance
			if !tt.wantErr && (got-tt.want > 0.01 || tt.want-got > 0.01) {
				t.Fatalf("expected %f, got %f", tt.want, got)
			}
		})
	}
}

func TestRedisReplyValue_AsString(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  string
	}{
		{"string", "hello", "hello"},
		{"int", int(42), "42"},
		{"int32", int32(32), "32"},
		{"int64", int64(64), "64"},
		{"nil", nil, ""},
		{"unsupported", []byte("bytes"), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := goscriptor.NewRedisReplyValue(tt.input)
			if got := v.AsString(); got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestRedisReplyValue_NullableInt(t *testing.T) {
	// Non-nil
	v := goscriptor.NewRedisReplyValue("42")
	ptr, err := v.NullableInt()
	if err != nil {
		t.Fatal(err)
	}
	if ptr == nil || *ptr != 42 {
		t.Fatalf("expected *42, got %v", ptr)
	}

	// Nil
	v2 := goscriptor.NewRedisReplyValue(nil)
	ptr2, err2 := v2.NullableInt()
	if err2 != nil {
		t.Fatal(err2)
	}
	if ptr2 != nil {
		t.Fatalf("expected nil, got %v", ptr2)
	}

	// Error
	v3 := goscriptor.NewRedisReplyValue("bad")
	_, err3 := v3.NullableInt()
	if err3 == nil {
		t.Fatal("expected error")
	}
}

func TestRedisReplyValue_NullableString(t *testing.T) {
	v := goscriptor.NewRedisReplyValue("hello")
	ptr := v.NullableString()
	if ptr == nil || *ptr != "hello" {
		t.Fatalf("expected *hello, got %v", ptr)
	}

	v2 := goscriptor.NewRedisReplyValue(nil)
	if v2.NullableString() != nil {
		t.Fatal("expected nil")
	}
}

func TestRedisReplyValue_IsNil(t *testing.T) {
	if !goscriptor.EmptyRedisReplyValue.IsNil() {
		t.Fatal("EmptyRedisReplyValue should be nil")
	}
	if goscriptor.NewRedisReplyValue("x").IsNil() {
		t.Fatal("non-nil value should not be nil")
	}
}

func TestRedisReplyValue_ToArrayReplyReader(t *testing.T) {
	// Valid array
	v := goscriptor.NewRedisReplyValue([]any{"a", "b"})
	r := v.ToArrayReplyReader()
	if r == nil {
		t.Fatal("expected non-nil reader")
	}
	if r.GetLength() != 2 {
		t.Fatalf("expected length 2, got %d", r.GetLength())
	}

	// Non-array
	v2 := goscriptor.NewRedisReplyValue("not an array")
	if v2.ToArrayReplyReader() != nil {
		t.Fatal("expected nil for non-array")
	}
}

func TestRedisArrayReplyReader_ReadFloat64(t *testing.T) {
	r := goscriptor.NewRedisArrayReplyReader([]any{"3.14", int64(7)})

	f1, err := r.ReadFloat64(0)
	if err != nil {
		t.Fatal(err)
	}
	if f1 < 3.13 || f1 > 3.15 {
		t.Fatalf("expected ~3.14, got %f", f1)
	}

	f2, err := r.ReadFloat64(0)
	if err != nil {
		t.Fatal(err)
	}
	if f2 != 7.0 {
		t.Fatalf("expected 7.0, got %f", f2)
	}
}

func TestRedisArrayReplyReader_ReadArray(t *testing.T) {
	inner := []any{"x", "y"}
	r := goscriptor.NewRedisArrayReplyReader([]any{inner, "not_array"})

	sub := r.ReadArray()
	if sub == nil || sub.GetLength() != 2 {
		t.Fatal("expected nested reader with 2 items")
	}

	nilSub := r.ReadArray()
	if nilSub != nil {
		t.Fatal("expected nil for non-array value")
	}
}

func TestRedisArrayReplyReader_BeyondBounds(t *testing.T) {
	r := goscriptor.NewRedisArrayReplyReader([]any{"only"})
	r.ReadString() // consume the only item

	v := r.ReadValue()
	if !v.IsNil() {
		t.Fatal("reading beyond bounds should return EmptyRedisReplyValue")
	}
}

func TestExec_EmptyScript(t *testing.T) {
	addr := redisAddr(t)
	host, port := splitAddr(t, addr)

	opt := &goscriptor.Option{Host: host, Port: port, DB: 0, PoolSize: 1}
	s, err := goscriptor.NewDB(opt, 1, "test_empty_script", nil)
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer s.Close()

	_, err = s.Exec(nil, "", nil)
	if err == nil {
		t.Fatal("expected error for empty script")
	}
}

func TestRedisReplyValue_Value(t *testing.T) {
	v := goscriptor.NewRedisReplyValue("raw")
	if v.Value().(string) != "raw" {
		t.Fatal("Value() should return underlying value")
	}
}

func TestRedisArrayReplyReader_HasNext(t *testing.T) {
	r := goscriptor.NewRedisArrayReplyReader([]any{"a"})
	if !r.HasNext() {
		t.Fatal("expected HasNext true")
	}
	r.ReadString()
	if r.HasNext() {
		t.Fatal("expected HasNext false after consuming all")
	}
}

func TestRedisArrayReplyReader_ReadInt32Int64(t *testing.T) {
	r := goscriptor.NewRedisArrayReplyReader([]any{"10", "20"})

	v32, err := r.ReadInt32(0)
	if err != nil {
		t.Fatal(err)
	}
	if v32 != 10 {
		t.Fatalf("expected 10, got %d", v32)
	}

	v64, err := r.ReadInt64(0)
	if err != nil {
		t.Fatal(err)
	}
	if v64 != 20 {
		t.Fatalf("expected 20, got %d", v64)
	}
}

func TestRedisArrayReplyReader_SkipValue(t *testing.T) {
	r := goscriptor.NewRedisArrayReplyReader([]any{"skip", "keep"})
	r.SkipValue()
	got := r.ReadString()
	if got != "keep" {
		t.Fatalf("expected keep, got %q", got)
	}
}

func TestRedisArrayReplyReader_ForEach(t *testing.T) {
	r := goscriptor.NewRedisArrayReplyReader([]any{"a", "b", "c"})
	var collected []string
	err := r.ForEach(func(i int, v *goscriptor.RedisReplyValue) error {
		collected = append(collected, v.AsString())
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(collected) != 3 || collected[0] != "a" || collected[2] != "c" {
		t.Fatalf("unexpected: %v", collected)
	}
}

func TestRedisArrayReplyReader_ForEach_Error(t *testing.T) {
	r := goscriptor.NewRedisArrayReplyReader([]any{"a", "b"})
	err := r.ForEach(func(i int, v *goscriptor.RedisReplyValue) error {
		if i == 1 {
			return goscriptor.ErrScriptNotFound
		}
		return nil
	})
	if err == nil {
		t.Fatal("expected error from ForEach callback")
	}
}

// TestRedisReplyValue_AsInt64_HighPrecision verifies that integer strings above 2^53
// are parsed exactly (not via float64 which would lose precision).
func TestRedisReplyValue_AsInt64_HighPrecision(t *testing.T) {
	// 9007199254740995 == 2^53 + 3; float64 cannot represent it exactly.
	const bigStr = "9007199254740995"
	const want int64 = 9007199254740995
	v := goscriptor.NewRedisReplyValue(bigStr)
	got, err := v.AsInt64(0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Fatalf("expected %d, got %d", want, got)
	}
}

// TestRedisReplyValue_AsInt32_HighPrecision verifies that an integer beyond float32
// precision is still parsed exactly.
func TestRedisReplyValue_AsInt32_HighPrecision(t *testing.T) {
	// 16777217 == 2^24 + 1; float32 cannot represent it exactly.
	const input = "16777217"
	const want int32 = 16777217
	v := goscriptor.NewRedisReplyValue(input)
	got, err := v.AsInt32(0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Fatalf("expected %d, got %d", want, got)
	}
}

// TestRedisReplyValue_AsString_Float verifies float64 formatting in AsString.
func TestRedisReplyValue_AsString_Float(t *testing.T) {
	v := goscriptor.NewRedisReplyValue(float64(3.14))
	s := v.AsString()
	if s != "3.14" {
		t.Fatalf("expected \"3.14\", got %q", s)
	}

	// Large float must use 'f' notation, not scientific notation.
	v2 := goscriptor.NewRedisReplyValue(float64(1e20))
	s2 := v2.AsString()
	if s2 != "100000000000000000000" {
		t.Fatalf("expected decimal notation, got %q", s2)
	}
}

// TestRedisArrayReplyReader_ForEach_Cursor verifies that ForEach starts from the
// current cursor position (not position 0) after ReadValue has consumed items.
func TestRedisArrayReplyReader_ForEach_Cursor(t *testing.T) {
	r := goscriptor.NewRedisArrayReplyReader([]any{"x", "y", "z"})

	// Consume the first two items.
	r.ReadValue() // index 0
	r.ReadValue() // index 1

	// ForEach should only see the remaining item at absolute index 2.
	var indices []int
	var values []string
	err := r.ForEach(func(i int, v *goscriptor.RedisReplyValue) error {
		indices = append(indices, i)
		values = append(values, v.AsString())
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 {
		t.Fatalf("expected 1 remaining item, got %d: %v", len(values), values)
	}
	if values[0] != "z" {
		t.Fatalf("expected \"z\", got %q", values[0])
	}
	if indices[0] != 2 {
		t.Fatalf("expected absolute index 2, got %d", indices[0])
	}
}

// TestRedisArrayReplyReader_OverRead_Fresh verifies that over-reading returns a fresh
// nil-valued reply that does not affect GetLength.
func TestRedisArrayReplyReader_OverRead_Fresh(t *testing.T) {
	r := goscriptor.NewRedisArrayReplyReader([]any{"only"})
	r.ReadValue() // consume

	v1 := r.ReadValue()
	if !v1.IsNil() {
		t.Fatal("over-read should return IsNil() == true")
	}
	v2 := r.ReadValue()
	if !v2.IsNil() {
		t.Fatal("second over-read should return IsNil() == true")
	}

	// GetLength must still reflect the original array length.
	if r.GetLength() != 1 {
		t.Fatalf("GetLength should remain 1 after over-reads, got %d", r.GetLength())
	}

	// Returned values are fresh (not the exported singleton), but IsNil must hold.
	if v1 == goscriptor.EmptyRedisReplyValue {
		t.Fatal("over-read should return a fresh value, not the shared EmptyRedisReplyValue singleton")
	}
}
