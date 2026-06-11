package goscriptor

import (
	"math"
	"strconv"
)

// EmptyRedisReplyValue represents a nil Redis reply value.
// It is kept for API compatibility; internal code allocates fresh values on over-read.
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

// AsInt32 converts the underlying value to an int32, returning defaultValue if parsing fails.
//
// For string values, ParseInt is tried first. If the string is not a plain integer (e.g. "3.9"),
// ParseFloat is used as a fallback and the result is truncated toward zero. Values outside the
// int32 range are clipped after the float parse.
func (v *RedisReplyValue) AsInt32(defaultValue int32) (int32, error) {
	if v.value != nil {
		switch val := v.value.(type) {
		case string:
			// Try exact integer parse first (no precision loss).
			if i, err := strconv.ParseInt(val, 10, 32); err == nil {
				return int32(i), nil
			}
			// Fall back to float for strings like "3.9" (truncation toward zero).
			r, err := strconv.ParseFloat(val, 64)
			if err != nil {
				return defaultValue, err
			}
			// Range-check into int32.
			if r > math.MaxInt32 {
				return math.MaxInt32, nil
			}
			if r < math.MinInt32 {
				return math.MinInt32, nil
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

// AsInt64 converts the underlying value to an int64, returning defaultValue if parsing fails.
//
// For string values, ParseInt is tried first (exact, no precision loss above 2^53).
// If the string is not a plain integer (e.g. "3.9"), ParseFloat is used as a fallback
// and the result is truncated toward zero.
func (v *RedisReplyValue) AsInt64(defaultValue int64) (int64, error) {
	if v.value != nil {
		switch val := v.value.(type) {
		case string:
			// Try exact integer parse first (no precision loss).
			if i, err := strconv.ParseInt(val, 10, 64); err == nil {
				return i, nil
			}
			// Fall back to float for strings like "3.9" (truncation toward zero).
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
//
// Supported types: string, int, int32, int64, float32, float64.
// float32 and float64 are formatted with 'f' notation (no scientific notation).
// For all other types (including nil) an empty string is returned.
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
		case float32:
			return strconv.FormatFloat(float64(val), 'f', -1, 32)
		case float64:
			return strconv.FormatFloat(val, 'f', -1, 64)
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
	position   int
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
	return r.position < len(r.redisReply)
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

// ReadValue reads the next value as a RedisReplyValue and advances the cursor.
// If the cursor is already at or past the end, a fresh nil-valued RedisReplyValue
// is returned (IsNil() == true) and the cursor is not moved further.
func (r *RedisArrayReplyReader) ReadValue() *RedisReplyValue {
	pos := r.position
	if pos < len(r.redisReply) {
		r.position++
		return &RedisReplyValue{value: r.redisReply[pos]}
	}
	// Return a fresh value rather than the shared EmptyRedisReplyValue singleton
	// so that callers cannot mutate it and affect others.
	return &RedisReplyValue{value: nil}
}

// ForEach iterates through the REMAINING items starting from the current cursor
// position, executing the action function for each item. The index passed to
// action is the absolute index within the underlying array (not zero-based from
// the start of the ForEach call). After ForEach returns, the cursor is advanced
// past the last consumed item.
func (r *RedisArrayReplyReader) ForEach(action func(i int, v *RedisReplyValue) error) error {
	for r.position < len(r.redisReply) {
		i := r.position
		v := &RedisReplyValue{value: r.redisReply[i]}
		r.position++
		if err := action(i, v); err != nil {
			return err
		}
	}
	return nil
}
