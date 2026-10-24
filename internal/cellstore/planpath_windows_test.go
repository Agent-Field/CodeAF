package cellstore

import "testing"

// TestU4CaptureCandidatesUseTheSameWindowsFolder proves U4/C9: drive and UNC
// extended-length aliases retain their relative scope, while outside paths
// remain outside rather than bypassing the screen.
func TestU4CaptureCandidatesUseTheSameWindowsFolder(t *testing.T) {
	for _, tc := range []struct{ root, candidate, want string }{
		{`C:\project`, `\\?\C:\project\src\config.py`, `src\config.py`},
		{`\\?\C:\project`, `C:\project\.env`, `.env`},
		{`\\server\share\project`, `\\?\UNC\server\share\project\.env`, `.env`},
		{`C:\project`, `\\?\C:\elsewhere\.env`, `..\elsewhere\.env`},
	} {
		got, err := planRelative(tc.root, tc.candidate)
		if err != nil || got != tc.want {
			t.Fatalf("relative %q in %q = %q, %v; want %q", tc.candidate, tc.root, got, err, tc.want)
		}
	}
}
