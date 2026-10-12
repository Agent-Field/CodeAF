package config

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

const AppHandoffLimit = 1 << 20

// ReadAppHandoff consumes exactly one bounded line, leaving a buffered reader
// on the same pipe for the app's lifetime. Parse errors never quote its bytes.
func ReadAppHandoff(input *bufio.Reader, source *AppCredentials) error {
	var raw []byte
	for {
		part, err := input.ReadSlice('\n')
		if len(raw)+len(part) > AppHandoffLimit {
			return errors.New("app key handoff is too large")
		}
		raw = append(raw, part...)
		if err == bufio.ErrBufferFull {
			continue
		}
		if err != nil {
			return errors.New("app key handoff needs one JSON line")
		}
		break
	}
	var body *struct {
		Keys            map[string]string `json:"keys"`
		FactorySessions map[string]string `json:"factory_sessions"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil || body == nil || decoder.Decode(new(any)) != io.EOF {
		return errors.New("app key handoff needs one JSON object")
	}
	for id, key := range body.Keys {
		if strings.TrimSpace(id) == "" || strings.ContainsAny(key, "\r\n\x00") {
			return errors.New("app key handoff contains an invalid credential")
		}
	}
	for id, key := range body.Keys {
		source.Set(id, key)
	}
	for address, token := range body.FactorySessions {
		source.SetFactorySession(address, token)
	}
	return nil
}

// PrepareAppCredentialPipe gives a local engine child a private inherited fd.
// The caller closes the resources after Start, including on a failed spawn;
// the writer is joined so a closed child cannot strand a credential goroutine.
func PrepareAppCredentialPipe(command *exec.Cmd) (func(), error) {
	source := appCredentials.Load()
	if source == nil {
		if AppMode() {
			return nil, ErrAppKeys
		}
		return func() {}, nil
	}
	keys, sessions := source.Snapshot()
	body, err := json.Marshal(struct {
		Keys            map[string]string `json:"keys"`
		FactorySessions map[string]string `json:"factory_sessions"`
	}{keys, sessions})
	if err != nil || len(body)+1 > AppHandoffLimit {
		return nil, errors.New("app key handoff is too large")
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	fd := 3 + len(command.ExtraFiles)
	command.ExtraFiles = append(command.ExtraFiles, reader)
	command.Args = append(command.Args, "--keys-fd", strconv.Itoa(fd))
	// Custom provider variables need the same treatment as conventional key
	// names, even when the shell carries a different key from the app's key.
	secretNames := map[string]bool{"GH_TOKEN": true, "GITHUB_TOKEN": true, AppModeEnv: true}
	for _, row := range NewSettings(SettingsOptions{}).Rows() {
		if row.Secret && row.Env != "" {
			secretNames[row.Env] = true
		}
	}
	for _, row := range PersistedSources(ProfileDir()) {
		if row.KeyEnv != "" {
			secretNames[row.KeyEnv] = true
		}
	}
	environment := command.Env
	if environment == nil {
		environment = os.Environ()
	}
	command.Env = nil
	for _, entry := range environment {
		name, value, _ := strings.Cut(entry, "=")
		secret := strings.HasSuffix(name, "API_KEY") || secretNames[name]
		for _, key := range keys {
			if key != "" && strings.Contains(value, key) {
				secret = true
			}
		}
		for _, token := range sessions {
			if token != "" && strings.Contains(value, token) {
				secret = true
			}
		}
		if !secret {
			command.Env = append(command.Env, entry)
		}
	}
	command.Env = append(command.Env, AppModeEnv+"=1")
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer writer.Close()
		_, _ = writer.Write(append(body, '\n'))
	}()
	return func() { _ = reader.Close(); <-done }, nil
}
