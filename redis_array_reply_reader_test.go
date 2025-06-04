package goscriptor_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yshengliao/goscriptor"
)

func TestRedisArrayReplyReader_Basic(t *testing.T) {
	assert := assert.New(t)

	reply := []interface{}{"1", "two", int64(3), []interface{}{"nested1", "nested2"}}
	r := goscriptor.NewRedisArrayReplyReader(reply)

	assert.Equal(4, r.GetLength())
	assert.True(r.HasNext())

	v, err := r.ReadInt32(0)
	assert.Nil(err)
	assert.Equal(int32(1), v)

	assert.True(r.HasNext())
	s := r.ReadString()
	assert.Equal("two", s)

	i64, err := r.ReadInt64(0)
	assert.Nil(err)
	assert.Equal(int64(3), i64)

	assert.True(r.HasNext())
	nested := r.ReadArray()
	assert.NotNil(nested)
	assert.Equal(2, nested.GetLength())

	assert.True(nested.HasNext())
	assert.Equal("nested1", nested.ReadString())
	nested.SkipValue()
	assert.False(nested.HasNext())
	assert.Equal("", nested.ReadString())

	assert.False(r.HasNext())
	assert.Equal("", r.ReadString())
	dv, err := r.ReadInt32(42)
	assert.Nil(err)
	assert.Equal(int32(42), dv)
}

func TestRedisArrayReplyReader_ForEach(t *testing.T) {
	assert := assert.New(t)

	arr := []interface{}{"a", "b", "c"}
	r := goscriptor.NewRedisArrayReplyReader(arr)

	collected := []string{}
	err := r.ForEach(func(i int, v *goscriptor.RedisReplyValue) error {
		collected = append(collected, v.AsString())
		return nil
	})
	assert.Nil(err)
	assert.Equal([]string{"a", "b", "c"}, collected)

	r2 := goscriptor.NewRedisArrayReplyReader(arr)
	count := 0
	err = r2.ForEach(func(i int, v *goscriptor.RedisReplyValue) error {
		count++
		if i == 1 {
			return errors.New("stop")
		}
		return nil
	})
	assert.NotNil(err)
	assert.Equal(2, count)
}
