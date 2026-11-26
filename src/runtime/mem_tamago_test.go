// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime_test

import (
	"runtime"
	"testing"
)

func TestSysUnusedZeroes(t *testing.T) {
	zero, relied := runtime.SysUnusedZeroes(1 << 20)
	if relied && !zero {
		t.Fatal("memory that sysUnused gave back keeps its data, but the runtime takes it for zero")
	}
	if !relied {
		t.Skip("the platform does not implement goospkg.Madvise")
	}
}
