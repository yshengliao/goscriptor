package goscriptor

import (
	"strconv"
)

// EmptyRedisReplyValue represents a nil Redis reply value.
var EmptyRedisReplyValue = &RedisReplyValue{value: nil}

// RedisReplyValue wraps a value returned from Redis.
type RedisReplyValue struct {
	value any
}

// NewRedisReplyValue creates a RedisReplyValue instance.
func NewRedisReplyValue(value any) *RedisReplyValue {
	return &RedisReplyValue{
		value: value,
	}
}

// Value returns the underlying value.
func (v *RedisReplyValue) Value() any {
	return v.value
}

// AsInt32 converts the underlying value to an int32, returning a default if parsing fails.
func (v *RedisReplyValue) AsInt32(defaultValue int32) (int32, error) {
	if v.value != nil {
		switch val := v.value.(type) {
		case string:
			r, err := strconv.ParseFloat(val, 32)
			if err != nil {
				return defaultValue, err
			}
			return int32(r), nil
		case int:
			return int32(val), nil
		case int32:
			return val, nil
		case int64:
			return int32(val), nil
		}
	}
	return defaultValue, nil
}

// AsInt64 converts the underlying value to an int64, returning a default if parsing fails.
func (v *RedisReplyValue) AsInt64(defaultValue int64) (int64, error) {
	if v.value != nil {
		switch val := v.value.(type) {
		case string:
			r, err := strconv.ParseFloat(val, 64)
			if err != nil {
				return defaultValue, err
			}
			return int64(r), nil
		case int:
			return int64(val), nil
		case int32:
			return int64(val), nil
		case int64:
			return val, nil
		}
	}
	return defaultValue, nil
}

// AsFloat64 converts the underlying value to a float64, returning a default if parsing fails.
func (v *RedisReplyValue) AsFloat64(defaultValue float64) (float64, error) {
	if v.value != nil {
		switch val := v.value.(type) {
		case string:
			r, err := strconv.ParseFloat(val, 64)
			if err != nil {
				return defaultValue, err
			}
			return r, nil
		case int:
			return float64(val), nil
		case int32:
			return float64(val), nil
		case int64:
			return float64(val), nil
		case float32:
			return float64(val), nil
		case float64:
			return val, nil
		}
	}
	return defaultValue, nil
}

// AsString converts the underlying value to a string.
func (v *RedisReplyValue) AsString() string {
	if v.value != nil {
		switch val := v.value.(type) {
		case string:
			return val
		case int:
			return strconv.FormatInt(int64(val), 10)
		case int32:
			return strconv.FormatInt(int64(val), 10)
		case int64:
			return strconv.FormatInt(val, 10)
		}
	}
	return ""
}

// IsNil returns true when the underlying value is nil.
func (v *RedisReplyValue) IsNil() bool {
	return v.value == nil
}

// ToArrayReplyReader converts the value to a RedisArrayReplyReader
// when it contains a slice of interfaces.
func (v *RedisReplyValue) ToArrayReplyReader() *RedisArrayReplyReader {
	i, ok := v.value.([]any)
	if ok {
		return NewRedisArrayReplyReader(i)
	}
	return nil
}

// NullableInt returns a pointer to the underlying int64 value, or nil if the value is nil.
func (v *RedisReplyValue) NullableInt() (*int64, error) {
	if v.value == nil {
		return nil, nil
	}
	result, err := v.AsInt64(0)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// NullableString returns a pointer to the underlying string value, or nil if the value is nil.
func (v *RedisReplyValue) NullableString() *string {
	if v.value == nil {
		return nil
	}
	s := v.AsString()
	return &s
}

// RedisArrayReplyReader provides sequential access to an array reply.
type RedisArrayReplyReader struct {
	redisReply []any
	position   uint32
}

// NewRedisArrayReplyReader creates a new reader for the given array reply.
func NewRedisArrayReplyReader(redisReply []any) *RedisArrayReplyReader {
	return &RedisArrayReplyReader{
		redisReply: redisReply,
		position:   0,
	}
}

// GetLength returns the total number of items in the array reply.
func (r *RedisArrayReplyReader) GetLength() int {
	return len(r.redisReply)
}

// HasNext returns true if there are more items to read.
func (r *RedisArrayReplyReader) HasNext() bool {
	values := r.redisReply
	pos := r.position
	return pos < uint32(len(values))
}

// ReadArray reads the next value as a nested array reply reader.
func (r *RedisArrayReplyReader) ReadArray() *RedisArrayReplyReader {
	val := r.ReadValue().value
	arr, ok := val.([]any)
	if !ok {
		return nil
	}
	return NewRedisArrayReplyReader(arr)
}

// ReadString reads the next value and converts it to a string.
func (r *RedisArrayReplyReader) ReadString() string {
	return r.ReadValue().AsString()
}

// ReadInt32 reads the next value and converts it to an int32.
func (r *RedisArrayReplyReader) ReadInt32(defaultValue int32) (int32, error) {
	return r.ReadValue().AsInt32(defaultValue)
}

// ReadInt64 reads the next value and converts it to an int64.
func (r *RedisArrayReplyReader) ReadInt64(defaultValue int64) (int64, error) {
	return r.ReadValue().AsInt64(defaultValue)
}

// ReadFloat64 reads the next value and converts it to a float64.
func (r *RedisArrayReplyReader) ReadFloat64(defaultValue float64) (float64, error) {
	return r.ReadValue().AsFloat64(defaultValue)
}

// SkipValue skips over the next value in the array.
func (r *RedisArrayReplyReader) SkipValue() {
	r.ReadValue()
}

// ReadValue reads the next value as a RedisReplyValue.
func (r *RedisArrayReplyReader) ReadValue() *RedisReplyValue {
	values := r.redisReply
	pos := r.position
	r.position++
	if pos < uint32(len(values)) {
		return &RedisReplyValue{value: values[pos]}
	}
	return EmptyRedisReplyValue
}

// ForEach iterates through the remaining items, executing the action function for each item.
func (r *RedisArrayReplyReader) ForEach(action func(i int, v *RedisReplyValue) error) error {
	for i, v := range r.redisReply {
		err := action(i, &RedisReplyValue{value: v})
		if err != nil {
			return err
		}
	}
	return nil
}
