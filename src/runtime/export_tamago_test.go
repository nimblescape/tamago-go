// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build tamago

package runtime

// RaiseSignal raises signal sig as an interrupt handler does with
// goospkg.Signal.
func RaiseSignal(sig uint32) { sigsend(sig) }

// WaitSignal waits for signal sig as goospkg.WaitSignal does.
func WaitSignal(sig int) { sigWait(sig) }

// SignalWaiterParked reports whether a goroutine is parked in WaitSignal
// for signal sig.
func SignalWaiterParked(sig int) bool { return sigDirectWaiter[sig].Load() != 0 }
