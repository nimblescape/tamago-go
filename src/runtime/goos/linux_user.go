// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build tamago && (amd64 || arm || arm64 || loong64 || riscv64)

// Package goos provides support for using `GOOS=tamago` in Linux user
// space.
package goos

import (
	"unsafe"
)

// adapted from runtime/os_linux.go
const (
	_CLONE_VM      = 0x100
	_CLONE_FS      = 0x200
	_CLONE_FILES   = 0x400
	_CLONE_SIGHAND = 0x800
	_CLONE_THREAD  = 0x10000
	_CLONE_SYSVSEM = 0x40000

	cloneFlags = _CLONE_VM | /* share memory */
		_CLONE_FS | /* share cwd, etc */
		_CLONE_FILES | /* share fd table */
		_CLONE_SIGHAND | /* share sig handler table */
		_CLONE_SYSVSEM | /* share SysV semaphore undo lists (see issue #20763) */
		_CLONE_THREAD /* revisit - okay for now */
)

const (
	bits = 32 << (^uint(0) >> 63) / 8

	ArenaBaseOffset     = 0
	HeapAddrBits        = (8-bits)*3 + bits*5 // 32-bit:32 64-bit:40
	LogHeapArenaBytes   = (2 + 20)
	LogPallocChunkPages = 9
	MinPhysPageSize     = 4096
	StackSystem         = 0
)

var (
	RamStart       uint = 0x80000000
	RamSize        uint = 0x20000000 // 512MB
	RamStackOffset uint = 0x100

	Bloc    = uintptr(RamStart)
	BlocMax = uintptr(RamStart+RamSize)
	Brk     = brk
	Exit    = sys_exit_group
	Madvise = madvise

	Idle   func(until int64)
	ProcID func() uint64
	Wake   func(uint64)

	Hwinit0  = func() {}
	InitRNG  = func() {}
	Hwinit1  = func() {}
	Nanotime = sys_clock_gettime
)

// defined in linux_user*.s
func CPUInit()
func sys_exit_group(code int32)
func sys_write(c *byte)
func sys_clock_gettime() (ns int64)
func sys_getrandom(b []byte, n int)

//go:noescape
func clone(flags int32, stk, mp, gp, fn unsafe.Pointer) int32

// sys_madvise calls madvise (defined in linux_user*.s). It crashes if
// madvise fails, because the runtime would then take memory that still holds
// old data for zero.
func sys_madvise(addr, n uintptr, advice int32)

// madvise gives the frames of a range back with MADV_DONTNEED (4). Linux
// backs each page on its first access, so the other advice needs no call.
func madvise(addr, n uintptr, advice int32) {
	if advice == 4 {
		sys_madvise(addr, n, advice)
	}
}

// brk raises the end of the heap to addr. CPUInit maps all of the RAM, and
// Linux backs each page on its first access, so brk only checks the limit.
// It reads RamStart and RamSize, whose values the linker sets, because the
// runtime calls brk before the initializer of BlocMax runs.
func brk(addr uintptr) bool {
	return addr <= uintptr(RamStart+RamSize)
}

func GetRandomData(b []byte) {
	sys_getrandom(b, len(b))
}

// preallocated memory to avoid malloc during panic
var a [1]byte

func Printk(c byte) {
	a[0] = c
	sys_write(&a[0])
}

var Task = func(sp, mp, gp, fn unsafe.Pointer) {
	clone(cloneFlags, sp, mp, gp, fn)
}
