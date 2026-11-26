// Copyright 2010 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime

import (
	"runtime/goos"
	"unsafe"
)

// _MADV_DONTNEED is the advice of goos.Madvise that gives the frames of
// a range back. It has the value of MADV_DONTNEED on Linux.
const _MADV_DONTNEED = 4

func sbrk(n uintptr) unsafe.Pointer {
	// Plan 9 sbrk from /sys/src/libc/9sys/sbrk.c
	bl := bloc
	n = memRound(n)
	if bl+n > blocMax {
		switch {
		case goos.Brk != nil:
			// As brk_ on Plan 9: the platform maps the memory up
			// to bl+n, and the new memory reads as zero.
			if !goos.Brk(bl + n) {
				return nil
			}
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

// sysUnusedOS gives the frames of [v, v+n) back to the platform, if it
// implements goos.Madvise, as madvise does on Linux.
func sysUnusedOS(v unsafe.Pointer, n uintptr) {
	if goos.Madvise != nil {
		goos.Madvise(uintptr(v), n, _MADV_DONTNEED)
	}
}

// needZeroAfterSysUnusedOS reports whether memory that sysUnusedOS gave back
// must be zeroed before its next use. After goos.Madvise, the memory reads
// as zero, as after madvise with MADV_DONTNEED on Linux.
func needZeroAfterSysUnusedOS() bool {
	return goos.Madvise == nil
}
