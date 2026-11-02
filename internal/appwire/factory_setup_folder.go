package appwire

import (
	"context"
	"encoding/json"
)

// FeatureFactorySetupFolderCheck is announced in hello when the engine
// answers factory.setup_folder_check.
const FeatureFactorySetupFolderCheck = "factory.setup_folder_check"

// FactorySetupFolderCheckParams names the folder a person chose for a
// checkout: an absolute path or one starting "~/".
type FactorySetupFolderCheckParams struct {
	Caller string `json:"caller"`
	Folder string `json:"folder"`
}

// FactorySetupFolder is what the folder check found, before anything is
// cloned. Folder and Checked are shown folders ("~/code/api"), never a
// private one. Exists says the chosen folder is already there; when it is
// not, Checked is the nearest folder above it that is, which is where a clone
// would first write. Writable and FreeBytes are measured at Checked: whether
// this account may create files there, and the bytes its disk still offers.
//
// Problem, when set, is one sentence saying what is wrong. A file standing
// where the folder or one above it should be ("~/notes.txt is a file, not a
// folder") or a folder on the way that cannot be looked into ("codeaf cannot
// look inside ~/private") means the folder cannot be used: Writable is false,
// FreeBytes 0, and Checked names that file or folder. On Windows it can also
// name a probe file the check could not remove; the measurements then stand.
type FactorySetupFolder struct {
	Folder    string `json:"folder"`
	Exists    bool   `json:"exists"`
	Checked   string `json:"checked"`
	Writable  bool   `json:"writable"`
	FreeBytes int64  `json:"free_bytes"`
	Problem   string `json:"problem,omitempty"`
}

// FactorySetupFolderChecker is the engine's half of
// factory.setup_folder_check. It makes no folder; on Windows it makes and
// removes one temporary probe file to learn whether the folder is writable.
type FactorySetupFolderChecker interface {
	FactorySetupFolderCheck(ctx context.Context, caller, folder string) (FactorySetupFolder, error)
}

func withFactorySetupFolderCheckFeature(hello Hello, e Engine) Hello {
	if _, err := folderCheckerOf(e); err == nil {
		hello = withFeature(hello, FeatureFactorySetupFolderCheck)
	}
	return hello
}

func folderCheckerOf(e Engine) (FactorySetupFolderChecker, error) {
	c, ok := e.(FactorySetupFolderChecker)
	if _, isFactory := e.(Factory); !ok || !isFactory {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve factory.setup_folder_check")
	}
	return c, nil
}

func factorySetupFolderCheck(e Engine, raw json.RawMessage) (*FactorySetupFolder, error) {
	c, err := folderCheckerOf(e)
	if err != nil {
		return nil, err
	}
	var p FactorySetupFolderCheckParams
	if err := decodeStrict("factory.setup_folder_check", raw, &p); err != nil {
		return nil, err
	}
	caller, err := factoryCaller(p.Caller)
	if err != nil {
		return nil, err
	}
	folder, err := setupFolder(p.Folder)
	if err != nil {
		return nil, err
	}
	ctx, cancel := factoryCall()
	defer cancel()
	out, err := c.FactorySetupFolderCheck(ctx, caller, folder)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func init() {
	methods["factory.setup_folder_check"] = shaped[FactorySetupFolderCheckParams, FactorySetupFolder](factorySetupFolderCheck)
}
