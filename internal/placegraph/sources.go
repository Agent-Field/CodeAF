package placegraph

// WHAT A PLACE MAY GIVE A CHAT, and the checks between a source a person (or the
// AI offering a place) listed and the line a model reads.
//
// A SOURCE IS A REFERENCE. Nothing here opens a file, lists a folder or fetches a
// URL. A path is stat'ed so the model is told the truth about whether it is
// there; a URL is parsed so only a web page is ever named; a chat id is checked
// for shape. Reading any of them is the model's own tool call, inside the
// harness, under the tool's own rules — this package launches nothing.
//
// SOME PATHS ARE NEVER GIVEN, whoever listed them. A source is named in the
// prompt of every chat in the place, and a model handed "~/.ssh" as context will
// read it when asked to "check the sources", sending private keys to a provider.
// The deny list is checked on the spelling AND on where symlinks lead, so a link
// in a project folder cannot launder a credentials folder into a place.

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// ErrSourceRefused is what NewSource answers for a source the policy will not
// accept. Callers match it with errors.Is; the wrapped text is the reason.
var ErrSourceRefused = errors.New("placegraph: that source cannot be given to a chat")

// SourcePolicy is what the source checks need to know about this machine.
type SourcePolicy struct {
	// Deny lists absolute paths (folders or files) that are never given to a
	// chat, nor anything beneath them, checked before and after symlinks.
	Deny []string
	// ChatTitle names another conversation for a chat source with no label. Nil
	// leaves the id.
	ChatTitle func(chatID string) string
}

func (p SourcePolicy) zero() bool { return p.Deny == nil && p.ChatTitle == nil }

// DefaultSourcePolicy denies the credential stores under userHome and the codeaf
// home itself (where the provider key and every conversation live). codeafHome
// may be "" when the caller has none to add.
func DefaultSourcePolicy(userHome, codeafHome string) SourcePolicy {
	var deny []string
	if userHome != "" {
		for _, rel := range []string{
			".ssh", ".gnupg", ".aws", ".azure", ".kube", ".docker",
			".config/gcloud", ".config/gh", ".password-store",
			".netrc", ".git-credentials", ".npmrc", ".pypirc",
			".codeaf",
		} {
			deny = append(deny, filepath.Join(userHome, filepath.FromSlash(rel)))
		}
	}
	if codeafHome != "" {
		deny = append(deny, filepath.Clean(codeafHome))
	}
	return SourcePolicy{Deny: deny}
}

func (p SourcePolicy) denied(path string) bool {
	for _, d := range p.Deny {
		if d == "" {
			continue
		}
		d = filepath.Clean(d)
		if path == d || strings.HasPrefix(path, d+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func pathKind(k SourceKind) bool { return k == SourceFile || k == SourceFolder || k == SourceRepo }

// SourceKey is the identity two places' sources are deduplicated by: a cleaned
// path (folder, repo and file share one namespace, since one path is one thing
// on disk), a URL with its scheme and host lowercased, or a chat id.
func SourceKey(s Source) string {
	ref := canonicalRef(s.Kind, s.Ref)
	switch {
	case pathKind(s.Kind):
		return "path:" + ref
	case s.Kind == SourceURL:
		return "url:" + ref
	default:
		return string(s.Kind) + ":" + ref
	}
}

func canonicalRef(k SourceKind, ref string) string {
	ref = strings.TrimSpace(ref)
	switch {
	case pathKind(k):
		if filepath.IsAbs(ref) {
			return filepath.Clean(ref)
		}
	case k == SourceURL:
		if u, err := url.Parse(ref); err == nil && u.Host != "" {
			u.Scheme = strings.ToLower(u.Scheme)
			u.Host = strings.ToLower(u.Host)
			return u.String()
		}
	}
	return ref
}

// inspect sets u's Status, Reason, RepoRoot and (for a chat) Label. It stats and
// never opens.
func (p SourcePolicy) inspect(chatID string, u *UsedSource) {
	refuse := func(reason string) { u.Status, u.Reason = SourceRefusedStatus, reason }
	switch {
	case pathKind(u.Kind):
		if !filepath.IsAbs(u.Ref) {
			refuse("not an absolute path")
			return
		}
		if p.denied(u.Ref) {
			refuse("a credentials or codeaf folder is never given to a chat")
			return
		}
		real, err := filepath.EvalSymlinks(u.Ref)
		if err != nil {
			// THE RECORD IS KEPT AND THE READING IS SAID, the attached-folder
			// law: a disk that is unplugged comes back, and a model told a path
			// is not there spends no calls discovering it.
			u.Status, u.Reason = SourceMissing, "not on this disk right now"
			return
		}
		if p.denied(real) {
			refuse("it leads into a credentials or codeaf folder")
			return
		}
		info, err := os.Stat(real)
		if err != nil {
			u.Status, u.Reason = SourceMissing, "not on this disk right now"
			return
		}
		if u.Kind == SourceFile && !info.Mode().IsRegular() {
			refuse("not a regular file")
			return
		}
		if u.Kind != SourceFile && !info.IsDir() {
			refuse("not a folder")
			return
		}
		u.RepoRoot = repoRoot(real, info.IsDir())
		u.Status = SourceOK
	case u.Kind == SourceURL:
		if reason := urlProblem(u.Ref); reason != "" {
			refuse(reason)
			return
		}
		u.Status = SourceOK
	case u.Kind == SourceChat:
		if reason := chatRefProblem(u.Ref); reason != "" {
			refuse(reason)
			return
		}
		if u.Ref == chatID {
			refuse("it is this conversation itself")
			return
		}
		if u.Label == "" && p.ChatTitle != nil {
			u.Label = strings.TrimSpace(p.ChatTitle(u.Ref))
		}
		u.Status = SourceOK
	default:
		refuse("unknown kind of source")
	}
}

// repoRoot walks up from path for a `.git` entry (a folder, or the file a
// worktree or submodule has), with no exec. "" when the path is in no work tree.
func repoRoot(path string, isDir bool) string {
	dir := path
	if !isDir {
		dir = filepath.Dir(path)
	}
	for range 256 {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
	return ""
}

func urlProblem(ref string) string {
	u, err := url.Parse(ref)
	switch {
	case err != nil:
		return "not a URL"
	case u.Scheme != "http" && u.Scheme != "https":
		return "only http and https pages can be sources"
	case u.Host == "" || u.Hostname() == "":
		return "the URL has no host"
	case u.User != nil:
		return "a URL carrying a user name or password is never given to a chat"
	}
	return ""
}

func chatRefProblem(ref string) string {
	switch {
	case ref == "" || ref == "." || ref == "..":
		return "not a conversation id"
	case len(ref) > MaxChatIDBytes || !utf8.ValidString(ref) || hasControl(ref):
		return "not a conversation id"
	case strings.ContainsAny(ref, `/\`):
		return "not a conversation id"
	}
	return ""
}

// NewSource validates and canonicalises one source before it is stored (the
// bridge's "add a source" door, then WithSource and Store.SetContext).
//
//   - file, folder, repo: the path must be absolute, exist, and not lie in a
//     denied folder before or after symlinks; it is stored resolved
//     (EvalSymlinks). A folder that IS a git work tree's root is stored as repo.
//     A folder INSIDE one keeps the folder the person pointed at — the source
//     record has no field for "the repository around it", and widening their
//     pick to the whole repository would give every chat in the place more than
//     they handed over; the repository is named at resolve time (RepoRoot).
//     Asking for kind repo on a folder inside a work tree takes the root, since
//     that is what was asked for.
//   - url: http or https with a host and no credentials.
//   - chat: a conversation id.
//
// The label defaults to the base name, the host, or nothing for a chat.
func NewSource(kind SourceKind, ref string, by AddedBy, pol SourcePolicy) (Source, error) {
	if !kind.Valid() {
		return Source{}, fmt.Errorf("%w: unknown source kind %q", ErrInvalid, kind)
	}
	if !by.Valid() {
		return Source{}, fmt.Errorf("%w: addedBy", ErrInvalid)
	}
	ref = strings.TrimSpace(ref)
	if ref == "" || len(ref) > MaxSourceRefBytes || !utf8.ValidString(ref) || hasControl(ref) {
		return Source{}, fmt.Errorf("%w: source ref", ErrInvalid)
	}
	src := Source{ID: newSourceID(), Kind: kind, AddedBy: by, At: time.Now().UTC()}
	refuse := func(reason string) (Source, error) {
		return Source{}, fmt.Errorf("%w: %s", ErrSourceRefused, reason)
	}
	switch {
	case pathKind(kind):
		if !filepath.IsAbs(ref) {
			return refuse("a path source must be absolute")
		}
		clean := filepath.Clean(ref)
		if pol.denied(clean) {
			return refuse("a credentials or codeaf folder is never given to a chat")
		}
		real, err := filepath.EvalSymlinks(clean)
		if err != nil {
			return refuse("that path is not on this disk")
		}
		if pol.denied(real) {
			return refuse("that path leads into a credentials or codeaf folder")
		}
		info, err := os.Stat(real)
		if err != nil {
			return refuse("that path is not on this disk")
		}
		switch {
		case kind == SourceFile && !info.Mode().IsRegular():
			return refuse("not a regular file")
		case kind != SourceFile && !info.IsDir():
			return refuse("not a folder")
		}
		if kind != SourceFile {
			root := repoRoot(real, true)
			switch {
			case root == real:
				kind = SourceRepo
			case kind == SourceRepo && root == "":
				return refuse("that folder is not in a git repository")
			case kind == SourceRepo:
				real = root
			}
		}
		if len(real) > MaxSourceRefBytes {
			return Source{}, fmt.Errorf("%w: source ref", ErrTooLarge)
		}
		src.Kind, src.Ref, src.Label = kind, real, filepath.Base(real)
	case kind == SourceURL:
		if reason := urlProblem(ref); reason != "" {
			return refuse(reason)
		}
		src.Ref = canonicalRef(kind, ref)
		u, _ := url.Parse(src.Ref)
		src.Label = u.Hostname()
	case kind == SourceChat:
		if reason := chatRefProblem(ref); reason != "" {
			return refuse(reason)
		}
		src.Ref = ref
	}
	if utf8.RuneCountInString(src.Label) > MaxSourceLabel {
		src.Label = string([]rune(src.Label)[:MaxSourceLabel])
	}
	return src, nil
}

// WithSource adds s to c unless a source with the same SourceKey is already
// there, in which case it answers c unchanged and false: dropping the same
// folder on a place twice is a no-op, not a second row. The bound on how many
// sources a place holds is Store.SetContext's to enforce.
func WithSource(c Context, s Source) (Context, bool) {
	key := SourceKey(s)
	for _, have := range c.Sources {
		if SourceKey(have) == key {
			return c, false
		}
	}
	out := Context{Instructions: c.Instructions, Sources: append(append([]Source(nil), c.Sources...), s)}
	return out, true
}

func newSourceID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return "src_" + hex.EncodeToString(b[:])
}
