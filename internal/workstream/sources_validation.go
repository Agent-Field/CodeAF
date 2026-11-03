package workstream

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

var sourceRepoPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}/[A-Za-z0-9._-]{1,100}$`)

// ValidateSourceAdd checks kind fields and local existence without writing or reading contents.
func ValidateSourceAdd(p SourceAdd) error {
	_, err := prepareSource(p)
	return err
}

func ValidateCreateSources(sources []SourceAdd) error {
	_, err := prepareCreateSources(sources)
	return err
}

func prepareCreateSources(sources []SourceAdd) ([]preparedSource, error) {
	if len(sources) > CreateSourcesMax {
		return nil, invalid("create takes at most 200 sources")
	}
	out := make([]preparedSource, 0, len(sources))
	for _, p := range sources {
		if p.Kind == SourceKept {
			return nil, invalid("create cannot keep a source")
		}
		prepared, err := prepareSource(p)
		if err != nil {
			return nil, err
		}
		out = append(out, prepared)
	}
	return out, nil
}

func sourceTitle(title *string) error {
	if title == nil {
		return nil
	}
	if len(*title) == 0 || !lineWithin(*title, SourceTitleMax) || strings.TrimSpace(*title) != *title || strings.TrimSpace(*title) == "" || strings.ContainsAny(*title, "\u2028\u2029") {
		return invalid("source title must be 1–200 bytes of trimmed single-line text")
	}
	return nil
}

func ValidateSourceWhy(why string) error {
	if len(why) == 0 || !textWithin(why, SourceWhyMax) {
		return invalid("a source proposal needs why of 1–500 bytes")
	}
	return nil
}

func sourcePath(path string, folder bool) error {
	if len(path) == 0 || !lineWithin(path, SourcePathMax) || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return invalid("source path must be an existing clean absolute path of 1–4096 bytes")
	}
	info, err := os.Stat(path)
	if err != nil {
		return invalid("nothing readable exists at the source path")
	}
	if folder && !info.IsDir() {
		return invalid("a local repo must be a folder")
	}
	return nil
}

func sourceRepoName(repo string) bool {
	if !sourceRepoPattern.MatchString(repo) {
		return false
	}
	_, name, _ := strings.Cut(repo, "/")
	return name != "." && name != ".."
}

func sourceURL(raw string) (string, string, error) {
	if len(raw) == 0 || len(raw) > SourceURLMax || !utf8.ValidString(raw) || strings.IndexFunc(raw, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return "", "", invalid("source URL must be 1–2048 bytes without whitespace or controls")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || !strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https") {
		return "", "", invalid("source URL needs an http or https scheme, a host and no credentials")
	}
	u.Scheme, u.Host = strings.ToLower(u.Scheme), strings.ToLower(u.Host)
	stored := u.String()
	if len(stored) > SourceURLMax {
		return "", "", invalid("normalized source URL must be at most " + strconv.Itoa(SourceURLMax) + " bytes")
	}
	return stored, strings.TrimRight(u.Host+u.Path, "/"), nil
}

func sourceFields(p SourceAdd) error {
	if p.Path != nil && p.Kind != SourceFile || p.Repo != nil && p.Kind != SourceRepo && p.Kind != SourceIssue || p.Number != nil && p.Kind != SourceIssue || p.URL != nil && p.Kind != SourceLink || p.Ref != nil && p.Kind != SourceKept {
		return invalid("source has a field used by another kind")
	}
	return sourceTitle(p.Title)
}

func prepareSource(p SourceAdd) (preparedSource, error) {
	out := preparedSource{SourceAdd: p}
	if err := sourceFields(p); err != nil {
		return out, err
	}
	var err error
	switch p.Kind {
	case SourceFile:
		err = prepareFileSource(&out)
	case SourceRepo:
		err = prepareRepoSource(&out)
	case SourceIssue:
		err = prepareIssueSource(&out)
	case SourceLink:
		err = prepareLinkSource(&out)
	case SourceKept:
		err = prepareKeptSource(&out)
	default:
		err = invalid("unknown source kind")
	}
	if p.Title != nil {
		out.title = *p.Title
	} else {
		out.title = sourceDerivedTitle(out.title)
	}
	return out, err
}

func prepareFileSource(p *preparedSource) error {
	if p.Path == nil {
		return invalid("file source needs path")
	}
	if err := sourcePath(*p.Path, false); err != nil {
		return err
	}
	p.target, p.title = *p.Path, filepath.Base(*p.Path)
	return nil
}

func prepareRepoSource(p *preparedSource) error {
	if p.Repo == nil {
		return invalid("repo source needs repo")
	}
	p.target, p.title = strings.ToLower(*p.Repo), *p.Repo
	if sourceRepoName(*p.Repo) {
		return nil
	}
	if err := sourcePath(*p.Repo, true); err != nil {
		return err
	}
	p.target, p.title = *p.Repo, filepath.Base(*p.Repo)
	return nil
}

func prepareIssueSource(p *preparedSource) error {
	if p.Repo == nil || !sourceRepoName(*p.Repo) || p.Number == nil || *p.Number < 1 || *p.Number > SourceIssueMax {
		return invalid("issue needs owner/name and number 1–2147483647")
	}
	p.title = *p.Repo + "#" + strconv.Itoa(*p.Number)
	p.target = strings.ToLower(p.title)
	return nil
}

func prepareLinkSource(p *preparedSource) error {
	if p.URL == nil {
		return invalid("link source needs url")
	}
	stored, title, err := sourceURL(*p.URL)
	if err != nil {
		return err
	}
	p.URL, p.target, p.title = &stored, stored, title
	return nil
}

func prepareKeptSource(p *preparedSource) error {
	if err := sourceKeptRef(p.Ref); err != nil {
		return err
	}
	b, err := json.Marshal(p.Ref)
	p.target = string(b)
	return err
}

func sourceRefID(id string) bool {
	return len(id) > 0 && len(id) <= SourceRefMax && utf8.ValidString(id)
}

func sourceKeptRef(ref *KeptRef) error {
	if ref == nil || !sourceRefID(ref.ID) {
		return invalid("kept source needs a ref with an id of 1–128 bytes")
	}
	if ref.Kind != "message" && ref.ChatID != nil || ref.Kind != "factory_job" && ref.Factory != nil {
		return invalid("kept ref has fields used by another kind")
	}
	switch ref.Kind {
	case "record", "page":
	case "message":
		if ref.ChatID == nil || !sourceRefID(*ref.ChatID) {
			return invalid("kept message needs chat_id of 1–128 bytes")
		}
	case "factory_job":
		if ref.Factory == nil || !sourceRefID(*ref.Factory) {
			return invalid("kept job needs factory of 1–128 bytes")
		}
		if _, err := sourceJobNumber(ref.ID); err != nil {
			return err
		}
	default:
		return invalid("unknown kept ref kind")
	}
	return nil
}

func sourceJobNumber(id string) (int, error) {
	if strings.IndexFunc(id, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		return 0, invalid("kept job needs a positive decimal job number")
	}
	n, err := strconv.Atoi(id)
	if err != nil || n < 1 {
		return 0, invalid("kept job needs a positive decimal job number")
	}
	return n, nil
}

func sourceKeptIn(ctx context.Context, tx *sql.Tx, id string, p *preparedSource) error {
	if p.Kind != SourceKept {
		return nil
	}
	var title string
	switch p.Ref.Kind {
	case "record":
		if err := tx.QueryRowContext(ctx, "SELECT text FROM workstream_records WHERE id=? AND workstream_id=?", p.Ref.ID, id).Scan(&title); err != nil {
			return found(err)
		}
		title, _, _ = strings.Cut(title, "\n")
	case "page":
		if err := tx.QueryRowContext(ctx, "SELECT title FROM workstream_pages WHERE id=? AND workstream_id=? AND deleted_at IS NULL", p.Ref.ID, id).Scan(&title); err != nil {
			return found(err)
		}
	case "message":
		title = "Kept message"
	case "factory_job":
		if err := sourceKeptJobIn(ctx, tx, id, p.Ref); err != nil {
			return err
		}
		title = "Job " + p.Ref.ID
	}
	if p.Title == nil {
		p.title = sourceDerivedTitle(title)
	}
	return nil
}

func sourceKeptJobIn(ctx context.Context, tx *sql.Tx, id string, ref *KeptRef) error {
	number, err := sourceJobNumber(ref.ID)
	if err != nil {
		return err
	}
	var member bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM workstream_jobs WHERE workstream_id=? AND factory=? AND job=?)", id, *ref.Factory, strconv.Itoa(number)).Scan(&member); err != nil {
		return err
	}
	if !member {
		return invalid("kept job is not a member of this workstream")
	}
	return nil
}

func sourceDerivedTitle(title string) string {
	title = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return ' '
		}
		return r
	}, title))
	if title == "" {
		title = "Source"
	}
	if len(title) <= SourceTitleMax {
		return title
	}
	end := SourceTitleMax
	for end > 0 && !utf8.RuneStart(title[end]) {
		end--
	}
	return strings.TrimSpace(title[:end])
}
