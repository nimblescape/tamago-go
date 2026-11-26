// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build tamago

#include "go_asm.h"
#include "go_tls.h"
#include "textflag.h"

// LOCALS is the space below the saved extended state: the arguments of
// preemptHandler (c and gp) and the copies of both that preemptInterrupt
// reads after the call.
#define LOCALS 32

// goos.PreemptInterrupt is the handler of the preemption interrupt (see
// preemptStart). The gate of the interrupt jumps to it with interrupts
// disabled, after the end of the interrupt, and with the frame of the
// interrupt (RIP, CS, RFLAGS, RSP, SS) at 0(SP), on the stack of the
// stopped code.
//
// As the kernel does for a signal on Linux, it moves the frame to the
// gsignal stack of the M and saves there the registers of the stopped code
// (preemptContext) and its extended state. It runs preemptHandler with
// g = gsignal and returns with the frame on the gsignal stack: to the
// stopped code, or to asyncPreempt if preemptHandler injected a call.
//
// On the stopped stack it uses 32 bytes below the frame of the interrupt,
// which the StackSystem bytes of every Go stack hold. It returns at once if
// the processor runs no M or if g is gsignal.
TEXT runtime∕goos·PreemptInterrupt(SB),NOSPLIT|NOFRAME,$0
	PUSHQ	DX
	PUSHQ	CX
	PUSHQ	BX
	PUSHQ	AX

	// AX = g, BX = g.m.gsignal
	get_tls(CX)
	MOVQ	g(CX), AX
	TESTQ	AX, AX
	JZ	stay
	MOVQ	g_m(AX), BX
	TESTQ	BX, BX
	JZ	stay
	MOVQ	m_gsignal(BX), BX
	TESTQ	BX, BX
	JZ	stay
	CMPQ	AX, BX
	JEQ	stay

	// CX = the context, at the top of the gsignal stack
	MOVQ	(g_stack+stack_hi)(BX), CX
	ANDQ	$~15, CX
	SUBQ	$preemptContext__size, CX

	// copy the four registers and the frame of the interrupt into it
	MOVQ	0(SP), DX
	MOVQ	DX, preemptContext_ax(CX)
	MOVQ	8(SP), DX
	MOVQ	DX, preemptContext_bx(CX)
	MOVQ	16(SP), DX
	MOVQ	DX, preemptContext_cx(CX)
	MOVQ	24(SP), DX
	MOVQ	DX, preemptContext_dx(CX)
	MOVQ	32(SP), DX
	MOVQ	DX, preemptContext_rip(CX)
	MOVQ	40(SP), DX
	MOVQ	DX, preemptContext_cs(CX)
	MOVQ	48(SP), DX
	MOVQ	DX, preemptContext_rflags(CX)
	MOVQ	56(SP), DX
	MOVQ	DX, preemptContext_rsp(CX)
	MOVQ	64(SP), DX
	MOVQ	DX, preemptContext_ss(CX)

	// go on with the context on the gsignal stack, the stopped stack
	// needs nothing more
	MOVQ	CX, SP
	MOVQ	SI, preemptContext_si(SP)
	MOVQ	DI, preemptContext_di(SP)
	MOVQ	BP, preemptContext_bp(SP)
	MOVQ	R8, preemptContext_r8(SP)
	MOVQ	R9, preemptContext_r9(SP)
	MOVQ	R10, preemptContext_r10(SP)
	MOVQ	R11, preemptContext_r11(SP)
	MOVQ	R12, preemptContext_r12(SP)
	MOVQ	R13, preemptContext_r13(SP)
	MOVQ	R14, preemptContext_r14(SP)
	MOVQ	R15, preemptContext_r15(SP)
	MOVQ	SP, R12			// R12 = context
	MOVQ	AX, R13			// R13 = stopped g

	// save the extended state below the context, 64-byte aligned
	SUBQ	runtime·preemptStateSize(SB), SP
	ANDQ	$~63, SP
	CMPB	runtime·preemptXsave(SB), $0
	JEQ	fxsave

	// XSAVE writes only the first word of the header that follows the
	// legacy area, and XRSTOR needs the rest of the header zero
	XORL	AX, AX
	MOVQ	AX, 512(SP)
	MOVQ	AX, 520(SP)
	MOVQ	AX, 528(SP)
	MOVQ	AX, 536(SP)
	MOVQ	AX, 544(SP)
	MOVQ	AX, 552(SP)
	MOVQ	AX, 560(SP)
	MOVQ	AX, 568(SP)
	MOVL	$-1, AX
	MOVL	$-1, DX
	XSAVE64	(SP)
	JMP	saved
fxsave:
	FXSAVE64	(SP)
saved:
	SUBQ	$LOCALS, SP
	MOVQ	R12, 0(SP)		// c
	MOVQ	R13, 8(SP)		// gp
	MOVQ	R12, 16(SP)
	MOVQ	R13, 24(SP)

	// g = gsignal, as for a signal, and the direction flag clear, as Go
	// code expects
	get_tls(CX)
	MOVQ	BX, g(CX)
	CLD
	CALL	runtime·preemptHandler(SB)

	// g = the stopped g
	MOVQ	24(SP), AX
	get_tls(CX)
	MOVQ	AX, g(CX)
	MOVQ	16(SP), R12
	ADDQ	$LOCALS, SP

	// restore the extended state
	CMPB	runtime·preemptXsave(SB), $0
	JEQ	fxrstor
	MOVL	$-1, AX
	MOVL	$-1, DX
	XRSTOR64	(SP)
	JMP	restored
fxrstor:
	FXRSTOR64	(SP)
restored:
	MOVQ	R12, SP
	MOVQ	preemptContext_r15(SP), R15
	MOVQ	preemptContext_r14(SP), R14
	MOVQ	preemptContext_r13(SP), R13
	MOVQ	preemptContext_r12(SP), R12
	MOVQ	preemptContext_r11(SP), R11
	MOVQ	preemptContext_r10(SP), R10
	MOVQ	preemptContext_r9(SP), R9
	MOVQ	preemptContext_r8(SP), R8
	MOVQ	preemptContext_di(SP), DI
	MOVQ	preemptContext_si(SP), SI
	MOVQ	preemptContext_bp(SP), BP
	MOVQ	preemptContext_dx(SP), DX
	MOVQ	preemptContext_cx(SP), CX
	MOVQ	preemptContext_bx(SP), BX
	MOVQ	preemptContext_ax(SP), AX
	ADDQ	$preemptContext_rip, SP
	IRETQ

stay:
	POPQ	AX
	POPQ	BX
	POPQ	CX
	POPQ	DX
	IRETQ

// func xsaveSize() uint32
//
// xsaveSize returns the size of the XSAVE area for the components that
// XCR0 enables.
TEXT runtime·xsaveSize(SB),NOSPLIT,$0-4
	MOVL	$0xd, AX
	XORL	CX, CX
	CPUID
	MOVL	BX, ret+0(FP)
	RET
