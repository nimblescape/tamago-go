// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build plan9 || wasm

package runtime

import "unsafe"

// The osMem functions let an sbrk platform change a memory transition of
// mem_sbrk.go. GOOS=tamago defines them in mem_tamago.go. Plan 9 and Wasm
// change nothing: each function reports that it did not handle the
// transition, or does nothing.

func osMemAlloc(n uintptr) (unsafe.Pointer, bool) { return nil, false }

func osMemReserve(v unsafe.Pointer, n uintptr) (unsafe.Pointer, bool) { return nil, false }

func osMemReserveAligned(size, align uintptr) (unsafe.Pointer, uintptr, bool) { return nil, 0, false }

func osMemFree(v unsafe.Pointer, n uintptr) bool { return false }

func osMemUsed(v unsafe.Pointer, n uintptr) {}

func osMemUnused(v unsafe.Pointer, n uintptr) {}

func osMemFault(v unsafe.Pointer, n uintptr) {}
