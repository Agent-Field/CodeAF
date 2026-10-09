package desktopbridge

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// terminalRoute serves /sessions/{id}/terminals[/{tid}[/{action}]]. It sits
// behind Bridge.ServeHTTP's bearer check like every other route, so none of
// it answers without the engine token.
//
//	POST   terminals                  start a terminal, or a job when "command" is set
//	GET    terminals                  list them
//	GET    terminals/{tid}            one terminal's state
//	GET    terminals/{tid}/stream     SSE: output records from ?after=<byte offset>, then exit
//	GET    terminals/{tid}/output     the recent output as plain text (for "ask codeaf")
//	POST   terminals/{tid}/input      write bytes
//	POST   terminals/{tid}/resize     change the window size
//	POST   terminals/{tid}/close      end the process group (a terminal also leaves the list)
//	POST   terminals/{tid}/remove     end it if running and drop it and its log
func (s *conversation) terminalRoute(w http.ResponseWriter, r *http.Request, rest []string) {
	set := s.terminals()
	if len(rest) == 0 {
		switch r.Method {
		case http.MethodGet:
			write(w, set.list())
		case http.MethodPost:
			s.startTerminal(w, r, set)
		default:
			fail(w, 405, "GET or POST required")
		}
		return
	}
	t := set.get(rest[0])
	if t == nil {
		fail(w, 404, "this terminal is gone")
		return
	}
	if len(rest) == 1 {
		if needGet(w, r) {
			write(w, t.info())
		}
		return
	}
	if len(rest) != 2 {
		fail(w, 404, "unknown terminal action")
		return
	}
	switch rest[1] {
	case "stream":
		if needGet(w, r) {
			t.stream(w, r)
		}
	case "output":
		if needGet(w, r) {
			t.output(w, r)
		}
	case "input":
		if !needPost(w, r) {
			return
		}
		var in struct {
			DataBase64 string `json:"dataBase64"`
		}
		if !decodeMax(w, r, &in, 256<<10) {
			return
		}
		data, err := base64.StdEncoding.DecodeString(in.DataBase64)
		if err != nil {
			fail(w, 400, "input must be base64")
			return
		}
		if err := t.write(data); err != nil {
			fail(w, 409, err.Error())
			return
		}
		write(w, map[string]bool{"accepted": true})
	case "resize":
		if !needPost(w, r) {
			return
		}
		var in struct {
			Cols int `json:"cols"`
			Rows int `json:"rows"`
		}
		if !decode(w, r, &in) {
			return
		}
		if err := t.resize(in.Cols, in.Rows); err != nil {
			fail(w, 409, err.Error())
			return
		}
		write(w, t.info())
	case "close":
		if !needPost(w, r) {
			return
		}
		t.close()
		if !t.job() {
			set.remove(t.id)
		}
		write(w, t.info())
	case "remove":
		if !needPost(w, r) {
			return
		}
		t.close()
		set.remove(t.id)
		write(w, map[string]bool{"accepted": true})
	default:
		fail(w, 404, "unknown terminal action")
	}
}

func (s *conversation) startTerminal(w http.ResponseWriter, r *http.Request, set *terminalSet) {
	var in struct {
		Command string `json:"command"`
		Title   string `json:"title"`
		Cols    int    `json:"cols"`
		Rows    int    `json:"rows"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.Command = strings.TrimSpace(in.Command)
	in.Title = strings.TrimSpace(in.Title)
	set.mu.Lock()
	live := 0
	for _, t := range set.items {
		if !t.info().ended() {
			live++
		}
	}
	set.mu.Unlock()
	if live >= maxTerminals {
		fail(w, 409, fmt.Sprintf("%d terminals are already running; close one first", maxTerminals))
		return
	}
	id, err := Token()
	if err != nil {
		fail(w, 500, "cannot create terminal identity")
		return
	}
	t, err := startTerminal(id[:16], in.Title, in.Command, s.conn.Welcome.Workspace, in.Cols, in.Rows)
	if err != nil {
		fail(w, 409, err.Error())
		return
	}
	set.mu.Lock()
	set.add(t)
	set.mu.Unlock()
	write(w, t.info())
}

func (i TerminalInfo) ended() bool { return i.State != "running" }

// termRecord is one stream record. Output carries bytes from offset Seq-len; an
// exit record carries the final state. Cut says the replay began after the
// first bytes were dropped from the bounded scrollback.
type termRecord struct {
	Seq        uint64        `json:"seq"`
	Type       string        `json:"type"`
	DataBase64 string        `json:"dataBase64,omitempty"`
	Cut        bool          `json:"cut,omitempty"`
	Info       *TerminalInfo `json:"info,omitempty"`
}

// stream replays the scrollback from ?after and then follows live output. One
// connection per open terminal tab keeps the browser's connection budget.
func (t *terminal) stream(w http.ResponseWriter, r *http.Request) {
	f, ok := w.(http.Flusher)
	if !ok {
		fail(w, 500, "streaming unavailable")
		return
	}
	after, _ := strconv.ParseUint(r.URL.Query().Get("after"), 10, 64)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)
	f.Flush()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	emit := func(rec termRecord) bool {
		data, _ := json.Marshal(rec)
		_, err := fmt.Fprintf(w, "id: %d\ndata: %s\n\n", rec.Seq, data)
		return err == nil
	}
	for {
		data, end, cut, changed, exited := t.since(after)
		for first := true; len(data) > 0; first = false {
			n := min(len(data), 32<<10)
			if !emit(termRecord{Seq: end - uint64(len(data)) + uint64(n), Type: "output", DataBase64: base64.StdEncoding.EncodeToString(data[:n]), Cut: cut && first}) {
				return
			}
			data = data[n:]
		}
		after = end
		if exited {
			info := t.info()
			emit(termRecord{Seq: end, Type: "exit", Info: &info})
			f.Flush()
			return
		}
		f.Flush()
		select {
		case <-r.Context().Done():
			return
		case <-changed:
		case <-heartbeat.C:
			if _, err := io.WriteString(w, ": alive\n\n"); err != nil {
				return
			}
			f.Flush()
		}
	}
}

// plainOutput turns raw terminal bytes into the text a person would have read:
// escape sequences gone, carriage-return redraws collapsed to their last state.
func plainOutput(raw []byte) string {
	text := ansi.Strip(strings.ReplaceAll(string(raw), "\r\n", "\n"))
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if cr := strings.LastIndex(line, "\r"); cr >= 0 && cr < len(line)-1 {
			lines[i] = line[cr+1:]
		} else {
			lines[i] = strings.TrimRight(line, "\r")
		}
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

// output is the scrollback as plain text, newest ?tail= bytes (default all).
func (t *terminal) output(w http.ResponseWriter, r *http.Request) {
	data, _, cut, _, _ := t.since(0)
	if tail, err := strconv.Atoi(r.URL.Query().Get("tail")); err == nil && tail > 0 && tail < len(data) {
		data, cut = data[len(data)-tail:], true
	}
	write(w, map[string]any{"text": plainOutput(data), "truncated": cut, "info": t.info()})
}
