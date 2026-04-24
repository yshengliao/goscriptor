package goscriptor_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/yshengliao/goscriptor"
)

func TestRedisArrayReplyReader_Basic(t *testing.T) {
	reply := []any{"1", "two", int64(3), []any{"nested1", "nested2"}}
	r := goscriptor.NewRedisArrayReplyReader(reply)

	if r.GetLength() != 4 {
		t.Fatalf("expected length 4, got %d", r.GetLength())
	}
	if !r.HasNext() {
		t.Fatal("expected HasNext true")
	}

	v, err := r.ReadInt32(0)
	if err != nil {
		t.Fatalf("ReadInt32: %v", err)
	}
	if v != 1 {
		t.Fatalf("expected 1, got %d", v)
	}

	if !r.HasNext() {
		t.Fatal("expected HasNext true")
	}
	s := r.ReadString()
	if s != "two" {
		t.Fatalf("expected 'two', got %q", s)
	}

	i64, err := r.ReadInt64(0)
	if err != nil {
		t.Fatalf("ReadInt64: %v", err)
	}
	if i64 != 3 {
		t.Fatalf("expected 3, got %d", i64)
	}

	if !r.HasNext() {
		t.Fatal("expected HasNext true")
	}
	nested := r.ReadArray()
	if nested == nil {
		t.Fatal("expected non-nil nested reader")
	}
	if nested.GetLength() != 2 {
		t.Fatalf("expected nested length 2, got %d", nested.GetLength())
	}

	if !nested.HasNext() {
		t.Fatal("expected nested HasNext true")
	}
	if ns := nested.ReadString(); ns != "nested1" {
		t.Fatalf("expected 'nested1', got %q", ns)
	}
	nested.SkipValue()
	if nested.HasNext() {
		t.Fatal("expected nested HasNext false after skip")
	}
	if ns := nested.ReadString(); ns != "" {
		t.Fatalf("expected empty string past end, got %q", ns)
	}

	if r.HasNext() {
		t.Fatal("expected HasNext false")
	}
	if s := r.ReadString(); s != "" {
		t.Fatalf("expected empty string past end, got %q", s)
	}
	dv, err := r.ReadInt32(42)
	if err != nil {
		t.Fatalf("ReadInt32 past end: %v", err)
	}
	if dv != 42 {
		t.Fatalf("expected default 42, got %d", dv)
	}
}

func TestRedisArrayReplyReader_ForEach(t *testing.T) {
	arr := []any{"a", "b", "c"}
	r := goscriptor.NewRedisArrayReplyReader(arr)

	var collected []string
	err := r.ForEach(func(i int, v *goscriptor.RedisReplyValue) error {
		collected = append(collected, v.AsString())
		return nil
	})
	if err != nil {
		t.Fatalf("ForEach: %v", err)
	}
	expected := []string{"a", "b", "c"}
	if !reflect.DeepEqual(collected, expected) {
		t.Fatalf("expected %v, got %v", expected, collected)
	}

	r2 := goscriptor.NewRedisArrayReplyReader(arr)
	count := 0
	err = r2.ForEach(func(i int, v *goscriptor.RedisReplyValue) error {
		count++
		if i == 1 {
			return errors.New("stop")
		}
		return nil
	})
	if err == nil {
		t.Fatal("expected error from ForEach")
	}
	if count != 2 {
		t.Fatalf("expected count 2, got %d", count)
	}
}
