package main

// ── codeaf app: THE PICTURE MAKER ──────────────────────────────────────────
//
// pictures.makers and pictures.maker.set (internal/appwire's pictures.go). A
// THIN DOOR, as the task models door is: the list, the two reaches and the
// route are internal/config's picturemaker.go, and this file only finds the
// catalogue a conversation's pictures would be made from and turns config's
// answers into the wire's shapes.

import (
	"context"
	"strings"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// pictureNoProvider is the sentence for an app with no provider connected:
// the same words the desk's model chips already say for it.
const pictureNoProvider = "No model provider connected. Connect one in Settings → Models."

// pictureCatalog is the catalogue a new conversation's pictures are made from
// (chatv3.go's activeModels): the process's own for the default service, a
// direct service's own otherwise, released by the caller. makes is false when
// that service cannot make pictures at all. A nil catalogue with connected
// false is an app with no provider.
func (b *appBackend) pictureCatalog() (models *catalog.Catalog, connected, makes bool, release func(), err error) {
	release = func() {}
	if !b.providerConnected() {
		return nil, false, false, release, nil
	}
	proc, err := b.process()
	if err != nil {
		return nil, true, false, release, appEngineError(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), appModelsWait)
	defer cancel()
	if proc.Models != nil {
		proc.Models.Warmed(ctx)
	}
	settings := proc.launchSettings()
	chosen := v3TalkModel("", settings)
	active, _, lists := v3CatalogForModel(proc.lifetime(), settings, chosen, proc.Models)
	if active != nil && active != proc.Models {
		release = active.Close
	}
	if active != nil {
		active = active.SnapshotNow()
	}
	return active, true, lists, release, nil
}

// PictureMakers is pictures.makers.
func (b *appBackend) PictureMakers(workspace string) (appwire.PictureMakers, error) {
	models, connected, makes, release, err := b.pictureCatalog()
	defer release()
	if err != nil {
		return appwire.PictureMakers{}, err
	}
	return pictureMakersWire(models, config.ProfileDir(), workspace, connected, makes, appPicturePin(workspace)), nil
}

// SetPictureMaker is pictures.maker.set. A model the catalogue knows does not
// draw is refused; one it does not know is kept as written, because a person
// may name a model their provider serves and the listing does not show, and
// the list then says it is not listed.
func (b *appBackend) SetPictureMaker(p appwire.PictureMakerSetParams) (appwire.PictureMakers, error) {
	models, connected, makes, release, err := b.pictureCatalog()
	defer release()
	if err != nil {
		return appwire.PictureMakers{}, err
	}
	if p.Model != "" && models != nil {
		if _, known := models.Model(p.Model); known && !models.Supports(p.Model, "output", "image") {
			return appwire.PictureMakers{}, appwire.Errorf(appwire.CodeBadParams, "%s doesn't make pictures", config.PictureMakerName(models, p.Model))
		}
	}
	profile := config.ProfileDir()
	switch p.Scope {
	case appwire.PictureScopeCanvas:
		err = config.SetCanvasPictureMakerAt(profile, p.Workspace, p.Model)
	default:
		err = config.SetEverywherePictureMaker(profile, p.Model)
	}
	if err != nil {
		return appwire.PictureMakers{}, appEngineError(err)
	}
	return pictureMakersWire(models, profile, p.Workspace, connected, makes, appPicturePin(p.Workspace)), nil
}

// appPicturePin is the operator's image role pin as a conversation in
// workspace would read it; a pins row that cannot be read is no pin here,
// since the list must still draw.
func appPicturePin(workspace string) string {
	source, err := v3RolesSource(workspace, config.ProfileDir())
	if err != nil || source == nil {
		return ""
	}
	pin, _ := roles.Pinned(source, roles.RoleImageGen)
	return strings.TrimSpace(pin)
}

// pictureMakersWire is the whole answer from config's facts.
func pictureMakersWire(models *catalog.Catalog, profile, workspace string, connected, makes bool, pin string) appwire.PictureMakers {
	out := appwire.PictureMakers{Makers: []appwire.PictureMaker{}}
	everywhere, variable := config.EverywherePictureMaker(profile)
	out.Everywhere = appwire.PictureMakerChoice{Model: everywhere, Variable: variable}
	if everywhere != "" {
		out.Everywhere.Name = config.PictureMakerName(models, everywhere)
	}
	if workspace != "" {
		model, folder := config.CanvasPictureMakerAt(profile, workspace)
		choice := appwire.PictureMakerChoice{Model: model, Folder: folder}
		if model != "" {
			choice.Name = config.PictureMakerName(models, model)
		}
		out.Canvas = &choice
	}
	switch {
	case !connected:
		out.Resolved = appwire.PictureMakerRoute{Scope: config.PictureScopeAutomatic, Reason: "no_provider", Message: pictureNoProvider}
		return out
	case !makes:
		out.Resolved = appwire.PictureMakerRoute{Scope: config.PictureScopeAutomatic, Reason: config.PictureNoneAdvertised,
			Message: config.PictureRefusalSentence(config.PictureNoneAdvertised)}
		return out
	}
	listed := map[string]bool{}
	for _, maker := range config.PictureMakers(models) {
		listed[maker.ID] = true
		out.Makers = append(out.Makers, pictureMakerWire(maker))
	}
	// A chosen model the listing does not show is still a row, so the desk can
	// draw the choice and say why it may not work.
	for _, chosen := range []string{everywhere, chosenOf(out.Canvas)} {
		if chosen != "" && !listed[chosen] {
			listed[chosen] = true
			out.Makers = append(out.Makers, pictureMakerWire(config.PictureMakerOf(models, chosen)))
		}
	}
	route := config.ResolvePicture(models, profile, config.PictureAsk{Workspace: workspace, Pin: pin})
	out.Resolved = appwire.PictureMakerRoute{Model: route.Model, Scope: route.Scope, Reason: route.Reason, Message: route.Message}
	if route.Model != "" {
		out.Resolved.Name = route.Name(route.Model)
	}
	return out
}

func chosenOf(choice *appwire.PictureMakerChoice) string {
	if choice == nil {
		return ""
	}
	return choice.Model
}

// pictureMakerWire is one row in the wire's words.
func pictureMakerWire(maker config.PictureMaker) appwire.PictureMaker {
	row := appwire.PictureMaker{
		ID: maker.ID, Name: maker.Name,
		ReadsPhotos: maker.ReadsPhotos, MasksEdits: maker.MasksEdits,
		MaxPictures: maker.MaxPictures, MaxPhotos: maker.MaxPhotos,
		Shapes: append([]string{}, maker.Shapes...), Transparent: maker.Transparent,
		Automatic: maker.Automatic, Unavailable: maker.Unavailable,
	}
	if maker.Priced {
		price := maker.PricePerPicture
		row.PricePerPictureUSD = &price
		row.PriceUnit = maker.PriceUnit
		row.PriceEstimated = maker.Estimated
	}
	switch maker.Unavailable {
	case "no_prompt":
		row.UnavailableReason = maker.Name + " only changes a picture you give it; it can't start from words."
	case "not_listed":
		row.UnavailableReason = maker.Name + " isn't offered by a connected provider right now."
	}
	return row
}
