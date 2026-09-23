//go:build !windows

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
	"strings"
	"unicode/utf16"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/calc"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/msgmodel"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/orclient"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/steploop"
	"github.com/Agent-Field/codeaf/internal/seniordev/session/compaction"
	"github.com/Agent-Field/codeaf/internal/seniordev/session/overflow"
	"github.com/Agent-Field/codeaf/internal/seniordev/util"
)

type seniorDevCompactionModels struct {
	resolver steploop.ModelResolver
}

func (models seniorDevCompactionModels) GetModel(
	ctx context.Context, providerID, modelID string,
) (compaction.Model, error) {
	resolved, err := models.resolver.Resolve(ctx, msgmodel.User{
		Model: msgmodel.UserModel{ProviderID: providerID, ModelID: modelID},
	})
	if err != nil {
		return compaction.Model{}, err
	}
	return compaction.Model{Message: resolved.Message, Overflow: resolved.Calc}, nil
}

func (seniorDevCompactionModels) GetProvider(
	context.Context, string,
) (compaction.ProviderInfo, error) {
	return compaction.ProviderInfo{}, nil
}

type seniorDevSummaryFactory struct {
	store  steploop.Store
	client steploop.LLMClient
}

func (factory seniorDevSummaryFactory) Create(
	_ context.Context,
	assistant *msgmodel.Assistant,
	_ string,
	model compaction.Model,
) (compaction.SummaryProcessor, error) {
	stepModel := steploop.Model{Message: model.Message, Calc: model.Overflow}
	return &seniorDevSummaryProcessor{
		processor: steploop.NewProcessor(steploop.ProcessorOptions{
			Store: factory.store, Assistant: *assistant, Model: stepModel,
		}),
		client: factory.client,
		model:  stepModel,
	}, nil
}

type seniorDevSummaryProcessor struct {
	processor *steploop.Processor
	client    steploop.LLMClient
	model     steploop.Model
}

func (processor *seniorDevSummaryProcessor) Process(
	ctx context.Context, request compaction.SummaryRequest,
) (steploop.Result, error) {
	params := processor.model.Request
	params.ModelID = processor.model.Message.ID
	params.Prompt = request.Messages
	params.Tools = nil
	params.ToolChoice = nil
	if params.MaxOutputTokens == nil {
		maximum := calc.MaxOutputTokens(processor.model.Calc)
		params.MaxOutputTokens = &maximum
	}
	stream, err := processor.client.Stream(ctx, params)
	if err != nil {
		stream = &steploop.SliceStream{Failure: err}
	}
	return processor.processor.Process(ctx, stream)
}

func (processor *seniorDevSummaryProcessor) Message() msgmodel.Assistant {
	return processor.processor.Message()
}

type seniorDevContextSizer struct {
	system func(context.Context) string
	tools  []steploop.ToolDefinition
}

func (sizer seniorDevContextSizer) EstimateContext(
	ctx context.Context, messages []msgmodel.WithParts, model compaction.Model,
) (float64, error) {
	projected, err := msgmodel.ToModelMessages(messages, model.Message, nil)
	if err != nil {
		return 0, err
	}
	tools := make([]orclient.Tool, 0, len(sizer.tools))
	for _, definition := range sizer.tools {
		tools = append(tools, definition.Provider)
	}
	system := ""
	if sizer.system != nil {
		system = sizer.system(ctx)
	}
	raw, err := json.Marshal(struct {
		System   string                  `json:"system,omitempty"`
		Messages []msgmodel.ModelMessage `json:"messages"`
		Tools    []orclient.Tool         `json:"tools,omitempty"`
	}{System: system, Messages: projected, Tools: tools})
	if err != nil {
		return 0, err
	}
	units := len(utf16.Encode([]rune(string(raw))))
	return math.Ceil(float64(units) / 4), nil
}

func newSeniorDevCompactionController(
	store steploop.Store,
	summaryClient steploop.LLMClient,
	resolver steploop.ModelResolver,
	workspace string,
	backend *modelAPIBackend,
	system func(context.Context) string,
	tools []steploop.ToolDefinition,
	decisions compaction.DecisionSink,
	sessionID string,
) compaction.Controller {
	service := compaction.NewService(compaction.Dependencies{
		Store: store,
		// The session's config, not the project's: under the window policy
		// a context-overflow rejection may have pinned this session's
		// capacity below the window (compaction_pin.go).
		Config: compaction.ConfigProviderFunc(func(context.Context) (overflow.Config, error) {
			return backend.overflowConfigFor(sessionID)
		}),
		Agents: compaction.AgentProviderFunc(func(
			context.Context, string,
		) (compaction.Agent, error) {
			return compaction.Agent{Name: "compaction"}, nil
		}),
		Provider: seniorDevCompactionModels{resolver: resolver},
		Processors: seniorDevSummaryFactory{
			store: store, client: summaryClient,
		},
		// Evidence is harvested deterministically by code: no second model call
		// is made per compaction boundary.
		Evidence:  compaction.FallbackEvidenceSelector{},
		Sizer:     seniorDevContextSizer{system: system, tools: tools},
		Decisions: decisions,
		Instance:  compaction.InstanceContext{Directory: workspace, Worktree: workspace},
		ChangedFiles: func(ctx context.Context) []string {
			return seniorDevChangedFiles(ctx, workspace)
		},
		NewID: func(prefix string) string {
			if prefix == "message" {
				prefix = "msg"
			} else if prefix == "part" {
				prefix = "prt"
			}
			return steploop.NewAscendingID(prefix)
		},
	})
	return compaction.Controller{Compaction: service}
}

// soloStartRef names the run's exact starting tree, written by the solo
// pipeline when the run begins (solo_finalize.go). It is what the changed-files
// record diffs against.
const soloStartRef = "refs/senior-dev/start"

const changedFilesMaxLines = 40

// seniorDevChangedFiles computes the changed-files record pinned beside every
// compaction summary: a diffstat of the working tree against the starting
// tree (tracked files, which eager-commit makes every file the model writes)
// plus the short status (untracked files, in-progress edits). Read-only, no
// diff contents, hard line cap. An unavailable git answers with nothing.
func seniorDevChangedFiles(ctx context.Context, workspace string) []string {
	if workspace == "" {
		return nil
	}
	git := func(args ...string) ([]string, bool) {
		argv := util.GitArgv(args...)
		command := exec.CommandContext(ctx, argv[0], argv[1:]...)
		command.Dir = workspace
		out, err := command.Output()
		if err != nil {
			return nil, false
		}
		lines := []string{}
		for _, line := range strings.Split(string(out), "\n") {
			if strings.TrimSpace(line) != "" {
				lines = append(lines, strings.TrimRight(line, " \t"))
			}
		}
		return lines, true
	}
	var record []string
	if _, ok := git("rev-parse", "--verify", "--quiet", soloStartRef+"^{commit}"); ok {
		if stat, ok := git("diff", "--stat=100", soloStartRef, "--"); ok {
			record = append(record, "Against the starting tree (git diff --stat "+soloStartRef+"):")
			if len(stat) == 0 {
				record = append(record, "  (no tracked file differs from the starting tree)")
			}
			record = append(record, capLines(stat, changedFilesMaxLines)...)
		}
	}
	if status, ok := git("status", "--short"); ok {
		record = append(record, "Working tree status (git status --short):")
		if len(status) == 0 {
			record = append(record, "  (clean)")
		}
		record = append(record, capLines(status, changedFilesMaxLines)...)
	}
	return record
}

func capLines(lines []string, maximum int) []string {
	if len(lines) <= maximum {
		return lines
	}
	return append(append([]string{}, lines[:maximum]...),
		fmt.Sprintf("  ... %d more lines", len(lines)-maximum))
}

var _ steploop.TaskController = compaction.Controller{}
