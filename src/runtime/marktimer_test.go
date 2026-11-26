// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime_test

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"
)

type markTimerNode struct {
	next *markTimerNode
	link *markTimerNode
	pad  [6]uintptr
}

// TestDedicatedMarkWorkerTimers checks that a goroutine that sleeps does
// not wait for the end of a mark phase when its timer is on a P that runs
// a dedicated mark worker and no P is idle.
func TestDedicatedMarkWorkerTimers(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode: needs a large heap and several seconds")
	}
	if runtime.GOARCH == "wasm" {
		t.Skip("skipping on wasm: no threads")
	}
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(4))

	// A live heap of small objects with pointers, so that a mark phase
	// takes long enough to tell a late timer from a normal one.
	const liveBytes = 512 << 20
	const chain = 1024
	live := make([]*markTimerNode, liveBytes/int(unsafe.Sizeof(markTimerNode{}))/chain)
	for i := range live {
		var head *markTimerNode
		for range chain {
			head = &markTimerNode{next: head, link: head}
		}
		live[i] = head
	}
	runtime.GC()

	var stop atomic.Bool
	var spins atomic.Uint64
	var wg sync.WaitGroup
	// Goroutines that keep every P busy.
	for i := range 4 {
		wg.Go(func() {
			x := uint64(i + 1)
			for !stop.Load() {
				for range 100_000 {
					x ^= x << 13
					x ^= x >> 7
					x ^= x << 17
				}
			}
			spins.Add(x & 1)
		})
	}
	// Goroutines that sleep and record the largest lateness.
	const sleep = 50 * time.Millisecond
	var maxLate atomic.Int64
	for range 16 {
		wg.Go(func() {
			for !stop.Load() {
				t0 := time.Now()
				time.Sleep(sleep)
				late := int64(time.Since(t0) - sleep)
				for m := maxLate.Load(); late > m && !maxLate.CompareAndSwap(m, late); m = maxLate.Load() {
				}
			}
		})
	}

	shortest := time.Duration(1<<63 - 1)
	for range 5 {
		t0 := time.Now()
		runtime.GC()
		shortest = min(shortest, time.Since(t0))
	}
	// A sleep that ended with the last mark phase records its lateness.
	time.Sleep(2 * sleep)
	late := time.Duration(maxLate.Load())
	stop.Store(true)
	wg.Wait()
	runtime.KeepAlive(live)

	if shortest < 200*time.Millisecond {
		t.Skipf("the shortest GC cycle took %v, too short to tell a late timer from a normal one", shortest)
	}
	if late > shortest/2 {
		t.Errorf("a sleep of %v woke %v late, the shortest GC cycle took %v", sleep, late, shortest)
	}
	t.Logf("largest lateness %v, shortest GC cycle %v", late, shortest)
}
