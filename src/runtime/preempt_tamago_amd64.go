// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build tamago

package runtime

import (
	"internal/abi"
	"internal/cpu"
	"internal/goarch"
	goospkg "internal/runtime/goospkg"
	"unsafe"
)

// preemptContext is the state of the code that the preemption interrupt
// stopped, as preemptInterrupt keeps it on the gsignal stack: the general
// registers, which preemptInterrupt saves, and the frame of the interrupt,
// which the processor pushed and which IRETQ restores.
type preemptContext struct {
	r15, r14, r13, r12, r11, r10, r9, r8 uint64
	di, si, bp, dx, cx, bx, ax           uint64
	rip, cs, rflags, rsp, ss             uint64
}

// pushCall makes it look as if the stopped code called targetPC at
// resumePC, as sigctxt.pushCall does for a signal on Linux.
func (c *preemptContext) pushCall(targetPC, resumePC uintptr) {
	sp := uintptr(c.rsp) - goarch.PtrSize
	*(*uintptr)(unsafe.Pointer(sp)) = resumePC
	c.rsp = uint64(sp)
	c.rip = uint64(targetPC)
}

// The extended state (x87, SSE, AVX and above) of the stopped code, which
// preemptInterrupt saves on the gsignal stack around preemptHandler.
var (
	// preemptXsave tells whether preemptInterrupt saves the state with
	// XSAVE (the operating system enabled it in CR4), otherwise with
	// FXSAVE.
	preemptXsave bool

	// preemptStateSize is the number of bytes of the saved state.
	preemptStateSize uintptr
)

// preemptStateLimit bounds the saved extended state, so that it leaves
// most of the gsignal stack to preemptHandler.
const preemptStateLimit = 16 << 10

// preemptInterrupt is the handler of the preemption interrupt, defined in
// preempt_tamago_amd64.s. The gate of the interrupt in the GOOSPKG overlay
// jumps to it through goospkg.PreemptInterrupt.
func preemptInterrupt()

// defined in preempt_tamago_amd64.s
func xsaveSize() uint32

// preemptStart turns preemption on (see preempt_tamago.go). raise raises
// the preemption interrupt on the processor with the identifier procid
// (see goospkg.ProcID). The platform raises it on every processor at least
// every forcePreemptNS once the first one has arrived there, and the gate
// of the interrupt jumps to preemptInterrupt.
//
// It must be called on the only M, after goospkg.ProcID is set. The runtime
// raises the first interrupt on the processor of this M now, and on the
// processor of every later M when the M starts (see minit).
//
//go:linkname preemptStart internal/runtime/goospkg.StartPreemption
func preemptStart(raise func(procid uint64)) {
	if raise == nil || goospkg.ProcID == nil {
		throw("StartPreemption: no raise or no goospkg.ProcID")
	}

	if preemptRaise != nil {
		throw("StartPreemption: preemption is on already")
	}

	if mcount() != 1 {
		throw("StartPreemption: more than one M")
	}

	if cpu.X86.HasOSXSAVE {
		preemptXsave = true
		preemptStateSize = uintptr(xsaveSize())
	} else {
		preemptStateSize = 512
	}

	if preemptStateSize > preemptStateLimit {
		throw("StartPreemption: the extended state is too large")
	}

	mp := acquirem()
	mp.procid = goospkg.ProcID()
	preemptRaise = raise
	raise(mp.procid)
	releasem(mp)
}

// preemptHandler runs in the preemption interrupt, on the gsignal stack of
// the M of the processor, with interrupts disabled. gp is the goroutine
// (or g0) that the interrupt stopped, and c its state. As the signal
// handler does on Linux for the signal of preemptM, it injects a call to
// asyncPreempt if gp wants to be preempted and stopped at an asynchronous
// safe point. Before, it runs the time-slice check of sysmon for the P of
// the M, which may mark gp.
//
//go:nowritebarrierrec
func preemptHandler(c *preemptContext, gp *g) {
	mp := gp.m

	if pp := mp.p.ptr(); pp != nil {
		sliceCheck(pp, nanotime())
	}

	if debug.asyncpreemptoff == 0 && wantAsyncPreempt(gp) {
		if ok, newpc := isAsyncSafePoint(gp, uintptr(c.rip), uintptr(c.rsp), 0); ok {
			c.pushCall(abi.FuncPCABI0(asyncPreempt), newpc)
		}
	}

	// acknowledge the preemption, as doSigPreempt does
	mp.preemptGen.Add(1)
	mp.signalPending.Store(0)
}
