// Copyright 2010 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime

import (
	goospkg "internal/runtime/goospkg"
	"unsafe"
)

// The advice of goospkg.Madvise, with the values of madvise on Linux:
// _MADV_DONTNEED gives the frames of a range back, and _MADV_POPULATE_WRITE
// backs each page of a range with a frame filled with zeros.
const (
	_MADV_DONTNEED       = 4
	_MADV_POPULATE_WRITE = 23
)

func sbrk(n uintptr) unsafe.Pointer {
	// Plan 9 sbrk from /sys/src/libc/9sys/sbrk.c
	bl := bloc
	n = memRound(n)
	if bl+n > blocMax {
		switch {
		case goospkg.Brk != nil:
			// As brk_ on Plan 9: the platform lets the heap grow
			// up to bl+n. The runtime uses the new memory only
			// after sysUsedOS, which also sysAllocOS calls.
			if !goospkg.Brk(bl + n) {
				return nil
			}
		case goospkg.BlocMax > 0:
			if bl+n > goospkg.BlocMax {
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

// sysUsedOS lets the platform back [v, v+n) before the runtime uses it, if
// it implements goospkg.Madvise, as madvise with MADV_POPULATE_WRITE does on
// Linux. The runtime then never touches a page without a frame. This
// matters on a platform that runs Go in its most privileged mode: a page
// fault on the stack in use cannot be served there, because the processor
// stores the state of the fault on that stack.
func sysUsedOS(v unsafe.Pointer, n uintptr) {
	if goospkg.Madvise != nil {
		goospkg.Madvise(uintptr(v), n, _MADV_POPULATE_WRITE)
	}
}

// sysUnusedOS gives the frames of [v, v+n) back to the platform, if it
// implements goospkg.Madvise, as madvise does on Linux.
func sysUnusedOS(v unsafe.Pointer, n uintptr) {
	if goospkg.Madvise != nil {
		goospkg.Madvise(uintptr(v), n, _MADV_DONTNEED)
	}
}

// needZeroAfterSysUnusedOS reports whether memory that sysUnusedOS gave back
// must be zeroed before its next use. After goospkg.Madvise, the memory reads
// as zero, as after madvise with MADV_DONTNEED on Linux.
func needZeroAfterSysUnusedOS() bool {
	return goospkg.Madvise == nil
}
