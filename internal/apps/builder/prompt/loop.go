package prompt

import "fmt"

// MaxCheckRounds is how many times Check looks at the app (CONTRACT section 5:
// the Building view counts rounds 1 to 3). A failed round that has another
// after it goes back to the Builder as one fix.
const MaxCheckRounds = 3

// MaxFixRounds is how many times a failed check may go back to the Builder.
const MaxFixRounds = MaxCheckRounds - 1

// Phase is where the build is. PhasePreview and PhaseCheckFailed are final.
type Phase string

const (
	PhasePlan        Phase = "plan"
	PhaseWrite       Phase = "write"
	PhaseCheck       Phase = "check"
	PhaseFix         Phase = "fix"
	PhasePreview     Phase = "preview"
	PhaseCheckFailed Phase = "check_failed"
)

// Loop is the quality loop's policy: plan → write → check → fix, at most
// MaxFixRounds fixes, then check_failed with the last report. It holds no
// clock, model or files; the caller reports what happened and acts on Phase.
type Loop struct {
	phase  Phase
	fixes  int
	report string
}

// NewLoop starts at the plan call.
func NewLoop() *Loop { return &Loop{phase: PhasePlan} }

func (l *Loop) Phase() Phase        { return l.phase }
func (l *Loop) FixRounds() int      { return l.fixes }
func (l *Loop) LastReport() string  { return l.report }
func (l *Loop) Done() bool          { return l.phase == PhasePreview || l.phase == PhaseCheckFailed }
func (l *Loop) PlanWritten() error  { return l.move(PhasePlan, PhaseWrite) }
func (l *Loop) FilesWritten() error { return l.move(PhaseWrite, PhaseCheck) }
func (l *Loop) FixApplied() error   { return l.move(PhaseFix, PhaseCheck) }

// CheckResult records a check and returns the next phase: preview on a pass,
// fix while rounds remain, otherwise check_failed keeping this report.
func (l *Loop) CheckResult(passed bool, report string) (Phase, error) {
	if l.phase != PhaseCheck {
		return l.phase, fmt.Errorf("prompt: loop is at %s, not check", l.phase)
	}
	switch {
	case passed:
		l.phase = PhasePreview
	case l.fixes >= MaxFixRounds:
		l.report, l.phase = report, PhaseCheckFailed
	default:
		l.report, l.phase = report, PhaseFix
		l.fixes++
	}
	return l.phase, nil
}

func (l *Loop) move(from, to Phase) error {
	if l.phase != from {
		return fmt.Errorf("prompt: loop is at %s, not %s", l.phase, from)
	}
	l.phase = to
	return nil
}
