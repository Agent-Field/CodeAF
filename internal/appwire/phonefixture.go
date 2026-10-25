package appwire

// PhoneFixtureAsk is the one durable ask a test build opened without a model
// call. The phone reads the same ask from its snapshot; these fields name it.
type PhoneFixtureAsk struct {
	Agent  string `json:"agent"`
	Ask    string `json:"ask"`
	Option string `json:"option"`
	Title  string `json:"title"`
}

// PhoneFixtureOpener is optional. An engine without it, and a normal app that
// was not started for the phone fixture, refuses testing.open_ask.
type PhoneFixtureOpener interface {
	OpenPhoneFixture() (PhoneFixtureAsk, error)
}

func init() {
	methods["testing.open_ask"] = typed(func(e Engine, _ struct{}) (*PhoneFixtureAsk, error) {
		opener, ok := e.(PhoneFixtureOpener)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve testing.open_ask")
		}
		ask, err := opener.OpenPhoneFixture()
		if err != nil {
			return nil, err
		}
		return &ask, nil
	})
}
