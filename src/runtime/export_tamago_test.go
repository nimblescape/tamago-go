// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build tamago

package runtime

// RaiseSignal raises signal sig as an interrupt handler does with
// os/signal.Relay.
func RaiseSignal(sig uint32) { sigRelay(sig) }

// WaitSignal waits for signal sig as os/signal.WaitSignal does.
func WaitSignal(sig int) { signal_wait(uint32(sig)) }

// SignalWaiterParked reports whether a goroutine is parked in WaitSignal
// for signal sig.
func SignalWaiterParked(sig int) bool { return sigDirectWaiter[sig].Load() != 0 }
