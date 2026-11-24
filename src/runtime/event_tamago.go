// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build tamago

// Interrupt events for GOOS=tamago.
//
// An event connects one interrupt source with one goroutine, so that each
// interrupt wakes its own goroutine on any processor, without a goroutine
// that relays all interrupts (as os/signal does with its loop).
//
// An interrupt handler raises event n through runtime/goos.RaiseEvent (see
// eventRaise in sys_tamago_amd64.s, the only architecture with events for
// now), which only sets bit n in eventPending with an atomic operation,
// without allocation, locking or runtime use. A goroutine waits for event
// n with runtime/goos.WaitEvent,
// which parks it until bit n is set and then clears the bit. The scheduler
// of every processor (see interruptReady) makes a parked waiter runnable
// once its event is pending.
//
// An event raised while its goroutine runs stays pending, so it is never
// lost, and events raised more than once before the wait count once.
// At most one goroutine waits for an event at a time.

package runtime

import (
	"internal/runtime/atomic"
	"internal/runtime/sys"
	"unsafe"
)

const numEvents = 256

var (
	// eventPending holds the raised events
	eventPending [numEvents / 32]uint32

	// eventParked holds the events whose waiter is parked
	eventParked [numEvents / 32]uint32

	// eventWaiter holds the *g parked in eventWait for each event
	eventWaiter [numEvents]atomic.Uintptr
)

// eventWait blocks the calling goroutine until event n is raised, and
// clears it.
//
//go:linkname eventWait runtime/goos.WaitEvent
func eventWait(n uint32) {
	if n >= numEvents {
		throw("WaitEvent: event out of range")
	}

	i, bit := n/32, uint32(1)<<(n%32)

	for atomic.And32(&eventPending[i], ^bit)&bit == 0 {
		gopark(eventParkCommit, unsafe.Pointer(&eventWaiter[n]), waitReasonIOWait, traceBlockGeneric, 1)
	}
}

// eventParkCommit records gp as the waiter of the event whose waiter slot
// is w.
func eventParkCommit(gp *g, w unsafe.Pointer) bool {
	waiter := (*atomic.Uintptr)(w)
	n := uint32((uintptr(w) - uintptr(unsafe.Pointer(&eventWaiter[0]))) / unsafe.Sizeof(eventWaiter[0]))
	i, bit := n/32, uint32(1)<<(n%32)
	p := uintptr(unsafe.Pointer(gp))

	if !waiter.CompareAndSwap(0, p) {
		throw("WaitEvent: two goroutines wait for one event")
	}

	atomic.Or(&eventParked[i], bit)

	if atomic.Load(&eventPending[i])&bit != 0 {
		// The event was raised while parking, abort unless
		// eventReady already took gp (in which case it is being made
		// runnable).
		if waiter.CompareAndSwap(p, 0) {
			atomic.And(&eventParked[i], ^bit)
			return false
		}
	}

	return true
}

// eventReady returns a parked event waiter, in _Gwaiting state, whose event
// is pending, the caller must make it runnable.
//
//go:nosplit
func eventReady() *g {
	for i := range eventParked {
		ready := atomic.Load(&eventPending[i]) & atomic.Load(&eventParked[i])

		for ready != 0 {
			b := uint32(sys.TrailingZeros32(ready))
			ready &^= 1 << b

			if p := eventWaiter[uint32(i)*32+b].Swap(0); p != 0 {
				atomic.And(&eventParked[i], ^(uint32(1) << b))
				return (*g)(unsafe.Pointer(p))
			}
		}
	}

	return nil
}
