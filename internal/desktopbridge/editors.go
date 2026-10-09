package desktopbridge

// editors.go lists the editors registered on the ENGINE machine for one
// workspace file, and can start one of those editors. The renderer names an
// id from this list. It never sends a command line, and this process never
// passes a renderer string to a shell.

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const editorCap = 8

const (
	reasonRemote    = "The engine is on another machine."
	reasonNoDisplay = "This machine has no display, so an editor cannot be opened."
	reasonNoList    = "This machine cannot list editors."
	reasonNoStart   = "This machine cannot start an editor."
)

// Editor is one handler the operating system has registered for a file's type.
// ID is a desktop-file basename or a macOS bundle id, never a command line.
type Editor struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Default bool   `json:"default"`
}

// editorList is the answer to GET /editors. Open is false when a program
// cannot be started here (another machine, or no display). Editors is empty
// in those cases only when there is nothing real to name; a headless machine
// may still report the handlers it found and set Open false.
type editorList struct {
	Editors []Editor `json:"editors"`
	Local   bool     `json:"local"`
	Open    bool     `json:"open"`
	Reason  string   `json:"reason,omitempty"`
}

var (
	errNoList  = errors.New(reasonNoList)
	errNoStart = errors.New(reasonNoStart)

	// Replaced in tests. Production calls the operating system.
	listEditors = discoverEditors
	startEditor = startEditorOS
	displayUp   = machineHasDisplay
	lookPath    = exec.LookPath
	runCmd      = defaultRun
)

// editorID accepts a desktop basename (code.desktop) or a bundle id
// (com.apple.Preview) and nothing a shell would interpret.
var editorID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.+_-]{0,180}$`)

func safeEditorID(id string) bool {
	return editorID.MatchString(id) && !strings.Contains(id, "..")
}

func machineHasDisplay() bool {
	if runtime.GOOS == "darwin" {
		return os.Getenv("SSH_TTY") == ""
	}
	return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
}

func defaultRun(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).Output()
}

func (s *conversation) editors(w http.ResponseWriter, r *http.Request) {
	if !needGet(w, r) {
		return
	}
	abs, status, msg := s.placedFile(r.URL.Query().Get("path"))
	if status != 0 {
		fail(w, status, msg)
		return
	}
	if !s.conn.Local {
		write(w, editorList{Editors: []Editor{}, Local: false, Open: false, Reason: reasonRemote})
		return
	}
	found, err := listEditors(abs)
	if err != nil {
		write(w, editorList{Editors: []Editor{}, Local: true, Open: false, Reason: reasonNoList})
		return
	}
	open := displayUp()
	reason := ""
	if !open {
		reason = reasonNoDisplay
	}
	write(w, editorList{Editors: capEditors(found), Local: true, Open: open, Reason: reason})
}

func (s *conversation) openEditor(w http.ResponseWriter, r *http.Request) {
	if !needPost(w, r) {
		return
	}
	var body struct {
		Path string `json:"path"`
		ID   string `json:"id"`
	}
	if !decode(w, r, &body) {
		return
	}
	abs, status, msg := s.placedFile(body.Path)
	if status != 0 {
		fail(w, status, msg)
		return
	}
	if !s.conn.Local {
		fail(w, 409, reasonRemote)
		return
	}
	if !displayUp() {
		fail(w, 409, reasonNoDisplay)
		return
	}
	// THE ID IS LOOKED UP, NOT RUN. A string that is not one of the handlers
	// just enumerated never becomes an argument, so a renderer cannot hand
	// this process a shell command.
	if !safeEditorID(body.ID) {
		fail(w, 400, "unknown editor")
		return
	}
	found, err := listEditors(abs)
	if err != nil || !editorKnown(found, body.ID) {
		fail(w, 400, "unknown editor")
		return
	}
	if err := startEditor(body.ID, abs); err != nil {
		fail(w, 409, reasonNoStart)
		return
	}
	write(w, map[string]bool{"accepted": true})
}

func editorKnown(list []Editor, id string) bool {
	for _, editor := range list {
		if editor.ID == id && safeEditorID(editor.ID) {
			return true
		}
	}
	return false
}

func capEditors(list []Editor) []Editor {
	ranked := rankEditors(list)
	if len(ranked) > editorCap {
		ranked = ranked[:editorCap]
	}
	if ranked == nil {
		ranked = []Editor{}
	}
	return ranked
}

// rankEditors puts the default first and leaves a single default mark, so a
// lister that flags two handlers still answers the way the menu draws one.
func rankEditors(list []Editor) []Editor {
	out := append([]Editor(nil), list...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Default != out[j].Default {
			return out[i].Default
		}
		return out[i].Name < out[j].Name
	})
	seenDefault := false
	for i := range out {
		if !out[i].Default {
			continue
		}
		if seenDefault {
			out[i].Default = false
			continue
		}
		seenDefault = true
	}
	return out
}

func discoverEditors(abs string) ([]Editor, error) {
	switch runtime.GOOS {
	case "linux":
		return discoverLinux(abs)
	case "darwin":
		return discoverDarwin(abs)
	default:
		return nil, errNoList
	}
}

func discoverLinux(abs string) ([]Editor, error) {
	mime, err := fileMime(abs)
	if err != nil {
		return nil, err
	}
	family := mimeFamily(mime, mimeParents(applicationDataDirs()))
	def := ""
	for _, kind := range family {
		if out, err := runCmd(mustLook("xdg-mime"), "query", "default", kind); err == nil {
			candidate := strings.TrimSpace(string(out))
			if safeEditorID(candidate) {
				def = candidate
				break
			}
		}
	}
	return scanDesktops(applicationDirs(), family, def), nil
}

// mimeFamily is the file's own type followed by the types it is a subclass
// of, the way shared-mime-info defines them: the subclasses table, and the
// spec's rule that every text/* type is also text/plain. Without it a Go or
// Rust source answers text/x-go, which almost no handler lists, and a machine
// with three text editors reports none of them.
func mimeFamily(mime string, parents map[string][]string) []string {
	family := []string{mime}
	seen := map[string]bool{mime: true}
	for i := 0; i < len(family) && len(family) < 8; i++ {
		for _, parent := range parents[family[i]] {
			if !seen[parent] {
				seen[parent] = true
				family = append(family, parent)
			}
		}
	}
	if strings.HasPrefix(mime, "text/") && !seen["text/plain"] {
		family = append(family, "text/plain")
	}
	return family
}

// mimeParents reads the subclasses tables shared-mime-info writes. A missing
// table is an empty map, never an error: the type's own handlers still count.
func mimeParents(dataDirs []string) map[string][]string {
	parents := map[string][]string{}
	for _, dir := range dataDirs {
		data, err := os.ReadFile(filepath.Join(dir, "mime", "subclasses"))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			child, parent, ok := strings.Cut(strings.TrimSpace(line), " ")
			if !ok {
				continue
			}
			if c, good := cleanMime(child); good {
				if p, good := cleanMime(parent); good {
					parents[c] = append(parents[c], p)
				}
			}
		}
	}
	return parents
}

func mustLook(name string) string {
	path, err := lookPath(name)
	if err != nil {
		return name
	}
	return path
}

func fileMime(abs string) (string, error) {
	if path, err := lookPath("xdg-mime"); err == nil {
		if out, err := runCmd(path, "query", "filetype", abs); err == nil {
			if mime, ok := cleanMime(string(out)); ok {
				return mime, nil
			}
		}
	}
	if path, err := lookPath("gio"); err == nil {
		if out, err := runCmd(path, "info", "-a", "standard::content-type", abs); err == nil {
			for _, line := range strings.Split(string(out), "\n") {
				if _, value, ok := strings.Cut(line, "standard::content-type:"); ok {
					if mime, good := cleanMime(value); good {
						return mime, nil
					}
				}
			}
		}
	}
	return "", errNoList
}

func cleanMime(raw string) (string, bool) {
	mime := strings.TrimSpace(raw)
	if mime == "" || strings.ContainsAny(mime, "\r\n\t ;|&$`<>\\\"'") || strings.Contains(mime, "..") || strings.Contains(mime, "/") && strings.Count(mime, "/") != 1 {
		return "", false
	}
	return mime, true
}

// applicationDataDirs is the XDG data search path, user directory first.
func applicationDataDirs() []string {
	home := os.Getenv("XDG_DATA_HOME")
	if home == "" {
		home = filepath.Join(os.Getenv("HOME"), ".local", "share")
	}
	dirs := []string{home}
	raw := os.Getenv("XDG_DATA_DIRS")
	var rest []string
	if raw == "" {
		rest = []string{"/usr/local/share", "/usr/share"}
	} else {
		rest = strings.Split(raw, string(os.PathListSeparator))
	}
	for _, dir := range rest {
		if strings.TrimSpace(dir) == "" {
			continue
		}
		dirs = append(dirs, dir)
	}
	return dirs
}

func applicationDirs() []string {
	var dirs []string
	for _, dir := range applicationDataDirs() {
		dirs = append(dirs, filepath.Join(dir, "applications"))
	}
	return dirs
}

// scanDesktops reads Name, MimeType and TryExec. Exec is ignored on purpose:
// the line is a command template, and the only thing that may run later is
// the desktop id through the platform launcher. An entry whose TryExec program
// is not installed is left out, because the launcher could not start it.
func scanDesktops(dirs []string, mimes []string, defaultID string) []Editor {
	seen := map[string]bool{}
	var found []Editor
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			id := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(id, ".desktop") || !safeEditorID(id) || seen[id] {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, id))
			if err != nil {
				continue
			}
			title, types, try, hidden, app := parseDesktop(string(data))
			if hidden || !app || title == "" || strings.ContainsAny(title, "\r\n") {
				continue
			}
			if try != "" {
				if _, err := lookPath(try); err != nil {
					continue
				}
			}
			if !mimeListedAny(types, mimes) && id != defaultID {
				continue
			}
			seen[id] = true
			found = append(found, Editor{ID: id, Name: title, Default: id == defaultID && defaultID != ""})
		}
	}
	return found
}

func parseDesktop(data string) (name, mime, tryExec string, hidden, app bool) {
	section := ""
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = line
			continue
		}
		if section != "[Desktop Entry]" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "Type":
			app = value == "Application"
		case "Name":
			if name == "" {
				name = strings.TrimSpace(value)
			}
		case "MimeType":
			mime = value
		case "TryExec":
			tryExec = strings.TrimSpace(value)
		case "Hidden":
			if value == "true" {
				hidden = true
			}
		}
	}
	return name, mime, tryExec, hidden, app
}

func mimeListedAny(field string, mimes []string) bool {
	for _, mime := range mimes {
		if mimeListed(field, mime) {
			return true
		}
	}
	return false
}

func mimeListed(field, mime string) bool {
	for _, part := range strings.Split(field, ";") {
		if strings.EqualFold(strings.TrimSpace(part), mime) {
			return true
		}
	}
	return false
}

// discoverDarwin asks Launch Services, through one fixed JavaScript that
// receives the path and a "text" flag as arguments, for the file's default
// application and every application registered for it. A file whose bytes are
// text also gets the editors registered for public.plain-text, and when it has
// no default of its own, the plain-text default: a Go source on a Mac with no
// Go tooling has a dynamic type no application claims, and TextEdit can still
// open it. Every row is a real bundle id Launch Services returned.
func discoverDarwin(abs string) ([]Editor, error) {
	path, err := lookPath("osascript")
	if err != nil {
		return nil, errNoList
	}
	flag := ""
	if looksText(abs) {
		flag = "text"
	}
	out, err := runCmd(path, "-l", "JavaScript", "-e", darwinEditorsScript, abs, flag)
	if err != nil {
		return nil, errNoList
	}
	return parseDarwinRows(string(out)), nil
}

// parseDarwinRows reads "bundle-id TAB name TAB 0|1" lines. A row whose id or
// name is not plain is dropped rather than repaired.
func parseDarwinRows(out string) []Editor {
	found := []Editor{}
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		fields := strings.Split(strings.TrimRight(line, "\r"), "\t")
		if len(fields) != 3 {
			continue
		}
		id, name := strings.TrimSpace(fields[0]), strings.TrimSpace(fields[1])
		if !safeEditorID(id) || name == "" || strings.ContainsAny(name, "\r\n") || seen[id] {
			continue
		}
		seen[id] = true
		found = append(found, Editor{ID: id, Name: name, Default: fields[2] == "1"})
	}
	return found
}

// looksText reads at most 8 KB: valid UTF-8 with no NUL byte is text. It is the
// same test a plain-text editor makes before it agrees to open a file.
func looksText(abs string) bool {
	file, err := os.Open(abs)
	if err != nil {
		return false
	}
	defer file.Close()
	buf := make([]byte, 8192)
	n, _ := file.Read(buf)
	head := buf[:n]
	if bytes.IndexByte(head, 0) >= 0 {
		return false
	}
	// A read may cut a multi-byte rune at the end; only the bytes before it count.
	for cut := 0; cut < 4 && len(head) > 0 && !utf8.Valid(head); cut++ {
		head = head[:len(head)-1]
	}
	return utf8.Valid(head)
}

const darwinEditorsScript = `function run(argv) {
  ObjC.import('AppKit');
  ObjC.import('Foundation');
  ObjC.import('CoreServices');
  var path = argv[0];
  var text = argv[1] === 'text';
  if (!path) return '';
  var ws = $.NSWorkspace.sharedWorkspace;
  var url = $.NSURL.fileURLWithPath(path);
  var rows = [], seen = {};
  function addURL(app, isDefault) {
    if (!app || app.isNil()) return;
    var bundle = $.NSBundle.bundleWithURL(app);
    if (!bundle || bundle.isNil() || !bundle.bundleIdentifier || bundle.bundleIdentifier.isNil()) return;
    var id = bundle.bundleIdentifier.js;
    if (seen[id]) return;
    seen[id] = true;
    var name = $.NSFileManager.defaultManager.displayNameAtPath(app.path).js.replace(/\.app$/, '');
    rows.push(id + '\t' + name + '\t' + (isDefault ? '1' : '0'));
  }
  function addID(id, isDefault) {
    if (!id || seen[id]) return;
    addURL(ws.URLForApplicationWithBundleIdentifier(id), isDefault);
  }
  var own = ws.URLForApplicationToOpenURL(url);
  if (own && !own.isNil()) addURL(own, true);
  else if (text) {
    var def = $.LSCopyDefaultRoleHandlerForContentType($('public.plain-text'), $.kLSRolesAll);
    if (def) addID(ObjC.unwrap(ObjC.castRefToObject(def)), true);
  }
  var list = ws.URLsForApplicationsToOpenURL(url);
  for (var i = 0; i < list.count; i++) addURL(list.objectAtIndex(i), false);
  if (text) {
    var ids = $.LSCopyAllRoleHandlersForContentType($('public.plain-text'), $.kLSRolesEditor);
    if (ids) {
      var arr = ObjC.castRefToObject(ids);
      for (var j = 0; j < arr.count; j++) addID(ObjC.unwrap(arr.objectAtIndex(j)), false);
    }
  }
  return rows.join('\n');
}`

func startEditorOS(id, abs string) error {
	if !displayUp() {
		return errNoStart
	}
	bin, args, err := launchPlan(id, abs)
	if err != nil {
		return err
	}
	cmd := exec.Command(bin, args...)
	if err := cmd.Start(); err != nil {
		return errNoStart
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// launchPlan is the only place an editor is turned into a process. The
// arguments are the platform launcher, the already-checked id, and the
// confined path, each a separate argv element.
func launchPlan(id, abs string) (string, []string, error) {
	if !safeEditorID(id) || abs == "" || strings.ContainsRune(abs, 0) {
		return "", nil, errNoStart
	}
	if runtime.GOOS == "darwin" {
		path, err := lookPath("open")
		if err != nil {
			return "", nil, errNoStart
		}
		return path, []string{"-b", id, "--", abs}, nil
	}
	if path, err := lookPath("gtk-launch"); err == nil {
		return path, []string{id, abs}, nil
	}
	if path, err := lookPath("gio"); err == nil {
		desktop, err := desktopFile(id)
		if err != nil {
			return "", nil, err
		}
		return path, []string{"launch", desktop, abs}, nil
	}
	return "", nil, errNoStart
}

// desktopFile resolves an id to a file under the application directories.
// The id is a basename, so it cannot point at an arbitrary path.
func desktopFile(id string) (string, error) {
	if !safeEditorID(id) || !strings.HasSuffix(id, ".desktop") {
		return "", errNoStart
	}
	for _, dir := range applicationDirs() {
		candidate := filepath.Join(dir, id)
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			continue
		}
		if filepath.Base(candidate) != id {
			continue
		}
		return candidate, nil
	}
	return "", errNoStart
}
