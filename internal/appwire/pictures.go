package appwire

import (
	"path/filepath"
	"strings"
	"unicode"
)

// THE PICTURE MAKER — which model makes pictures, at two reaches.
//
// pictures.makers lists every model the catalogue says draws, with what a
// person needs to choose one (a price per picture, whether it reads a photo,
// how many pictures one request returns, the shapes it draws, a transparent
// background) and why one cannot be used now; and it says what is chosen
// Everywhere, what is chosen for one canvas, and which model a picture made
// there would use right now and why. pictures.maker.set chooses at one reach;
// an empty model is Automatic.
//
// A CANVAS IS NAMED BY ITS WORKING FOLDER, the folder its agents run in (the
// desk's canvas_work_folder), because that is the one thing about a canvas the
// engine and the desk both know. A picture made by an agent working in that
// folder, or in a folder inside it, follows the canvas's choice.

// FeaturePictureMakers is the hello's word for an engine that serves
// pictures.makers and pictures.maker.set, whose renders carry picked and
// passed_over, and whose generate_image asks before swapping a chosen model.
const FeaturePictureMakers = "pictures.makers"

// The reaches a picture maker can be chosen at.
const (
	PictureScopeEverywhere = "everywhere"
	PictureScopeCanvas     = "canvas"
)

// PictureMakerKeeper is an engine that serves the picture maker. It is
// asserted, as [TaskModelsLister] is, so an engine without it answers
// unknown_method.
type PictureMakerKeeper interface {
	PictureMakers(workspace string) (PictureMakers, error)
	SetPictureMaker(p PictureMakerSetParams) (PictureMakers, error)
}

// PictureMakersParams asks for the list. Workspace, when given, is a canvas's
// working folder, and the answer then carries that canvas's choice and the
// route a picture made there would take.
type PictureMakersParams struct {
	Workspace string `json:"workspace,omitempty"`
}

// PictureMakerSetParams chooses the picture maker at one reach. Model empty is
// Automatic: Everywhere goes back to the engine's ladder, a canvas goes back
// to following Everywhere. Workspace is required for the canvas reach.
type PictureMakerSetParams struct {
	Scope     string `json:"scope" enum:"everywhere,canvas"`
	Workspace string `json:"workspace,omitempty"`
	Model     string `json:"model"`
}

// PictureMakers is both methods' answer.
type PictureMakers struct {
	// Makers is every model the catalogue says draws, sorted by name. Empty
	// when no connected provider makes pictures (Resolved says why).
	Makers []PictureMaker `json:"makers"`
	// Everywhere is the choice for every picture; Canvas is the asked canvas's
	// own, present only when the request named a workspace.
	Everywhere PictureMakerChoice  `json:"everywhere"`
	Canvas     *PictureMakerChoice `json:"canvas,omitempty"`
	// Resolved is the model a picture made at the asked reach would use now,
	// with no model of its own named and no photo to start from.
	Resolved PictureMakerRoute `json:"resolved"`
}

// PictureMaker is one model that draws. Every capability is a published fact:
// false or zero means the listing did not say, never "known not to".
type PictureMaker struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// PricePerPictureUSD is one picture at the model's default size; null
	// when no price was published. PriceUnit is what was priced: per image or
	// request as published, per megapixel or per output token converted at a
	// 1K picture (PriceEstimated).
	PricePerPictureUSD *float64 `json:"price_per_picture_usd"`
	PriceUnit          string   `json:"price_unit,omitempty" enum:"image,request,megapixel,token"`
	PriceEstimated     bool     `json:"price_estimated,omitempty"`
	// ReadsPhotos: it can change or start from a picture. MasksEdits: it
	// takes a real mask and changes only that area.
	ReadsPhotos bool `json:"reads_photos"`
	MasksEdits  bool `json:"masks_edits"`
	// MaxPictures is how many pictures one request returns, absent when
	// unpublished (more are asked for as several requests). MaxPhotos is how
	// many pictures it takes in.
	MaxPictures int `json:"max_pictures,omitempty"`
	MaxPhotos   int `json:"max_photos,omitempty"`
	// Shapes are the width:height frames it draws; empty when unpublished.
	Shapes      []string `json:"shapes"`
	Transparent bool     `json:"transparent"`
	// Automatic says Automatic may pick it (a published price, takes words).
	Automatic bool `json:"automatic"`
	// Unavailable is why it cannot make a picture now: no_prompt (it takes no
	// words) or not_listed (a chosen model no connected provider lists).
	// UnavailableReason is that in a person's words.
	Unavailable       string `json:"unavailable,omitempty" enum:"no_prompt,not_listed"`
	UnavailableReason string `json:"unavailable_reason,omitempty"`
}

// PictureMakerChoice is what is chosen at one reach.
type PictureMakerChoice struct {
	// Model is the chosen id; empty is Automatic (for a canvas: follows
	// Everywhere). Name is its person-facing name.
	Model string `json:"model"`
	Name  string `json:"name,omitempty"`
	// Folder is, for a canvas, the folder the choice is kept under (the asked
	// workspace or the folder above it that holds the choice).
	Folder string `json:"folder,omitempty"`
	// Variable is, for Everywhere, the environment variable that set the
	// choice and outranks the setting: the desk shows it locked and says where.
	Variable string `json:"variable,omitempty"`
}

// PictureMakerRoute is the model a picture would use now.
type PictureMakerRoute struct {
	// Model is empty when no picture can be made; Message then says why in a
	// sentence a person can act on.
	Model string `json:"model"`
	Name  string `json:"name,omitempty"`
	// Scope is where Model came from.
	Scope string `json:"scope" enum:"canvas,everywhere,pin,automatic"`
	// Reason is how Automatic chose (curated: a remembered default; listed:
	// the best priced model listed), configured for a choice, or why there is
	// no model.
	Reason  string `json:"reason" enum:"curated,listed,configured,none_advertised,unconfirmed,model_required,not_a_picture_maker,no_provider"`
	Message string `json:"message,omitempty"`
}

func init() {
	methods["pictures.makers"] = typed(func(e Engine, p PictureMakersParams) (*PictureMakers, error) {
		k, err := pictureMakerKeeper(e, "pictures.makers")
		if err != nil {
			return nil, err
		}
		workspace, err := pictureWorkspace(p.Workspace, false)
		if err != nil {
			return nil, err
		}
		got, err := k.PictureMakers(workspace)
		if err != nil {
			return nil, err
		}
		return &got, nil
	})
	methods["pictures.maker.set"] = typed(func(e Engine, p PictureMakerSetParams) (*PictureMakers, error) {
		k, err := pictureMakerKeeper(e, "pictures.maker.set")
		if err != nil {
			return nil, err
		}
		switch p.Scope {
		case PictureScopeEverywhere:
			p.Workspace = ""
		case PictureScopeCanvas:
			if p.Workspace, err = pictureWorkspace(p.Workspace, true); err != nil {
				return nil, err
			}
		default:
			return nil, Errorf(CodeBadParams, "scope is everywhere or canvas")
		}
		p.Model = strings.TrimSpace(p.Model)
		if len(p.Model) > 256 || strings.ContainsFunc(p.Model, unicode.IsSpace) {
			return nil, Errorf(CodeBadParams, "model is one model id, at most 256 bytes")
		}
		got, err := k.SetPictureMaker(p)
		if err != nil {
			return nil, err
		}
		return &got, nil
	})
}

// pictureWorkspace is a canvas folder as the engine keeps it: absolute and
// clean. Required says the method needs one.
func pictureWorkspace(raw string, required bool) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		if required {
			return "", Errorf(CodeBadParams, "a canvas is named by its working folder: send workspace")
		}
		return "", nil
	}
	if !filepath.IsAbs(raw) || len(raw) > 4096 || strings.ContainsFunc(raw, unicode.IsControl) {
		return "", Errorf(CodeBadParams, "workspace is the canvas's working folder, as an absolute path")
	}
	return filepath.Clean(raw), nil
}

func pictureMakerKeeper(e Engine, name string) (PictureMakerKeeper, error) {
	k, ok := e.(PictureMakerKeeper)
	if !ok {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
	}
	return k, nil
}

// withPictureMakersFeature is hello carrying pictures.makers when the engine
// serves it.
func withPictureMakersFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(PictureMakerKeeper); !ok {
		return hello
	}
	return withFeature(hello, FeaturePictureMakers)
}
