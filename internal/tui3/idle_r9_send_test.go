package tui3

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/session"
)

type r9SendAgent struct {
	*r8SendAgent
	reopen bool
}

func (a *r9SendAgent) CheckWork() error {
	if a.reopen {
		a.checks++
		a.source.holding = false
		return &executor.WorkRefusal{Line: chatlist.Available}
	}
	if err := a.r8SendAgent.CheckWork(); err != nil {
		return err
	}
	a.source.change(func(row *chatlist.Row) {
		row.Status, row.Released, row.AutoOpen = chatlist.Here, false, false
	})
	return nil
}

// r9ReceiptSource models the concrete source's local receipt shortcut.
// A holding writer returns no relay facts until admission retires that receipt.
type r9ReceiptSource struct{ *r8SendSource }

func (s *r9ReceiptSource) SendLease(string) (string, uint64, error) {
	if s.holding {
		return heldCell, 2, nil
	}
	return "", 0, os.ErrNotExist
}

func (s *r9ReceiptSource) RowsForChatSend(ctx context.Context, file string, offline, holding bool) ([]chatlist.Row, uint64, bool, error) {
	if holding {
		return []chatlist.Row{{Cell: heldCell, Status: chatlist.Here, Mine: true, Fence: 2}}, 0, true, nil
	}
	return s.r7SendSource.RowsForChatSend(ctx, file, offline, false)
}

func (s *r9ReceiptSource) RowsForSend(ctx context.Context) ([]chatlist.Row, uint64, error) {
	rows, version, _, err := s.r7SendSource.RowsForChatSend(ctx, "", false, false)
	return rows, version, err
}

func TestIdleOpen_R9_EnterMatrix(t *testing.T) {
	cells := []struct{ relay, local, turn string }{
		{"revived", "expired", "idle"},
		{"lapsed", "holding", "idle"}, {"lapsed", "holding", "working"},
		{"lapsed", "expired", "idle"},
		{"lapsed", "release_undone", "idle"}, {"lapsed", "release_undone", "working"},
		{"released", "holding", "idle"}, {"released", "holding", "working"},
		{"released", "expired", "idle"},
		{"released", "release_undone", "idle"}, {"released", "release_undone", "working"},
	}
	for _, c := range cells {
		for _, unpublished := range []string{"none", "some"} {
			t.Run(c.relay+"/"+c.local+"/answering/"+c.turn+"/"+unpublished, func(t *testing.T) {
				a, old, source := r8OwnLiveWindow(t, false)
				a.agent = &r9SendAgent{r8SendAgent: old, reopen: true}
				source.holding = c.local == "holding" || c.local == "release_undone"
				a.machines = &r9ReceiptSource{source}
				if c.turn == "working" {
					a.state = stateWorking
				}
				source.change(func(row *chatlist.Row) {
					if c.relay == "lapsed" {
						row.Status = chatlist.Off
					}
					if c.relay == "released" {
						row.Status, row.Released, row.AutoOpen = chatlist.Idle, true, true
					}
				})
				if unpublished == "some" {
					path := filepath.Join(session.DirOf(a.file), ".cell", "adopted.json")
					if err := os.WriteFile(path, []byte(`{"V":1,"head":"unpublished","turns_size":0}`), 0600); err != nil {
						t.Fatal(err)
					}
				}
				taker := &returningTaker{}
				taker.taken = Taken{Transcript: a.file, Fence: 3, Head: "saved"}
				taker.on = func(string) {
					source.change(func(row *chatlist.Row) {
						row.Status, row.Fence, row.Released, row.AutoOpen = chatlist.Here, 3, false, false
					})
				}
				a.taker = taker
				const words = "R9 first Enter via reopen"
				a.input.setText(words)
				drive(t, a, key("enter"))
				if taker.seen == nil || taker.seen.Device != "A" || taker.seen.Fence != 2 {
					t.Fatalf("reopen lost exact observed expectation: %+v", taker.seen)
				}
				arrived, ok := a.agent.(*switchAgent)
				if len(old.sent) != 0 || !ok || len(arrived.sent) != 1 || arrived.sent[0] != words || !a.input.empty() || a.at(pageHome) || strings.Contains(homeText(a), continueAsk) {
					t.Fatalf("first Enter did not reopen and send without a card: old=%v new=%T box=%q notes=%v", old.sent, a.agent, a.input.String(), noteTexts(a))
				}
				if source.reads != 1 {
					t.Fatalf("probes=%d; want one", source.reads)
				}
			})
		}
	}
}

func TestIdleOpen_R9_RelayOutcomes(t *testing.T) {
	for _, rowState := range []string{"live", "revived", "lapsed", "released", "other_live", "other_released", "other_lapsed"} {
		for _, service := range []string{"answering", "refusing", "hanging"} {
			t.Run(rowState+"/"+service, func(t *testing.T) {
				a, agent, source := r8OwnLiveWindow(t, rowState != "live")
				a.agent = &r9SendAgent{r8SendAgent: agent, reopen: rowState == "lapsed"}
				taker := &returningTaker{}
				taker.taken = Taken{Transcript: a.file, Fence: 3, Head: "saved"}
				taker.on = func(string) { source.change(func(row *chatlist.Row) { row.Status, row.Fence = chatlist.Here, 3 }) }
				a.taker = taker
				source.change(func(row *chatlist.Row) {
					switch rowState {
					case "lapsed", "other_lapsed":
						row.Status = chatlist.Off
					case "released", "other_released":
						row.Status, row.Released, row.AutoOpen = chatlist.Idle, true, true
					}
					if strings.HasPrefix(rowState, "other_") {
						row.Mine, row.DeviceID, row.Device, row.Fence = false, "B", "laptop", 3
						row.AutoOpen = false
					}
				})
				if service == "refusing" {
					source.err = syscall.ECONNREFUSED
				} else if service == "hanging" {
					source.err = context.DeadlineExceeded
				}
				before, err := os.ReadFile(a.file)
				if err != nil {
					t.Fatal(err)
				}
				const words = "R9 first Enter"
				a.input.setText(words)
				drive(t, a, key("enter"))
				other := strings.HasPrefix(rowState, "other_")
				if service != "answering" || other {
					after, err := os.ReadFile(a.file)
					if err != nil || string(after) != string(before) || len(agent.sent) != 0 || a.input.String() != words {
						t.Fatalf("refusal spent words: sent=%v box=%q error=%v", agent.sent, a.input.String(), err)
					}
					if service == "answering" {
						if !a.at(pageHome) || !strings.Contains(homeText(a), continueAsk) || taker.seen != nil {
							t.Fatalf("other holder needs consent: %s", homeText(a))
						}
						drive(t, a, key("esc"))
						if len(agent.sent) != 0 || a.input.String() != words {
							t.Fatal("Esc changed the held chat or draft")
						}
						return
					}
					notes := strings.Join(noteTexts(a), "\n")
					if strings.Count(notes, chatlist.TakeBackUnreachable)+strings.Count(notes, "has not answered; your message is still in the box") != 1 {
						t.Fatalf("outage needs one honest line: %v", noteTexts(a))
					}
					source.err = nil
					if other {
						drive(t, a, key("enter"))
						if !strings.Contains(homeText(a), continueAsk) || len(agent.sent) != 0 {
							t.Fatal("recovery bypassed consent")
						}
						return
					}
					drive(t, a, key("enter"))
				}
				if rowState == "lapsed" {
					arrived, ok := a.agent.(*switchAgent)
					if taker.seen == nil || len(agent.sent) != 0 || !ok || len(arrived.sent) != 1 || arrived.sent[0] != words || a.at(pageHome) {
						t.Fatalf("lapsed retry did not reopen: old=%v agent=%T notes=%v", agent.sent, a.agent, noteTexts(a))
					}
					return
				}
				if agent.checks != 1 || len(agent.sent) != 1 || agent.sent[0] != words || !a.input.empty() || a.at(pageHome) {
					t.Fatalf("first admitted Enter did not send: checks=%d sent=%v box=%q notes=%v", agent.checks, agent.sent, a.input.String(), noteTexts(a))
				}
			})
		}
	}
}

func TestIdleOpen_R9_FreeHigherOwnFenceNeedsConsent(t *testing.T) {
	for _, status := range []chatlist.Status{chatlist.Here, chatlist.Idle, chatlist.Off} {
		t.Run(string(status), func(t *testing.T) {
			a, agent, source := r8OwnLiveWindow(t, true)
			taker := &returningTaker{}
			a.taker = taker
			source.change(func(row *chatlist.Row) {
				row.Fence, row.Status = 3, status
				row.Released, row.AutoOpen = status == chatlist.Idle, status == chatlist.Idle
			})
			a.input.setText("keep this choice mine")
			drive(t, a, key("enter"))
			if agent.checks != 0 || taker.seen != nil || len(agent.sent) != 0 || a.input.String() != "keep this choice mine" || !strings.Contains(homeText(a), continueAsk) {
				t.Fatalf("higher fence skipped consent: checks=%d take=%+v sent=%v box=%q", agent.checks, taker.seen, agent.sent, a.input.String())
			}
		})
	}
}

func TestIdleOpen_R9_EngineRefusalRestoresWords(t *testing.T) {
	for _, hosted := range []bool{false, true} {
		for _, next := range []string{"", "new draft"} {
			t.Run(fmt.Sprintf("hosted=%v/next=%s", hosted, next), func(t *testing.T) {
				agent := &fakeAgent{model: "a/b", failing: errors.New(chatlist.Available)}
				a := newTestApp(agent)
				keepSurfaceWorkersInTest(t, a)
				if hosted {
					a.host = "fixture"
				}
				a.input.setText("keep the refused words")
				msg := runSubmit(t, a.enter())
				a.input.setText(next)
				a.adopt(msg)
				want := "keep the refused words"
				if next != "" {
					want += "\n" + next
				}
				if a.input.String() != want {
					t.Fatalf("engine refusal lost words: box=%q want=%q", a.input.String(), want)
				}
				agent.failing = nil
				drive(t, a, key("enter"))
				if len(agent.sent) != 2 || agent.sent[1] != want || !a.input.empty() {
					t.Fatalf("engine recovery did not send the retained draft: sent=%v box=%q", agent.sent, a.input.String())
				}
			})
		}
	}
}

func TestIdleOpen_R9_AttachmentRefusalRestoresWordsOnce(t *testing.T) {
	for _, hosted := range []bool{false, true} {
		for _, next := range []string{"", "new draft"} {
			t.Run(fmt.Sprintf("hosted=%v/next=%s", hosted, next), func(t *testing.T) {
				a, agent, dir := attachLab(t, map[string]int{"shot.png": 12})
				keepSurfaceWorkersInTest(t, a)
				if hosted {
					a.host = "fixture"
				}
				agent.refuse = errors.New(chatlist.Available)
				a.attach(filepath.Join(dir, "shot.png"))
				a.input.setText("keep these picture words")
				msg := runSubmit(t, a.enter())
				a.input.setText(next)
				a.adopt(msg)
				want := "keep these picture words"
				if next != "" {
					want += "\n" + next
				}
				if a.input.String() != want || len(a.chips) != 1 {
					t.Fatalf("attachment refusal lost or duplicated the draft: words=%q chips=%v", a.input.String(), chipNames(a))
				}
				agent.refuse = nil
				drive(t, a, key("enter"))
				if agent.calls != 2 || !strings.HasPrefix(agent.text, want) || !a.input.empty() || len(a.chips) != 0 {
					t.Fatalf("attachment retry did not send once: calls=%d words=%q box=%q chips=%v", agent.calls, agent.text, a.input.String(), chipNames(a))
				}
			})
		}
	}
}

func TestIdleOpen_R9_RetiredReceiptProbeKeepsWordsOnFailure(t *testing.T) {
	for _, cause := range []error{syscall.ECONNREFUSED, context.DeadlineExceeded} {
		t.Run(cause.Error(), func(t *testing.T) {
			a, old, source := r8OwnLiveWindow(t, false)
			a.agent = &r9SendAgent{r8SendAgent: old, reopen: true}
			a.machines = &r9ReceiptSource{source}
			source.holding, source.err = true, &net.OpError{Op: "dial", Net: "tcp", Err: cause}
			source.change(func(row *chatlist.Row) { row.Status = chatlist.Off })
			taker := &returningTaker{}
			taker.taken = Taken{Transcript: a.file, Fence: 3, Head: "saved"}
			taker.on = func(string) { source.change(func(row *chatlist.Row) { row.Status, row.Fence = chatlist.Here, 3 }) }
			a.taker = taker
			before, err := os.ReadFile(a.file)
			if err != nil {
				t.Fatal(err)
			}
			const words = "keep this retired receipt draft"
			a.input.setText(words)
			drive(t, a, key("enter"))
			after, err := os.ReadFile(a.file)
			if err != nil || string(before) != string(after) || len(old.sent) != 0 || taker.seen != nil || a.input.String() != words || source.reads != 1 || source.within <= 0 || source.within > idleSendProbeTimeout {
				t.Fatalf("failed receipt probe spent the gesture: sent=%v take=%+v box=%q probes=%d budget=%v error=%v", old.sent, taker.seen, a.input.String(), source.reads, source.within, err)
			}
			if notes := strings.Join(noteTexts(a), "\n"); strings.Count(notes, chatlist.TakeBackUnreachable) != 1 || strings.Contains(homeText(a), continueAsk) {
				t.Fatalf("failed receipt probe needs one honest line and no card: %s", notes)
			}
			source.err = nil
			drive(t, a, key("enter"))
			arrived, ok := a.agent.(*switchAgent)
			if !ok || taker.seen == nil || taker.seen.Device != "A" || taker.seen.Fence != 2 || len(old.sent) != 0 || len(arrived.sent) != 1 || arrived.sent[0] != words || !a.input.empty() || source.reads != 2 {
				t.Fatalf("receipt recovery did not reopen and send: agent=%T take=%+v old=%v box=%q reads=%d", a.agent, taker.seen, old.sent, a.input.String(), source.reads)
			}
		})
	}
}

type p9xBashAgent struct {
	*r9SendAgent
	bashSent []string
	bashErr  error
}

func (a *p9xBashAgent) SubmitBash(_ context.Context, line string) (<-chan session.Event, error) {
	a.bashSent = append(a.bashSent, line)
	if a.bashErr != nil {
		return nil, a.bashErr
	}
	ch := make(chan session.Event)
	close(ch)
	return ch, nil
}

func TestIdleOpen_P9X1_BashCommandRunsOwnershipCheckAndKeepsWords(t *testing.T) {
	t.Run("offline refusal keeps bash line in box and prints one line", func(t *testing.T) {
		a, old, source := r8OwnLiveWindow(t, false)
		bash := &p9xBashAgent{r9SendAgent: &r9SendAgent{r8SendAgent: old, reopen: true}}
		a.agent = bash
		source.holding, source.err = false, syscall.ECONNREFUSED
		source.change(func(row *chatlist.Row) { row.Status = chatlist.Idle })
		const cmdLine = "!sleep 400"
		a.input.setText(cmdLine)
		drive(t, a, key("enter"))
		if len(bash.bashSent) != 0 || a.input.String() != cmdLine {
			t.Fatalf("offline bash bypassed checkBeforeSend: bashSent=%v box=%q", bash.bashSent, a.input.String())
		}
		if notes := strings.Join(noteTexts(a), "\n"); strings.Count(notes, chatlist.TakeBackUnreachable)+strings.Count(notes, "has not answered; your message is still in the box") != 1 {
			t.Fatalf("offline bash refusal must print one line: %v", noteTexts(a))
		}
	})

	t.Run("held elsewhere raises Continue here and keeps bash line in box", func(t *testing.T) {
		a, old, source := r8OwnLiveWindow(t, false)
		bash := &p9xBashAgent{r9SendAgent: &r9SendAgent{r8SendAgent: old}}
		a.agent = bash
		source.holding = false
		source.change(func(row *chatlist.Row) {
			row.Mine, row.DeviceID, row.Device, row.Status, row.Fence = false, "B", "laptop", chatlist.Running, 3
		})
		const cmdLine = "!sleep 400"
		a.input.setText(cmdLine)
		drive(t, a, key("enter"))
		if len(bash.bashSent) != 0 || !a.at(pageHome) || !strings.Contains(homeText(a), continueAsk) || a.input.String() != cmdLine {
			t.Fatalf("displaced bash did not offer Continue here: bashSent=%v home=%v box=%q", bash.bashSent, a.at(pageHome), a.input.String())
		}
	})

	t.Run("engine refusal puts bash line back in box and prints one line", func(t *testing.T) {
		bash := &p9xBashAgent{r9SendAgent: &r9SendAgent{r8SendAgent: &r8SendAgent{fakeAgent: &fakeAgent{model: "a/b"}}}, bashErr: errors.New(chatlist.Available)}
		a := newTestApp(bash)
		keepSurfaceWorkersInTest(t, a)
		const cmdLine = "!sleep 400"
		a.input.setText(cmdLine)
		msg := runSubmit(t, a.enter())
		a.adopt(msg)
		if a.input.String() != cmdLine {
			t.Fatalf("refused bash line lost from box: got %q want %q", a.input.String(), cmdLine)
		}
		if notes := strings.Join(noteTexts(a), "\n"); strings.Count(notes, chatlist.Available) != 1 {
			t.Fatalf("refused bash must print one line: %v", noteTexts(a))
		}
	})
}

func TestIdleOpen_P9X2_DisplacedKeptBranchReopensOnEnterForPlainAndBash(t *testing.T) {
	for _, words := range []string{"!sleep 400", "plain message after kept branch"} {
		t.Run(words, func(t *testing.T) {
			a, old, source := r8OwnLiveWindow(t, false)
			bash := &p9xBashAgent{r9SendAgent: &r9SendAgent{r8SendAgent: old, reopen: true}}
			a.agent = bash
			source.holding = false
			// Simulate P9-X: J was taken via openTaken (awaitingWriter: true),
			// then its own lease at f4 lapsed and the aborted turn was branched
			// into a kept copy (so local head is "orphan-turn" != relay head "saved").
			a.machineRead.sendHolds = map[string]sendHold{heldCell: {fence: 4, awaitingWriter: true}}
			source.change(func(row *chatlist.Row) {
				row.Status, row.Mine, row.DeviceID, row.Fence, row.Head = chatlist.Idle, true, "A", 4, "saved"
				row.Released, row.AutoOpen = false, false
			})
			path := filepath.Join(session.DirOf(a.file), ".cell", "adopted.json")
			if err := os.WriteFile(path, []byte(`{"V":1,"head":"orphan-turn","turns_size":0}`), 0600); err != nil {
				t.Fatal(err)
			}
			taker := &returningTaker{}
			taker.taken = Taken{Transcript: a.file, Fence: 5, Head: "orphan-turn"}
			taker.on = func(string) {
				source.change(func(row *chatlist.Row) {
					row.Status, row.Fence, row.Head = chatlist.Here, 5, "orphan-turn"
				})
			}
			a.taker = taker
			// First Enter while relay is unreachable must print one refusal line and keep words.
			source.err = syscall.ECONNREFUSED
			a.input.setText(words)
			drive(t, a, key("enter"))
			if a.input.String() != words || taker.seen != nil || len(old.sent) != 0 || len(bash.bashSent) != 0 {
				t.Fatalf("refused Enter spent words or took early: box=%q seen=%+v", a.input.String(), taker.seen)
			}
			if notes := strings.Join(noteTexts(a), "\n"); strings.Count(notes, chatlist.TakeBackUnreachable)+strings.Count(notes, "has not answered; your message is still in the box") != 1 {
				t.Fatalf("refused Enter must print one line, got: %v", noteTexts(a))
			}

			// Next Enter after relay answers takes the reopen road on the original cell.
			source.err = nil
			drive(t, a, key("enter"))
			if taker.seen == nil || taker.seen.Device != "A" || taker.seen.Fence != 4 || len(taker.cells) != 1 || taker.cells[0] != heldCell {
				t.Fatalf("Enter did not reopen original cell at f4: cells=%v seen=%+v", taker.cells, taker.seen)
			}
			arrived, ok := a.agent.(*switchAgent)
			if !ok || len(arrived.sent) != 1 || arrived.sent[0] != words || !a.input.empty() || a.at(pageHome) {
				t.Fatalf("reopen did not run %q on replacement session: agent=%T sent=%v box=%q notes=%v", words, a.agent, arrived.sent, a.input.String(), noteTexts(a))
			}
		})
	}
}
