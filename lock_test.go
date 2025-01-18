package redis_lock

import (
	"context"
	"errors"
	"github.com/golang/mock/gomock"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"redis-lock/mocks"
	"testing"
	"time"
)

func TestClient_TryLock(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	testCases := []struct {
		//名字：表达该测试用例测试的是什么场景
		name string
		//测试的输入，基本和测试目标方法参数保持一致
		key        string
		expiration time.Duration
		//设置mock数据，不需要mock数据的测试不需要这部分
		mock func() redis.Cmdable
		//测试的预期输出，基本和测试目标的方法返回值一致
		wantErr  error
		wantLock *Lock
	}{
		{
			name:       "locked",
			key:        "locked_key",
			expiration: time.Minute * 1,
			mock: func() redis.Cmdable {
				rdb := mocks.NewMockCmdable(ctrl)
				res := redis.NewBoolResult(true, nil)
				rdb.EXPECT().
					SetNX(gomock.Any(), "locked_key", gomock.Any(), time.Minute).
					Return(res)
				return rdb
			},
			wantLock: &Lock{
				key: "locked_key",
			},
		},
		{
			name:       "network_error",
			key:        "network_key",
			expiration: time.Minute * 1,
			mock: func() redis.Cmdable {
				rdb := mocks.NewMockCmdable(ctrl)
				res := redis.NewBoolResult(false, errors.New("network error"))
				rdb.EXPECT().
					SetNX(gomock.Any(), "network_key", gomock.Any(), time.Minute).
					Return(res)
				return rdb
			},
			wantErr: errors.New("network error"),
		},
		{
			name:       "failed_lock_error",
			key:        "failed_lock_key",
			expiration: time.Minute * 1,
			mock: func() redis.Cmdable {
				rdb := mocks.NewMockCmdable(ctrl)
				res := redis.NewBoolResult(false, nil)
				rdb.EXPECT().
					SetNX(gomock.Any(), "failed_lock_key", gomock.Any(), time.Minute).
					Return(res)
				return rdb
			},
			wantErr: ErrFiledToPreemptLock,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := NewClient(tc.mock())
			l, err := c.TryLock(context.Background(), tc.key, tc.expiration)
			assert.Equal(t, tc.wantErr, err)
			if err != nil {
				return
			}
			assert.NotEmpty(t, l.client)
			assert.Equal(t, tc.wantLock.key, l.key)
			assert.NotEmpty(t, l.value)
		})
	}
}
