package github

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
)

// The Connectors page's doors keep exactly what the factory reads, each way,
// and forgetting is forgetting for both (connection.go).
func TestTheConnectorsDoorsKeepWhatTheFactoryReads(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	ctx := context.Background()
	profile := t.TempDir()

	if err := KeepToken(profile, "pasted"); err != nil {
		t.Fatal(err)
	}
	if token, via := TokenAt(ctx, profile); token != "pasted" || via != factory.ViaToken {
		t.Fatalf("after a paste the factory reads %q via %q", token, via)
	}

	fakeGH(t, "echo gh-account")
	token, err := KeepGH(ctx, profile)
	if err != nil || token != "gh-account" {
		t.Fatalf("gh: %q %v", token, err)
	}
	if token, via := TokenAt(ctx, profile); token != "gh-account" || via != factory.ViaGH {
		t.Fatalf("after gh the factory reads %q via %q", token, via)
	}
	if config.GitHubTokenAt(profile) != "" {
		t.Fatal("the yes to gh left the pasted token beside it")
	}

	if err := Forget(profile); err != nil {
		t.Fatal(err)
	}
	if token, _ := TokenAt(ctx, profile); token != "" {
		t.Fatalf("after a disconnect the factory still reads %q", token)
	}

	fakeGH(t, "exit 1")
	if _, err := KeepGH(ctx, profile); err == nil {
		t.Fatal("kept a yes to a gh that holds no token")
	}
}

func TestADisconnectSaysWhenTheShellStillHoldsAToken(t *testing.T) {
	t.Setenv("GH_TOKEN", "shell-token")
	t.Setenv("GITHUB_TOKEN", "")
	err := Forget(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "$GH_TOKEN") {
		t.Fatalf("%v", err)
	}
}

func TestInTheAppTheTokenIsTheAppsToKeepAndForget(t *testing.T) {
	t.Setenv("GH_TOKEN", "shell-token")
	profile := t.TempDir()
	keys := config.NewAppCredentials()
	var told []string
	keys.OnKept(func(provider, key string) { told = append(told, provider+"="+key) })
	defer config.UseAppCredentials(keys)()

	if err := KeepToken(profile, "device-token"); err != nil {
		t.Fatal(err)
	}
	if token, _ := TokenAt(context.Background(), profile); token != "device-token" {
		t.Fatalf("the factory reads %q", token)
	}
	if err := Forget(profile); err != nil {
		t.Fatal(err)
	}
	// THE REMOVAL WINS OVER THE SHELL in the app, as the handoff law says.
	if token, _ := TokenAt(context.Background(), profile); token != "" {
		t.Fatalf("after a disconnect the factory reads %q", token)
	}
	if strings.Join(told, ",") != "github=device-token,github=" {
		t.Fatalf("the app was told %v", told)
	}
}
