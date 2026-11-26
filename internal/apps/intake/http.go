package intake

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

const maxWebBody = 1 << 20

// Local serves only on this machine. The URL's token permits submissions to
// one version of one form. Connector endpoints use a separate shared secret.
type Local struct {
	service Service
	mu      sync.Mutex
	server  *http.Server
	host    string
	closed  bool
	key     [32]byte
	limit   requestLimit
	slots   chan struct{}
}

// requestLimit bounds local intake across all endpoints and resets without a
// clock goroutine or an entry for each caller.
type requestLimit struct {
	mu    sync.Mutex
	until time.Time
	count int
}

func (l *requestLimit) allow(now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !now.Before(l.until) {
		l.until = now.Add(10 * time.Second)
		l.count = 0
	}
	if l.count >= 20 {
		return false
	}
	l.count++
	return true
}

func NewLocal(s Service) *Local {
	l := &Local{service: s, slots: make(chan struct{}, 8)}
	rand.Read(l.key[:])
	return l
}

func (l *Local) address() (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return "", errors.New("the intake server is closed")
	}
	if l.server == nil {
		ln, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			return "", err
		}
		l.host = ln.Addr().String()
		l.server = &http.Server{
			Handler: l, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second,
			WriteTimeout: 20 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10,
		}
		go l.server.Serve(ln)
	}
	return l.host, nil
}

func (l *Local) Close() error {
	l.mu.Lock()
	l.closed = true
	server := l.server
	l.mu.Unlock()
	if server == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		return server.Close()
	}
	return nil
}

func (l *Local) List(app string) ([]Channel, error) {
	if !safeApp(app) {
		return nil, Missing()
	}
	def, err := l.service.Load(app)
	if err != nil {
		return nil, err
	}
	result := make([]Channel, 0, len(def.Manifest.Channels))
	for id, ch := range def.Manifest.Channels {
		if !safeName(id) {
			continue
		}
		entry := Channel{ID: id, Kind: ch.Kind, Record: ch.Record, Run: ch.Run, Form: ch.Form, Active: enabled(ch)}
		if entry.Active {
			switch ch.Kind {
			case "form-link", "connector-event", "webhook-in":
				host, err := l.address()
				if err != nil {
					return nil, err
				}
				entry.URL = "http://" + host + "/intake/" + app + "/" + id
				if ch.Kind == "form-link" {
					entry.URL += "/" + l.token(app, id, def.Version)
				}
			case "email-in", "file-drop":
				entry.Folder, err = l.service.inbox(app, id)
				if err != nil {
					return nil, err
				}
			}
		}
		result = append(result, entry)
	}
	slices.SortFunc(result, func(a, b Channel) int { return strings.Compare(a.ID, b.ID) })
	return result, nil
}

func (l *Local) token(app, channel string, version int) string {
	mac := hmac.New(sha256.New, l.key[:])
	io.WriteString(mac, app+"/"+channel+"/"+strconv.Itoa(version))
	return hex.EncodeToString(mac.Sum(nil))
}

func (l *Local) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
	if r.Host != l.host {
		http.Error(w, "open this link on the machine running the engine", http.StatusForbidden)
		return
	}
	path, ok := strings.CutPrefix(r.URL.Path, "/intake/")
	parts := strings.Split(path, "/")
	if !ok || (len(parts) != 2 && len(parts) != 3) || !safeApp(parts[0]) || !safeName(parts[1]) {
		http.NotFound(w, r)
		return
	}
	select {
	case l.slots <- struct{}{}:
		defer func() { <-l.slots }()
	default:
		http.Error(w, "too many deliveries. Try again shortly", http.StatusTooManyRequests)
		return
	}
	// The limit covers authorization and definition reads as well as writes.
	if !l.limit.allow(time.Now()) {
		http.Error(w, "too many deliveries. Try again shortly", http.StatusTooManyRequests)
		return
	}
	app, id := parts[0], parts[1]
	def, ch, err := l.service.channel(app, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	post := Post{App: app, Channel: id}
	switch ch.Kind {
	case "form-link":
		if len(parts) != 3 || !sameSecret(parts[2], l.token(app, id, def.Version)) {
			http.NotFound(w, r)
			return
		}
		l.form(w, r, def, ch, post)
	case "webhook-in", "connector-event":
		if len(parts) != 2 || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		l.webhook(w, r, def, ch, post)
	default:
		http.NotFound(w, r)
	}
}

func sameSecret(got, want string) bool {
	a, b := sha256.Sum256([]byte(got)), sha256.Sum256([]byte(want))
	return subtle.ConstantTimeCompare(a[:], b[:]) == 1
}

func (l *Local) webhook(w http.ResponseWriter, r *http.Request, def Definition, ch schema.Channel, post Post) {
	secret := l.service.secret(post.App, ch.Secret)
	got, present := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if secret == "" || !present || !sameSecret(got, secret) {
		http.Error(w, "a valid channel secret is required", http.StatusUnauthorized)
		return
	}
	post.Key = r.Header.Get("Idempotency-Key")
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxWebBody))
	if err := decoder.Decode(&post.Values); err != nil || post.Values == nil || decoder.Decode(new(any)) != io.EOF {
		http.Error(w, "send one JSON object", http.StatusBadRequest)
		return
	}
	receipt, err := l.service.deliver(def, ch, post)
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if !receipt.Replayed {
		w.WriteHeader(http.StatusCreated)
	}
	_ = json.NewEncoder(w).Encode(receipt)
}

func (l *Local) form(w http.ResponseWriter, r *http.Request, def Definition, ch schema.Channel, post Post) {
	if r.Method == http.MethodGet {
		model, err := l.formModel(def.Manifest.Process.Forms[ch.Form], post.App, ch.Record)
		if err != nil {
			writeError(w, err)
			return
		}
		_ = formPage.Execute(w, model)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "open or submit the form", http.StatusMethodNotAllowed)
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+l.host {
		http.Error(w, "submit the form from its own page", http.StatusForbidden)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxWebBody)
	if err := r.ParseForm(); err != nil || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
		http.Error(w, "submit the web form", http.StatusBadRequest)
		return
	}
	values, err := formValues(r.PostForm)
	if err != nil {
		writeError(w, err)
		return
	}
	post.Key, post.Values = r.PostForm.Get("_dedupe"), values
	if _, err := l.service.deliver(def, ch, post); err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintln(w, "Submission received.")
}

func formValues(fields url.Values) (map[string]any, error) {
	values := make(map[string]any, len(fields))
	for name, entries := range fields {
		if len(entries) != 1 {
			return nil, BadInput("submit one value per field")
		}
		if name != "_dedupe" && strings.TrimSpace(entries[0]) != "" {
			values[name] = entries[0]
		}
	}
	return values, nil
}

func writeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	if e, ok := data.AsError(err); ok {
		switch e.Code {
		case data.CodeDuplicate, data.CodeStale:
			status = http.StatusConflict
		case data.CodeInvalid, data.CodeLifecycle:
			status = http.StatusBadRequest
		case data.CodeNotFound:
			status = http.StatusNotFound
		case data.CodeNotAllowed:
			status = http.StatusForbidden
		}
	}
	http.Error(w, publicMessage(err), status)
}

type fieldInput struct {
	Name, Label, Kind string
	Choices           []string
	Required          bool
}

type formModel struct {
	Title, Key string
	Fields     []fieldInput
}

func (l *Local) formModel(form schema.ProcessForm, app, record string) (formModel, error) {
	db, err := l.service.Store()
	if err != nil {
		return formModel{}, err
	}
	t, err := db.Type(data.Scope{App: app}, record)
	if err != nil {
		return formModel{}, err
	}
	model := formModel{Title: form.Title, Key: rand.Text()}
	seen := map[string]bool{}
	for _, step := range form.Steps {
		for _, f := range step.Fields {
			field := t.Fields[f.Field]
			if field == nil || seen[f.Field] || field.Type == data.TypeFile || field.Type == data.TypeFiles {
				continue
			}
			seen[f.Field] = true
			input := fieldInput{Name: f.Field, Label: field.Label, Kind: "text",
				Required: step.When == "" && f.When == "" && (f.Required || field.Required && field.Default == nil)}
			switch field.Type {
			case data.TypeBool:
				input.Choices = []string{"true", "false"}
			case data.TypeEnum:
				input.Choices = field.Values
			case data.TypeLongtext:
				input.Kind = "longtext"
			case data.TypeEmail, data.TypeDate, data.TypeURL:
				input.Kind = field.Type
			}
			model.Fields = append(model.Fields, input)
		}
	}
	return model, nil
}

var formPage = template.Must(template.New("intake").Parse(`<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>{{.Title}}</title></head>
<body><main><h1>{{.Title}}</h1><form method="post">
<input type="hidden" name="_dedupe" value="{{.Key}}">
{{range .Fields}}<p><label>{{.Label}}
{{if .Choices}}<select name="{{.Name}}" {{if .Required}}required{{end}}>
<option value=""></option>{{range .Choices}}<option>{{.}}</option>{{end}}
</select>{{else if eq .Kind "longtext"}}<textarea name="{{.Name}}" {{if .Required}}required{{end}}></textarea>
{{else}}<input type="{{.Kind}}" name="{{.Name}}" {{if .Required}}required{{end}}>{{end}}
</label></p>{{end}}
<button type="submit">Submit</button>
</form></main></body></html>
`))
