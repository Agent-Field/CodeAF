package opener

import "testing"

// Contract 5: Windows hands both URL and file targets to its desktop opener.
func TestWindowsOpenerUsesFileProtocolHandler(t *testing.T) {
	name, args := Command()
	if name != "rundll32.exe" || len(args) != 1 || args[0] != "url.dll,FileProtocolHandler" {
		t.Fatalf("Windows opener = %q %v", name, args)
	}
	if err := Start(" "); err == nil {
		t.Fatal("blank target accepted")
	}
}
