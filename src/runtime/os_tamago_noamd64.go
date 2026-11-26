// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build tamago && !amd64

package runtime

import "internal/abi"

// taskEntry returns the function that goospkg.Task runs for a new M. These
// architectures keep g in a register, so the M needs no TLS block.
func taskEntry() uintptr {
	return abi.FuncPCABI0(mstart)
}
