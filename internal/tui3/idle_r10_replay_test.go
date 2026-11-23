package tui3

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/directory/directorytest"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/session"
)

// r10ReplaySource preserves the production source's distinction between a
// cached writer status and the current local send receipt.
type r10ReplaySource struct {
	*continuationListing
	clock                    *directorytest.FakeClock
	priorHome, receipt, down bool
	probes                   int
}

type r10OrderingWriter struct {
	*fakeAgent
	source *r10ReplaySource
}

func (w *r10OrderingWriter) RenewalStatus() (time.Time, time.Duration, bool) {
	return time.Time{}, time.Second, w.source.receipt
}

func (w *r10OrderingWriter) CheckWorkContext(context.Context) error {
	if !w.source.receipt {
		return &executor.WorkRefusal{Line: chatlist.Available}
	}
	return nil
}

// The drive matrix proves preservation and fence effects. This matrix crosses
// those outcomes with Home history at the real Enter and composer boundary.
func TestIdleOpen_R10_RecoveryOrderingPriorHomeMatrix(t *testing.T) {
	t.Setenv("CODEAF_IDLE_HANDOFF", "on")
	for _, relay := range []string{"live", "lapsed"} {
		for _, posture := range []string{"idle", "working"} {
			for _, pending := range []string{"none", "some"} {
				for _, call := range []string{"no_call", "call_in_flight_at_expiry"} {
					for _, ordering := range []string{"observation_first", "own_beat_first", "restart_renew_holds_first"} {
						for _, prior := range []bool{false, true} {
							priorName := map[bool]string{false: "prior_Home_other_holder_no", true: "prior_Home_other_holder_yes"}[prior]
							t.Run(strings.Join([]string{relay, posture, pending, call, ordering, priorName}, "/"), func(t *testing.T) {
								source := &r10ReplaySource{continuationListing: &continuationListing{row: chatlist.Row{Cell: heldCell, Title: "I", Mine: true, DeviceID: "A", Status: chatlist.Here, Fence: 2, Head: "saved"}}, clock: directorytest.NewFakeClock(), priorHome: prior, receipt: true}
								taker := &returningTaker{}
								a, old := heldLab(t, source, taker)
								keepSurfaceWorkersInTest(t, a)
								a.closeHome()
								a.clock = source.clock.Now
								a.agent = &r10OrderingWriter{fakeAgent: old, source: source}
								if posture == "working" {
									a.state = stateWorking
								}
								if relay == "lapsed" {
									source.clock.Advance(directory.LeaseTTL + time.Second)
								}
								source.down = true
								source.clock.Advance(directory.LeaseTTL + time.Second)
								drain(t, a, a.askMachines())
								if pending == "some" || call != "no_call" {
									root := filepath.Join(session.DirOf(a.file), ".cell")
									if err := os.MkdirAll(root, 0700); err != nil {
										t.Fatal(err)
									}
									if err := os.WriteFile(filepath.Join(root, "adopted.json"), []byte(`{"V":1,"head":"offline-and-late-call","turns_size":0}`), 0600); err != nil {
										t.Fatal(err)
									}
								}
								const words = "the recovered Enter spends this draft once"
								a.input.setText(words)
								source.down = false
								if ordering == "observation_first" {
									source.receipt = false
									source.change(func(row *chatlist.Row) { row.Status = chatlist.Idle })
									taker.taken = Taken{Transcript: a.file, Fence: 3, Head: "saved"}
									taker.on = func(string) { source.change(func(row *chatlist.Row) { row.Status, row.Fence = chatlist.Here, 3 }) }
								}
								drive(t, a, key("enter"))
								if !a.input.empty() || a.at(pageHome) {
									t.Fatalf("recovery kept the box or showed a card: box=%q notes=%v", a.input.String(), noteTexts(a))
								}
								if ordering == "observation_first" {
									arrived, ok := a.agent.(*switchAgent)
									if !ok || len(arrived.sent) != 1 || arrived.sent[0] != words || len(old.sent) != 0 || taker.seen == nil || taker.seen.Fence != 2 || taker.seen.Device != "A" {
										t.Fatal("retirement did not send through the conditional replacement")
									}
								} else {
									delivered := old.sent
									if posture == "working" {
										delivered = old.steered
									}
									if len(delivered) != 1 || delivered[0] != words || taker.seen != nil {
										t.Fatalf("revival did not deliver once through the retained writer: sent=%v steered=%v", old.sent, old.steered)
									}
								}
							})
						}
					}
				}
			}
		}
	}
}

func (*r10ReplaySource) RelayHost() string { return "127.0.0.1" }

func (s *r10ReplaySource) Rows(ctx context.Context) ([]chatlist.Row, error) {
	if s.down {
		return nil, directory.ErrUnreachable
	}
	rows, err := s.continuationListing.Rows(ctx)
	if s.priorHome {
		rows = append(rows, chatlist.Row{Cell: "chat-J", Title: "J", DeviceID: "B", Device: "computer B", Status: chatlist.Running, Fence: 9})
	}
	return rows, err
}

func (s *r10ReplaySource) SendLease(string) (string, uint64, error) {
	if !s.receipt {
		return "", 0, os.ErrNotExist
	}
	return heldCell, 2, nil
}

func (s *r10ReplaySource) RowsForChatSend(ctx context.Context, _ string, offline, holding bool) ([]chatlist.Row, uint64, bool, error) {
	if holding && s.receipt {
		return []chatlist.Row{{Cell: heldCell, Mine: true, Status: chatlist.Here, Fence: 2}}, 0, true, nil
	}
	if offline {
		return nil, 0, false, directory.ErrUnreachable
	}
	s.probes++
	rows, err := s.Rows(ctx)
	return rows, 10, false, err
}

type r10ReplayWriter struct{ *fakeAgent }

func (*r10ReplayWriter) RenewalStatus() (time.Time, time.Duration, bool) {
	// The engine retired its receipt before its status frame reached this window.
	return time.Time{}, time.Second, true
}

func (*r10ReplayWriter) CheckWorkContext(context.Context) error {
	return &executor.WorkRefusal{Line: chatlist.Available}
}

func TestIdleOpen_R10_Run1PriorHomeMatrix(t *testing.T) {
	t.Setenv("CODEAF_IDLE_HANDOFF", "on")
	for _, prior := range []bool{false, true} {
		for _, recovered := range []string{"lapsed", "revived", "released"} {
			t.Run(map[bool]string{false: "prior_Home_other_holder_no", true: "prior_Home_other_holder_yes"}[prior]+"/"+recovered, func(t *testing.T) {
				source := &r10ReplaySource{continuationListing: &continuationListing{row: chatlist.Row{Cell: heldCell, Title: "I", Mine: true, DeviceID: "A", Status: chatlist.Idle, Fence: 2, Head: "saved"}}, clock: directorytest.NewFakeClock(), priorHome: prior}
				taker := &returningTaker{}
				a, old := heldLab(t, source, taker)
				keepSurfaceWorkersInTest(t, a)
				if prior && !strings.Contains(homeText(a), "computer B") {
					t.Fatal("Home did not read J's other holder")
				}
				a.closeHome()
				a.agent = &r10ReplayWriter{old}
				a.clock = source.clock.Now
				source.down = true
				source.clock.Advance(directory.LeaseTTL + time.Second)
				drain(t, a, a.askMachines())
				const words = "run 1 sends on the first recovered Enter"
				before, err := os.ReadFile(a.file)
				if err != nil {
					t.Fatal(err)
				}
				a.input.setText(words)
				drive(t, a, key("enter"))
				after, err := os.ReadFile(a.file)
				if err != nil || string(after) != string(before) || len(old.sent) != 0 || a.input.String() != words {
					t.Fatal("outage spent the draft")
				}
				if !strings.Contains(strings.Join(noteTexts(a), "\n"), chatlist.TakeBackUnreachable) {
					t.Fatal("outage did not report the fresh take-back attempt")
				}
				if _, card := a.homeAsking(); card {
					t.Fatal("outage raised an own-chat card")
				}
				source.down = false
				source.change(func(row *chatlist.Row) {
					if recovered == "revived" {
						row.Status = chatlist.Here
					}
					if recovered == "released" {
						row.Status, row.Released, row.AutoOpen = chatlist.Idle, true, true
					}
				})
				source.clock.Advance(5 * time.Second)
				taker.taken = Taken{Transcript: a.file, Fence: 3, Head: "saved"}
				taker.on = func(string) { source.change(func(row *chatlist.Row) { row.Status, row.Fence = chatlist.Here, 3 }) }
				drive(t, a, key("enter"))
				arrived, ok := a.agent.(*switchAgent)
				if !ok || len(arrived.sent) != 1 || arrived.sent[0] != words || !a.input.empty() || a.at(pageHome) {
					t.Fatalf("first recovered Enter stranded the message: agent=%T old=%v box=%q notes=%v", a.agent, old.sent, a.input.String(), noteTexts(a))
				}
				if taker.seen == nil || taker.seen.Device != "A" || taker.seen.Fence != 2 || source.probes != 2 {
					t.Fatalf("reopen did not use fresh facts: seen=%+v probes=%d", taker.seen, source.probes)
				}
			})
		}
	}
}

func TestIdleOpen_R10_Run1AfterFailedOtherChatMove(t *testing.T) {
	t.Setenv("CODEAF_IDLE_HANDOFF", "on")
	source := &r10ReplaySource{continuationListing: &continuationListing{row: chatlist.Row{Cell: heldCell, Title: "I", Mine: true, DeviceID: "A", Status: chatlist.Here, Fence: 2, Head: "saved"}}, clock: directorytest.NewFakeClock(), priorHome: true, receipt: true}
	taker := &returningTaker{}
	taker.err = directory.ErrUnreachable
	a, old := heldLab(t, source, taker)
	keepSurfaceWorkersInTest(t, a)
	a.agent = &r10OrderingWriter{fakeAgent: old, source: source}
	a.clock = source.clock.Now
	standOn(t, a, "chat-J")
	confirm(t, a)
	a.closeHome()
	source.down = true
	source.clock.Advance(directory.LeaseTTL + time.Second)
	drain(t, a, a.askMachines())
	const words = "I still sends after a failed move to J"
	a.input.setText(words)
	drive(t, a, key("enter"))
	if len(old.sent) != 0 || a.input.String() != words {
		t.Fatal("outage spent I's draft")
	}
	source.down = false
	source.change(func(row *chatlist.Row) { row.Status = chatlist.Idle })
	source.clock.Advance(5 * time.Second)
	taker.err = nil
	taker.taken = Taken{Transcript: a.file, Fence: 3, Head: "saved"}
	taker.on = func(string) { source.change(func(row *chatlist.Row) { row.Status, row.Fence = chatlist.Here, 3 }) }
	drive(t, a, key("enter"))
	arrived, ok := a.agent.(*switchAgent)
	if !ok || len(arrived.sent) != 1 || arrived.sent[0] != words || !a.input.empty() || a.at(pageHome) {
		t.Fatalf("J's failed move stranded I: agent=%T old=%v box=%q notes=%v", a.agent, old.sent, a.input.String(), noteTexts(a))
	}
}
