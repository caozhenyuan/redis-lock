package redis_lock

import (
	"context"
	_ "embed"
	"errors"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"
	"time"
)

var (
	ErrLockNotHold        = errors.New("未持有锁")
	ErrFiledToPreemptLock = errors.New("加锁失败")
	//go:embed unlock.lua
	luaUnlock string
	//go:embed refresh.lua
	luaRefresh string
	//go:embed lock.lua
	luaLock string
)

type Client struct {
	client redis.Cmdable
	s      singleflight.Group
}

func NewClient(c redis.Cmdable) *Client {
	return &Client{
		client: c,
	}
}

// golang防缓存击穿神器
func (c *Client) SingleFlightLock(ctx context.Context, key string, expiration time.Duration, retry RetryStrategy, timeout time.Duration) (*Lock, error) {
	for {
		//标记是不是自己拿到了锁
		flag := false
		resCh := c.s.DoChan(key, func() (interface{}, error) {
			flag = true
			return c.Lock(ctx, key, expiration, retry, timeout)
		})
		select {
		case res := <-resCh:
			//确实是自己拿到了锁
			if flag {
				if res.Err != nil {
					return nil, res.Err
				}
				return res.Val.(*Lock), nil
			}
		case <-ctx.Done():
			//监听超时
			return nil, ctx.Err()
		}
	}
}

func (c *Client) Lock(ctx context.Context, key string, expiration time.Duration, retry RetryStrategy, timeout time.Duration) (*Lock, error) {
	value := uuid.New().String()
	var timer *time.Timer
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	for {
		lctx, cancel := context.WithTimeout(ctx, timeout)
		res, err := c.client.Eval(lctx, luaLock, []string{key}, value, expiration).Bool()
		cancel()
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			// 非超时错误，那么基本上代表遇到了一些不可挽回的场景，所以没必要重试
			// 比如Redis server崩了，或者EOF了
			return nil, err
		}
		if res {
			return NewLock(c.client, key, value, expiration), nil
		}
		interval, ok := retry.Next()
		if !ok {
			//不用重试
			return nil, ErrFiledToPreemptLock
		}
		if timer == nil {
			timer = time.NewTimer(interval)
		}
		timer.Reset(interval)
		select {
		case <-ctx.Done(): //整个过程超时
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func (c *Client) TryLock(ctx context.Context, key string, expiration time.Duration) (*Lock, error) {
	value := uuid.New().String()
	res, err := c.client.SetNX(ctx, key, value, expiration).Result()
	if err != nil {
		return nil, err
	}
	if !res {
		return nil, ErrFiledToPreemptLock
	}
	return NewLock(c.client, key, value, expiration), nil
}

type Lock struct {
	client     redis.Cmdable
	key        string
	value      string
	expiration time.Duration
	unlock     chan struct{}
}

func NewLock(client redis.Cmdable, key string, value string, expiration time.Duration) *Lock {
	return &Lock{client: client, key: key, value: value, expiration: expiration,
		unlock: make(chan struct{}, 1)}
}

// AutoRefresh interval 间隔多久 timeout 每次续约调用的超时
func (l *Lock) AutoRefresh(interval time.Duration, timeout time.Duration) error {
	ticker := time.NewTicker(interval)
	//刷新超时的channel
	ch := make(chan struct{}, 1)
	defer close(ch)
	for {
		select {
		case <-ch:
			ctx, cannel := context.WithTimeout(context.Background(), timeout)
			err := l.Refresh(ctx)
			cannel()
			if errors.Is(err, context.DeadlineExceeded) {
				// 超时错误，立即重试
				ch <- struct{}{}
				continue
			}
			if err != nil {
				return err
			}
		case <-ticker.C:
			ctx, cannel := context.WithTimeout(context.Background(), timeout)
			err := l.Refresh(ctx)
			cannel()
			if errors.Is(err, context.DeadlineExceeded) {
				// 超时错误，立即重试
				ch <- struct{}{}
				continue
			}
			if err != nil {
				return err
			}
		case <-l.unlock:
			return nil
		}
	}
}

func (l *Lock) Refresh(ctx context.Context) error {
	res, err := l.client.Eval(ctx, luaRefresh, []string{l.key}, l.value, l.expiration.Milliseconds()).Int64()
	if errors.Is(err, redis.Nil) {
		return ErrLockNotHold
	}
	if err != nil {
		return err
	}
	if res != 1 {
		return ErrLockNotHold
	}
	return nil
}

func (l *Lock) Unlock(ctx context.Context) error {
	defer func() {
		l.unlock <- struct{}{}
		//关闭通道，防止多次调用
		close(l.unlock)
	}()
	res, err := l.client.Eval(ctx, luaUnlock, []string{l.key}, l.value).Int64()
	if errors.Is(err, redis.Nil) {
		return ErrLockNotHold
	}
	if err != nil {
		return err
	}
	if res == 0 {
		//这把锁不是你的，或者这把锁不存在
		return ErrLockNotHold
	}
	return nil
}
