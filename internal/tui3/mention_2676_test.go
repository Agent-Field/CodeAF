package tui3

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// TestHostedConversationAtFileFindsWorkspaceFiles2676 verifies that @file: and
// /export path completion in a local hosted conversation walk the conversation's
// workspace and offer hello.txt (including after /atlas word-argument completion
// or a workspace switch), while preserving --host's local-root walk and Home's
// @file: completion.
func TestHostedConversationAtFileFindsWorkspaceFiles2676(t *testing.T) {
	t.Run("hosted_conversation_direct_and_after_atlas", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte("hello\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		a := newApp(t.Context(), Options{
			Agent:      &fakeAgent{model: "m"},
			EngineRoad: true,
			Workspace:  root,
			ProfileDir: t.TempDir(),
		})
		a.width, a.height = 100, 28
		a.pal = newPalette(tokens.ANSI256, false)
		a.entries = nil
		a.touch()

		// Completing /atlas word arguments must not mark the workspace file walk
		// as loaded when no walk has run yet.
		typeInto(t, a, "/atlas ")
		drive(t, a, key("tab"))
		if a.comp.loaded {
			t.Fatal("/atlas word-argument completion marked the workspace file walk loaded before walkFiles ran")
		}

		// Clear the composer and type @file: in the hosted conversation.
		a.input.setText("")
		a.closeLists()
		typeInto(t, a, "@file:")
		if !a.comp.open || !a.comp.loaded {
			t.Fatalf("@file: open=%v loaded=%v, want both true", a.comp.open, a.comp.loaded)
		}
		got := plain(strings.Join(a.overlayRows(a.width, a.overlayHeight()), "\n"))
		if strings.Contains(got, "no file matches") {
			t.Fatalf("@file: in hosted conversation showed no file matches:\n%s", got)
		}
		if !strings.Contains(got, "hello.txt") {
			t.Fatalf("@file: in hosted conversation did not offer hello.txt:\n%s", got)
		}
		if choice, ok := a.comp.choice(); !ok || choice != "hello.txt" {
			t.Fatalf("@file: choice = %q, %v; want hello.txt, true", choice, ok)
		}

		// /export followed by Tab must also offer hello.txt from the workspace walk
		// rather than keeping stale word candidates or reporting no matches.
		a.input.setText("")
		a.closeLists()
		typeInto(t, a, "/export ")
		drive(t, a, key("tab"))
		exportRows := plain(strings.Join(a.overlayRows(a.width, a.overlayHeight()), "\n"))
		if strings.Contains(exportRows, "no matches") || !strings.Contains(exportRows, "hello.txt") {
			t.Fatalf("/export + Tab in hosted conversation = %q, want hello.txt", exportRows)
		}

		// Moving the conversation to another workspace re-walks the new workspace.
		other := t.TempDir()
		if err := os.WriteFile(filepath.Join(other, "second.txt"), []byte("two\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		a.input.setText("")
		a.closeLists()
		a.workspace = other
		typeInto(t, a, "@file:")
		switched := plain(strings.Join(a.overlayRows(a.width, a.overlayHeight()), "\n"))
		if !strings.Contains(switched, "second.txt") || strings.Contains(switched, "hello.txt") {
			t.Fatalf("@file: after workspace change = %q, want second.txt only", switched)
		}
	})

	t.Run("remote_host_control_and_home_control", func(t *testing.T) {
		a, localDir := atHome(t)
		if err := os.WriteFile(filepath.Join(localDir, "hello.txt"), []byte("hello\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		a.width = 200
		typeInto(t, a, "@file:")
		if text := homeText(a); !strings.Contains(text, "hello.txt") || strings.Contains(text, "no file matches") {
			t.Fatalf("Home @file: did not offer hello.txt:\n%s", text)
		}

		// Over --host, the conversation walks the local root this process can open.
		remoteApp, _ := hostLab(t)
		remoteApp.localRoot = localDir
		remoteApp.width, remoteApp.height = 100, 28
		typeInto(t, remoteApp, "@file:")
		got := plain(strings.Join(remoteApp.overlayRows(remoteApp.width, remoteApp.overlayHeight()), "\n"))
		if !strings.Contains(got, "hello.txt") || strings.Contains(got, "no file matches") {
			t.Fatalf("--host @file: did not offer local hello.txt:\n%s", got)
		}
	})
}
