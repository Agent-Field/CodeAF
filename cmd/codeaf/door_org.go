package main

// ── THE TERMINAL DOOR: SIGNING IN TO AN ORGANIZATION ───────────────────────
//
// A door lets a person start a task, follow it in one message edited in place,
// and answer a quick Ask, from where they already work (Workstreams PRD,
// "Doors"). This file is the sign-in half of the terminal's door: `codeaf org
// login <url>` asks for a username and password on the terminal, the password
// without echo, and sends them to the organization's own password sign-in
// (POST /v1/auth/password, the door the desktop's password sign-in uses). The
// person token the server answers is kept in doors/orgs.json under the codeaf
// home, readable by this person only, and is never printed.
//
// Everything the door does afterwards runs through the organization's server
// with that token, as that person: the server decides who may do what, and the
// door adds no rule of its own (door_task.go).

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"

	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/client"
	"github.com/Agent-Field/codeaf/internal/home"
)

// doorMadeWith is the one line every door message ends with (PRD: every door
// message says "Made with WorkAF"). On a terminal one short trailing line is
// the whole of it.
const doorMadeWith = "Made with WorkAF"

// doorCallWithin bounds one request to the organization's server. A poll that
// hangs must not freeze the line a person is watching.
const doorCallWithin = 20 * time.Second

// The door's terminal: where it reads, where it writes, and whether each is a
// person at a terminal. They are variables so a test can stand in for both.
var (
	doorIn            io.Reader = os.Stdin
	doorOut           io.Writer = os.Stdout
	doorInIsTerminal            = func() bool { return term.IsTerminal(os.Stdin.Fd()) }
	doorOutIsTerminal           = func() bool { return term.IsTerminal(os.Stdout.Fd()) }
	doorReadPassword            = func() ([]byte, error) { return term.ReadPassword(os.Stdin.Fd()) }
	doorWidth                   = func() int {
		if w, _, err := term.GetSize(os.Stdout.Fd()); err == nil && w > 0 {
			return w
		}
		return 80
	}
	// doorLines reads doorIn line by line; one reader for the process, so a
	// line typed ahead is not lost between two prompts.
	doorLines *bufio.Reader
)

// doorReadLine is one line typed at the door, without its newline. io.EOF
// means nobody is there to type.
func doorReadLine() (string, error) {
	if doorLines == nil {
		doorLines = bufio.NewReader(doorIn)
	}
	line, err := doorLines.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// doorOrg is one organization this terminal is signed in to.
type doorOrg struct {
	URL       string    `json:"url"`
	Name      string    `json:"name,omitempty"`
	Handle    string    `json:"handle"`
	Person    string    `json:"person,omitempty"`
	Token     string    `json:"token"`
	Session   string    `json:"session,omitempty"`
	ExpiresAt time.Time `json:"expires_at,omitzero"`
}

// label is how a sentence names the organization: its name, else its address.
func (o doorOrg) label() string {
	if o.Name != "" {
		return o.Name
	}
	return o.URL
}

// doorTaskRef is a task started from this terminal, so `codeaf task follow
// <task>` finds its workstream without asking the server to search.
type doorTaskRef struct {
	Org   string `json:"org"`
	WS    string `json:"ws"`
	Task  string `json:"task"`
	Title string `json:"title,omitempty"`
}

// doorTasksKept is how many started tasks the door remembers.
const doorTasksKept = 200

// doorState is doors/orgs.json.
type doorState struct {
	Current string        `json:"current,omitempty"`
	Orgs    []doorOrg     `json:"orgs"`
	Tasks   []doorTaskRef `json:"tasks,omitempty"`
}

func doorStatePath() string { return home.Join("doors", "orgs.json") }

func readDoorState() (doorState, error) {
	var s doorState
	body, err := os.ReadFile(doorStatePath())
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(body, &s); err != nil {
		return doorState{}, fmt.Errorf("the door's sign-ins in %s cannot be read; sign in again with codeaf org login <url>", doorStatePath())
	}
	return s, nil
}

// writeDoorState keeps s readable by this person only: it holds their tokens.
func writeDoorState(s doorState) error {
	body, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return writeFactoryPrivate(doorStatePath(), append(body, '\n'))
}

// org is the signed-in organization named by want (its address or its name),
// or the current one when want is empty.
func (s doorState) org(want string) (doorOrg, error) {
	want = strings.TrimSpace(want)
	if len(s.Orgs) == 0 {
		return doorOrg{}, errors.New("sign in to your organization first: codeaf org login <server-url>")
	}
	if want == "" {
		want = s.Current
		if want == "" {
			want = s.Orgs[0].URL
		}
	}
	if base, err := factoryURL(want); err == nil {
		for _, o := range s.Orgs {
			if o.URL == base {
				return o, nil
			}
		}
		return doorOrg{}, fmt.Errorf("this terminal is not signed in to %s: codeaf org login %s", base, base)
	}
	for _, o := range s.Orgs {
		if strings.EqualFold(o.Name, want) {
			return o, nil
		}
	}
	return doorOrg{}, fmt.Errorf("this terminal is not signed in to %q; codeaf org lists where it is", want)
}

// keep puts o in s as the current organization, replacing its old sign-in.
func (s *doorState) keep(o doorOrg) {
	out := []doorOrg{o}
	for _, old := range s.Orgs {
		if old.URL != o.URL {
			out = append(out, old)
		}
	}
	s.Orgs, s.Current = out, o.URL
}

// drop takes the organization at url out of s, and the tasks started there.
func (s *doorState) drop(url string) {
	orgs := s.Orgs[:0:0]
	for _, o := range s.Orgs {
		if o.URL != url {
			orgs = append(orgs, o)
		}
	}
	tasks := s.Tasks[:0:0]
	for _, t := range s.Tasks {
		if t.Org != url {
			tasks = append(tasks, t)
		}
	}
	s.Orgs, s.Tasks = orgs, tasks
	if s.Current == url {
		s.Current = ""
		if len(orgs) > 0 {
			s.Current = orgs[0].URL
		}
	}
}

// remember keeps a started task, newest first, at most doorTasksKept.
func (s *doorState) remember(ref doorTaskRef) {
	out := []doorTaskRef{ref}
	for _, t := range s.Tasks {
		if !(t.Org == ref.Org && t.Task == ref.Task) && len(out) < doorTasksKept {
			out = append(out, t)
		}
	}
	s.Tasks = out
}

// doorClient is a client for o's server that speaks as o's person.
func doorClient(o doorOrg) *client.Client { return client.Open(o.URL, o.Token) }

// doorCall is one request to o's server, bounded, with a refusal said in the
// door's own words where the status says more than the server's sentence: a
// sign-in that ended is told how to sign in again.
func doorCall(ctx context.Context, o doorOrg, method, path string, in, out any) error {
	ctx, cancel := context.WithTimeout(ctx, doorCallWithin)
	defer cancel()
	err := doorClient(o).Call(ctx, method, path, in, out)
	if err == nil {
		return nil
	}
	var refused client.Refusal
	if errors.As(err, &refused) {
		if refused.SignedOut {
			return doorSignInEnded{o}
		}
		return refused
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("%s did not answer in time", o.label())
	}
	var syntax *json.SyntaxError
	var shape *json.UnmarshalTypeError
	if errors.As(err, &syntax) || errors.As(err, &shape) {
		return fmt.Errorf("%s sent an answer this terminal cannot read", o.label())
	}
	return fmt.Errorf("%s is not answering at %s", o.label(), o.URL)
}

// doorSignInEnded is a request the server refused because the person's
// session is no longer good: the follow stops, and the person is told how to
// sign in again.
type doorSignInEnded struct{ org doorOrg }

func (e doorSignInEnded) Error() string {
	return fmt.Sprintf("your sign-in to %s has ended; sign in again: codeaf org login %s", e.org.label(), e.org.URL)
}

// runOrg is `codeaf org [login|logout]`.
func runOrg(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "login":
			return runOrgLogin(args[1:])
		case "logout":
			return runOrgLogout(args[1:])
		}
	}
	flags := commandFlags("org")
	if err := parseCommandFlags(flags, args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return wrongCall("use codeaf org login <url>, codeaf org logout, or codeaf org")
	}
	s, err := readDoorState()
	if err != nil {
		return err
	}
	if len(s.Orgs) == 0 {
		fmt.Fprintln(doorOut, "This terminal is not signed in to an organization: codeaf org login <server-url>")
		return nil
	}
	for _, o := range s.Orgs {
		mark := ""
		if o.URL == s.Current {
			mark = " · used by codeaf task"
		}
		fmt.Fprintf(doorOut, "%s (%s) as %s%s\n", o.label(), o.URL, o.Handle, mark)
	}
	return nil
}

// runOrgLogin is `codeaf org login <url> [--user name]`.
func runOrgLogin(args []string) error {
	flags := commandFlags("org login")
	user := flags.String("user", "", "your username on the organization (asked when not given)")
	if err := parseCommandFlags(flags, reorder(flags, args)); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return wrongCall("use codeaf org login <server-url> [--user name]")
	}
	base, err := factoryURL(flags.Arg(0))
	if err != nil {
		return wrongCall(err.Error())
	}
	ctx := context.Background()
	// The address answers as an organization, and says how its people sign in,
	// before anybody types a password at it.
	var about struct {
		Name   string   `json:"name"`
		SignIn []string `json:"sign_in"`
	}
	probe := doorOrg{URL: base}
	if err := doorCall(ctx, probe, http.MethodGet, "/v1/about", nil, &about); err != nil {
		return fmt.Errorf("%s does not answer as an organization: %v", base, err)
	}
	if about.SignIn != nil && !containsString(about.SignIn, "password") {
		return fmt.Errorf("%s does not take a password from a terminal; sign in from WorkAF instead", firstNonEmptyString(about.Name, base))
	}
	interactive := doorInIsTerminal()
	username := strings.TrimSpace(*user)
	if username == "" {
		if interactive {
			fmt.Fprint(doorOut, "Username: ")
		}
		line, err := doorReadLine()
		if err != nil {
			return errors.New("no username was given; use --user or type it")
		}
		username = strings.TrimSpace(line)
	}
	if username == "" {
		return errors.New("no username was given; use --user or type it")
	}
	var password string
	if interactive {
		fmt.Fprint(doorOut, "Password: ")
		raw, err := doorReadPassword()
		fmt.Fprintln(doorOut)
		if err != nil {
			return errors.New("the password could not be read")
		}
		password = string(raw)
	} else {
		// A script hands the password on stdin, one line, never on the
		// command line where every process listing would show it.
		line, err := doorReadLine()
		if err != nil {
			return errors.New("no password was given on stdin")
		}
		password = line
	}
	if password == "" {
		return errors.New("no password was given")
	}
	coverSecret(password)
	var signed api.PasswordSignedIn
	signCtx, cancel := context.WithTimeout(ctx, doorCallWithin)
	err = doorClient(probe).Call(signCtx, http.MethodPost, "/v1/auth/password",
		api.PasswordSignIn{Username: username, Password: password}, &signed)
	cancel()
	if err != nil {
		var refused client.Refusal
		if errors.As(err, &refused) {
			switch {
			case refused.SignedOut || refused.Forbidden:
				return errors.New("that username and password don't match")
			case refused.Missing:
				return errors.New("this organization does not take passwords")
			}
			return refused
		}
		return fmt.Errorf("%s did not answer the sign-in", firstNonEmptyString(about.Name, base))
	}
	if signed.AccessToken == "" {
		return errors.New("the organization answered the sign-in without a session")
	}
	o := doorOrg{
		URL:     base,
		Name:    firstNonEmptyString(signed.Factory.Name, about.Name),
		Handle:  firstNonEmptyString(signed.Person.Handle, signed.Me.Handle, username),
		Person:  firstNonEmptyString(signed.Person.Name, signed.Me.Name),
		Token:   signed.AccessToken,
		Session: signed.Me.SessionID,
	}
	if !signed.ExpiresAt.IsZero() {
		o.ExpiresAt = signed.ExpiresAt
	} else if signed.ExpiresIn > 0 {
		o.ExpiresAt = time.Now().Add(time.Duration(signed.ExpiresIn) * time.Second)
	}
	s, err := readDoorState()
	if err != nil {
		s = doorState{}
	}
	s.keep(o)
	if err := writeDoorState(s); err != nil {
		return err
	}
	who := o.Handle
	if o.Person != "" && o.Person != o.Handle {
		who = o.Person + " (" + o.Handle + ")"
	}
	fmt.Fprintf(doorOut, "Signed in to %s as %s. Start work with codeaf task \"<what to do>\".\n%s\n", o.label(), who, doorMadeWith)
	return nil
}

// runOrgLogout is `codeaf org logout [<url>]`: the session ends on the server
// when it still answers, and is forgotten here either way.
func runOrgLogout(args []string) error {
	flags := commandFlags("org logout")
	if err := parseCommandFlags(flags, reorder(flags, args)); err != nil {
		return err
	}
	if flags.NArg() > 1 {
		return wrongCall("use codeaf org logout [<server-url>]")
	}
	s, err := readDoorState()
	if err != nil {
		return err
	}
	if len(s.Orgs) == 0 {
		fmt.Fprintln(doorOut, "This terminal is not signed in to an organization.")
		return nil
	}
	o, err := s.org(flags.Arg(0))
	if err != nil {
		return err
	}
	if o.Session != "" {
		// Best effort: a server that is down still loses the token here.
		_ = doorCall(context.Background(), o, http.MethodDelete, "/v1/me/sessions/"+o.Session, nil, nil)
	}
	s.drop(o.URL)
	if err := writeDoorState(s); err != nil {
		return err
	}
	fmt.Fprintf(doorOut, "Signed out of %s on this terminal.\n", o.label())
	return nil
}
