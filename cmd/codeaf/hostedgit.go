package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/fsreplace"
)

func hostedPrepareGit() {
	home, gitURL := os.Getenv("HOME"), env.Get("CODEAF_HOSTED_GIT_URL")
	if home == "" || gitURL == "" {
		return
	}
	if err := writeHostedGitSetup(home, gitURL); err != nil {
		fmt.Fprintf(os.Stderr, "Could not prepare git for this chat (%q).\n", err)
	}
}

func writeHostedGitSetup(home, gitURL string) error {
	broker := hostedGitBroker(home)
	access := os.Getenv("OPENAI_API_KEY")
	if access == "" {
		if err := os.Remove(broker); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if hostedGitURL(gitURL) == nil {
		return errors.New("invalid git address")
	}
	if strings.ContainsAny(access, "\r\n") {
		return errors.New("invalid chat access")
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	var config strings.Builder
	for _, raw := range strings.Split(env.Get("CODEAF_HOSTED_GIT_HOSTS"), ",") {
		if raw == "" {
			continue
		}
		origin := hostedGitURL(raw)
		if origin == nil || origin.Path != "" {
			return errors.New("invalid git host")
		}
		// End both URLs with a slash so a host with a similar name
		// cannot match this rewrite.
		fmt.Fprintf(&config, "[url %s]\n\tinsteadOf = %s/\n", gitConfigQuote(gitURL+"/"+origin.Host+"/"), raw)
	}
	helper := "!" + shellQuote(filepath.ToSlash(executable)) + " git-credential"
	fmt.Fprintf(&config, "[credential %s]\n\thelper = %s\n\tuseHttpPath = false\n",
		gitConfigQuote(gitURL+"/"), gitConfigQuote(helper))
	if access != "" {
		if err := writeHostedGitFile(broker, "url="+gitURL+"\nbearer="+access+"\n"); err != nil {
			return err
		}
	}
	return writeHostedGitFile(filepath.Join(home, ".config", "git", "config"), config.String())
}

func gitConfigQuote(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\t", `\t`, "\b", `\b`)
	return `"` + replacer.Replace(value) + `"`
}

func hostedGitURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" ||
		u.User != nil || u.RawQuery != "" || u.ForceQuery || strings.Contains(raw, "#") || strings.HasSuffix(u.Path, "/") {
		return nil
	}
	return u
}

func hostedGitBroker(home string) string {
	return filepath.Join(home, ".config", "codeaf-hosted", "broker")
}

// Replace whole files so a helper starting during an attach sees complete
// contents, and an existing file cannot carry older permissions forward.
func writeHostedGitFile(path, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".codeaf-git-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.WriteString(content)
	if err := errors.Join(err, f.Close()); err != nil {
		return err
	}
	return fsreplace.Rename(f.Name(), path)
}

func runHostedGitCredential(args []string, in io.Reader, out io.Writer) {
	if len(args) != 1 {
		return
	}
	get := args[0] == "get"
	if !get && args[0] != "store" && args[0] != "erase" {
		return
	}
	var protocol, host string
	valid := true
	seenProtocol, seenHost := false, false
	scan := bufio.NewScanner(in)
	for scan.Scan() && scan.Text() != "" {
		if !get {
			continue
		}
		name, value, ok := strings.Cut(scan.Text(), "=")
		if !ok {
			valid = false
			continue
		}
		switch name {
		case "protocol":
			valid = valid && !seenProtocol
			protocol, seenProtocol = value, true
		case "host":
			valid = valid && !seenHost
			host, seenHost = value, true
		}
	}
	home := os.Getenv("HOME")
	if !get || !valid || scan.Err() != nil || scan.Text() != "" || protocol == "" || host == "" || home == "" {
		return
	}

	f, err := os.Open(hostedGitBroker(home))
	if err != nil {
		return
	}
	defer f.Close()
	broker := bufio.NewScanner(f)
	if !broker.Scan() {
		return
	}
	raw, ok := strings.CutPrefix(broker.Text(), "url=")
	if !ok {
		return
	}
	u := hostedGitURL(raw)
	// Repository paths are untrusted. Only the broker's exact protocol and
	// host, including its port, may receive the chat's credential.
	if u == nil || protocol != u.Scheme || host != u.Host || !broker.Scan() {
		return
	}
	access, ok := strings.CutPrefix(broker.Text(), "bearer=")
	if !ok || access == "" || strings.ContainsAny(access, "\r\x00") || broker.Scan() || broker.Err() != nil {
		return
	}
	fmt.Fprintf(out, "username=codeaf\npassword=%s\n\n", access)
}
