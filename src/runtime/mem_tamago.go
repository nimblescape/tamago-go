// Copyright 2010 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime

import (
	"internal/runtime/atomic"
	"runtime/goos"
	"unsafe"
)

func sbrk(n uintptr) unsafe.Pointer {
	// Plan 9 sbrk from /sys/src/libc/9sys/sbrk.c
	bl := bloc
	n = memRound(n)
	if bl+n > blocMax {
		switch {
		case goos.BlocMax > 0:
			if bl+n > goos.BlocMax {
				return nil
			}
		case bl+n > uintptr(g0.stack.lo):
			return nil
		default:
			memclrNoHeapPointers(unsafe.Pointer(bl), n)
		}
		blocMax = bl + n
	}
	bloc += n
	return unsafe.Pointer(bl)
}

// Memory provider.
//
// The runtime starts on the break region: the memory from the end of the
// program (or goos.Bloc) up to goos.BlocMax (or the stack of g0), which
// sbrk gives out. A program may install a memory provider with
// runtime/goos.SetMemory, so that the memory of the runtime grows beyond
// the break region and goes back. From then on:
//
//   - every new reservation and allocation comes from the provider;
//   - a transition of a range outside the break region goes to the
//     provider, because only the provider gives out such ranges;
//   - the break region keeps its sbrk behaviour for its whole life.
//
// The transitions of mem.go map onto the four methods of the provider as
// on Windows, where the runtime also works with memory that faults until
// sysUsed: sysMap does nothing, sysUsed is Use, sysUnused and sysFault are
// Unuse, and sysFree is Release.
//
// The runtime calls the provider with its locks held, on the system stack
// and on any processor. So the provider never allocates from the Go heap,
// never blocks and never uses the scheduler, and its addresses lie outside
// the break region.
//
// Without a provider, every transition does what it did before, except
// that sysUnused clears its range, so that memory that the runtime gave
// back keeps no data.

// memoryProvider is the type runtime/goos.Memory. It is an alias of the
// same interface literal, so that the runtime needs no import of it.
type memoryProvider = interface {
	Reserve(hint, size, align uintptr) uintptr
	Use(addr, size uintptr)
	Unuse(addr, size uintptr)
	Release(addr, size uintptr)
}

// The states of the memory provider.
const (
	providerNone = iota
	providerInstalling
	providerReady
)

var (
	// memProvider is the memory provider, valid once memProviderState
	// is providerReady.
	memProvider      memoryProvider
	memProviderState atomic.Uint32

	// breakBase is the start of the break region (see osinit).
	breakBase uintptr
)

// setMemory installs m as the memory provider, once.
//
//go:linkname setMemory runtime/goos.SetMemory
func setMemory(m memoryProvider) {
	if m == nil {
		throw("SetMemory: no provider")
	}

	if !memProviderState.CompareAndSwap(providerNone, providerInstalling) {
		throw("SetMemory: provider already installed")
	}

	memProvider = m
	memProviderState.Store(providerReady)
}

// memProviderReady reports whether new requests go to the provider.
//
//go:nosplit
func memProviderReady() bool {
	return memProviderState.Load() == providerReady
}

// provided reports whether the range at v comes from the provider: it lies
// outside the break region.
//
//go:nosplit
func provided(v unsafe.Pointer) bool {
	if !memProviderReady() {
		return false
	}

	end := goos.BlocMax

	if end == 0 {
		end = uintptr(g0.stack.lo)
	}

	return uintptr(v) < breakBase || uintptr(v) >= end
}

func osMemAlloc(n uintptr) (unsafe.Pointer, bool) {
	if !memProviderReady() {
		return nil, false
	}

	var p uintptr

	n = memRound(n)

	systemstack(func() {
		if p = memProvider.Reserve(0, n, physPageSize); p != 0 {
			memProvider.Use(p, n)
		}
	})

	return unsafe.Pointer(p), true
}

func osMemReserve(v unsafe.Pointer, n uintptr) (unsafe.Pointer, bool) {
	if !memProviderReady() {
		return nil, false
	}

	var p uintptr

	n = memRound(n)

	systemstack(func() {
		p = memProvider.Reserve(uintptr(v), n, physPageSize)
	})

	return unsafe.Pointer(p), true
}

func osMemReserveAligned(size, align uintptr) (unsafe.Pointer, uintptr, bool) {
	if !memProviderReady() {
		return nil, 0, false
	}

	var p uintptr

	size = memRound(size)

	systemstack(func() {
		p = memProvider.Reserve(0, size, align)
	})

	if p == 0 {
		return nil, 0, true
	}

	return unsafe.Pointer(p), size, true
}

func osMemFree(v unsafe.Pointer, n uintptr) bool {
	if !provided(v) {
		return false
	}

	systemstack(func() {
		memProvider.Release(uintptr(v), memRound(n))
	})

	return true
}

// osMemUsed backs every page that the range touches.
func osMemUsed(v unsafe.Pointer, n uintptr) {
	if !provided(v) {
		return
	}

	start := alignDown(uintptr(v), physPageSize)
	end := alignUp(uintptr(v)+n, physPageSize)

	systemstack(func() {
		memProvider.Use(start, end-start)
	})
}

// osMemUnused gives back the pages that lie whole in the range, or clears
// the range in the break region.
func osMemUnused(v unsafe.Pointer, n uintptr) {
	if !provided(v) {
		memclrNoHeapPointers(v, n)
		return
	}

	start := alignUp(uintptr(v), physPageSize)
	end := alignDown(uintptr(v)+n, physPageSize)

	if start >= end {
		return
	}

	systemstack(func() {
		memProvider.Unuse(start, end-start)
	})
}

func osMemFault(v unsafe.Pointer, n uintptr) {
	if provided(v) {
		osMemUnused(v, n)
	}
}
