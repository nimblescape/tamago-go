// Copyright 2025 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build tamago

package runtime

import (
	"runtime/goos"
)

// interruptReady returns a parked goroutine, in _Gwaiting state, whose
// signal (see sigReady) or event (see eventReady) an interrupt handler
// raised, the caller must make it runnable.
//
//go:nosplit
func interruptReady() *g {
	if gp := sigReady(); gp != nil {
		return gp
	}

	return eventReady()
}

// beforeIdle gets called by the scheduler if no goroutine is awake.
//
//go:yeswritebarrierrec
func beforeIdle(now, pollUntil int64) (gp *g, otherReady bool) {
	if gp = interruptReady(); gp != nil {
		return gp, false
	}

	if goos.Idle != nil {
		goos.Idle(pollUntil)
	}

	if now > 0 {
		sched.idleTime.Add(nanotime() - now)
	}

	// always return otherReady to ensure that no M is ever dropped
	return nil, true
}

func checkTimeouts() {}
