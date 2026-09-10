package tui

import (
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/cas"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// renderMarkdown styles the subset of markdown that actually appears in
// model output — code fences, inline code, bold, headings, bullets, quotes —
// and preserves blank-line spacing. It never fails: anything unrecognised
// passes through as plain wrapped text, so a half-formed document degrades
// to exactly what was written.
var (
	mdCode    = butterStyle
	mdHeading = inkStyle.Bold(true)
	mdQuote   = lipgloss.NewStyle().Foreground(muted)
	mdBold    = inkStyle.Bold(true)
)

func renderMarkdown(text string, width int) string {
	if width < 4 {
		width = 4
	}
	var out []string
	inFence := false
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			out = append(out, mutedStyle.Faint(true).Render(truncate("  "+trimmed, width)))
			continue
		}
		if inFence {
			out = append(out, mdCode.Render(truncate("  "+strings.TrimRight(line, " "), width)))
			continue
		}
		switch {
		case trimmed == "":
			out = append(out, "")
		case strings.HasPrefix(trimmed, "/") && !strings.ContainsAny(trimmed, " \t"):
			out = append(out, pathLink(trimmed, width))
		case strings.HasPrefix(trimmed, "#"):
			heading := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
			out = append(out, mdHeading.Render(truncate(heading, width)))
		case strings.HasPrefix(trimmed, "> "):
			for _, wrapped := range strings.Split(wrapText(strings.TrimPrefix(trimmed, "> "), width-2), "\n") {
				out = append(out, mdQuote.Render("│ "+wrapped))
			}
		case strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") || strings.HasPrefix(trimmed, "+ "):
			body := renderInlineMarkdown(trimmed[2:])
			for index, wrapped := range strings.Split(wrapText(body, width-2), "\n") {
				prefix := "• "
				if index > 0 {
					prefix = "  "
				}
				out = append(out, prefix+wrapped)
			}
		default:
			out = append(out, strings.Split(wrapText(renderInlineMarkdown(line), width), "\n")...)
		}
	}
	return strings.Join(out, "\n")
}

// renderInlineMarkdown styles `code` and **bold** spans. Splitting on the
// delimiters keeps it allocation-cheap and impossible to break: an unpaired
// delimiter leaves the text untouched.
func renderInlineMarkdown(text string) string {
	if strings.Count(text, "`") >= 2 {
		parts := strings.Split(text, "`")
		var rebuilt strings.Builder
		for index, part := range parts {
			if index%2 == 1 && index < len(parts)-(len(parts)%2) {
				rebuilt.WriteString(mdCode.Render(part))
			} else {
				rebuilt.WriteString(styleBold(part))
			}
		}
		return rebuilt.String()
	}
	return styleBold(text)
}

// pathLink renders an absolute file path as an OSC 8 hyperlink: the display
// text may be shortened to fit, but the link target is always the full path,
// so cmd-click opens the real file even when the line was too narrow to show
// all of it.
func pathLink(path string, width int) string {
	return artifactLink(path, path, "", width)
}

func artifactLink(target, display, glyph string, width int) string {
	prefix := glyph
	if prefix != "" {
		prefix += " "
	}
	room := max(1, width-lipgloss.Width(prefix))
	display = truncate(display, room)
	styled := powderStyle.Underline(true).Render(display)
	return mutedStyle.Render(prefix) + osc8FileLink(target, styled)
}

func osc8FileLink(target, display string) string {
	link := (&url.URL{Scheme: "file", Path: target}).String()
	return "\x1b]8;;" + link + "\x1b\\" + display + "\x1b]8;;\x1b\\"
}

// workspaceLink is one remembered answer from the resolver. An entry that is
// only asked carries the sole answer a render is allowed to give — plain text —
// and holds the place until the real one arrives.
type workspaceLink struct {
	target string
	found  bool
	asked  bool
}

// workspaceQuestion is one lookup a render walked past without an answer.
type workspaceQuestion struct {
	key      string
	nodeID   string
	relative string
}

// workspaceLinksMsg carries a batch of answers back from the resolver.
type workspaceLinksMsg struct {
	epoch   uint64
	answers []workspaceAnswer
}

type workspaceAnswer struct {
	key    string
	nodeID string
	link   workspaceLink
}

// resolveWorkspacePath answers from memory, and from memory only. Every
// whitespace-separated token of every answer on screen is a question, and in
// the running product each one is a row read out of SQLite, a walk up the
// node's parents for the job it belongs to, and a stat — so a forty-line reply
// was several hundred queries, made inside Update with the frame waiting on
// them. A question memory cannot answer is written down here and asked off the
// render path; until the answer lands the token is drawn as what it is.
//
// What makes remembering safe is the stamp: a job that is still running is
// still writing files, so "no such file" is only durable for as long as the
// node's status holds. When the status moves the question is asked again, which
// is the moment a finished job's deliverables become links.
func (m *Model) resolveWorkspacePath(nodeID, relative string) (string, bool) {
	key := workspaceLinkKey(nodeID, m.workspaceStamp(nodeID), relative)
	if link, remembered := m.workspaceLinks[key]; remembered {
		return link.target, link.found
	}
	// The placeholder is what keeps one question from being asked once per
	// frame for as long as the answer is in flight.
	m.keepWorkspaceLink(key, nodeID, workspaceLink{asked: true})
	m.workspaceAsk = append(m.workspaceAsk,
		workspaceQuestion{key: key, nodeID: nodeID, relative: relative})
	return "", false
}

// resolveWorkspacePathNow is the form for the caller that cannot draw something
// else and try again: copying a file needs the file. Nothing on the render path
// may use it.
func (m *Model) resolveWorkspacePathNow(nodeID, relative string) (string, bool) {
	key := workspaceLinkKey(nodeID, m.workspaceStamp(nodeID), relative)
	if link, remembered := m.workspaceLinks[key]; remembered && !link.asked {
		return link.target, link.found
	}
	target, found := askWorkspacePath(m.commander, nodeID, relative)
	m.keepWorkspaceLink(key, nodeID, workspaceLink{target: target, found: found})
	return target, found
}

func workspaceLinkKey(nodeID, stamp, relative string) string {
	return nodeID + "\x00" + stamp + "\x00" + relative
}

// keepWorkspaceLink writes an answer down. Only a file that exists changes what
// the thread draws, so only a file that exists moves the node's link
// generation — and the generation is what retires the blocks that name that
// node, and only those.
func (m *Model) keepWorkspaceLink(key, nodeID string, link workspaceLink) {
	if m.workspaceLinks == nil {
		m.workspaceLinks = make(map[string]workspaceLink, 256)
	}
	previous, remembered := m.workspaceLinks[key]
	m.workspaceLinks[key] = link
	if !link.found || (remembered && previous.found) {
		return
	}
	if m.workspaceNodeGen == nil {
		m.workspaceNodeGen = make(map[string]uint64, 8)
	}
	m.workspaceNodeGen[nodeID]++
}

// askWorkspaceLinks hands every question the last render wrote down to the
// resolver, on a command rather than on the UI thread. The commander is taken
// here rather than read there: a window that promotes mid-flight replaces it.
func (m *Model) askWorkspaceLinks() tea.Cmd {
	if len(m.workspaceAsk) == 0 {
		return nil
	}
	questions := m.workspaceAsk
	m.workspaceAsk = nil
	commander, epoch := m.commander, m.workspaceGen
	return func() tea.Msg {
		answers := make([]workspaceAnswer, 0, len(questions))
		for _, question := range questions {
			target, found := askWorkspacePath(commander, question.nodeID, question.relative)
			answers = append(answers, workspaceAnswer{
				key: question.key, nodeID: question.nodeID,
				link: workspaceLink{target: target, found: found},
			})
		}
		return workspaceLinksMsg{epoch: epoch, answers: answers}
	}
}

// applyWorkspaceLinks takes the batch back. A round of answers in which nothing
// turned out to be a file changes not one byte of the thread, and re-renders
// nothing.
func (m *Model) applyWorkspaceLinks(message workspaceLinksMsg) {
	if message.epoch != m.workspaceGen {
		// The window changed which brain it asks. These answers are about a
		// workspace it no longer has.
		return
	}
	moved := false
	for _, answer := range message.answers {
		before := m.workspaceNodeGen[answer.nodeID]
		m.keepWorkspaceLink(answer.key, answer.nodeID, answer.link)
		moved = moved || m.workspaceNodeGen[answer.nodeID] != before
	}
	if !moved {
		return
	}
	m.refreshChat()
	if m.nodeViewID != "" {
		m.refreshNodeView(false)
	}
}

func askWorkspacePath(commander Commander, nodeID, relative string) (string, bool) {
	if commander == nil {
		return "", false
	}
	return commander.ResolveWorkspacePath(nodeID, relative)
}

// workspaceStamp is the one fact about a node that can change what its
// directory holds. A node the snapshots no longer carry has been folded away,
// and a folded node has finished writing.
func (m *Model) workspaceStamp(nodeID string) string {
	if node, ok := m.cardSnapshotIndex.lookup(m.cardSnapshot.Nodes, nodeID); ok {
		return string(node.Status)
	}
	if node, ok := m.snapshotIndex.lookup(m.snapshot.Nodes, nodeID); ok {
		return string(node.Status)
	}
	return ""
}

// workspaceMark names the state a node's links were answered under: the status
// that bounds what its directory holds, and the count of files found under it
// since. A block keyed by the mark is retired by an answer about its own node,
// and by nothing else.
func (m *Model) workspaceMark(nodeID string) string {
	return m.workspaceStamp(nodeID) + "/" + strconv.FormatUint(m.workspaceNodeGen[nodeID], 10)
}

// forgetWorkspaceLinks drops every remembered answer and moves the generation
// the rendered blocks are keyed by. One caller: a window that has just taken
// over the brain, and is therefore asking a different process about a different
// workspace.
func (m *Model) forgetWorkspaceLinks() {
	clear(m.workspaceLinks)
	clear(m.workspaceNodeGen)
	m.workspaceAsk = nil
	m.workspaceGen++
}

// forgetUnsettledWorkspaceLinks drops the "no such file" answers about work
// that is still running. A finished node has finished writing, so its answers
// stand; a running one may have written the file since it was asked, and the
// minute is when the thread goes and looks. Nothing is resolved here — what the
// next render finds is a question again, and a question is asked off the UI
// thread.
func (m *Model) forgetUnsettledWorkspaceLinks() {
	for key, link := range m.workspaceLinks {
		if link.found {
			continue
		}
		nodeID, _, _ := strings.Cut(key, "\x00")
		if workspaceIsSettled(m.workspaceStamp(nodeID)) {
			continue
		}
		delete(m.workspaceLinks, key)
	}
}

// workspaceIsSettled reads a stamp as "this directory is finished". The empty
// stamp is a node the snapshots have folded away, which is the most finished a
// node gets.
func workspaceIsSettled(stamp string) bool {
	switch store.Status(stamp) {
	case store.Done, store.Failed, store.Cancelled, "":
		return true
	}
	return false
}

func (m *Model) workspaceDirectoryLink(nodeID string) string {
	if m.commander == nil {
		return ""
	}
	target, found := m.commander.WorkspacePath(nodeID)
	if !found {
		return ""
	}
	return osc8FileLink(target, controlStyle.Render("▸ workspace"))
}

func (m *Model) renderMediaArtifacts(message store.Message, width int) string {
	paths := make([]string, 0, len(message.Attachments))
	for _, attachment := range message.Attachments {
		// An attachment is journaled as a reference to our own copy; what a
		// person wants to click is still their own file, under its own name.
		paths = append(paths, cas.SourcePath(attachment))
	}
	paths = append(paths, mediaReferences(message.Body)...)
	return m.renderMediaPaths(message.NodeID, paths, width)
}

// renderMediaPaths is the same rendering from a list of references already in
// hand, for the one caller — the activity feed — that scans an append-only
// file and keeps what it found rather than reading it again.
func (m *Model) renderMediaPaths(nodeID string, paths []string, width int) string {
	seen := make(map[string]bool, len(paths))
	lines := make([]string, 0, len(paths))
	for _, path := range paths {
		path = filepath.Clean(path)
		if seen[path] {
			continue
		}
		seen[path] = true
		slot, ok := mediaSlot(path)
		if !ok {
			continue
		}
		glyph := m.icon(slot)
		target := path
		if !filepath.IsAbs(target) {
			var found bool
			target, found = m.resolveWorkspacePath(nodeID, path)
			if !found {
				continue
			}
		}
		lines = append(lines, artifactLink(target, filepath.ToSlash(path), glyph, width))
	}
	return strings.Join(lines, "\n")
}

var workspaceToken = regexp.MustCompile(`\S+`)

type workspaceReferenceSpan struct {
	start int
	end   int
}

// linkWorkspaceReferences turns only existing workspace-relative files into
// inline OSC 8 links. Escape sequences wrap the original bytes, so punctuation,
// markdown, and visible text remain byte-for-byte recognizable after ANSI is
// stripped. The resolver enforces both containment and existence.
func (m *Model) linkWorkspaceReferences(nodeID, body string) string {
	if nodeID == "" || body == "" {
		return body
	}
	spans := workspaceReferenceSpans(body)
	if len(spans) == 0 {
		return body
	}
	var linked strings.Builder
	linked.Grow(len(body))
	previous := 0
	for _, span := range spans {
		if span.start < previous {
			continue
		}
		candidate := body[span.start:span.end]
		if candidate == "" || filepath.IsAbs(candidate) || strings.Contains(candidate, "://") {
			continue
		}
		target, found := m.resolveWorkspacePath(nodeID, filepath.Clean(candidate))
		if !found {
			continue
		}
		linked.WriteString(body[previous:span.start])
		linked.WriteString(osc8FileLink(target, candidate))
		previous = span.end
	}
	if previous == 0 {
		return body
	}
	linked.WriteString(body[previous:])
	return linked.String()
}

func workspaceReferenceSpans(body string) []workspaceReferenceSpan {
	spans := make([]workspaceReferenceSpan, 0)
	// A quoted or inline-code path may contain spaces. Try the complete visible
	// span first; existence checking keeps ordinary quoted prose untouched.
	for start := 0; start < len(body); start++ {
		quote := body[start]
		if quote != '\'' && quote != '"' && quote != '`' {
			continue
		}
		if end := strings.IndexByte(body[start+1:], quote); end >= 0 {
			end += start + 1
			if end > start+1 && !strings.ContainsAny(body[start+1:end], "\r\n") {
				spans = append(spans, workspaceReferenceSpan{start: start + 1, end: end})
			}
			start = end
		}
	}
	for _, match := range workspaceToken.FindAllStringIndex(body, -1) {
		start, end := match[0], match[1]
		token := body[start:end]
		leading := len(token) - len(strings.TrimLeft(token, "\"'`()[]{}<>*"))
		trimmed := strings.TrimRight(token[leading:], "\"'`()[]{}<>,.!?:;*")
		if len(trimmed) > 0 {
			spans = append(spans, workspaceReferenceSpan{start: start + leading, end: start + leading + len(trimmed)})
		}
	}
	sort.SliceStable(spans, func(i, j int) bool {
		if spans[i].start != spans[j].start {
			return spans[i].start < spans[j].start
		}
		return spans[i].end > spans[j].end
	})
	return spans
}

func mediaReferences(body string) []string {
	var paths []string
	for _, field := range strings.Fields(body) {
		candidate := strings.Trim(field, "\"'`()[]{}<>,.!?:;")
		if strings.HasPrefix(filepath.ToSlash(candidate), "media/") {
			if _, ok := mediaSlot(candidate); ok {
				paths = append(paths, candidate)
			}
		}
	}
	return paths
}

// mediaSlot says WHAT KIND of thing a path names, as a slot in the shared
// vocabulary rather than as a character, so the chip a person sees is drawn in
// whichever repertoire their terminal has (icons.go). It reports false for
// anything this surface does not draw a chip for, which is what the reference
// scanner reads it for.
func mediaSlot(path string) (tokens.GlyphID, bool) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif":
		return tokens.GFileImage, true
	case ".pdf":
		return tokens.GFileDocument, true
	case ".mp3", ".wav", ".m4a", ".ogg", ".flac":
		return tokens.GFileAudio, true
	case ".mp4", ".mov", ".m4v", ".webm":
		return tokens.GFileVideo, true
	default:
		return tokens.GFileDocument, false
	}
}

func styleBold(text string) string {
	if strings.Count(text, "**") < 2 {
		return inputTextStyle.Render(text)
	}
	parts := strings.Split(text, "**")
	var rebuilt strings.Builder
	for index, part := range parts {
		if index%2 == 1 && index < len(parts)-(len(parts)%2) {
			rebuilt.WriteString(mdBold.Render(part))
		} else {
			rebuilt.WriteString(inputTextStyle.Render(part))
		}
	}
	return rebuilt.String()
}
