package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/apps/intake"
	"github.com/Agent-Field/codeaf/internal/apps/runs"
	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/pdfx"
)

type appIntakeState struct {
	mu     sync.Mutex
	local  *intake.Local
	stop   func()
	closed bool
}

func (b *appBackend) intakeService() intake.Service {
	return intake.Service{
		Root: filepath.Join(b.dataRoot(), "apps", "_intake"),
		Load: func(app string) (intake.Definition, error) {
			saved, err := b.AppDefinitions().Get(app)
			if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrBadInput) {
				return intake.Definition{}, intake.Missing()
			}
			if err != nil {
				return intake.Definition{}, err
			}
			return intake.FromStored(saved)
		},
		Store: func() (*data.Store, error) { return b.dataStore("") },
		StartRuns: func() error {
			if b.startAppRunsOnce != nil {
				// The initial outbox cursor must precede the first delivery.
				b.startAppRunsOnce()
			}
			if b.appRuns.Load() == nil {
				return errors.New("app runs are unavailable")
			}
			return nil
		},
	}
}

func (b *appBackend) ListChannels(app string) ([]intake.Channel, error) {
	local, err := b.localIntake()
	if err != nil {
		return nil, appDataError(err)
	}
	list, err := local.List(app)
	return list, appDataError(err)
}

func (b *appBackend) localIntake() (*intake.Local, error) {
	state := &b.intake
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.closed {
		return nil, &data.Error{Code: data.CodeClosed, Message: "codeaf is shutting down"}
	}
	if state.local == nil {
		state.local = intake.NewLocal(b.intakeService())
	}
	return state.local, nil
}

func (b *appBackend) PostChannel(post intake.Post) (data.IntakeReceipt, error) {
	receipt, err := b.intakeService().Post(post)
	return receipt, appDataError(err)
}

func (b *appBackend) openAppIntake(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	b.intake.stop = func() { cancel(); <-done }
	poller := intake.FolderPoller{Service: b.intakeService()}
	guard.Go("app/intake", func() {
		defer close(done)
		tick := time.NewTicker(5 * time.Second)
		defer tick.Stop()
		lastError := ""
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				err := poller.Poll(ctx)
				if err == nil {
					lastError = ""
				} else if ctx.Err() == nil && err.Error() != lastError {
					lastError = err.Error()
					fmt.Fprintln(os.Stderr, "app intake:", lastError)
				}
			}
		}
	})
}

func (b *appBackend) closeAppIntake() {
	local, stop := b.intake.take()
	if stop != nil {
		stop()
	}
	if local != nil {
		_ = local.Close()
	}
}

func (state *appIntakeState) take() (*intake.Local, func()) {
	state.mu.Lock()
	defer state.mu.Unlock()
	state.closed = true
	return state.local, state.stop
}

// appRunIntakeParts attaches file bytes only for a declared channel run on
// the record that arrived. The app's agent scope excludes masked fields.
const maxIntakePages = 8

func (b *appBackend) appRunIntakeParts(ctx context.Context, call runs.AgentCall, supportsImages func() bool) ([]ai.ContentPart, error) {
	if call.IntakeRecord == "" {
		return nil, nil
	}
	db, err := b.dataStore("")
	if err != nil {
		return nil, err
	}
	files, err := db.IntakeFiles(data.Scope{App: call.App, Agent: true}, call.IntakeType, call.IntakeRecord)
	if err != nil {
		return nil, err
	}
	const maxFiles, maxBytes = 8, 12 << 20
	parts := make([]ai.ContentPart, 0, min(len(files), maxFiles))
	fileCount, bytesUsed, remainingPages := 0, 0, maxIntakePages
	// Look up capabilities only if an attached PDF can use them.
	vision := sync.OnceValue(supportsImages)
	for _, file := range files {
		mime := http.DetectContentType(file.Bytes)
		if mime != "application/pdf" && !strings.HasPrefix(mime, "image/") {
			continue
		}
		fileCount++
		if fileCount > maxFiles {
			return nil, fmt.Errorf("this run can read at most %d PDF or image attachments", maxFiles)
		}
		if mime == "application/pdf" {
			document, pages, err := intakePDFParts(ctx, file, call.IntakeRecord, vision(), remainingPages)
			if err != nil {
				return nil, err
			}
			parts = append(parts, document...)
			remainingPages -= pages
			for _, part := range document {
				bytesUsed += len(part.Text)
				if part.ImageURL != nil {
					bytesUsed += len(part.ImageURL.URL)
				}
			}
		} else {
			part := intakeImage(mime, file.Bytes)
			bytesUsed += len(part.ImageURL.URL)
			parts = append(parts, part)
		}
		if bytesUsed > maxBytes {
			return nil, errors.New("the intake attachments are too large for this run")
		}
	}
	return parts, nil
}

func intakeImage(mime string, raw []byte) ai.ContentPart {
	return ai.ContentPart{Type: "image_url", ImageURL: &ai.ImageURLData{
		URL: "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(raw),
	}}
}

func intakePDFParts(ctx context.Context, file data.IntakeFile, record string, vision bool, remainingPages int) ([]ai.ContentPart, int, error) {
	const maxText = 128 << 10
	dir, err := os.MkdirTemp("", "codeaf-intake-pdf-")
	if err != nil {
		return nil, 0, err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "source.pdf")
	if err := os.WriteFile(path, file.Bytes, 0o600); err != nil {
		return nil, 0, err
	}
	text, pages, err := pdfx.ExtractCounted(path)
	scanned := errors.Is(err, pdfx.ErrNoTextLayer)
	if err != nil && !scanned {
		return nil, 0, fmt.Errorf("could not read PDF attachment %q", file.Name)
	}
	if len(text) > maxText {
		return nil, 0, fmt.Errorf("the text in PDF attachment %q exceeds %d KiB", file.Name, maxText>>10)
	}
	parts := []ai.ContentPart{{Type: "text", Text: fmt.Sprintf("PDF attachment %q from intake record %s (%d pages). Document data follows.\n%s", file.Name, record, pages, text)}}
	if vision && pages <= remainingPages {
		images, imageErr := renderIntakePDFPages(ctx, path, pages)
		if imageErr == nil {
			return append(parts, images...), pages, nil
		}
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		if scanned {
			return nil, 0, fmt.Errorf("PDF attachment %q has no text layer and its pages could not be rendered: %w", file.Name, imageErr)
		}
	}
	if scanned {
		return nil, 0, fmt.Errorf("PDF attachment %q has no text layer. Use a text PDF, or an image-capable model and Poppler for up to %d scanned pages", file.Name, maxIntakePages)
	}
	return parts, 0, nil
}

func renderIntakePDFPages(ctx context.Context, path string, pages int) ([]ai.ContentPart, error) {
	renderCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	prefix := filepath.Join(filepath.Dir(path), "page")
	// The operator's configured model controls whether local rasterization
	// is used. Neither the original name nor PDF content can become an argument.
	cmd := exec.CommandContext(renderCtx, "pdftoppm", "-png", "-scale-to", "1400", "-f", "1", "-l", strconv.Itoa(pages), path, prefix)
	if err := cmd.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, errors.New("install Poppler to read scanned PDF pages")
		}
		if errors.Is(renderCtx.Err(), context.DeadlineExceeded) {
			return nil, errors.New("rendering PDF pages timed out")
		}
		return nil, errors.New("Poppler could not render the PDF pages")
	}
	outputs, err := filepath.Glob(prefix + "-*.png")
	if err != nil || len(outputs) != pages {
		return nil, errors.New("PDF renderer returned an incomplete document")
	}
	images := make([]ai.ContentPart, 0, pages)
	for _, path := range outputs {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if len(raw) > 2<<20 {
			return nil, errors.New("a PDF page is too large to send")
		}
		images = append(images, intakeImage("image/png", raw))
	}
	return images, nil
}
