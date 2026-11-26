// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package signal

import (
	"os"
	"syscall"
)

// Defined by the runtime package.
func signal_disable(uint32)
func signal_enable(uint32)
func signal_recv() uint32
func signal_wait(uint32)

func init() {
	watchSignalLoop = loop
}

func loop() {
	for {
		process(syscall.Signal(signal_recv()))
	}
}

const numSig = 256

func signum(sig os.Signal) int {
	switch sig := sig.(type) {
	case syscall.Signal:
		i := int(sig)
		if i < 0 || i >= numSig {
			return -1
		}
		return i
	default:
		return -1
	}
}

// The loop receives only the enabled signals, so that a goroutine can
// receive any other signal with [WaitSignal] (see
// runtime/sigqueue_tamago.go).
func enableSignal(sig int)  { signal_enable(uint32(sig)) }
func disableSignal(sig int) { signal_disable(uint32(sig)) }
func ignoreSignal(sig int)  { signal_disable(uint32(sig)) }

func signalIgnored(sig int) bool {
	return false
}

// Relay sends a signal to the [Notify] channel.
//
// To make it suitable for invocation in bare metal interrupt/exception
// handlers, the function is implemented in assembly avoiding allocation and
// runtime use.
//
//go:nosplit
func Relay(sig syscall.Signal)

// WaitSignal blocks the calling goroutine until signal sig is raised with
// [Relay], and clears it. A signal raised while the goroutine does not wait
// stays pending, and a signal raised more than once before a wait counts
// once. At most one goroutine waits for a signal at a time.
//
// WaitSignal wakes its goroutine on the processor that handles the signal
// first, without the goroutine of this package that relays the signals of
// [Notify]. So a device with one interrupt for each of its queues, such as
// a network card with a receive queue for each processor, can serve each
// queue with its own goroutine. Once a goroutine has waited for a signal
// with WaitSignal, [Notify] no longer receives it.
func WaitSignal(sig syscall.Signal) {
	signal_wait(uint32(sig))
}
