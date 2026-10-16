package appwire

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
)

// MEDIA.LIST and MEDIA.GET: the media files a conversation made or was handed,
// as structured facts the app can draw a card from without reading prose.
//
// A source is GRANTED BY THE ENGINE. The app names a source by the opaque id the
// engine gave it, and the engine answers with the path it recorded; there is no
// parameter through which a caller can name a path, so there is no way to ask
// the engine to vouch for one. Whether a file may be opened is the engine's
// answer in State, and only [MediaAvailable] may be.
//
// Unknown stays unknown: a size, a duration or a charge nothing reported is
// absent, never zero. The capability is optional — an engine that is not a
// [MediaReader] refuses both methods as unknown_method, which an app must read
// as "this host cannot say", not as "no media".

// MediaPageDefault and MediaPageMax bound one media.list page.
const EventMediaChanged = "media.changed"

// MediaChanged invalidates sources after a successful index append. The source
// descriptor must still be read through the authorized media APIs.
type MediaSourceChanged struct {
	Agent  string `json:"agent"`
	Source string `json:"source"`
	Run    string `json:"run,omitempty"`
	Call   string `json:"call,omitempty"`
}

const (
	MediaPageDefault = 50
	MediaPageMax     = 100
)

// The bounds of a media reply. The transport refuses a frame far above these;
// they are held well below it, and they are checked BEFORE anything is marshaled
// so a bad record can neither blow memory nor be cut in the middle.
const (
	// MediaReplyBudget is the most one media.list or media.get answer may
	// carry as JSON, escaping included.
	MediaReplyBudget = 4 << 20
	// mediaStringMax and mediaArrayMax bound one field of a record.
	mediaStringMax = 256 << 10
	mediaArrayMax  = 256
	// mediaSourceEstimateMax bounds one record's worst-case size (every byte
	// escaped to six) before it is marshaled at all.
	mediaSourceEstimateMax = 1 << 20
)

// ErrMediaTooLarge is a record that cannot be sent within the bounds.
var ErrMediaTooLarge = errors.New("a media record is too large to send")

// MediaSourceSize validates one record against the field bounds and answers the
// exact number of bytes it will take as JSON, escaping included. A record with
// a string or an array past its bound, or whose worst case is past the record
// bound, is [ErrMediaTooLarge] and is not marshaled.
func MediaSourceSize(source MediaSource) (int, error) {
	estimate := 512
	value := reflect.ValueOf(source)
	for i := 0; i < value.NumField(); i++ {
		field := value.Field(i)
		switch field.Kind() {
		case reflect.String:
			if field.Len() > mediaStringMax {
				return 0, ErrMediaTooLarge
			}
			estimate += field.Len() * 6
		case reflect.Slice:
			if field.Len() > mediaArrayMax {
				return 0, ErrMediaTooLarge
			}
			for j := 0; j < field.Len(); j++ {
				if item := field.Index(j); item.Kind() == reflect.String {
					if item.Len() > mediaStringMax {
						return 0, ErrMediaTooLarge
					}
					estimate += item.Len()*6 + 4
				}
			}
		}
		if estimate > mediaSourceEstimateMax {
			return 0, ErrMediaTooLarge
		}
	}
	raw, err := json.Marshal(source)
	if err != nil {
		return 0, err
	}
	return len(raw), nil
}

func mediaReplyError() error {
	return Errorf(CodeEngine, "the media answer is larger than the reply bound of %d bytes", MediaReplyBudget)
}

// What a source is now.
const (
	MediaAvailable  = "available"  // a regular file, in scope, as recorded: may be opened
	MediaChanged    = "changed"    // there, but its size or time differ from the record: never opened
	MediaMissing    = "missing"    // gone, or a dangling link
	MediaOutside    = "outside"    // resolves somewhere the engine will not vouch for
	MediaUnreadable = "unreadable" // not a regular file, or not readable
)

// MediaSource is one media file and what the engine recorded about it.
//
// Everything but State, LiveVersion and LiveBytes is the RECORD and is returned
// unchanged whatever the file is now: a missing file keeps the size it had.
type MediaSource struct {
	ID     string `json:"id"`
	Kind   string `json:"kind" enum:"image,audio,video,document"`
	Origin string `json:"origin" enum:"attachment,generated,evidence"`
	// Path is the recorded canonical path, for display and for the app's
	// native side to open ONLY when State is available.
	Path  string `json:"path"`
	State string `json:"state" enum:"available,changed,missing,outside,unreadable"`
	// Recorded says the engine recorded structured facts with this source;
	// false is an older record that knows only its path, kind and time.
	Recorded bool `json:"recorded"`
	// Version is the recorded version: sha256:<hex> where the engine held the
	// bytes, else file:<size>:<mtime_ns>:<ctime_ns>:<dev>:<ino> as recorded.
	// Absent for an older record. LiveVersion is the file's identity now, in
	// the file: form; a caller that opens the file compares it again.
	Version     string `json:"version,omitempty"`
	LiveVersion string `json:"live_version,omitempty"`
	MIME        string `json:"mime,omitempty"`
	// Bytes is the recorded size; LiveBytes the size now, when it is a file.
	Bytes      *int64 `json:"bytes,omitempty"`
	LiveBytes  *int64 `json:"live_bytes,omitempty"`
	Width      *int   `json:"width,omitempty"`
	Height     *int   `json:"height,omitempty"`
	DurationMS *int64 `json:"duration_ms,omitempty"`
	// Probed says Width and Height were read from the file's header just now
	// because the record had none.
	Probed  bool   `json:"probed,omitempty"`
	Created string `json:"created"`
	Title   string `json:"title,omitempty"`
	// Model is the model the call ASKED for, kept for compatibility;
	// RequestedModel says the same thing under the name that cannot be misread.
	// No provider reports which model served a call, so there is no served
	// model on this record, and none is guessed.
	Model          string `json:"model,omitempty"`
	RequestedModel string `json:"requested_model,omitempty"`
	// RequestedCount is how many files the request asked for in one call; files
	// with the same Tool id were asked for together.
	RequestedCount *int `json:"requested_count,omitempty"`
	// Run is the turn's immutable execution id and Tool the provider's id for
	// the tool call that made the file, both read off the call itself.
	Run                  string   `json:"run,omitempty"`
	Message              string   `json:"message,omitempty"`
	Tool                 string   `json:"tool,omitempty"`
	References           []string `json:"references,omitempty"`
	InputMessages        []string `json:"input_messages,omitempty"`
	InputMessagesOmitted bool     `json:"input_messages_omitted,omitempty"`
	Said                 string   `json:"said,omitempty"`
	// Evidence only (origin evidence — a file the agent looked at): Verb is
	// looked_at · listened_to · watched · read; Saw is the looking model's words
	// verbatim (SawOmitted when too long to keep whole); SeenBy is the model
	// that looked and WorkingModel the one the session was running, which an
	// app says only when they differ; Transcribed says the words are a speech
	// transcript. An app shows the person's own attachments once, by comparing
	// an evidence source's path with the attachment sources' paths.
	Verb         string `json:"verb,omitempty"`
	Saw          string `json:"saw,omitempty"`
	SawOmitted   bool   `json:"saw_omitted,omitempty"`
	SeenBy       string `json:"seen_by,omitempty"`
	WorkingModel string `json:"working_model,omitempty"`
	Transcribed  bool   `json:"transcribed,omitempty"`
	// PageCount and TextLayer are on a "document" source (a PDF the agent read)
	// and only where the parser reported them: a billed parse that returns no
	// page boundaries leaves PageCount absent, and TextLayer false means the
	// extractor found a scanned file, not that nothing is known. A document is
	// evidence only (Origin evidence, Verb read) and is never an attachment or
	// generated.
	PageCount *int  `json:"page_count,omitempty"`
	TextLayer *bool `json:"text_layer,omitempty"`
	// Charge is charged, not_charged or unknown, from the provider's reported
	// usage; CostUSD is present only when charged.
	Charge  string   `json:"charge" enum:"charged,not_charged,unknown"`
	CostUSD *float64 `json:"cost_usd,omitempty"`
	// Variant fields describe one actual provider request; call cost is never per file.
	Batch         string   `json:"batch,omitempty"`
	VariantIndex  int      `json:"variant_index,omitempty"`
	ReturnedCount int      `json:"returned_count,omitempty"`
	CostScope     string   `json:"cost_scope,omitempty"`
	CallCostUSD   *float64 `json:"call_cost_usd,omitempty"`
}

// MediaPage is media.list's answer. Next resumes the listing below this page;
// absent means the whole history has been read. Incomplete says a page is short
// only because the engine spent its read budget, not because history ended —
// keep following Next.
type MediaPage struct {
	Sources    []MediaSource `json:"sources"`
	Next       string        `json:"next,omitempty"`
	Incomplete bool          `json:"incomplete,omitempty"`
}

// MediaResult is media.get's answer.
type MediaResult struct {
	Source MediaSource `json:"source"`
}

// MediaReader is the optional engine half of media.list and media.get.
type MediaReader interface {
	// ListMedia answers one page of the agent's sources, newest first.
	// limit is already clamped to 1..MediaPageMax. A cursor the engine cannot
	// resume is a bad_params error; the caller lists again from the start.
	ListMedia(agent, cursor string, limit int) (MediaPage, error)
	// GetMedia answers one source of the agent by id, found anywhere in its
	// history. An unknown id is not_found only after the whole history was
	// read; a search that ran out of time is busy.
	GetMedia(agent, source string) (MediaSource, error)
}

func init() {
	eventShapes[EventMediaChanged] = reflect.TypeFor[MediaSourceChanged]()
	methods["media.list"] = typed(func(e Engine, p struct {
		Agent  string `json:"agent"`
		Cursor string `json:"cursor"`
		Limit  int    `json:"limit"`
	}) (*MediaPage, error) {
		r, ok := e.(MediaReader)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve media.list")
		}
		if strings.TrimSpace(p.Agent) == "" || p.Limit < 0 {
			return nil, Errorf(CodeBadParams, "media.list needs an agent and a limit of zero or more")
		}
		limit := p.Limit
		if limit == 0 {
			limit = MediaPageDefault
		}
		page, err := r.ListMedia(p.Agent, p.Cursor, min(limit, MediaPageMax))
		if err != nil {
			return nil, err
		}
		if page.Sources == nil {
			page.Sources = []MediaSource{}
		}
		// The engine ends a page before the reply bound; one that did not is
		// refused whole rather than cut, because a cut page has no honest cursor.
		total := 256 + len(page.Next)
		for _, source := range page.Sources {
			size, err := MediaSourceSize(source)
			if err != nil {
				return nil, mediaReplyError()
			}
			if total += size + 1; total > MediaReplyBudget {
				return nil, mediaReplyError()
			}
		}
		return &page, nil
	})
	methods["media.get"] = typed(func(e Engine, p struct {
		Agent  string `json:"agent"`
		Source string `json:"source"`
	}) (*MediaResult, error) {
		r, ok := e.(MediaReader)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve media.get")
		}
		if strings.TrimSpace(p.Agent) == "" || strings.TrimSpace(p.Source) == "" {
			return nil, Errorf(CodeBadParams, "media.get needs an agent and a source")
		}
		source, err := r.GetMedia(p.Agent, p.Source)
		if err != nil {
			return nil, err
		}
		if size, err := MediaSourceSize(source); err != nil || size+64 > MediaReplyBudget {
			return nil, mediaReplyError()
		}
		return &MediaResult{source}, nil
	})
}
