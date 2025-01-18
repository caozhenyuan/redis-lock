package redis_lock

import (
	"context"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestClient_TryLock2(t *testing.T) {
	rbd := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "P@ssW0rld",
		DB:       0,
	})
	c := NewClient(rbd)
	c.Wait()
	testCases := []struct {
		//名字：表达该测试用例测试的是什么场景
		name string

		//准备数据
		before func()
		//校验 Redis 数据并且清理数据
		after func()

		//测试的输入，基本和测试目标方法参数保持一致
		key        string
		expiration time.Duration
		//测试的预期输出，基本和测试目标的方法返回值一致
		wantErr  error
		wantLock *Lock
	}{
		{
			name: "locked",
			before: func() {

			},
			after: func() {
				//验证一下，Redis里的数据
				res, err := rbd.Del(context.Background(), "locked-key").Result()
				require.NoError(t, err)
				require.Equal(t, int64(1), res)
			},
			key:        "locked-key",
			expiration: time.Minute,
			wantLock: &Lock{
				key: "locked-key",
			},
		},
		{
			name: "failed to lock",
			before: func() {
				res, err := rbd.SetNX(context.Background(), "failed-key", "123", time.Minute).Result()
				require.NoError(t, err)
				require.Equal(t, true, res)
			},
			after: func() {
				res, err := rbd.Get(context.Background(), "failed-key").Result()
				require.NoError(t, err)
				require.Equal(t, "123", res)
			},
			key:        "failed-key",
			expiration: time.Minute,
			wantLock: &Lock{
				key: "failed-key",
			},
			wantErr: ErrFiledToPreemptLock,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tc.before()
			l, err := c.TryLock(context.Background(), tc.key, tc.expiration)
			assert.Equal(t, tc.wantErr, err)
			if err != nil {
				return
			}
			tc.after()
			assert.NotEmpty(t, l.client)
			assert.Equal(t, tc.wantLock.key, l.key)
			assert.NotEmpty(t, l.value)
		})
	}
}

func (c *Client) Wait() {
	for c.client.Ping(context.Background()) != nil {

	}
}
