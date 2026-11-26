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
// receive any other signal with WaitSignal of the GOOSPKG package (see
// runtime/sigqueue_tamago.go).
func enableSignal(sig int)  { signal_enable(uint32(sig)) }
func disableSignal(sig int) { signal_disable(uint32(sig)) }
func ignoreSignal(sig int)  { signal_disable(uint32(sig)) }

func signalIgnored(sig int) bool {
	return false
}
