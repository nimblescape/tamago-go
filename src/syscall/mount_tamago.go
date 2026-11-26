// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// File systems that a program mounts on a directory of the in-memory file
// system of tamago (see fs_tamago.go).

package syscall

import "sync"

// FS is a file system that a program mounts with [Mount].
//
// A name is relative to the directory of the mount. Its elements are
// separated by single slashes, and it has no "." or ".." element and no
// slash at the start or the end. The name "." is the directory itself.
//
// An FS can also have these methods, for the functions of the same names in
// this package:
//
//	Stat(name string, st *Stat_t) error
//	Mkdir(name string, perm uint32) error
//	Unlink(name string) error
//	Rmdir(name string) error
//	Rename(from, to string) error
//
// Errors are [Errno] values, as the corresponding Linux system calls return
// them.
type FS interface {
	// Open opens the named file with the flags of open(2), such as
	// O_CREATE, O_EXCL, O_TRUNC and O_DIRECTORY. A new file gets the
	// permission bits perm.
	Open(name string, flag int, perm uint32) (File, error)
}

// File is a file that is open in a file system that a program mounted with
// [Mount]. It can also have these methods:
//
//	Pread(b []byte, offset int64) (int, error)
//	Pwrite(b []byte, offset int64) (int, error)
//	ReadDir() ([]string, error)
//	Truncate(size int64) error
//	Sync() error
//
// Pread of a directory returns EISDIR. ReadDir returns the names in the
// directory, without "." and "..".
type File interface {
	// Stat returns the status of the file.
	Stat(st *Stat_t) error

	// Close closes the file when its last file descriptor is closed.
	Close() error
}

// The optional methods of FS and File.
type (
	statFS    interface{ Stat(string, *Stat_t) error }
	mkdirFS   interface{ Mkdir(string, uint32) error }
	unlinkFS  interface{ Unlink(string) error }
	rmdirFS   interface{ Rmdir(string) error }
	renameFS  interface{ Rename(from, to string) error }
	preadFile interface {
		Pread([]byte, int64) (int, error)
	}
	pwriteFile interface {
		Pwrite([]byte, int64) (int, error)
	}
	readDirFile  interface{ ReadDir() ([]string, error) }
	truncateFile interface{ Truncate(int64) error }
	syncFile     interface{ Sync() error }
)

// Mount mounts the file system fsys on the directory dir. From then on, the
// file calls for dir and for the paths below it go to fsys, with names
// relative to dir. All other paths stay in the in-memory file system. A file
// system stays mounted until the program exits.
//
// This package keeps the path lookup, the current directory and the file
// descriptors for fsys. It resolves "." and ".." in fsys by the name, and
// ".." in dir leads to the parent of dir. It gives the files of fsys their
// own Stat_t.Dev, and it calls fsys and its files without its lock, so that
// they can block and use other files.
//
// A file call returns ENOSYS, which matches errors.ErrUnsupported, if fsys or
// the file does not have the method that the call needs. Only Stat then uses
// Open and the Stat method of the file. Link, Chmod, Fchmod, Chown, Fchown,
// Lchown and UtimesNano always return ENOSYS in fsys. As on Linux, Rename and
// Link between fsys and another file system return EXDEV, and Rmdir and
// Rename of dir return EBUSY.
//
// Mount returns EINVAL if fsys is nil, ENOTDIR if dir is not a directory,
// and EBUSY if a file system is mounted on dir or dir lies in one.
func Mount(dir string, fsys FS) error {
	if fsys == nil {
		return EINVAL
	}
	fs.mu.Lock()
	defer fs.mu.Unlock()
	ip, _, err := fs.namei(dir, false)
	if err != nil {
		return err
	}
	if ip.mnt != nil {
		return EBUSY
	}
	if ip.Mode&S_IFMT != S_IFDIR {
		return ENOTDIR
	}
	fs.mounts++
	ip.mnt = &mount{fsys: fsys, dir: ip, dev: fs.mounts}
	return nil
}

// A mount is a file system that is mounted on a directory. Its methods
// expect fs.mu to be locked, and release it while they call the file system,
// as preadLocked does for a device.
type mount struct {
	fsys FS
	dir  *inode // directory that fsys is mounted on
	dev  int64  // Stat_t.Dev of the files of fsys
}

// mountwalk resolves the path element elem in the file system mounted on ip,
// from the directory name. It resolves "." and ".." by the name, and ".." at
// the root of the file system leads to the parent of ip.
func (fs *fsys) mountwalk(ip *inode, name, elem string) (*inode, string, error) {
	switch {
	case elem == ".":
	case elem == ".." && name == ".":
		de, _, err := fs.dirlookup(ip, "..")
		if err != nil {
			return nil, "", err
		}
		ip, name = de.inode, "."
	case elem == "..":
		i := len(name) - 1
		for i >= 0 && name[i] != '/' {
			i--
		}
		if i < 0 {
			name = "."
		} else {
			name = name[:i]
		}
	case len(elem) >= 256:
		return nil, "", ENAMETOOLONG
	case name == ".":
		name = elem
	default:
		name += "/" + elem
	}
	return ip, name, nil
}

// dotelem reports whether the last element of path is "." or "..", or path
// names the root.
func dotelem(path string) bool {
	for len(path) > 0 && path[len(path)-1] == '/' {
		path = path[:len(path)-1]
	}
	i := len(path)
	for i > 0 && path[i-1] != '/' {
		i--
	}
	elem := path[i:]
	return elem == "" || elem == "." || elem == ".."
}

// dirpath reports whether path can only name a directory, because it ends in
// a slash or in a "." or ".." element.
func dirpath(path string) bool {
	return path != "" && path[len(path)-1] == '/' || dotelem(path)
}

// open opens the named file for fsys.open. For a directory mode, it creates
// the directory and returns no file. dir reports whether the path can only
// name a directory.
func (m *mount) open(name string, openmode int, mode uint32, dir bool) (fileImpl, error) {
	perm := mode &^ S_IFMT
	switch mode & S_IFMT {
	case S_IFREG:
	case S_IFDIR:
		fsys, ok := m.fsys.(mkdirFS)
		if !ok {
			return nil, ENOSYS
		}
		fs.mu.Unlock()
		defer fs.mu.Lock()
		return nil, fsys.Mkdir(name, perm)
	default:
		return nil, EPERM // a special file, see MkDev
	}
	if dir {
		if openmode&O_CREATE != 0 {
			return nil, EISDIR
		}
		openmode |= O_DIRECTORY
	}
	fs.mu.Unlock()
	defer fs.mu.Lock()
	f, err := m.fsys.Open(name, openmode, perm)
	if err != nil {
		return nil, err
	}
	return &mountFile{mnt: m, name: name, file: f, openmode: openmode}, nil
}

// stat returns the status of the named file. If dir is true, the file must
// be a directory.
func (m *mount) stat(name string, dir bool, st *Stat_t) error {
	fs.mu.Unlock()
	defer fs.mu.Lock()
	if err := m.statName(name, st); err != nil {
		return err
	}
	if dir && st.Mode&S_IFMT != S_IFDIR {
		return ENOTDIR
	}
	st.Dev = m.dev
	return nil
}

// statName returns the status of the named file. Without the Stat method of
// the file system, it opens the file. It expects fs.mu to be released.
func (m *mount) statName(name string, st *Stat_t) error {
	if fsys, ok := m.fsys.(statFS); ok {
		return fsys.Stat(name, st)
	}
	f, err := m.fsys.Open(name, O_RDONLY, 0)
	if err != nil {
		return err
	}
	err = f.Stat(st)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// isDir reports whether the named file is a directory. It expects fs.mu to
// be released.
func (m *mount) isDir(name string) (bool, error) {
	var st Stat_t
	if err := m.statName(name, &st); err != nil {
		return false, err
	}
	return st.Mode&S_IFMT == S_IFDIR, nil
}

// unlink removes the named file, or the named directory if isdir is true.
// dir reports whether the path can only name a directory.
func (m *mount) unlink(name string, isdir, dir bool) error {
	fs.mu.Unlock()
	defer fs.mu.Lock()
	switch {
	case isdir:
		fsys, ok := m.fsys.(rmdirFS)
		if !ok {
			return ENOSYS
		}
		return fsys.Rmdir(name)
	case dir:
		// As on Linux, unlink never removes a file through a path that
		// can only name a directory.
		d, err := m.isDir(name)
		if err != nil {
			return err
		}
		if d {
			return EISDIR
		}
		return ENOTDIR
	}
	fsys, ok := m.fsys.(unlinkFS)
	if !ok {
		return ENOSYS
	}
	return fsys.Unlink(name)
}

// rename moves the file from to the name to. dir reports whether one of the
// paths can only name a directory, so that from must be one.
func (m *mount) rename(from, to string, dir bool) error {
	fsys, ok := m.fsys.(renameFS)
	if !ok {
		return ENOSYS
	}
	fs.mu.Unlock()
	defer fs.mu.Lock()
	if dir {
		d, err := m.isDir(from)
		if err != nil {
			return err
		}
		if !d {
			return ENOTDIR
		}
	}
	return fsys.Rename(from, to)
}

// truncate changes the size of the named file, which it opens for writing as
// Linux requires.
func (m *mount) truncate(name string, dir bool, length int64) error {
	if length < 0 {
		return EINVAL
	}
	f, err := m.open(name, O_WRONLY, S_IFREG, dir)
	if err != nil {
		return err
	}
	fs.mu.Unlock()
	defer fs.mu.Lock()
	mf := f.(*mountFile)
	err = mf.truncate(length)
	if cerr := mf.close(); err == nil {
		err = cerr
	}
	return err
}

// chdir makes the named directory the current directory.
func (m *mount) chdir(name string) error {
	var st Stat_t
	if err := m.stat(name, true, &st); err != nil {
		return err
	}
	fs.cwd, fs.cwdName = m.dir, name
	return nil
}

// A mountFile is the fileImpl of a file that is open in a mounted file
// system.
type mountFile struct {
	mnt      *mount
	name     string // name of the file in mnt
	file     File
	openmode int

	mu     sync.Mutex // protects offset and names
	offset int64
	names  []string // names of the directory, for readDirent
}

// fdToMountFile returns the file of fd if it is open in a mounted file
// system.
func fdToMountFile(fd int) (*mountFile, bool) {
	f, err := fdToFile(fd)
	if err != nil {
		return nil, false
	}
	mf, ok := f.impl.(*mountFile)
	return mf, ok
}

func (f *mountFile) stat(st *Stat_t) error {
	if err := f.file.Stat(st); err != nil {
		return err
	}
	st.Dev = f.mnt.dev
	return nil
}

func (f *mountFile) read(b []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n, err := f.pread(b, f.offset)
	f.offset += int64(n)
	return n, err
}

func (f *mountFile) write(b []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.openmode&O_APPEND != 0 {
		var st Stat_t
		if err := f.file.Stat(&st); err != nil {
			return 0, err
		}
		f.offset = st.Size
	}
	n, err := f.pwrite(b, f.offset)
	f.offset += int64(n)
	return n, err
}

func (f *mountFile) seek(offset int64, whence int) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch whence {
	case 0: // SEEK_SET
	case 1: // SEEK_CUR
		offset += f.offset
	case 2: // SEEK_END
		var st Stat_t
		if err := f.file.Stat(&st); err != nil {
			return 0, err
		}
		offset += st.Size
	default:
		return 0, EINVAL
	}
	if offset < 0 {
		return 0, EINVAL
	}
	f.offset = offset
	return offset, nil
}

func (f *mountFile) pread(b []byte, offset int64) (int, error) {
	if f.openmode&O_ACCMODE == O_WRONLY {
		return 0, EBADF
	}
	if offset < 0 {
		return 0, EINVAL
	}
	r, ok := f.file.(preadFile)
	if !ok {
		return 0, ENOSYS
	}
	return r.Pread(b, offset)
}

func (f *mountFile) pwrite(b []byte, offset int64) (int, error) {
	if f.openmode&O_ACCMODE == O_RDONLY {
		return 0, EBADF
	}
	if offset < 0 {
		return 0, EINVAL
	}
	w, ok := f.file.(pwriteFile)
	if !ok {
		return 0, ENOSYS
	}
	return w.Pwrite(b, offset)
}

func (f *mountFile) close() error {
	return f.file.Close()
}

// readDirent returns the names of the directory as Dirent records, from the
// offset of f. It reads the names from the file system when it starts at
// offset 0.
func (f *mountFile) readDirent(buf []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.offset%direntSize != 0 || len(buf) != 0 && len(buf) < direntSize {
		return 0, EINVAL
	}
	if f.offset == 0 || f.names == nil {
		d, ok := f.file.(readDirFile)
		if !ok {
			return 0, ENOSYS
		}
		names, err := d.ReadDir()
		if err != nil {
			return 0, err
		}
		f.names = names
	}
	n := 0
	for i := f.offset / direntSize; i < int64(len(f.names)) && len(buf)-n >= direntSize; i++ {
		// The inode number is not known here, and direntIno does
		// not read it.
		putDirent(buf[n:], 0, f.offset, f.names[i])
		n += direntSize
		f.offset += direntSize
	}
	return n, nil
}

// truncate changes the size of the file, which must be open for writing.
func (f *mountFile) truncate(length int64) error {
	if f.openmode&O_ACCMODE == O_RDONLY || length < 0 {
		return EINVAL
	}
	t, ok := f.file.(truncateFile)
	if !ok {
		return ENOSYS
	}
	return t.Truncate(length)
}

// sync writes the file to its storage.
func (f *mountFile) sync() error {
	s, ok := f.file.(syncFile)
	if !ok {
		return ENOSYS
	}
	return s.Sync()
}

// chdir makes the file, which must be a directory, the current directory.
func (f *mountFile) chdir() error {
	var st Stat_t
	if err := f.stat(&st); err != nil {
		return err
	}
	if st.Mode&S_IFMT != S_IFDIR {
		return ENOTDIR
	}
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.cwd, fs.cwdName = f.mnt.dir, f.name
	return nil
}
