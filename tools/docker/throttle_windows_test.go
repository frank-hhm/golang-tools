//go:build windows

package docker

// ============================================================
// 引擎访问闸门的回归测试：并发合并、缓存命中、失败回落旧值、熔断期免请求。
// 这些行为是「起容器时多个面板同时轮询把 CPU 打满」这个故障的护栏，
// 任何一条退化都会让请求重新成倍压到 daemon 上。
// ============================================================

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestMemoReadMergesConcurrentCalls(t *testing.T) {
	apiInvalidate()
	apiHeal()

	var calls int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := memoRead("test:merge", time.Second, func() (string, error) {
				atomic.AddInt32(&calls, 1)
				time.Sleep(20 * time.Millisecond)
				return "v", nil
			})
			if err != nil || v != "v" {
				t.Errorf("返回值错误: %q %v", v, err)
			}
		}()
	}
	wg.Wait()
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Fatalf("并发同一 key 应合并成一次请求, 实际 %d 次", n)
	}

	// TTL 之内再读一次：仍然命中缓存
	if _, err := memoRead("test:merge", time.Second, func() (string, error) {
		atomic.AddInt32(&calls, 1)
		return "v2", nil
	}); err != nil {
		t.Fatal(err)
	}
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Fatalf("TTL 内应命中缓存, 实际又请求了 %d 次", n)
	}

	// 写操作（apiInvalidate）之后必须重新取
	apiInvalidate()
	if _, err := memoRead("test:merge", time.Second, func() (string, error) {
		atomic.AddInt32(&calls, 1)
		return "v3", nil
	}); err != nil {
		t.Fatal(err)
	}
	if n := atomic.LoadInt32(&calls); n != 2 {
		t.Fatalf("写操作后缓存应作废, 实际请求 %d 次", n)
	}
}

func TestMemoReadFallsBackToStaleValue(t *testing.T) {
	apiInvalidate()
	apiHeal()

	if _, err := memoRead("test:stale", time.Millisecond, func() (string, error) { return "old", nil }); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	// 请求失败但手里有旧值：静默返回旧值，面板不该被一次抖动刷成红字
	v, err := memoRead("test:stale", time.Millisecond, func() (string, error) {
		return "", errors.New("daemon 挂了")
	})
	if err != nil || v != "old" {
		t.Fatalf("应回落旧值, 实际 %q %v", v, err)
	}
}

func TestMemoReadFailsFastWhileTripped(t *testing.T) {
	apiInvalidate()
	apiTrip(errors.New("等待响应头超时"))
	defer apiHeal()

	var calls int32
	_, err := memoRead("test:down", time.Millisecond, func() (int, error) {
		atomic.AddInt32(&calls, 1)
		return 1, nil
	})
	if err == nil {
		t.Fatal("熔断期内应直接报错, 不该再发请求")
	}
	if n := atomic.LoadInt32(&calls); n != 0 {
		t.Fatalf("熔断期内不应触达 daemon, 实际请求 %d 次", n)
	}
	if _, msg := apiDown(); msg == "" {
		t.Fatal("熔断原因应被记录，便于面板给出可读提示")
	}
}
