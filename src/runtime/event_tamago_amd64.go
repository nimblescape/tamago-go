// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime

import _ "unsafe" // for go:linkname

// eventRaise sets event n in eventPending, defined in sys_tamago_amd64.s.
//
// eventRaise should be an internal detail, but runtime/goos.RaiseEvent
// jumps to it from the GOOSPKG overlay.
//
//go:linkname eventRaise
func eventRaise(n uint32)
