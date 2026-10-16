//go:build unix

package main

import (
	"io"
	"os"
	"strings"
	"syscall"
)

// THE CONTAINED OPEN: how checkout.diff reads an untracked file and how
// checkout.locate decides a path may be offered for Open and Reveal.
//
// EVERYTHING GOES THROUGH AN [os.Root] ANCHORED AT THE GIT TOPLEVEL, one
// component at a time, and nothing is resolved to a string and then opened. The
// checkout's owner can swap any name under it at any moment; EvalSymlinks and
// then open leaves a window in which the answer to the first is not the target
// of the second. Here every parent must be a real directory (a link component is
// refused, because git never lists a file through a linked folder), the leaf is
// opened O_NOFOLLOW and O_NONBLOCK, so a pipe swapped in cannot hold the read,
// and the opened descriptor must be the very file the leaf stat described.

// appOpenSeam and appReadSeam are tests' hooks: between the leaf's stat and its
// open, and between the open and the read.
var (
	appOpenSeam = func() {}
	appReadSeam = func(*os.File) {}
)

// appWalkContained is the leaf's Lstat when every parent of rel is a real
// directory inside root and the leaf exists; ok is false otherwise.
func appWalkContained(root *os.Root, rel string) (os.FileInfo, bool) {
	parts := strings.Split(rel, "/")
	for i := range parts[:len(parts)-1] {
		info, err := root.Lstat(strings.Join(parts[:i+1], "/"))
		if err != nil || !info.IsDir() {
			return nil, false
		}
	}
	leaf, err := root.Lstat(rel)
	if err != nil {
		return nil, false
	}
	return leaf, true
}

// appReachable says whether rel is an existing regular file, not a link,
// reached through real directories under top.
func appReachable(top, rel string) bool {
	root, err := os.OpenRoot(top)
	if err != nil {
		return false
	}
	defer root.Close()
	leaf, ok := appWalkContained(root, rel)
	return ok && leaf.Mode().IsRegular()
}

// appReadContained reads at most limit bytes of the regular file rel under top.
// reason is "" on success, "too_large", or "unreadable" for everything else (a
// link, a pipe, a folder, a swap, a change while read).
func appReadContained(top, rel string, limit int64) (data []byte, reason string) {
	root, err := os.OpenRoot(top)
	if err != nil {
		return nil, "unreadable"
	}
	defer root.Close()
	leaf, ok := appWalkContained(root, rel)
	if !ok || !leaf.Mode().IsRegular() {
		return nil, "unreadable"
	}
	if leaf.Size() > limit {
		return nil, "too_large"
	}
	appOpenSeam()
	file, err := root.OpenFile(rel, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, "unreadable"
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil || !before.Mode().IsRegular() || !os.SameFile(leaf, before) {
		return nil, "unreadable"
	}
	appReadSeam(file)
	data, err = io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, "unreadable"
	}
	if int64(len(data)) > limit {
		return nil, "too_large"
	}
	after, err := file.Stat()
	if err != nil || after.Size() != int64(len(data)) || !after.ModTime().Equal(before.ModTime()) {
		return nil, "unreadable"
	}
	return data, ""
}
