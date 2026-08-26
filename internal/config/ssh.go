package config

// SSHTransport is the local surface's policy for the ssh process carrying a
// hosted conversation. These are settings rather than environment-only pins
// because a person on a slow or unusually filtered network may need to change
// them, and the settings registry is the one supported path for such choices.
type SSHTransport struct {
	ControlPersistSeconds int
	ServerAliveSeconds    int
	ServerAliveMisses     int
	IPQoS                 string
}

const (
	KeySSHControlPersist = "ssh.control_persist_seconds"
	KeySSHServerAlive    = "ssh.server_alive_seconds"
	KeySSHServerMisses   = "ssh.server_alive_misses"
	KeySSHIPQoS          = "ssh.ip_qos"

	DefaultSSHControlPersist = 5 * 60
	DefaultSSHServerAlive    = 3
	DefaultSSHServerMisses   = 3
	DefaultSSHIPQoS          = "lowdelay"
)

var SSHIPQoSChoices = []string{"lowdelay", "af21", "none"}

// SSHTransportAt resolves all four knobs from the profile's one config file.
// A malformed hand edit falls back one field at a time rather than disabling
// every transport improvement because one value could not be read.
func SSHTransportAt(profileDir string) SSHTransport {
	settings := SSHTransport{
		ControlPersistSeconds: DefaultSSHControlPersist,
		ServerAliveSeconds:    DefaultSSHServerAlive,
		ServerAliveMisses:     DefaultSSHServerMisses,
		IPQoS:                 DefaultSSHIPQoS,
	}
	if value, ok := persistedInt(profileDir, KeySSHControlPersist); ok && value >= 0 {
		settings.ControlPersistSeconds = value
	}
	if value, ok := persistedInt(profileDir, KeySSHServerAlive); ok && value >= 0 {
		settings.ServerAliveSeconds = value
	}
	if value, ok := persistedInt(profileDir, KeySSHServerMisses); ok && value >= 0 {
		settings.ServerAliveMisses = value
	}
	if value, ok := persistedString(profileDir, KeySSHIPQoS); ok {
		for _, choice := range SSHIPQoSChoices {
			if value == choice {
				settings.IPQoS = value
				break
			}
		}
	}
	return settings
}
