//go:build !unix

package main

// Without the descriptor-relative opens this guarantee needs, a path is never
// read or offered: the honest answer is a refusal, not a best effort.
func appReachable(top, rel string) bool { return false }

func appReadContained(top, rel string, limit int64) ([]byte, string) { return nil, "unreadable" }
