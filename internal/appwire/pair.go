package appwire

import "time"

const (
	FeatureDevicesPair = "devices.pair"
	EventPairChanged   = "pair.changed"
)

type PairStartParams struct {
	Mode string `json:"mode" enum:"offer_code,use_code,request_link,approve_link"`
	Code string `json:"code,omitempty"`
	Link string `json:"link,omitempty"`
	Name string `json:"name,omitempty"`
}
type PairGetParams struct {
	ID string `json:"id,omitempty"`
}
type PairAnswerParams struct {
	ID       string `json:"id"`
	Question string `json:"question"`
	Option   string `json:"option"`
}
type PairOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}
type PairQuestion struct {
	ID      string       `json:"id"`
	Kind    string       `json:"kind" enum:"approve,replace_own_chats"`
	Title   string       `json:"title"`
	Detail  string       `json:"detail,omitempty"`
	Options []PairOption `json:"options"`
}
type PairError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type Pairing struct {
	ID        string        `json:"id"`
	Mode      string        `json:"mode" enum:"offer_code,use_code,request_link,approve_link"`
	State     string        `json:"state" enum:"preparing,waiting,question,done,failed,cancelled"`
	Lines     []string      `json:"lines"`
	Code      string        `json:"code,omitempty"`
	Link      string        `json:"link,omitempty"`
	Words     string        `json:"words,omitempty"`
	ExpiresAt time.Time     `json:"expires_at,omitzero"`
	Question  *PairQuestion `json:"question,omitempty"`
	Error     *PairError    `json:"error,omitempty"`
	Restart   bool          `json:"restart,omitempty"`
}
type PairResult struct {
	Pairing Pairing `json:"pairing"`
}
type PairGetResult struct {
	Pairing *Pairing `json:"pairing,omitempty"`
}

type Pairer interface {
	StartPair(PairStartParams) (Pairing, error)
	GetPair(string) (*Pairing, error)
	AnswerPair(PairAnswerParams) (Pairing, error)
	CancelPair(string) (Pairing, error)
}

func withPairFeatures(h Hello, e Engine) Hello {
	if d, ok := e.(interface{ PairingAvailable() bool }); ok && !d.PairingAvailable() {
		return h
	}
	if _, ok := e.(Pairer); ok {
		h = withFeature(h, FeatureDevicesPair)
	}
	return h
}
func init() {
	methods["pair.start"] = typed(func(e Engine, p PairStartParams) (*PairResult, error) {
		d, ok := e.(Pairer)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not pair computers")
		}
		switch p.Mode {
		case "offer_code", "request_link":
		case "use_code":
			if _, err := required("code", p.Code); err != nil {
				return nil, err
			}
		case "approve_link":
			if _, err := required("link", p.Link); err != nil {
				return nil, err
			}
		default:
			return nil, Errorf(CodeBadParams, "unknown pairing mode")
		}
		got, err := d.StartPair(p)
		return &PairResult{Pairing: pairingArrays(got)}, err
	})
	methods["pair.get"] = typed(func(e Engine, p PairGetParams) (*PairGetResult, error) {
		d, ok := e.(Pairer)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not pair computers")
		}
		got, err := d.GetPair(p.ID)
		if got != nil {
			copy := pairingArrays(*got)
			got = &copy
		}
		return &PairGetResult{Pairing: got}, err
	})
	methods["pair.answer"] = typed(func(e Engine, p PairAnswerParams) (*PairResult, error) {
		d, ok := e.(Pairer)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not pair computers")
		}
		for field, value := range map[string]string{"id": p.ID, "question": p.Question, "option": p.Option} {
			if _, err := required(field, value); err != nil {
				return nil, err
			}
		}
		got, err := d.AnswerPair(p)
		return &PairResult{Pairing: pairingArrays(got)}, err
	})
	methods["pair.cancel"] = typed(func(e Engine, p IDParams) (*PairResult, error) {
		d, ok := e.(Pairer)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not pair computers")
		}
		if _, err := required("id", p.ID); err != nil {
			return nil, err
		}
		got, err := d.CancelPair(p.ID)
		return &PairResult{Pairing: pairingArrays(got)}, err
	})
}

func pairingArrays(p Pairing) Pairing {
	p.Lines = nonNil(p.Lines)
	if p.Question != nil {
		q := *p.Question
		q.Options = nonNil(q.Options)
		p.Question = &q
	}
	return p
}
