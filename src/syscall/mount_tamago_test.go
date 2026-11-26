// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package syscall_test

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
)

// memFS is a small file system for the tests of Mount. It has all the
// optional methods.
type memFS struct {
	mu     sync.Mutex
	nodes  map[string]*memNode // by name, "." is the root
	inum   uint64
	syncs  int    // calls of Sync
	closes int    // calls of Close
	stat   func() // runs in Stat if set
}

type memNode struct {
	st   syscall.Stat_t
	data []byte
}

func newMemFS() *memFS {
	fsys := &memFS{nodes: make(map[string]*memNode)}
	fsys.nodes["."] = fsys.newNode(syscall.S_IFDIR | 0755)
	return fsys
}

func (fsys *memFS) newNode(mode uint32) *memNode {
	fsys.inum++
	return &memNode{st: syscall.Stat_t{Ino: fsys.inum, Mode: mode, Dev: 99}}
}

func isDir(n *memNode) bool {
	return n.st.Mode&syscall.S_IFMT == syscall.S_IFDIR
}

// parentName returns the name of the directory that holds name.
func parentName(name string) string {
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		return name[:i]
	}
	return "."
}

// checkParent checks that the directory of name exists.
func (fsys *memFS) checkParent(name string) error {
	d, ok := fsys.nodes[parentName(name)]
	switch {
	case !ok:
		return syscall.ENOENT
	case !isDir(d):
		return syscall.ENOTDIR
	}
	return nil
}

// names returns the names in the directory d, in order.
func (fsys *memFS) names(d string) []string {
	var names []string
	for name := range fsys.nodes {
		if name != "." && parentName(name) == d {
			names = append(names, name[strings.LastIndexByte(name, '/')+1:])
		}
	}
	slices.Sort(names)
	return names
}

func (fsys *memFS) Open(name string, flag int, perm uint32) (syscall.File, error) {
	fsys.mu.Lock()
	defer fsys.mu.Unlock()
	n, ok := fsys.nodes[name]
	switch {
	case !ok && flag&syscall.O_CREATE == 0:
		return nil, syscall.ENOENT
	case !ok:
		if err := fsys.checkParent(name); err != nil {
			return nil, err
		}
		n = fsys.newNode(syscall.S_IFREG | perm)
		fsys.nodes[name] = n
	case flag&(syscall.O_CREATE|syscall.O_EXCL) == syscall.O_CREATE|syscall.O_EXCL:
		return nil, syscall.EEXIST
	case isDir(n):
		if flag&syscall.O_ACCMODE != syscall.O_RDONLY || flag&syscall.O_CREATE != 0 {
			return nil, syscall.EISDIR
		}
	case flag&syscall.O_DIRECTORY != 0:
		return nil, syscall.ENOTDIR
	case flag&syscall.O_TRUNC != 0:
		n.data = nil
	}
	return &memFile{fsys: fsys, name: name, node: n}, nil
}

func (fsys *memFS) Stat(name string, st *syscall.Stat_t) error {
	if fsys.stat != nil {
		fsys.stat()
	}
	fsys.mu.Lock()
	defer fsys.mu.Unlock()
	n, ok := fsys.nodes[name]
	if !ok {
		return syscall.ENOENT
	}
	n.statLocked(st)
	return nil
}

func (n *memNode) statLocked(st *syscall.Stat_t) {
	*st = n.st
	st.Size = int64(len(n.data))
}

func (fsys *memFS) Mkdir(name string, perm uint32) error {
	fsys.mu.Lock()
	defer fsys.mu.Unlock()
	if _, ok := fsys.nodes[name]; ok {
		return syscall.EEXIST
	}
	if err := fsys.checkParent(name); err != nil {
		return err
	}
	fsys.nodes[name] = fsys.newNode(syscall.S_IFDIR | perm)
	return nil
}

func (fsys *memFS) Unlink(name string) error {
	fsys.mu.Lock()
	defer fsys.mu.Unlock()
	n, ok := fsys.nodes[name]
	switch {
	case !ok:
		return syscall.ENOENT
	case isDir(n):
		return syscall.EISDIR
	}
	delete(fsys.nodes, name)
	return nil
}

func (fsys *memFS) Rmdir(name string) error {
	fsys.mu.Lock()
	defer fsys.mu.Unlock()
	n, ok := fsys.nodes[name]
	switch {
	case !ok:
		return syscall.ENOENT
	case !isDir(n):
		return syscall.ENOTDIR
	case name == ".":
		return syscall.EBUSY
	case len(fsys.names(name)) > 0:
		return syscall.ENOTEMPTY
	}
	delete(fsys.nodes, name)
	return nil
}

func (fsys *memFS) Rename(from, to string) error {
	fsys.mu.Lock()
	defer fsys.mu.Unlock()
	if _, ok := fsys.nodes[from]; !ok {
		return syscall.ENOENT
	}
	if err := fsys.checkParent(to); err != nil {
		return err
	}
	var moved []string
	for name := range fsys.nodes {
		if name == from || strings.HasPrefix(name, from+"/") {
			moved = append(moved, name)
		}
	}
	for _, name := range moved {
		fsys.nodes[to+name[len(from):]] = fsys.nodes[name]
		delete(fsys.nodes, name)
	}
	return nil
}

// memFile is a file of memFS.
type memFile struct {
	fsys *memFS
	name string
	node *memNode
}

func (f *memFile) Stat(st *syscall.Stat_t) error {
	f.fsys.mu.Lock()
	defer f.fsys.mu.Unlock()
	f.node.statLocked(st)
	return nil
}

func (f *memFile) Close() error {
	f.fsys.mu.Lock()
	defer f.fsys.mu.Unlock()
	f.fsys.closes++
	return nil
}

func (f *memFile) Pread(b []byte, offset int64) (int, error) {
	f.fsys.mu.Lock()
	defer f.fsys.mu.Unlock()
	if isDir(f.node) {
		return 0, syscall.EISDIR
	}
	if offset >= int64(len(f.node.data)) {
		return 0, nil
	}
	return copy(b, f.node.data[offset:]), nil
}

func (f *memFile) Pwrite(b []byte, offset int64) (int, error) {
	f.fsys.mu.Lock()
	defer f.fsys.mu.Unlock()
	if end := offset + int64(len(b)); end > int64(len(f.node.data)) {
		f.node.data = append(f.node.data, make([]byte, end-int64(len(f.node.data)))...)
	}
	return copy(f.node.data[offset:], b), nil
}

func (f *memFile) ReadDir() ([]string, error) {
	f.fsys.mu.Lock()
	defer f.fsys.mu.Unlock()
	if !isDir(f.node) {
		return nil, syscall.ENOTDIR
	}
	return f.fsys.names(f.name), nil
}

func (f *memFile) Truncate(size int64) error {
	f.fsys.mu.Lock()
	defer f.fsys.mu.Unlock()
	if size < int64(len(f.node.data)) {
		f.node.data = f.node.data[:size]
	} else {
		f.node.data = append(f.node.data, make([]byte, size-int64(len(f.node.data)))...)
	}
	return nil
}

func (f *memFile) Sync() error {
	f.fsys.mu.Lock()
	defer f.fsys.mu.Unlock()
	f.fsys.syncs++
	return nil
}

// mountFS mounts fsys on a new directory and returns the directory. A file
// system stays mounted, so the directory stays when the test ends.
func mountFS(t *testing.T, fsys syscall.FS) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "mount")
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mount(dir, fsys); err != nil {
		t.Fatal(err)
	}
	return dir
}

// checkErr reports an error if err does not match want.
func checkErr(t *testing.T, call string, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Errorf("%s: got error %v, want %v", call, err, want)
	}
}

func TestMountErrors(t *testing.T) {
	dir := mountFS(t, newMemFS())
	checkErr(t, "Mount on a missing directory", syscall.Mount("/missing", newMemFS()), syscall.ENOENT)
	checkErr(t, "Mount on a file", syscall.Mount("/dev/null", newMemFS()), syscall.ENOTDIR)
	checkErr(t, "Mount of nil", syscall.Mount(os.TempDir(), nil), syscall.EINVAL)
	checkErr(t, "Mount on a mount", syscall.Mount(dir, newMemFS()), syscall.EBUSY)
	checkErr(t, "Mount in a mount", syscall.Mount(dir+"/sub", newMemFS()), syscall.EBUSY)
}

func TestMountFiles(t *testing.T) {
	fsys := newMemFS()
	dir := mountFS(t, fsys)
	name := dir + "/file"
	if err := os.WriteFile(name, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	f, err := os.OpenFile(name, os.O_RDWR|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte(" world")); err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 5)
	if _, err := f.ReadAt(b, 6); err != nil || string(b) != "world" {
		t.Errorf("ReadAt: got %q, %v, want %q", b, err, "world")
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if b, err := io.ReadAll(f); err != nil || string(b) != "hello world" {
		t.Errorf("ReadAll: got %q, %v, want %q", b, err, "hello world")
	}
	if err := f.Truncate(5); err != nil {
		t.Fatal(err)
	}
	if err := f.Sync(); err != nil || fsys.syncs != 1 {
		t.Errorf("Sync: got %v and %d calls, want 1 call", err, fsys.syncs)
	}
	if fi, err := f.Stat(); err != nil || fi.Size() != 5 || fi.Mode() != 0644 {
		t.Errorf("Stat: got %v, %v, want size 5 and mode 0644", fi, err)
	}
	closes := fsys.closes
	if err := f.Close(); err != nil || fsys.closes != closes+1 {
		t.Errorf("Close: got %v and %d calls of Close, want %d", err, fsys.closes, closes+1)
	}

	if err := os.Truncate(name, 2); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(name); err != nil || string(b) != "he" {
		t.Errorf("ReadFile: got %q, %v, want %q", b, err, "he")
	}

	f, err = os.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	_, err = f.Write([]byte("x"))
	checkErr(t, "Write of a read-only file", err, syscall.EBADF)
	checkErr(t, "Truncate of a read-only file", f.Truncate(0), syscall.EINVAL)
	checkErr(t, "Chmod", f.Chmod(0600), syscall.ENOSYS)
	checkErr(t, "Chown", f.Chown(1, 1), syscall.ENOSYS)
}

func TestMountDirs(t *testing.T) {
	dir := mountFS(t, newMemFS())
	if err := os.MkdirAll(dir+"/a/b", 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a/y", "a/x", "a/b/z"} {
		if err := os.WriteFile(dir+"/"+name, nil, 0644); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(dir + "/a")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if want := []string{"b", "x", "y"}; !slices.Equal(names, want) || !entries[0].IsDir() || entries[1].IsDir() {
		t.Errorf("ReadDir: got %v, want %v with b as the only directory", names, want)
	}

	checkErr(t, "Remove of a full directory", os.Remove(dir+"/a/b"), syscall.ENOTEMPTY)
	checkErr(t, "Remove of the mount", os.Remove(dir), syscall.EBUSY)
	checkErr(t, "Rmdir of .", syscall.Rmdir(dir+"/a/b/."), syscall.EINVAL)
	checkErr(t, "Unlink with a slash at the end", syscall.Unlink(dir+"/a/x/"), syscall.ENOTDIR)
	checkErr(t, "MkDev", syscall.MkDev(dir+"/dev", 0666, nil), syscall.EPERM)
	if err := os.Remove(dir + "/a/x"); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(dir + "/a"); err != nil {
		t.Fatal(err)
	}
	_, err = os.Stat(dir + "/a")
	checkErr(t, "Stat after RemoveAll", err, syscall.ENOENT)

	// A directory with more entries than one ReadDirent call returns.
	if err := os.Mkdir(dir+"/many", 0755); err != nil {
		t.Fatal(err)
	}
	var want []string
	for i := range 100 {
		name := "f" + strconv.Itoa(1000+i)
		want = append(want, name)
		if err := os.WriteFile(dir+"/many/"+name, nil, 0644); err != nil {
			t.Fatal(err)
		}
	}
	f, err := os.Open(dir + "/many")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for range 2 {
		var got []string
		for {
			names, err := f.Readdirnames(7)
			got = append(got, names...)
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		if !slices.Equal(got, want) {
			t.Errorf("Readdirnames: got %v, want %v", got, want)
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMountPaths(t *testing.T) {
	dir := mountFS(t, newMemFS())
	parent := filepath.Dir(dir)
	if err := os.Mkdir(dir+"/d", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/file", nil, 0644); err != nil {
		t.Fatal(err)
	}

	stat := func(name string) os.FileInfo {
		t.Helper()
		fi, err := os.Stat(name)
		if err != nil {
			t.Fatal(err)
		}
		return fi
	}
	if !os.SameFile(stat(dir+"/d/../file"), stat(dir+"/file")) {
		t.Errorf("%s/d/../file is not %s/file", dir, dir)
	}
	if !os.SameFile(stat(dir+"/.."), stat(parent)) {
		t.Errorf("%s/.. is not %s", dir, parent)
	}
	if !os.SameFile(stat(dir+"/d/.."), stat(dir)) || !os.SameFile(stat(dir+"/."), stat(dir)) {
		t.Errorf("%s/d/.. or %s/. is not %s", dir, dir, dir)
	}
	if os.SameFile(stat(dir), stat(parent)) {
		t.Errorf("%s is %s", dir, parent)
	}
	if dev := stat(dir).Sys().(*syscall.Stat_t).Dev; dev == 0 || dev == 99 {
		t.Errorf("Stat_t.Dev of the mount: got %d, want a device of its own", dev)
	}

	_, err := os.Stat(dir + "/file/")
	checkErr(t, "Stat of a file with a slash at the end", err, syscall.ENOTDIR)
	_, err = os.Open(dir + "/file/")
	checkErr(t, "Open of a file with a slash at the end", err, syscall.ENOTDIR)
	_, err = os.Create(dir + "/new/")
	checkErr(t, "Create with a slash at the end", err, syscall.EISDIR)
}

func TestMountRename(t *testing.T) {
	fsys := newMemFS()
	dir := mountFS(t, fsys)
	if err := os.WriteFile(dir+"/f", []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(dir+"/f", dir+"/g"); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(dir + "/g"); err != nil || string(b) != "data" {
		t.Errorf("ReadFile after Rename: got %q, %v, want %q", b, err, "data")
	}

	tmp, err := os.CreateTemp("", "file")
	if err != nil {
		t.Fatal(err)
	}
	tmp.Close()
	defer os.Remove(tmp.Name())
	other := mountFS(t, newMemFS())
	checkErr(t, "Rename into the mount", os.Rename(tmp.Name(), dir+"/h"), syscall.EXDEV)
	checkErr(t, "Rename out of the mount", os.Rename(dir+"/g", tmp.Name()+"2"), syscall.EXDEV)
	checkErr(t, "Rename to another mount", os.Rename(dir+"/g", other+"/g"), syscall.EXDEV)
	checkErr(t, "Rename of the mount", os.Rename(dir, dir+"2"), syscall.EBUSY)
	checkErr(t, "Link in the mount", os.Link(dir+"/g", dir+"/g2"), syscall.ENOSYS)
	checkErr(t, "Link out of the mount", os.Link(dir+"/g", tmp.Name()+"3"), syscall.EXDEV)
	checkErr(t, "Chmod", os.Chmod(dir+"/g", 0600), syscall.ENOSYS)
}

func TestMountChdir(t *testing.T) {
	dir := mountFS(t, newMemFS())
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)
	if err := os.MkdirAll(dir+"/a/b", 0755); err != nil {
		t.Fatal(err)
	}

	getwd := func(want string) {
		t.Helper()
		if got, err := os.Getwd(); err != nil || got != want {
			t.Errorf("Getwd: got %q, %v, want %q", got, err, want)
		}
	}
	if err := os.Chdir(dir + "/a"); err != nil {
		t.Fatal(err)
	}
	getwd(dir + "/a")
	if err := os.WriteFile("rel", nil, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir + "/a/rel"); err != nil {
		t.Errorf("Stat of a file created by a relative path: %v", err)
	}
	if err := os.Chdir("b"); err != nil {
		t.Fatal(err)
	}
	getwd(dir + "/a/b")
	if err := os.Chdir("../.."); err != nil {
		t.Fatal(err)
	}
	getwd(dir)
	if err := os.Chdir(".."); err != nil {
		t.Fatal(err)
	}
	getwd(filepath.Dir(dir))

	f, err := os.Open(dir + "/a/b")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := f.Chdir(); err != nil {
		t.Fatal(err)
	}
	getwd(dir + "/a/b")
	checkErr(t, "Chdir to a file", os.Chdir(dir+"/a/rel"), syscall.ENOTDIR)
	checkErr(t, "Chdir to a missing directory", os.Chdir(dir+"/missing"), syscall.ENOENT)
}

// minFS is a file system with only the methods that FS and File need.
type minFS struct{}

type minFile struct{ mode uint32 }

func (minFS) Open(name string, flag int, perm uint32) (syscall.File, error) {
	switch name {
	case ".":
		return minFile{syscall.S_IFDIR | 0555}, nil
	case "file":
		return minFile{syscall.S_IFREG | 0444}, nil
	}
	return nil, syscall.ENOENT
}

func (f minFile) Stat(st *syscall.Stat_t) error {
	*st = syscall.Stat_t{Mode: f.mode, Ino: 1}
	return nil
}

func (minFile) Close() error { return nil }

func TestMountMinimal(t *testing.T) {
	dir := mountFS(t, minFS{})
	if fi, err := os.Stat(dir + "/file"); err != nil || fi.Mode() != 0444 {
		t.Errorf("Stat: got %v, %v, want mode 0444", fi, err)
	}
	f, err := os.Open(dir + "/file")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	_, err = f.Read(make([]byte, 1))
	checkErr(t, "Read", err, errors.ErrUnsupported)
	checkErr(t, "Sync", f.Sync(), errors.ErrUnsupported)
	checkErr(t, "Mkdir", os.Mkdir(dir+"/d", 0755), errors.ErrUnsupported)
	checkErr(t, "Remove", os.Remove(dir+"/file"), errors.ErrUnsupported)
	checkErr(t, "Rename", os.Rename(dir+"/file", dir+"/g"), errors.ErrUnsupported)
	_, err = os.ReadDir(dir)
	checkErr(t, "ReadDir", err, errors.ErrUnsupported)
}

func TestMountUnlocked(t *testing.T) {
	fsys := newMemFS()
	dir := mountFS(t, fsys)
	fsys.stat = func() {
		// A call of the in-memory file system from a mounted file
		// system must not wait for a lock.
		var st syscall.Stat_t
		if err := syscall.Stat("/dev/null", &st); err != nil {
			t.Error(err)
		}
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatal(err)
	}
}
