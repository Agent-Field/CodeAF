package appwire

import (
	"time"
)

const (
	FeatureDevicesManage = "devices.manage"
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

func withDevicesFeatures(hello Hello, e Engine) Hello {
	if _, ok := e.(DeviceManager); ok {
		return withFeature(hello, FeatureDevicesManage)
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
