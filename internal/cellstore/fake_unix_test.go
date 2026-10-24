//go:build !windows

package cellstore

import "testing"

func fakeNativeEngine(t *testing.T, help string) string {
	t.Helper()
	t.Fatal("native Windows fixture on Unix")
	return ""
}
