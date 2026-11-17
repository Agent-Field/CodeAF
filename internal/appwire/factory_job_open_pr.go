package appwire

import (
	"context"
	"encoding/json"
)

// FeatureFactoryJobOpenPR announces "Open a pull request" on a finished
// folder job: the job's branch pushed to its folder's GitHub remote and a
// draft pull request opened from it. Whether ONE job may is its can.open_pr.
const FeatureFactoryJobOpenPR = "factory.job_open_pr"

// JobActOpenPR names a pull request opened from a folder job.
const JobActOpenPR = "open_pr"

// FactoryFolderPROpener opens a folder job's pull request once, as the caller.
type FactoryFolderPROpener interface {
	FactoryJobOpenPR(context.Context, FactoryJobActionParams) (FactoryJobActed, error)
}

func withFactoryJobOpenPRFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryFolderPROpener); ok {
		hello = withFeature(hello, FeatureFactoryJobOpenPR)
	}
	return hello
}

func init() {
	methods[FeatureFactoryJobOpenPR] = shaped[FactoryJobActionParams, FactoryJobActed](openFolderPR)
}

func openFolderPR(e Engine, raw json.RawMessage) (*FactoryJobActed, error) {
	o, ok := e.(FactoryFolderPROpener)
	if _, isFactory := e.(Factory); !ok || !isFactory {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", FeatureFactoryJobOpenPR)
	}
	var p FactoryJobActionParams
	if err := decodeStrict(FeatureFactoryJobOpenPR, raw, &p); err != nil {
		return nil, err
	}
	if err := checkJob(&p.Factory, p.Job, p.Expect); err != nil {
		return nil, err
	}
	ctx, cancel := factoryCall()
	defer cancel()
	out, err := o.FactoryJobOpenPR(ctx, p)
	if err != nil {
		return nil, err
	}
	return &out, nil
}
