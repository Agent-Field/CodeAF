// The image command: the belt's generate_image hand, from a shell.
//
// One prompt in, one picture written where the caller says, its path on
// stdout — and the spend recorded the way the tool records it, one row in the
// usage ledger, so a run's books see a picture a script paid for exactly as
// they see one a conversation paid for. The road itself is
// [session]'s, shared with the tool: the same model ladder, the same
// references, the same refusals, the same accounting.
package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
)

func runImage(args []string) error {
	flags := commandFlags("image")
	out := flags.String("o", "", "where to write the picture; an existing file is overwritten")
	model := flags.String("model", "", "which image model for this one call — a name or fragment from the catalog, or 'best'")
	if err := parseCommandFlags(flags, reorder(flags, args)); err != nil {
		return err
	}

	positionals := flags.Args()
	if len(positionals) < 1 || strings.TrimSpace(strings.Join(positionals, " ")) == "" {
		return errors.New("what to draw — codeaf image \"PROMPT\" -o PATH")
	}
	if strings.TrimSpace(*out) == "" {
		return errors.New("where to write it — codeaf image \"PROMPT\" -o PATH")
	}

	settings, err := config.Load()
	if err != nil {
		return err
	}
	// The ledger write is queued, not synchronous; this process must outlive
	// the queue, or the row it just recorded dies with it.
	defer session.FlushUsage()

	// The same media pair a conversation is wired with: the client from the
	// profile's key, the default model and the picker from the catalog and the
	// role pins. A nil client is not an error here — it is the honest answer
	// that this install has no image model, and the refusal says so.
	mediaSettings := settings
	mediaSettings.Model = settings.Model
	client := v3MediaClient(mediaSettings)
	models := catalog.LoadLazy(context.Background(), catalog.Options{
		BaseURL: settings.BaseURL, APIKey: settings.APIKey, Dir: settings.ProfileDir,
	})
	source, err := v3RolesSource(mustWorkspace(), settings.ProfileDir)
	if err != nil {
		return err
	}
	defaultModel := v3MediaModel(models, settings.ProfileDir, source)("image")
	if client == nil || defaultModel == "" {
		return errors.New("this install has no image model — pick one with /model in the chat, or set an image slot in /settings")
	}

	said, failed := session.GenerateImage(context.Background(), session.ImageGen{
		Client:       client,
		DefaultModel: defaultModel,
		Pick:         v3MediaPick(models),
		Workspace:    mustWorkspace(),
		Directory:    session.ImagesDir(session.Place{}, mustWorkspace()),
		Account:      accountBilledUsage,
	}, session.GenerateImageArgs{
		Prompt: strings.Join(positionals, " "),
		Path:   *out,
		Model:  *model,
	})
	if failed {
		return errors.New(said)
	}
	fmt.Println(sessionPathOf(said))
	return nil
}

// sessionPathOf takes the path back off the tool result, which opens with it
// ("path — 1024×1024 png, 41KB, generated on model"): a script wants the file,
// and the file is the one thing the result carries whole.
func sessionPathOf(result string) string {
	if at := strings.Index(result, " — "); at > 0 {
		return result[:at]
	}
	return result
}

// accountBilledUsage writes the one usage row a billed call owns, the way the
// belt's [Agent.addAuxiliaryUsage] does for a picture or a document rung: the
// model, the tokens, the price, one call, and the workspace it was made
// against. The row is written the moment a call answers, judged answer or
// not, and [session.FlushUsage] waits for it before this process leaves.
func accountBilledUsage(model string, usage *ai.Usage) {
	if usage == nil {
		return
	}
	line := session.UsageLine{
		Model:     model,
		Calls:     1,
		Input:     usage.PromptTokens,
		Output:    usage.CompletionTokens,
		Workspace: mustWorkspace(),
	}
	if usage.Cost != nil {
		line.USD = *usage.Cost
	}
	session.RecordUsage(session.UsageLedgerPath(), line)
}
