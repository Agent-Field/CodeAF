package appwire

import (
	"context"
	"encoding/json"
	"testing"
)

type folderEngine struct {
	factoryEngine
	got []string
}

func (f *folderEngine) FactorySetupFolderCheck(_ context.Context, caller, folder string) (FactorySetupFolder, error) {
	f.got = append(f.got, caller+" "+folder)
	return FactorySetupFolder{Folder: folder, Checked: "~/code", Writable: true, FreeBytes: 5 << 30}, nil
}

type onlyFolderChecker struct{ fakeEngine }

func (*onlyFolderChecker) FactorySetupFolderCheck(context.Context, string, string) (FactorySetupFolder, error) {
	return FactorySetupFolder{}, nil
}

func TestFactorySetupFolderCheckIsRegisteredAnnouncedAndChecked(t *testing.T) {
	const name = "factory.setup_folder_check"
	good := `{"caller":"w1","folder":" ~/code/api "}`
	for _, e := range []Engine{&fakeEngine{}, &factoryEngine{}, &onlyFolderChecker{}} {
		if _, err := methods[name].call(e, json.RawMessage(good)); wireCode(t, err) != CodeUnknownMethod {
			t.Errorf("%T: %v", e, err)
		}
		if h := withFactorySetupFolderCheckFeature(Hello{}, e); len(h.Features) != 0 {
			t.Errorf("%T announced %v", e, h.Features)
		}
	}
	e := &folderEngine{}
	out, err := methods[name].call(e, json.RawMessage(good))
	if err != nil {
		t.Fatal(err)
	}
	if len(e.got) != 1 || e.got[0] != "w1 ~/code/api" {
		t.Fatalf("handed %v", e.got)
	}
	if raw, _ := json.Marshal(out); string(raw) != `{"folder":"~/code/api","exists":false,"checked":"~/code","writable":true,"free_bytes":5368709120}` {
		t.Fatalf("answer %s", raw)
	}
	if h := withFactorySetupFolderCheckFeature(Hello{}, e); len(h.Features) != 1 || h.Features[0] != FeatureFactorySetupFolderCheck || FeatureFactorySetupFolderCheck != "factory.setup_folder_check" {
		t.Fatalf("announced %v", h.Features)
	}
	for _, bad := range []string{`{"caller":"w1"}`, `{"folder":"/x"}`, `{"caller":"w1","folder":"code/api"}`, `{"caller":"w1","folder":"/x\ny"}`, `{"caller":"w1","folder":"/x","repo":"a/b"}`} {
		if _, err := methods[name].call(e, json.RawMessage(bad)); wireCode(t, err) != CodeBadParams {
			t.Errorf("%s: %v", bad, err)
		}
	}
	if len(e.got) != 1 {
		t.Fatalf("a refused request reached the engine: %v", e.got)
	}
}

// A setup folder is a full path on either kind of machine, or one under the
// person's home: Windows drive and share paths pass, a drive-relative or
// relative one does not.
func TestSetupFolderTakesFullPathsOfEveryPlatform(t *testing.T) {
	for _, ok := range []string{"/work/api", "~/code/api", `~\code\api`, `C:\code\api`, "C:/code/api", `d:\api`, `\\server\share\repo`, "//server/share/repo", `\\server\share`} {
		if got, err := setupFolder(" " + ok + " "); err != nil || got != ok {
			t.Errorf("%q: %q %v", ok, got, err)
		}
	}
	for _, bad := range []string{"C:repo", "C:", "code/api", `code\api`, "~other/api", "~", `\code\api`, `\\server`, `\\server\`, `\\`} {
		if _, err := setupFolder(bad); err == nil {
			t.Errorf("%q was taken", bad)
		}
	}
}
