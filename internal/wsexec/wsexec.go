// Package wsexec is the one execution adapter over both existing task roads.
//
// Launch-or-join, inspect, steer-with-authority, pause work, stop work, and
// observe share this door. Session-task maps to StartTask; bash-run maps to
// the registered RunEngine / PlanDB store. The mapping file lives in the host
// so this package does not import session, run, or plandb.
//
// REQUEST KEY BEFORE ADMISSION. The reserved execution_bindings row is written
// first. Runtime.Admit is never first. A second call with the same request key
// or an equivalent in-flight row joins; it does not Admit again.
//
// RECOVER FINDS, IT DOES NOT RELAUNCH. A crash after the runtime accepted and
// before BindRuntime is A14: FindByRequestKey plus BindRuntime, never a second
// Admit for that key.
//
// THE RUN-INSTANCE ID IS IMMUTABLE once bound. A reused plan-database path is
// not a run identity. Folder membership is never written to plandb.ParentID.
//
// Pause work and stop work act on an existing binding. They are not pause
// coordination. A missing store or runtime is labelled absence, never a
// fabricated completed launch.
package wsexec

import (
	"errors"
	"fmt"
)

const (
	GrantActive     = "active"
	GrantRevoked    = "revoked"
	GrantSuperseded = "superseded"

	ClassRead     = "read"
	ClassDiscuss  = "discuss"
	ClassOrganize = "organize"
	ClassExecute  = "execute"
	ClassSteer    = "steer"
	ClassStop     = "stop"

	BindReserved  = "reserved"
	BindAdmitted  = "admitted"
	BindBound     = "bound"
	BindPaused    = "paused"
	BindStopped   = "stopped"
	BindCompleted = "completed"
	BindFailed    = "failed"

	// RoadSessionTask is CODEAF_TASK_BELT unset. Numeric session task ids stay
	// on this road and are not plan ids.
	RoadSessionTask = "session-task"
	// RoadBashRun is CODEAF_TASK_BELT=bash. Plan ids are qualified by the
	// immutable run-instance id, not by a reused database path.
	RoadBashRun = "bash-run"

	OriginPerson = "person"
	OriginAgent  = "agent"
)

var (
	ErrInvalid  = errors.New("invalid execution request")
	ErrNotFound = errors.New("execution binding not found")
	// ErrAbsent is a missing store or runtime. Callers must leave the verb off
	// the belt rather than invent a completed view.
	ErrAbsent = errors.New("executor is absent")
	// ErrConflict is a second distinct run-instance id for one request key
	// (word "binding"), matching BindRuntime's refusal.
	ErrConflict = errors.New("conflict")
)

func bindingConflict() error {
	return fmt.Errorf("%w: binding", ErrConflict)
}
