// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build tamago

// Preemption for GOOS=tamago.
//
// On Linux, sysmon marks a goroutine that ran for forcePreemptNS (retake),
// so that it stops at its next function call, and preemptM sends a signal
// to its thread, whose handler moves it to an asynchronous safe point
// (doSigPreempt). TamaGo has no sysmon and no signals. Instead, a platform
// that turns preemption on (see preemptStart, amd64 only for now) raises
// one interrupt, the preemption interrupt, on each processor at least
// every forcePreemptNS, and preemptM raises it on the processor of an M.
// Its handler runs the time-slice check of retake for the P of that
// processor (sliceCheck) and then does what doSigPreempt does.
//
// Without preemption, nothing changes: preemptM does nothing. runqput uses
// no runnext slot in both cases, as before, because tamago has no sysmon.

package runtime

import "internal/runtime/atomic"

// preemptRaise raises the preemption interrupt on the processor with the
// identifier procid (see goos.ProcID). It is nil until the platform turns
// preemption on.
var preemptRaise func(procid uint64)

const preemptMSupported = true

// preemptM raises the preemption interrupt on the processor of mp, as
// signalM sends the preemption signal on Linux. It raises at most one at a
// time: the handler clears mp.signalPending.
func preemptM(mp *m) {
	if preemptRaise == nil {
		return
	}

	if mp.signalPending.CompareAndSwap(0, 1) {
		preemptRaise(mp.procid)
	}
}

// sliceCheck is the time-slice check of retake for pp, the P of the M of
// the processor that runs it: it marks the goroutine of pp for preemption
// once pp has run it, or goroutines that share its time slice, for
// forcePreemptNS. It marks it as preemptone does, but it raises no
// preemption interrupt, as its caller is that interrupt.
//
//go:nowritebarrierrec
func sliceCheck(pp *p, now int64) {
	if atomic.Load(&pp.status) != _Prunning {
		return
	}

	pd := &pp.sysmontick
	t := int64(pp.schedtick)

	if int64(pd.schedtick) != t {
		pd.schedtick = uint32(t)
		pd.schedwhen = now
		return
	}

	if pd.schedwhen+forcePreemptNS > now {
		return
	}

	mp := pp.m.ptr()

	if mp == nil {
		return
	}

	gp := mp.curg

	if gp == nil || gp == mp.g0 {
		return
	}

	gp.preempt = true
	gp.stackguard0 = stackPreempt

	if debug.asyncpreemptoff == 0 {
		pp.preempt = true
	}
}
