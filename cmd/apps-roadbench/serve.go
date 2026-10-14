package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/Agent-Field/codeaf/internal/appapi"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/modelsource"
)

// server is one `codeaf app serve` process on one road, launched the way the
// desk launches it: a handoff line on stdin, a hello line on stdout.
type server struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	hello appapi.Hello
	home  string
	road  string
}

// startServe launches the binary with the road and model in ITS environment
// only. The first stdin line is the key handoff, the way the desk gives the
// served process its OpenRouter key. Stdin stays open, because closing it is
// how the app stops.
func startServe(ctx context.Context, bin, road, model, home string) (*server, error) {
	if err := os.MkdirAll(home, 0o700); err != nil {
		return nil, err
	}
	logFile, err := os.Create(filepath.Join(home, "serve.stderr.log"))
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, bin, "app", "serve", "--home", home, "--listen", "127.0.0.1:0")
	cmd.Env = append(os.Environ(), "CODEAF_APPS_BUILDER_ROAD="+road, "CODEAF_APPS_BUILDER_MODEL="+model)
	cmd.Stderr = logFile
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	s := &server{cmd: cmd, stdin: stdin, home: home, road: road}
	if err := writeHandoff(stdin); err != nil {
		s.stop()
		return nil, err
	}
	if err := s.readHello(stdout); err != nil {
		s.stop()
		return nil, err
	}
	return s, nil
}

func (s *server) readHello(stdout io.Reader) error {
	line := make(chan string, 1)
	go func() {
		text, _ := bufio.NewReader(stdout).ReadString('\n')
		line <- text
	}()
	select {
	case text := <-line:
		if err := json.Unmarshal([]byte(text), &s.hello); err != nil || s.hello.URL == "" {
			return fmt.Errorf("no hello from app serve (road %s); see %s", s.road, filepath.Join(s.home, "serve.stderr.log"))
		}
		return nil
	case <-time.After(60 * time.Second):
		return fmt.Errorf("app serve (road %s) printed no hello in 60s", s.road)
	}
}

// stop closes stdin so the app shuts itself down, and kills only this pid if
// it has not left in 10 seconds.
func (s *server) stop() {
	_ = s.stdin.Close()
	done := make(chan struct{})
	go func() { _ = s.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		_ = s.cmd.Process.Kill()
		<-done
	}
}

// writeHandoff sends the profile's OpenRouter key on the pipe, as the desk
// does. The key is never printed or logged.
func writeHandoff(w io.Writer) error {
	key := config.APIKeyAt(config.ProfilePath(config.ProfileDir(), ""))
	if key == "" {
		return errors.New("no OpenRouter key in the codeaf profile")
	}
	line, err := json.Marshal(map[string]map[string]string{"keys": {modelsource.DefaultID: key}})
	if err != nil {
		return err
	}
	_, err = w.Write(append(line, '\n'))
	return err
}
