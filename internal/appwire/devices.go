package appwire

import (
	"time"
)

const (
	FeatureDevicesManage = "devices.manage"
	// FeatureDevicesPermit says devices carry permit and devices.permit exists:
	// what each paired computer may do in chats HERE, edited in this computer's
	// own book. Absent unless remote control is on.
	FeatureDevicesPermit = "devices.permit"
	EventDevicesChanged  = "devices.changed"
	DevicesQuiet         = "quiet"
	DevicesOn            = "on"
	DevicesOff           = "off"
	DevicesUnavailable   = "unavailable"
)

// Device is a computer paired to this identity. Unknown presence is absent.
type Device struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Platform   string    `json:"platform,omitempty"`
	Self       bool      `json:"self"`
	Removed    bool      `json:"removed"`
	LastSeen   time.Time `json:"last_seen,omitzero"`
	Online     *bool     `json:"online,omitempty"`
	RemoveLine string    `json:"remove_line,omitempty"`
	// Permit is what this paired computer may do in chats on THIS one. Absent on
	// this computer's own row, on a removed one, and from an engine without the
	// feature.
	Permit *DevicePermit `json:"permit,omitempty"`
}

// DevicePermit is the two switches kept for each paired computer: whether it
// may watch chats here, and whether it may type into them.
type DevicePermit struct {
	Watch bool `json:"watch"`
	Drive bool `json:"drive"`
}

type Devices struct {
	Devices []Device `json:"devices"`
	Mode    string   `json:"mode" enum:"quiet,on,off,unavailable"`
	Line    string   `json:"line,omitempty"`
}

// DevicesResult also carries devices.changed, without a second snapshot shape.
type DevicesResult struct {
	Devices Devices `json:"devices"`
}

type DeviceRenameParams struct {
	Name string `json:"name"`
}

type DeviceListParams struct{}

// DevicePermitParams is devices.permit. Only the switches named change; one
// that is left out keeps what it was.
type DevicePermitParams struct {
	ID    string `json:"id"`
	Watch *bool  `json:"watch,omitempty"`
	Drive *bool  `json:"drive,omitempty"`
}

// DevicePermitResult is the row as it stands after the change.
type DevicePermitResult struct {
	Device *Device `json:"device,omitempty"`
	Line   string  `json:"line,omitempty"`
}

type DeviceRemoveParams struct {
	ID string `json:"id"`
}

func (p *DeviceListParams) UnmarshalJSON(raw []byte) error {
	type plain DeviceListParams
	return decodeStrict("devices.list", raw, (*plain)(p))
}

func (p *DeviceRenameParams) UnmarshalJSON(raw []byte) error {
	type plain DeviceRenameParams
	return decodeStrict("devices.rename", raw, (*plain)(p))
}

func (p *DevicePermitParams) UnmarshalJSON(raw []byte) error {
	type plain DevicePermitParams
	return decodeStrict("devices.permit", raw, (*plain)(p))
}

func (p *DeviceRemoveParams) UnmarshalJSON(raw []byte) error {
	type plain DeviceRemoveParams
	return decodeStrict("devices.remove", raw, (*plain)(p))
}

type DeviceRenameResult struct {
	Device Device `json:"device"`
	Line   string `json:"line"`
}

type DeviceRemoveResult struct {
	ID   string `json:"id"`
	Line string `json:"line"`
}

type DeviceManager interface {
	ListDevices() (Devices, error)
	RenameDevice(string) (DeviceRenameResult, error)
	RemoveDevice(string) (DeviceRemoveResult, error)
}

// DevicePermitter edits what a paired computer may do in this computer's chats.
type DevicePermitter interface {
	PermitDevice(DevicePermitParams) (DevicePermitResult, error)
}

// DevicePermitOffer is an engine's answer to whether permits are on now.
type DevicePermitOffer interface{ DevicePermitsOffered() bool }

func withDevicesFeatures(hello Hello, e Engine) Hello {
	if _, ok := e.(DeviceManager); ok {
		hello = withFeature(hello, FeatureDevicesManage)
	}
	if _, ok := e.(DevicePermitter); ok {
		if offer, asked := e.(DevicePermitOffer); asked && offer.DevicePermitsOffered() {
			hello = withFeature(hello, FeatureDevicesPermit)
		}
	}
	return hello
}

func init() {
	methods["devices.list"] = typed(func(e Engine, _ DeviceListParams) (*DevicesResult, error) {
		door, ok := e.(DeviceManager)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not manage computers")
		}
		got, err := door.ListDevices()
		got.Devices = nonNil(got.Devices)
		return &DevicesResult{Devices: got}, err
	})
	methods["devices.rename"] = typed(func(e Engine, p DeviceRenameParams) (*DeviceRenameResult, error) {
		door, ok := e.(DeviceManager)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not manage computers")
		}
		got, err := door.RenameDevice(p.Name)
		return &got, err
	})
	methods["devices.permit"] = typed(func(e Engine, p DevicePermitParams) (*DevicePermitResult, error) {
		door, ok := e.(DevicePermitter)
		if offer, asked := e.(DevicePermitOffer); !ok || !asked || !offer.DevicePermitsOffered() {
			return nil, Errorf(CodeUnknownMethod, "this engine does not set what other computers may do here")
		}
		if _, err := required("id", p.ID); err != nil {
			return nil, err
		}
		if p.Watch == nil && p.Drive == nil {
			return nil, Errorf(CodeBadParams, "name the switch to change: watch, drive or both")
		}
		got, err := door.PermitDevice(p)
		return &got, err
	})
	methods["devices.remove"] = typed(func(e Engine, p DeviceRemoveParams) (*DeviceRemoveResult, error) {
		door, ok := e.(DeviceManager)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not manage computers")
		}
		if _, err := required("id", p.ID); err != nil {
			return nil, err
		}
		got, err := door.RemoveDevice(p.ID)
		return &got, err
	})
}
