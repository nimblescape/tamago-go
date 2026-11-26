// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build tamago && amd64

package runtime

import "internal/abi"

// defined in asm_amd64.s
func cputicks() int64

// taskstart is the entry of an M that goospkg.Task starts (see
// sys_tamago_amd64.s).
func taskstart()

// taskEntry returns the function that goospkg.Task runs for a new M: it
// sets up the TLS block of the M before any Go code runs.
func taskEntry() uintptr {
	return abi.FuncPCABI0(taskstart)
}
