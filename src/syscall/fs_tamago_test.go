// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package syscall_test

import (
	"os"
	"syscall"
	"testing"
)

func TestFchown(t *testing.T) {
	f, err := os.Create(t.TempDir() + "/file")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, tt := range []struct {
		uid, gid         int
		wantUID, wantGID uint32
	}{
		{1, 2, 1, 2},
		{-1, 3, 1, 3}, // -1 keeps the owner
		{4, -1, 4, 3}, // -1 keeps the group
	} {
		if err := f.Chown(tt.uid, tt.gid); err != nil {
			t.Fatal(err)
		}
		fi, err := f.Stat()
		if err != nil {
			t.Fatal(err)
		}
		st := fi.Sys().(*syscall.Stat_t)
		if st.Uid != tt.wantUID || st.Gid != tt.wantGID {
			t.Errorf("Chown(%d, %d): got owner %d and group %d, want %d and %d",
				tt.uid, tt.gid, st.Uid, st.Gid, tt.wantUID, tt.wantGID)
		}
	}
}
