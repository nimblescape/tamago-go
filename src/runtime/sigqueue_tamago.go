// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build tamago

// Signal delivery for GOOS=tamago.
//
// Signals are raised through os/signal.Relay, which is suitable for
// invocation from bare metal interrupt/exception handlers. Relay (see
// sigRelay in sys_tamago_$GOARCH.s) only sets the signal bit in sigPending
// with an atomic operation, without allocation, locking or runtime use.
//
// The os/signal loop receives signals through signal_recv, which parks its
// goroutine whenever no signal is pending. The scheduler (see sigReady)
// makes it runnable again once a signal becomes pending. The loop receives
// only the signals enabled with signal.Notify (see signal_enable).
//
// A goroutine can instead receive one signal directly with
// os/signal.WaitSignal (see signal_wait), which parks it until the signal
// is pending. The scheduler makes it runnable on the processor that handles
// the signal first, without the os/signal loop. So a device with one
// interrupt for each of its queues, such as a network card with a receive
// queue for each processor, can serve each queue with its own goroutine,
// while all other signals keep the single os/signal loop. Once a goroutine
// has waited for a signal with WaitSignal, the os/signal loop no longer
// receives it.
//
// As signals are held in sigPending, a signal raised at any time, and in
// particular while the receiver is not parked, is never lost.

package runtime

import (
	"internal/runtime/atomic"
	"internal/runtime/sys"
	"unsafe"
)

const numSig = 256

var (
	// sigPending holds pending signals
	sigPending [numSig / 32]uint32

	// sigWaiter holds the *g parked in signal_recv
	sigWaiter atomic.Uintptr

	// sigRecv holds signals taken by signal_recv but not yet returned
	sigRecv [numSig / 32]uint32

	// sigEnabled holds the signals enabled with signal.Notify
	sigEnabled [numSig / 32]uint32

	// sigDirect holds the signals received with WaitSignal
	sigDirect [numSig / 32]uint32

	// sigParked holds the signals whose goroutine is parked in signal_wait
	sigParked [numSig / 32]uint32

	// sigDirectWaiter holds the *g parked in signal_wait for each signal
	sigDirectWaiter [numSig]atomic.Uintptr
)

// sigRelay sets signal in sigPending, defined in sys_tamago_$GOARCH.s
func sigRelay(sig uint32)

// sigLoopMask returns the signals of sigPending[i] that the os/signal loop
// receives: the signals enabled with signal.Notify, except the signals
// received with WaitSignal.
//
//go:nosplit
func sigLoopMask(i int) uint32 {
	return atomic.Load(&sigEnabled[i]) &^ atomic.Load(&sigDirect[i])
}

// signal_recv returns the next pending signal, blocking until one is
// available.
//
//go:linkname signal_recv os/signal.signal_recv
func signal_recv() uint32 {
	for {
		for i := range sigRecv {
			if v := sigRecv[i]; v != 0 {
				n := uint32(sys.TrailingZeros32(v))
				sigRecv[i] &^= 1 << n
				return uint32(i)*32 + n
			}
		}

		received := false

		for i := range sigPending {
			mask := sigLoopMask(i)

			if mask == 0 {
				continue
			}

			// take only the signals of the loop, the others stay
			// pending for their own goroutine
			if v := atomic.And32(&sigPending[i], ^mask) & mask; v != 0 {
				sigRecv[i] |= v
				received = true
			}
		}

		if !received {
			gopark(sigParkCommit, nil, waitReasonIOWait, traceBlockGeneric, 1)
		}
	}
}

// sigAnyPending reports whether a signal of the os/signal loop is pending.
//
//go:nosplit
func sigAnyPending() bool {
	for i := range sigPending {
		if atomic.Load(&sigPending[i])&sigLoopMask(i) != 0 {
			return true
		}
	}

	return false
}

func sigParkCommit(gp *g, _ unsafe.Pointer) bool {
	p := uintptr(unsafe.Pointer(gp))
	sigWaiter.Store(p)

	if sigAnyPending() {
		// A signal was raised while parking, abort unless sigReady
		// already took gp (in which case it is being made runnable).
		return !sigWaiter.CompareAndSwap(p, 0)
	}

	return true
}

// signal_wait blocks the calling goroutine until signal sig is pending,
// and clears it.
//
//go:linkname signal_wait os/signal.signal_wait
func signal_wait(sig uint32) {
	if sig >= numSig {
		panic(plainError("WaitSignal: signal out of range"))
	}

	i, bit := sig/32, uint32(1)<<(sig%32)

	// from now on the os/signal loop no longer receives the signal
	if atomic.Load(&sigDirect[i])&bit == 0 {
		atomic.Or(&sigDirect[i], bit)
	}

	for atomic.And32(&sigPending[i], ^bit)&bit == 0 {
		gopark(sigWaitParkCommit, unsafe.Pointer(&sigDirectWaiter[sig]), waitReasonIOWait, traceBlockGeneric, 1)
	}
}

// sigWaitParkCommit records gp as the goroutine parked in signal_wait for the
// signal whose waiter slot is w, unless the signal was raised while
// parking.
func sigWaitParkCommit(gp *g, w unsafe.Pointer) bool {
	waiter := (*atomic.Uintptr)(w)
	sig := (uintptr(w) - uintptr(unsafe.Pointer(&sigDirectWaiter[0]))) / unsafe.Sizeof(sigDirectWaiter[0])
	i, bit := sig/32, uint32(1)<<(sig%32)
	p := uintptr(unsafe.Pointer(gp))

	if !waiter.CompareAndSwap(0, p) {
		throw("WaitSignal: two goroutines wait for one signal")
	}

	atomic.Or(&sigParked[i], bit)

	if atomic.Load(&sigPending[i])&bit != 0 {
		// The signal was raised while parking, abort unless sigReady
		// already took gp (in which case it is being made runnable).
		if waiter.CompareAndSwap(p, 0) {
			atomic.And(&sigParked[i], ^bit)
			return false
		}
	}

	return true
}

// sigReady returns a parked signal receiver, in _Gwaiting state, whose
// signal is pending, the caller must make it runnable. A goroutine parked
// in signal_wait comes before the os/signal loop.
//
//go:nosplit
func sigReady() *g {
	for i := range sigParked {
		ready := atomic.Load(&sigPending[i]) & atomic.Load(&sigParked[i])

		for ready != 0 {
			n := uint32(sys.TrailingZeros32(ready))
			ready &^= 1 << n

			if p := sigDirectWaiter[uint32(i)*32+n].Swap(0); p != 0 {
				atomic.And(&sigParked[i], ^(uint32(1) << n))
				return (*g)(unsafe.Pointer(p))
			}
		}
	}

	if !sigAnyPending() {
		return nil
	}

	return (*g)(unsafe.Pointer(sigWaiter.Swap(0)))
}

// signal_enable makes the os/signal loop receive signal sig.
//
//go:linkname signal_enable os/signal.signal_enable
func signal_enable(sig uint32) {
	if sig < numSig {
		atomic.Or(&sigEnabled[sig/32], 1<<(sig%32))
	}
}

// signal_disable stops the os/signal loop from receiving signal sig.
//
//go:linkname signal_disable os/signal.signal_disable
func signal_disable(sig uint32) {
	if sig < numSig {
		atomic.And(&sigEnabled[sig/32], ^(uint32(1) << (sig % 32)))
	}
}

// signalWaitUntilIdle waits until the signal delivery mechanism is idle.
// This is used to ensure that we do not drop a signal notification due
// to a race between disabling a signal and receiving a signal.
// This assumes that signal delivery has already been disabled for
// the signal(s) in question, and here we are just waiting to make sure
// that all the signals have been delivered to the user channels
// by the os/signal package.
//
//go:linkname signalWaitUntilIdle os/signal.signalWaitUntilIdle
func signalWaitUntilIdle() {
	for sigAnyPending() || sigWaiter.Load() == 0 {
		Gosched()
	}
}
