package appwire

// FeatureSettingsRemoteControl announces the Remote control switch: the
// `remote_control` field of settings.get and settings.set, the same row the
// terminal's Settings > Devices tab writes. A window draws its switch only
// when this word is in the hello, because an engine without the field refuses
// the whole of a settings.set that names it.
const FeatureSettingsRemoteControl = "settings.remote_control"

// RemoteControlSettingsServer marks an engine that serves the Remote control
// switch. Like [MemorySettingsServer] it is a marker, because the answer is the
// same for the whole life of the engine: the row is always there, off or on.
type RemoteControlSettingsServer interface{ RemoteControlSettings() }

func withRemoteControlSettingsFeature(h Hello, e Engine) Hello {
	if _, ok := e.(RemoteControlSettingsServer); !ok {
		return h
	}
	return withFeature(h, FeatureSettingsRemoteControl)
}
