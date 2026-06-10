package redis

import (
	"context"
	"fmt"
	"time"
)

// --- String commands ---

// Get returns the value of key, or ("", nil) if the key does not exist.
//
// WARNING: a missing key and a key holding an empty string are indistinguishable
// — both return ("", nil). Use Exists to tell them apart when the distinction matters.
func (c *Client) Get(ctx context.Context, key string) (string, error) {
	reply, err := c.Do(ctx, "GET", key)
	if err != nil {
		return "", err
	}
	if reply == nil {
		return "", nil
	}
	s, ok := reply.(string)
	if !ok {
		return "", fmt.Errorf("redis: unexpected type %T from GET", reply)
	}
	return s, nil
}

// Set sets key to value. If ttl > 0, sets an expiry using PX (millisecond precision).
//
// Sub-millisecond TTLs are floored to 1 ms to avoid sending "PX 0", which Redis
// rejects. If you need sub-millisecond precision, consider SET with EXAT/PXAT.
func (c *Client) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	if ttl > 0 {
		ms := ttl.Milliseconds()
		if ms <= 0 {
			ms = 1
		}
		_, err := c.Do(ctx, "SET", key, value, "PX", ms)
		return err
	}
	_, err := c.Do(ctx, "SET", key, value)
	return err
}

// Del deletes one or more keys and returns the number of keys removed.
func (c *Client) Del(ctx context.Context, keys ...string) (int64, error) {
	args := make([]any, 0, 1+len(keys))
	args = append(args, "DEL")
	for _, k := range keys {
		args = append(args, k)
	}
	reply, err := c.Do(ctx, args...)
	if err != nil {
		return 0, err
	}
	n, ok := reply.(int64)
	if !ok {
		return 0, fmt.Errorf("redis: unexpected type %T from DEL", reply)
	}
	return n, nil
}

// Exists returns the number of specified keys that exist.
func (c *Client) Exists(ctx context.Context, keys ...string) (int64, error) {
	args := make([]any, 0, 1+len(keys))
	args = append(args, "EXISTS")
	for _, k := range keys {
		args = append(args, k)
	}
	reply, err := c.Do(ctx, args...)
	if err != nil {
		return 0, err
	}
	n, ok := reply.(int64)
	if !ok {
		return 0, fmt.Errorf("redis: unexpected type %T from EXISTS", reply)
	}
	return n, nil
}

// Incr increments the integer value of key by 1.
func (c *Client) Incr(ctx context.Context, key string) (int64, error) {
	reply, err := c.Do(ctx, "INCR", key)
	if err != nil {
		return 0, err
	}
	n, ok := reply.(int64)
	if !ok {
		return 0, fmt.Errorf("redis: unexpected type %T from INCR", reply)
	}
	return n, nil
}

// IncrBy increments the integer value of key by delta.
func (c *Client) IncrBy(ctx context.Context, key string, delta int64) (int64, error) {
	reply, err := c.Do(ctx, "INCRBY", key, delta)
	if err != nil {
		return 0, err
	}
	n, ok := reply.(int64)
	if !ok {
		return 0, fmt.Errorf("redis: unexpected type %T from INCRBY", reply)
	}
	return n, nil
}

// --- Key commands ---

// Expire sets a timeout on key. Returns true if the timeout was set.
//
// Because EXPIRE accepts whole seconds, sub-second TTLs are rounded UP to 1 second
// to avoid accidental data loss: passing ttl < 1s would otherwise truncate to 0,
// and "EXPIRE key 0" immediately deletes the key.
//
// If ttl <= 0 the value is passed through unchanged, which causes Redis to delete
// the key immediately — this is intentional (matches plain EXPIRE semantics) but
// callers should be aware.
func (c *Client) Expire(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	var secs int64
	if ttl > 0 {
		// Round up to avoid truncating sub-second TTLs to 0 (which deletes the key).
		secs = int64((ttl + time.Second - 1) / time.Second)
	} else {
		secs = int64(ttl.Seconds())
	}
	reply, err := c.Do(ctx, "EXPIRE", key, secs)
	if err != nil {
		return false, err
	}
	n, ok := reply.(int64)
	if !ok {
		return false, fmt.Errorf("redis: unexpected type %T from EXPIRE", reply)
	}
	return n == 1, nil
}

// TTL returns the remaining time to live of a key in seconds.
// Returns -1 if the key exists but has no expiry, -2 if the key does not exist.
func (c *Client) TTL(ctx context.Context, key string) (int64, error) {
	reply, err := c.Do(ctx, "TTL", key)
	if err != nil {
		return 0, err
	}
	n, ok := reply.(int64)
	if !ok {
		return 0, fmt.Errorf("redis: unexpected type %T from TTL", reply)
	}
	return n, nil
}

// --- Hash commands ---

// HSet sets a hash field.
func (c *Client) HSet(ctx context.Context, key, field, value string) error {
	_, err := c.Do(ctx, "HSET", key, field, value)
	return err
}

// HGet returns the value of field in hash key, or ("", nil) if the key or field does not exist.
//
// WARNING: a missing field and a field holding an empty string are indistinguishable
// — both return ("", nil). Use HExists to tell them apart when the distinction matters.
func (c *Client) HGet(ctx context.Context, key, field string) (string, error) {
	reply, err := c.Do(ctx, "HGET", key, field)
	if err != nil {
		return "", err
	}
	if reply == nil {
		return "", nil
	}
	s, ok := reply.(string)
	if !ok {
		return "", fmt.Errorf("redis: unexpected type %T from HGET", reply)
	}
	return s, nil
}

// HGetAll returns all field-value pairs in hash key.
func (c *Client) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	reply, err := c.Do(ctx, "HGETALL", key)
	if err != nil {
		return nil, err
	}
	arr, ok := reply.([]any)
	if !ok {
		return nil, fmt.Errorf("redis: unexpected type %T from HGETALL", reply)
	}
	if len(arr)%2 != 0 {
		return nil, fmt.Errorf("redis: HGETALL returned odd number of elements (%d)", len(arr))
	}
	m := make(map[string]string, len(arr)/2)
	for i := 0; i < len(arr); i += 2 {
		k, _ := arr[i].(string)
		v, _ := arr[i+1].(string)
		m[k] = v
	}
	return m, nil
}

// HDel deletes one or more hash fields.
func (c *Client) HDel(ctx context.Context, key string, fields ...string) (int64, error) {
	args := make([]any, 0, 2+len(fields))
	args = append(args, "HDEL", key)
	for _, f := range fields {
		args = append(args, f)
	}
	reply, err := c.Do(ctx, args...)
	if err != nil {
		return 0, err
	}
	n, ok := reply.(int64)
	if !ok {
		return 0, fmt.Errorf("redis: unexpected type %T from HDEL", reply)
	}
	return n, nil
}

// HExists returns whether field exists in hash key.
func (c *Client) HExists(ctx context.Context, key, field string) (bool, error) {
	reply, err := c.Do(ctx, "HEXISTS", key, field)
	if err != nil {
		return false, err
	}
	n, ok := reply.(int64)
	if !ok {
		return false, fmt.Errorf("redis: unexpected type %T from HEXISTS", reply)
	}
	return n == 1, nil
}

// --- List commands ---

// LPush prepends values to a list and returns the list length.
func (c *Client) LPush(ctx context.Context, key string, values ...string) (int64, error) {
	args := make([]any, 0, 2+len(values))
	args = append(args, "LPUSH", key)
	for _, v := range values {
		args = append(args, v)
	}
	reply, err := c.Do(ctx, args...)
	if err != nil {
		return 0, err
	}
	n, ok := reply.(int64)
	if !ok {
		return 0, fmt.Errorf("redis: unexpected type %T from LPUSH", reply)
	}
	return n, nil
}

// RPush appends values to a list and returns the list length.
func (c *Client) RPush(ctx context.Context, key string, values ...string) (int64, error) {
	args := make([]any, 0, 2+len(values))
	args = append(args, "RPUSH", key)
	for _, v := range values {
		args = append(args, v)
	}
	reply, err := c.Do(ctx, args...)
	if err != nil {
		return 0, err
	}
	n, ok := reply.(int64)
	if !ok {
		return 0, fmt.Errorf("redis: unexpected type %T from RPUSH", reply)
	}
	return n, nil
}

// LPop removes and returns the first element of a list, or ("", nil) if the list is empty or the key does not exist.
//
// WARNING: a missing/empty list and a list whose first element is an empty string are
// indistinguishable — both return ("", nil). Use Exists or LLen when the distinction matters.
func (c *Client) LPop(ctx context.Context, key string) (string, error) {
	reply, err := c.Do(ctx, "LPOP", key)
	if err != nil {
		return "", err
	}
	if reply == nil {
		return "", nil
	}
	s, ok := reply.(string)
	if !ok {
		return "", fmt.Errorf("redis: unexpected type %T from LPOP", reply)
	}
	return s, nil
}

// RPop removes and returns the last element of a list, or ("", nil) if the list is empty or the key does not exist.
//
// WARNING: a missing/empty list and a list whose last element is an empty string are
// indistinguishable — both return ("", nil). Use Exists or LLen when the distinction matters.
func (c *Client) RPop(ctx context.Context, key string) (string, error) {
	reply, err := c.Do(ctx, "RPOP", key)
	if err != nil {
		return "", err
	}
	if reply == nil {
		return "", nil
	}
	s, ok := reply.(string)
	if !ok {
		return "", fmt.Errorf("redis: unexpected type %T from RPOP", reply)
	}
	return s, nil
}

// LLen returns the length of a list.
func (c *Client) LLen(ctx context.Context, key string) (int64, error) {
	reply, err := c.Do(ctx, "LLEN", key)
	if err != nil {
		return 0, err
	}
	n, ok := reply.(int64)
	if !ok {
		return 0, fmt.Errorf("redis: unexpected type %T from LLEN", reply)
	}
	return n, nil
}

// LRange returns elements from a list within the given range.
func (c *Client) LRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	reply, err := c.Do(ctx, "LRANGE", key, start, stop)
	if err != nil {
		return nil, err
	}
	arr, ok := reply.([]any)
	if !ok {
		return nil, fmt.Errorf("redis: unexpected type %T from LRANGE", reply)
	}
	result := make([]string, len(arr))
	for i, v := range arr {
		result[i], _ = v.(string)
	}
	return result, nil
}

// --- Set commands ---

// SAdd adds members to a set and returns the number of new members added.
func (c *Client) SAdd(ctx context.Context, key string, members ...string) (int64, error) {
	args := make([]any, 0, 2+len(members))
	args = append(args, "SADD", key)
	for _, m := range members {
		args = append(args, m)
	}
	reply, err := c.Do(ctx, args...)
	if err != nil {
		return 0, err
	}
	n, ok := reply.(int64)
	if !ok {
		return 0, fmt.Errorf("redis: unexpected type %T from SADD", reply)
	}
	return n, nil
}

// SMembers returns all members of a set.
func (c *Client) SMembers(ctx context.Context, key string) ([]string, error) {
	reply, err := c.Do(ctx, "SMEMBERS", key)
	if err != nil {
		return nil, err
	}
	arr, ok := reply.([]any)
	if !ok {
		return nil, fmt.Errorf("redis: unexpected type %T from SMEMBERS", reply)
	}
	result := make([]string, len(arr))
	for i, v := range arr {
		result[i], _ = v.(string)
	}
	return result, nil
}

// SRem removes members from a set and returns the number removed.
func (c *Client) SRem(ctx context.Context, key string, members ...string) (int64, error) {
	args := make([]any, 0, 2+len(members))
	args = append(args, "SREM", key)
	for _, m := range members {
		args = append(args, m)
	}
	reply, err := c.Do(ctx, args...)
	if err != nil {
		return 0, err
	}
	n, ok := reply.(int64)
	if !ok {
		return 0, fmt.Errorf("redis: unexpected type %T from SREM", reply)
	}
	return n, nil
}

// SIsMember returns whether member belongs to the set.
func (c *Client) SIsMember(ctx context.Context, key, member string) (bool, error) {
	reply, err := c.Do(ctx, "SISMEMBER", key, member)
	if err != nil {
		return false, err
	}
	n, ok := reply.(int64)
	if !ok {
		return false, fmt.Errorf("redis: unexpected type %T from SISMEMBER", reply)
	}
	return n == 1, nil
}

// SCard returns the number of members in a set.
func (c *Client) SCard(ctx context.Context, key string) (int64, error) {
	reply, err := c.Do(ctx, "SCARD", key)
	if err != nil {
		return 0, err
	}
	n, ok := reply.(int64)
	if !ok {
		return 0, fmt.Errorf("redis: unexpected type %T from SCARD", reply)
	}
	return n, nil
}
