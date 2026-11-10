package usbserial

import (
	"bytes"
	"sync"
	"testing"
	"time"
)

// TestDataPumpMergesBurst 验证洪峰下 dataPump 把多次 feed 合并成少量 serial:data
// 事件，且总字节数、来源与 hex 编码一致性不丢。
func TestDataPumpMergesBurst(t *testing.T) {
	var mu sync.Mutex
	var got []SerialData
	SetEmitter(func(name string, data ...any) {
		if name != "serial:data" {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		got = append(got, data[0].(SerialData))
	})
	defer SetEmitter(nil)

	p := newDataPump()
	for i := 0; i < 20; i++ {
		p.feed("COM9", bytes.Repeat([]byte{0xAB}, 100))
	}
	p.finish()

	mu.Lock()
	defer mu.Unlock()
	total := 0
	for _, e := range got {
		if e.Source != "COM9" {
			t.Fatalf("来源应为 COM9，实得 %s", e.Source)
		}
		if len(e.Hex) != e.Count*2 {
			t.Fatalf("hex 长度应为字节数的 2 倍：%d vs %d", len(e.Hex), e.Count)
		}
		total += e.Count
	}
	if total != 2000 {
		t.Fatalf("合并后总字节应为 2000，实得 %d", total)
	}
	if len(got) >= 20 {
		t.Fatalf("洪峰应被合并：%d 次 feed 产生了 %d 条事件", 20, len(got))
	}
}

// TestDataPumpPeriodicFlush 验证 burst 停住后，窗内数据在 100ms 周期内发出，
// 而不是憋到下一包或 finish 才显示。
func TestDataPumpPeriodicFlush(t *testing.T) {
	ch := make(chan SerialData, 8)
	SetEmitter(func(name string, data ...any) {
		if name == "serial:data" {
			ch <- data[0].(SerialData)
		}
	})
	defer SetEmitter(nil)

	p := newDataPump()
	p.feed("COM9", []byte{1, 2, 3})
	select {
	case e := <-ch:
		if e.Count != 3 {
			t.Fatalf("应收到 3 字节，实得 %d", e.Count)
		}
	case <-time.After(2 * time.Second):
		p.finish()
		t.Fatal("周期 flush 应在 100ms 量级发出，而非等 finish")
	}
	p.finish()
}

// TestDataPumpMultiSource 验证多来源（UDP 多发送方场景）按来源分流合并，互不混包。
func TestDataPumpMultiSource(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]int{}
	SetEmitter(func(name string, data ...any) {
		if name != "serial:data" {
			return
		}
		e := data[0].(SerialData)
		mu.Lock()
		defer mu.Unlock()
		seen[e.Source] += e.Count
	})
	defer SetEmitter(nil)

	p := newDataPump()
	p.feed("1.1.1.1:7", bytes.Repeat([]byte{0xAA}, 10))
	p.feed("2.2.2.2:7", bytes.Repeat([]byte{0xBB}, 30))
	p.finish()

	mu.Lock()
	defer mu.Unlock()
	if seen["1.1.1.1:7"] != 10 || seen["2.2.2.2:7"] != 30 {
		t.Fatalf("来源分流合并丢数据：%v", seen)
	}
}
