// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build tamago

package runtime_test

import (
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"
)

// waitSignalLimit is the time in which a raised signal must wake its
// goroutine.
const waitSignalLimit = 10 * time.Second

// waitSignal waits for signal sig in a new goroutine and returns a channel
// that is closed when the wait returns.
func waitSignal(sig int) <-chan struct{} {
	woken := make(chan struct{})

	go func() {
		runtime.WaitSignal(sig)
		close(woken)
	}()

	return woken
}

// expectWoken fails t unless woken is closed within waitSignalLimit.
func expectWoken(t *testing.T, woken <-chan struct{}, what string) {
	t.Helper()

	select {
	case <-woken:
	case <-time.After(waitSignalLimit):
		t.Fatalf("%s: the goroutine did not wake within %v", what, waitSignalLimit)
	}
}

// expectAsleep fails t if woken is closed within a short time.
func expectAsleep(t *testing.T, woken <-chan struct{}, what string) {
	t.Helper()

	select {
	case <-woken:
		t.Fatalf("%s: the goroutine woke", what)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestWaitSignal(t *testing.T) {
	const sig = 100

	woken := waitSignal(sig)
	runtime.RaiseSignal(sig + 1)
	expectAsleep(t, woken, "signal of another goroutine")

	runtime.RaiseSignal(sig)
	expectWoken(t, woken, "own signal")

	// The signal raised above with no goroutine waiting is still pending.
	expectWoken(t, waitSignal(sig+1), "pending signal")
}

func TestWaitSignalCountsOnce(t *testing.T) {
	const sig = 102

	runtime.RaiseSignal(sig)
	runtime.RaiseSignal(sig)
	expectWoken(t, waitSignal(sig), "first wait")

	woken := waitSignal(sig)
	expectAsleep(t, woken, "second wait after two raises")

	runtime.RaiseSignal(sig)
	expectWoken(t, woken, "second wait")
}

// TestWaitSignalParallel raises the signals of many goroutines, as the
// interrupts of the queues of a device, and checks that every goroutine
// takes its own signals and no other.
func TestWaitSignalParallel(t *testing.T) {
	const (
		first   = 128
		signals = 64
		rounds  = 1000
	)

	var wg sync.WaitGroup
	done := make([]chan struct{}, signals)

	for i := range signals {
		done[i] = make(chan struct{}, 1)
		wg.Add(1)

		go func() {
			defer wg.Done()

			for range rounds {
				runtime.WaitSignal(first + i)
				done[i] <- struct{}{}
			}
		}()
	}

	for range rounds {
		for i := range signals {
			runtime.RaiseSignal(uint32(first + i))
		}

		for i := range signals {
			select {
			case <-done[i]:
			case <-time.After(waitSignalLimit):
				t.Fatalf("signal %d did not wake its goroutine within %v", first+i, waitSignalLimit)
			}
		}
	}

	wg.Wait()
}

func TestWaitSignalOutOfRange(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("WaitSignal of signal 256 did not panic")
		}
	}()

	runtime.WaitSignal(256)
}

// receiveSignal fails t unless c receives signal sig within waitSignalLimit.
func receiveSignal(t *testing.T, c <-chan os.Signal, sig int) {
	t.Helper()

	select {
	case s := <-c:
		if s != syscall.Signal(sig) {
			t.Fatalf("received %v, want signal %d", s, sig)
		}
	case <-time.After(waitSignalLimit):
		t.Fatalf("signal %d did not reach its channel within %v", sig, waitSignalLimit)
	}
}

// TestSignalNotifyAndWaitSignal checks that the os/signal loop relays the
// signals of Notify while a goroutine takes its own signal with WaitSignal.
func TestSignalNotifyAndWaitSignal(t *testing.T) {
	const (
		notify = 200 // and notify+1
		direct = 210
		rounds = 100
	)

	var c [2]chan os.Signal

	for i := range c {
		c[i] = make(chan os.Signal, 1)
		signal.Notify(c[i], syscall.Signal(notify+i))
		defer signal.Stop(c[i])
	}

	woken := make(chan struct{}, 1)

	go func() {
		for range rounds {
			runtime.WaitSignal(direct)
			woken <- struct{}{}
		}
	}()

	for range rounds {
		runtime.RaiseSignal(direct)
		for i := range c {
			runtime.RaiseSignal(uint32(notify + i))
		}

		for i := range c {
			receiveSignal(t, c[i], notify+i)
		}

		expectWoken(t, woken, "direct signal")
	}
}

// TestSignalNotifyLeavesOtherSignals checks that the os/signal loop takes
// only the signals of Notify, so that a signal raised before the first
// WaitSignal stays pending for it.
func TestSignalNotifyLeavesOtherSignals(t *testing.T) {
	const (
		notify = 220
		other  = 221
	)

	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.Signal(notify))
	defer signal.Stop(c)

	runtime.RaiseSignal(other)
	runtime.RaiseSignal(notify)
	receiveSignal(t, c, notify)

	// The loop has run since the raise of other and left it pending.
	expectWoken(t, waitSignal(other), "signal raised before the first wait")
}

// TestWaitSignalTakesNotifySignal checks that once a goroutine waits for a
// signal with WaitSignal, the os/signal loop no longer receives it.
func TestWaitSignalTakesNotifySignal(t *testing.T) {
	const sig = 230

	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.Signal(sig))
	defer signal.Stop(c)

	woken := waitSignal(sig)

	for deadline := time.Now().Add(waitSignalLimit); !runtime.SignalWaiterParked(sig); {
		if time.Now().After(deadline) {
			t.Fatalf("WaitSignal did not park within %v", waitSignalLimit)
		}
		time.Sleep(time.Millisecond)
	}

	runtime.RaiseSignal(sig)
	expectWoken(t, woken, "signal of WaitSignal and Notify")

	select {
	case s := <-c:
		t.Fatalf("Notify received %v after WaitSignal took the signal", s)
	case <-time.After(100 * time.Millisecond):
	}
}
