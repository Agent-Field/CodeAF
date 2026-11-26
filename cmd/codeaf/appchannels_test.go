package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/apps/intake"
	"github.com/Agent-Field/codeaf/internal/apps/outputs"
	"github.com/Agent-Field/codeaf/internal/apps/runs"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/appwire"
)

const intakeEmail = "From: Person <person@example.com>\r\n" +
	"Subject: =?UTF-8?Q?New_contact?=\r\nMIME-Version: 1.0\r\n" +
	"Content-Type: multipart/mixed; boundary=parts\r\n\r\n" +
	"--parts\r\nContent-Type: text/plain; charset=utf-8\r\n" +
	"Content-Transfer-Encoding: quoted-printable\r\n\r\nHello=21\r\n" +
	"--parts\r\nContent-Type: application/pdf\r\nContent-Disposition: attachment; filename=card.pdf\r\n" +
	"Content-Transfer-Encoding: base64\r\n\r\nJVBERi0xLjQK\r\n--parts--\r\n"

func intakeWire(t *testing.T) (*factoryWire, string, map[string]intake.Channel) {
	t.Helper()
	backend := newFakeBackend(newAppRecorder(), nil)
	backend.data.home = t.TempDir()
	t.Cleanup(func() { backend.Close() })
	apps := backend.AppDefinitions()
	draft, err := apps.NewDraft("")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(draft.Dir(), os.DirFS("testdata/intake-crm")); err != nil {
		t.Fatal(err)
	}
	kept, err := apps.Promote(draft, "Keep contact intake")
	if err != nil {
		t.Fatal(err)
	}
	wire := wireOver(t, backend)
	var list appwire.ChannelListResult
	wire.must("channels.list", appwire.ChannelListParams{App: kept.ID}, &list)
	channels := make(map[string]intake.Channel, len(list.Channels))
	for _, c := range list.Channels {
		if !c.Active {
			t.Fatalf("channel %s is inactive", c.ID)
		}
		channels[c.ID] = c
	}
	if len(channels) != 4 {
		t.Fatalf("channels = %+v", list)
	}
	return wire, kept.ID, channels
}

func TestAppIntakeWireParity(t *testing.T) {
	wire, app, _ := intakeWire(t)
	for _, tc := range []struct {
		name  string
		input intake.Post
		want  []string
		email string
		notes string
		asset string
		work  bool
	}{
		{name: "form", input: intake.Post{Channel: "request", Key: "form-1", Values: map[string]any{"name": "Request", "email": "request@example.com"}},
			want: []string{"Request"}, email: "request@example.com", work: true},
		{name: "email", input: intake.Post{Channel: "mail", Name: "message.eml", Content: []byte(intakeEmail)},
			want: []string{"New contact"}, email: "person@example.com", notes: "Hello!", asset: "card.pdf"},
		{name: "csv", input: intake.Post{Channel: "drop", Name: "contacts.csv", Content: []byte("Full Name,Email Address\nFirst,first@example.com\nSecond,second@example.com\n")},
			want: []string{"First", "Second"}, email: "first@example.com"},
		{name: "pdf", input: intake.Post{Channel: "drop", Name: "directory.pdf", Content: []byte("%PDF-1.4\n")},
			want: []string{"directory.pdf"}, asset: "directory.pdf"},
		{name: "image", input: intake.Post{Channel: "drop", Name: "contact.png", Content: []byte("\x89PNG\r\n\x1a\n")},
			want: []string{"contact.png"}, asset: "contact.png"},
		{name: "connector", input: intake.Post{Channel: "events", Key: "evt-1", Values: map[string]any{"full_name": "Webhook", "ignored": "value"}},
			want: []string{"Webhook"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.input.App = app
			var receipt, replay data.IntakeReceipt
			wire.must("channels.post", tc.input, &receipt)
			wire.must("channels.post", tc.input, &replay)
			if receipt.Created != len(tc.want) || receipt.Replayed || !replay.Replayed ||
				replay.Batch != receipt.Batch || (receipt.WorkItem != "") != tc.work || replay.WorkItem != receipt.WorkItem {
				t.Fatalf("receipt = %+v, replay = %+v", receipt, replay)
			}
			for i, id := range receipt.Rows {
				var got struct{ Row map[string]any }
				p := appwire.DataRecordParams{DataScope: appwire.DataScope{App: app}, Type: "contact", ID: id}
				wire.must("data.get", p, &got)
				meta, _ := got.Row["_meta"].(map[string]any)
				by, _ := meta["by"].(map[string]any)
				if got.Row["name"] != tc.want[i] || by["name"] != "import" {
					t.Fatalf("row = %+v", got.Row)
				}
				if i == 0 && ((tc.email != "" && got.Row["email"] != tc.email) || (tc.notes != "" && got.Row["notes"] != tc.notes)) {
					t.Fatalf("mapped email or body missing: %+v", got.Row)
				}
				if tc.asset != "" {
					parts, err := wire.backend.appRunIntakeParts(t.Context(), runs.AgentCall{
						Call: runs.Call{App: app}, IntakeType: "contact", IntakeRecord: id,
					}, func() bool { return false })
					if tc.name == "image" {
						if err != nil || len(parts) != 1 || parts[0].ImageURL == nil || parts[0].ImageURL.URL != "data:image/png;base64,iVBORw0KGgo=" {
							t.Fatalf("image parts = %+v, %v", parts, err)
						}
					} else if err == nil || !strings.Contains(err.Error(), "could not read PDF attachment") {
						t.Fatalf("malformed PDF = %+v, %v", parts, err)
					}
					files, _ := got.Row["files"].([]any)
					if len(files) != 1 || files[0].(map[string]any)["name"] != tc.asset {
						t.Fatalf("file field = %+v", files)
					}
				}
			}
		})
	}
}

func TestAppIntakeLocalFormAndWebhook(t *testing.T) {
	wire, app, channels := intakeWire(t)
	get, err := http.Get(channels["request"].URL)
	if err != nil {
		t.Fatal(err)
	}
	page := responseBody(t, get, http.StatusOK)
	match := regexp.MustCompile(`name="_dedupe" value="([^"]+)"`).FindStringSubmatch(page)
	if len(match) != 2 || !strings.Contains(page, "Contact us") {
		t.Fatalf("form = %s", page)
	}
	form := url.Values{"_dedupe": {match[1]}, "name": {"Barbara"}, "email": {"broken"}}
	for _, tc := range []struct {
		email, want string
		status      int
	}{
		{"broken", "email is an email address\n", http.StatusBadRequest},
		{"barbara@example.com", "Submission received.\n", http.StatusOK},
		{"barbara@example.com", "Submission received.\n", http.StatusOK},
	} {
		form.Set("email", tc.email)
		response, err := http.PostForm(channels["request"].URL, form)
		if err != nil {
			t.Fatal(err)
		}
		if got := responseBody(t, response, tc.status); got != tc.want {
			t.Fatalf("form response = %q, want %q", got, tc.want)
		}
	}
	var receipt data.IntakeReceipt
	wire.must("channels.post", intake.Post{App: app, Channel: "request", Key: match[1],
		Values: map[string]any{"name": "Barbara", "email": "barbara@example.com"}}, &receipt)
	if !receipt.Replayed || receipt.WorkItem == "" {
		t.Fatalf("browser and wire disagree: %+v", receipt)
	}
	secretDir := filepath.Join(wire.backend.dataRoot(), "apps", "_intake", "secrets", app)
	if err := os.MkdirAll(secretDir, 0o700); err != nil {
		t.Fatal(err)
	}
	const secret = "fixture-secret-01234567890123456789"
	if err := os.WriteFile(filepath.Join(secretDir, "events"), []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		auth   string
		status int
	}{{"", http.StatusUnauthorized}, {secret, http.StatusCreated}, {secret, http.StatusOK}} {
		req, err := http.NewRequest(http.MethodPost, channels["events"].URL, strings.NewReader(`{"full_name":"Local connector"}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Idempotency-Key", "local-1")
		req.Header.Set("Authorization", "Bearer "+tc.auth)
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		responseBody(t, response, tc.status)
	}
	wire.must("channels.post", intake.Post{App: app, Channel: "events", Key: "local-1", Values: map[string]any{"full_name": "Local connector"}}, &receipt)
	if !receipt.Replayed {
		t.Fatal("webhook was not visible to wire replay")
	}
}

func TestAppIntakeFolderAndWireAgree(t *testing.T) {
	wire, app, channels := intakeWire(t)
	for _, tc := range []struct {
		channel, name, content string
	}{
		{"mail", "drop.eml", intakeEmail},
		{"drop", "contacts.csv", "Full Name,Email Address\nFrom folder,folder@example.com\n"},
		{"drop", "attachment.pdf", "%PDF-1.4\n"},
	} {
		p := intake.FolderPoller{Service: wire.backend.intakeService()}
		if err := os.WriteFile(filepath.Join(channels[tc.channel].Folder, tc.name), []byte(tc.content), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := p.Poll(t.Context()); err != nil {
			t.Fatal(err)
		}
		var out data.IntakeReceipt
		wire.must("channels.post", intake.Post{App: app, Channel: tc.channel, Name: tc.name, Content: []byte(tc.content)}, &out)
		if !out.Replayed || out.Created != 1 {
			t.Fatalf("%s was not delivered by the folder: %+v", tc.name, out)
		}
	}
}

func responseBody(t *testing.T, response *http.Response, status int) string {
	t.Helper()
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != status {
		t.Fatalf("HTTP %d, want %d, body %s, error %v", response.StatusCode, status, body, err)
	}
	return string(body)
}

func TestAppIntakeReplaysAfterMappingChanges(t *testing.T) {
	wire, app, _ := intakeWire(t)
	input := intake.Post{App: app, Channel: "events", Key: "before-edit",
		Values: map[string]any{"full_name": "Original"}}
	var original data.IntakeReceipt
	wire.must("channels.post", input, &original)

	apps := wire.backend.AppDefinitions()
	draft, err := apps.NewDraft(app)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(draft.Dir(), "app.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	channel := manifest["channels"].(map[string]any)["events"].(map[string]any)
	channel["mapping"] = map[string]any{"different_key": "name"}
	raw, err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := apps.Promote(draft, "Change event mapping"); err != nil {
		t.Fatal(err)
	}

	var replay data.IntakeReceipt
	wire.must("channels.post", input, &replay)
	if !replay.Replayed || replay.Batch != original.Batch {
		t.Fatalf("replay after edit = %+v, original = %+v", replay, original)
	}
	input.Values["different_key"] = "Different"
	if _, code, _ := wire.call("channels.post", input); code != appwire.CodeDuplicate {
		t.Fatalf("different payload with saved key got %s", code)
	}
}

func TestIntakePDFProvidesTextAndImagesToRun(t *testing.T) {
	wire, app, _ := intakeWire(t)
	raw, err := outputs.Render(schema.OutputTemplate{Label: "Invoice", Format: "pdf", Body: "Amount due: 42"}, []map[string]any{{}})
	if err != nil {
		t.Fatal(err)
	}
	var receipt data.IntakeReceipt
	wire.must("channels.post", intake.Post{App: app, Channel: "drop", Name: "invoice.pdf", Content: raw}, &receipt)
	call := runs.AgentCall{Call: runs.Call{App: app}, IntakeType: "contact", IntakeRecord: receipt.Rows[0]}
	parts, err := wire.backend.appRunIntakeParts(t.Context(), call, func() bool { return false })
	if err != nil || len(parts) != 1 || parts[0].Type != "text" || !strings.Contains(parts[0].Text, "Amount due: 42") {
		t.Fatalf("text-only model parts = %+v, %v", parts, err)
	}
	if _, err := exec.LookPath("pdftoppm"); err != nil {
		t.Skip("Poppler is unavailable")
	}
	parts, err = wire.backend.appRunIntakeParts(t.Context(), call, func() bool { return true })
	if err != nil || len(parts) != 2 || parts[1].ImageURL == nil ||
		!strings.HasPrefix(parts[1].ImageURL.URL, "data:image/png;base64,") {
		t.Fatalf("image-capable model parts = %+v, %v", parts, err)
	}
}
