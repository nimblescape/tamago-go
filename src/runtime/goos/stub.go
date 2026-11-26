// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !tamago

// Package goos describes required, as well as optional, runtime
// functions/variables for custom GOOS implementations as supported by the
// GOOSPKG variable.
//
// These hooks act as a "Rosetta Stone" for integration of a freestanding Go
// runtime within an arbitrary environment, whether bare metal or OS supported.
//
// For bare metal examples see the following packages: [usbarmory], [uefi],
// [microvm].
//
// For OS supported examples see the following tamago packages: [linux],
// [applet].
//
// This package is a stub and is only used for documentation purposes,
// applications need to define the described functions/variables or import them
// from external packages (such as the ones provided by [tamago]) relevant to
// the target environment.
//
// [tamago]: https://github.com/usbarmory/tamago
// [usbarmory]: https://github.com/usbarmory/tamago/tree/master/board/usbarmory
// [uefi]: https://github.com/usbarmory/go-boot/tree/main/uefi
// [microvm]: https://github.com/usbarmory/tamago/tree/master/board/firecracker/microvm
// [linux]: https://github.com/usbarmory/tamago/tree/master/user/linux
// [applet]: https://github.com/usbarmory/GoTEE/tree/master/applet
package goos

import "unsafe"

// Required constants.
const (
	// ArenaBaseOffset is the pointer value that corresponds to index 0 in
	// the heap arena map (see runtime.arenaBaseOffset).
	ArenaBaseOffset = 0

	// HeapAddrBits is the number of bits in a heap address (see
	// runtime.heapAddrBits).
	HeapAddrBits = 40

	// LogHeapArenaBytes is the size of a runtime heap arena in log_2
	// bytes (see runtime.logHeapArenaBytes).
	LogHeapArenaBytes = (2 + 20)

	// LogPallocChunkPages is the size of a runtime bitmap chunk in log_2
	// bytes (see runtime.logPallocChunkPages).
	LogPallocChunkPages = 9

	// MinPhysPageSize is a lower-bound on the physical page size (see
	// runtime.minPhysPageSize).
	MinPhysPageSize = 4096

	// StackSystem is a number of additional bytes to add to each stack
	// below the usual guard area.
	StackSystem = 0
)

// Required variables.
var (
	// RamStart defines the start address of the physical or virtual memory
	// available to the runtime for allocation (including the code segment
	// which must be mapped within).
	RamStart uint

	// RamSize defines the total size of the physical or virtual memory
	// available to the runtime for allocation (including the code segment
	// which must be mapped within).
	RamSize uint

	// RamStackOffset, defines the negative offset from the end of the
	// available memory for stack allocation.
	RamStackOffset uint
)

// CPUInit handles immediate startup CPU initialization as it represents the
// first instruction set executed.
func CPUInit() {}

// Hwinit0 takes care of the lower level initialization triggered before
// runtime setup (pre World start).
//
// It must be defined using Go's Assembler to retain Go's commitment to
// backward compatibility, otherwise extreme care must be taken as the lack of
// World start does not allow memory allocation.
func Hwinit0() {}

// InitRNG initializes random number generation.
func InitRNG() {}

// GetRandomData generates len(b) random bytes and writes them into b.
func GetRandomData(b []byte) {}

// Nanotime returns the system time in nanoseconds.
//
// Before [Hwinit1] it must be defined using Go's Assembler to retain Go's
// commitment to backward compatibility, otherwise extreme care must be taken
// as the lack of World start does not allow memory allocation.
func Nanotime() int64 { return 0 }

// Printk handles character printing to standard output.
//
// Before [Hwinit1] it must be defined using Go's Assembler to retain Go's
// commitment to backward compatibility, otherwise extreme care must be taken
// as the lack of World start does not allow memory allocation.
func Printk(c byte) {}

// Hwinit1 takes care of the lower level initialization triggered early in
// runtime setup (post World start).
func Hwinit1() {}

// Optional variables/functions.
var (
	// Bloc is an optional variable which can be set in [Hwinit0] to
	// override the heap memory start address, which otherwise defaults to
	// the program break (i.e. end of the data segments).
	Bloc uintptr

	// BlocMax is an optional variable which can be set to override the
	// upper bound for heap memory allocation, which otherwise defaults to
	// the top of g0 stack.
	BlocMax uintptr

	// Brk is an optional function which can be set to back the heap with
	// virtual memory, as brk(2) does on Linux. The runtime calls it to
	// raise the end of the heap to addr, and it reports whether the heap
	// may grow up to addr. Brk needs to back no memory: the runtime uses
	// a page of the new memory only after it called Madvise with the
	// advice MADV_POPULATE_WRITE for it. Brk runs on the system stack with
	// runtime locks held, so it must not allocate memory or block. When
	// used it must be set in [Hwinit0] or with a static initializer, and
	// it must not change afterwards, and Madvise must be set as well.
	Brk func(addr uintptr) bool

	// Madvise is an optional function which can be set to back memory and
	// to give it back, as madvise(2) does on Linux. The runtime calls it
	// with two advice values:
	//
	//   - MADV_POPULATE_WRITE (23) before it uses a range of the heap. The
	//     platform must then back each page of the range that has no frame
	//     with a frame filled with zeros, so that an access does not fault.
	//     It must keep the pages that have a frame.
	//   - MADV_DONTNEED (4) for heap pages that it does not use. The
	//     platform can then release their frames, and the pages must read
	//     as zero after the next MADV_POPULATE_WRITE.
	//
	// Other advice can be ignored. Madvise runs on the system stack, so it
	// must not allocate memory or block. A lack of memory is fatal. When
	// used it must be set in [Hwinit0] or with a static initializer, and
	// it must not change afterwards, because the runtime then relies on it
	// to back and to zero the pages.
	Madvise func(addr, n uintptr, advice int32)

	// Exit is an optional function which can be set to override default
	// runtime termination.
	Exit func(code int32)

	// Idle is an optional function which can be set to implement CPU idle
	// time management.
	Idle func(until int64)

	// ProcID is an optional function which can be set to provide the
	// processor identifier for tracing purposes.
	ProcID func() uint64

	// Task is an optional function which can be set to provide an
	// implementation for HW/OS threading (e.g. [runtime.newosproc]).
	Task func(sp, mp, gp, fn unsafe.Pointer)

	// Wake is an optional function which can be set to provide a wake up
	// call for halted processors as reported by [ProcID], it must be
	// defined as required to handle [Idle] implementations which halt a
	// processor.
	Wake func(procid uint64)
)
