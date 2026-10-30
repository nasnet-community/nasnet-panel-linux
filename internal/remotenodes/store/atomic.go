// SPDX-License-Identifier: AGPL-3.0-or-later

// writeAtomic follows the same routine in the Telegram MTProto plugin
// (github.com/nasnet-community/mtproto-remote-plugin), which is MIT licensed:
//
//	MIT License
//
//	Copyright (c) 2026 NASNET Community
//
//	Permission is hereby granted, free of charge, to any person obtaining a copy
//	of this software and associated documentation files (the "Software"), to deal
//	in the Software without restriction, including without limitation the rights
//	to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
//	copies of the Software, and to permit persons to whom the Software is
//	furnished to do so, subject to the following conditions:
//
//	The above copyright notice and this permission notice shall be included in all
//	copies or substantial portions of the Software.
//
//	THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
//	IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
//	FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
//	AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
//	LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
//	OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
//	SOFTWARE.

package store

import (
	"os"
	"path/filepath"
)

// tempFile is the part of *os.File a save uses.
type tempFile interface {
	Name() string
	Chmod(os.FileMode) error
	Write([]byte) (int, error)
	Sync() error
	Close() error
}

// diskOps are the calls a save makes on the disk, so a test can make any one
// of them fail.
type diskOps struct {
	createTemp func(dir, pattern string) (tempFile, error)
	rename     func(oldpath, newpath string) error
	syncDir    func(dir string) error
}

func osOps() diskOps {
	return diskOps{
		createTemp: func(dir, pattern string) (tempFile, error) {
			f, err := os.CreateTemp(dir, pattern)
			if err != nil {
				return nil, err
			}
			return f, nil
		},
		rename:  os.Rename,
		syncDir: syncDir,
	}
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	err = d.Sync()
	if cerr := d.Close(); err == nil {
		err = cerr
	}
	return err
}

// writeAtomic leaves path holding what it held before or all of data, never a
// mix, even if the power goes in the middle. The data is on disk before the
// rename and the rename is on disk before we return.
func writeAtomic(ops diskOps, path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := ops.createTemp(dir, "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if err := writeTemp(tmp, data); err != nil {
		os.Remove(name)
		return err
	}
	if err := ops.rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	// The rename has happened, so the new file is the file. A directory that
	// can't be synced (some filesystems refuse) only risks losing the rename
	// to a power cut; failing here would leave memory behind the disk.
	_ = ops.syncDir(dir)
	return nil
}

// WriteFileAtomic saves data to path the way state.json is saved: the bytes
// are on disk before the rename and the rename is on disk before it returns,
// the file ends up 0600, and a failure leaves what was there. It is for the
// other files in the data folder that keep their own contents, such as the
// usage history, so they get the same care without a second copy of the
// routine.
func WriteFileAtomic(path string, data []byte) error {
	return writeAtomic(osOps(), path, data)
}

// writeTemp fills the file and closes it, whatever happens.
func writeTemp(f tempFile, data []byte) error {
	err := f.Chmod(0o600) // CreateTemp asks for 0600, but a strict umask can take bits away
	if err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}
