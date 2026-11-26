// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime

import "unsafe"

// SysUnusedZeroes fills n bytes of new memory, gives them back with
// sysUnused and reports whether they read as zero afterwards. It also
// reports whether the runtime relies on that, which it does when
// needZeroAfterSysUnused reports false.
func SysUnusedZeroes(n uintptr) (zero, relied bool) {
	systemstack(func() {
		p := sysAlloc(n, testSysStat, "test")
		if p == nil {
			throw("out of memory")
		}
		b := unsafe.Slice((*byte)(p), n)
		for i := range b {
			b[i] = 0xff
		}
		sysUnused(p, n)
		zero = true
		for _, c := range b {
			if c != 0 {
				zero = false
				break
			}
		}
		sysUsed(p, n, n)
		sysFree(p, n, testSysStat)
	})
	return zero, !needZeroAfterSysUnused()
}
