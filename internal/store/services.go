package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// ServiceStatus is the journal-derived state of one promoted process.
type ServiceStatus string

const (
	ServiceRunning ServiceStatus = "running"
	ServiceStopped ServiceStatus = "stopped"
	ServiceFailed  ServiceStatus = "failed"
	ServiceResting ServiceStatus = "resting"
)

type ServiceHealthKind string

const (
	ServiceHealthPort ServiceHealthKind = "port"
	ServiceHealthURL  ServiceHealthKind = "url"
	ServiceHealthCmd  ServiceHealthKind = "cmd"
)

// ServiceHealth is deliberately small: one cheap probe with one bounded value.
type ServiceHealth struct {
	Kind  ServiceHealthKind `json:"kind"`
	Value string            `json:"value"`
}

func (h ServiceHealth) String() string {
	switch h.Kind {
	case ServiceHealthPort:
		return "port:" + h.Value
	case ServiceHealthURL:
		return "url:" + h.Value
	case ServiceHealthCmd:
		return "cmd:" + h.Value
	default:
		return ""
	}
}

// Suffix is the compact rail/receipt spelling.
func (h ServiceHealth) Suffix() string {
	if h.Kind == ServiceHealthPort {
		return ":" + strings.TrimPrefix(h.Value, ":")
	}
	return h.Value
}

func ParseServiceHealth(raw string) (ServiceHealth, error) {
	kind, value, ok := strings.Cut(strings.TrimSpace(raw), ":")
	if !ok || strings.TrimSpace(value) == "" {
		return ServiceHealth{}, fmt.Errorf("parse service health: %w: expected port:, url:, or cmd:", ErrInvalid)
	}
	health := ServiceHealth{Kind: ServiceHealthKind(strings.ToLower(strings.TrimSpace(kind))), Value: strings.TrimSpace(value)}
	switch health.Kind {
	case ServiceHealthPort:
		health.Value = strings.TrimPrefix(health.Value, ":")
		port, err := strconv.Atoi(health.Value)
		if err != nil || port < 1 || port > 65535 {
			return ServiceHealth{}, fmt.Errorf("parse service health: %w: invalid port %q", ErrInvalid, health.Value)
		}
	case ServiceHealthURL:
		parsed, err := url.Parse(health.Value)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return ServiceHealth{}, fmt.Errorf("parse service health: %w: invalid URL %q", ErrInvalid, health.Value)
		}
	case ServiceHealthCmd:
		// The command is intentionally preserved verbatim after trimming.
	default:
		return ServiceHealth{}, fmt.Errorf("parse service health: %w: unknown kind %q", ErrInvalid, health.Kind)
	}
	return health, nil
}

type ServiceProvenance struct {
	OriginJobID int    `json:"origin_job_id"`
	LeafNodeID  string `json:"leaf_node_id"`
}

// Service is the materialized view of a process the user meant to keep.
type Service struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Command      string            `json:"command"`
	Dir          string            `json:"dir"`
	Health       ServiceHealth     `json:"health"`
	LogPath      string            `json:"log_path"`
	Provenance   ServiceProvenance `json:"provenance"`
	PID          int               `json:"pid"`
	StartedAt    time.Time         `json:"started_at"`
	Status       ServiceStatus     `json:"status"`
	AutoRestart  bool              `json:"auto_restart"`
	RestartCount int               `json:"restart_count"`
	CreatedSeq   int64             `json:"created_seq"`
	UpdatedSeq   int64             `json:"updated_seq"`
}

const serviceSchema = `
CREATE TABLE IF NOT EXISTS services (
    id             TEXT PRIMARY KEY,
    name           TEXT NOT NULL,
    command        TEXT NOT NULL,
    dir            TEXT NOT NULL,
    health         JSON NOT NULL CHECK (json_valid(health)),
    log_path       TEXT NOT NULL,
    provenance     JSON NOT NULL CHECK (json_valid(provenance)),
    pid            INTEGER NOT NULL CHECK (pid >= 0),
    started_at     TEXT NOT NULL,
    status         TEXT NOT NULL CHECK (status IN ('running', 'stopped', 'failed', 'resting')),
    auto_restart   INTEGER NOT NULL DEFAULT 0 CHECK (auto_restart IN (0, 1)),
    restart_count  INTEGER NOT NULL DEFAULT 0 CHECK (restart_count >= 0),
    created_seq    INTEGER NOT NULL REFERENCES events(seq),
    updated_seq    INTEGER NOT NULL REFERENCES events(seq)
);
CREATE UNIQUE INDEX IF NOT EXISTS services_live_name
    ON services (name COLLATE NOCASE) WHERE status != 'stopped';
CREATE INDEX IF NOT EXISTS services_status_name ON services (status, name);
CREATE VIRTUAL TABLE IF NOT EXISTS services_fts USING fts5(service_id UNINDEXED, name, command);
`

type servicePromotedPayload struct {
	Service Service `json:"service"`
}

type serviceAdoptedPayload struct {
	AutoRestart *bool `json:"auto_restart,omitempty"`
}

type serviceStoppedPayload struct {
	Reason string `json:"reason,omitempty"`
}

type serviceFailedPayload struct {
	Reason       string `json:"reason,omitempty"`
	RestartCount int    `json:"restart_count"`
}

type serviceRestartedPayload struct {
	PID          int       `json:"pid"`
	StartedAt    time.Time `json:"started_at"`
	RestartCount int       `json:"restart_count"`
}

// PromoteService transfers a live job into the durable service view. The
// partial unique index is the race-safe enforcement of live name ownership.
func (s *Store) PromoteService(service Service) (Service, error) {
	service.ID = strings.TrimSpace(service.ID)
	service.Name = strings.TrimSpace(service.Name)
	service.Command = strings.TrimSpace(service.Command)
	service.Dir = strings.TrimSpace(service.Dir)
	service.LogPath = strings.TrimSpace(service.LogPath)
	service.Provenance.LeafNodeID = strings.TrimSpace(service.Provenance.LeafNodeID)
	if service.ID == "" || service.Name == "" || service.Command == "" || service.Dir == "" ||
		service.LogPath == "" || service.PID <= 0 || service.StartedAt.IsZero() ||
		service.Provenance.OriginJobID <= 0 || service.Provenance.LeafNodeID == "" {
		return Service{}, fmt.Errorf("promote service: %w: incomplete service identity", ErrInvalid)
	}
	if _, err := ParseServiceHealth(service.Health.String()); err != nil {
		return Service{}, fmt.Errorf("promote service: %w", err)
	}
	service.Status = ServiceRunning
	if service.RestartCount < 0 {
		return Service{}, fmt.Errorf("promote service: %w: negative restart count", ErrInvalid)
	}
	payload := servicePromotedPayload{Service: service}
	tx, err := s.beginWrite()
	if err != nil {
		return Service{}, fmt.Errorf("promote service: %w", err)
	}
	defer tx.Rollback()
	if err := requireNode(tx, service.Provenance.LeafNodeID); err != nil {
		return Service{}, fmt.Errorf("promote service: %w", err)
	}
	seq, _, err := appendEvent(tx, service.ID, EventServicePromoted, payload)
	if err != nil {
		return Service{}, fmt.Errorf("promote service: %w", err)
	}
	if err := applyServicePromoted(tx, payload, seq); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique constraint") {
			return Service{}, fmt.Errorf("promote service: %w: service name %q is already active", ErrInvalid, service.Name)
		}
		return Service{}, fmt.Errorf("promote service: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Service{}, fmt.Errorf("promote service: %w", err)
	}
	service.CreatedSeq, service.UpdatedSeq = seq, seq
	return service, nil
}

func applyServicePromoted(tx *sql.Tx, payload servicePromotedPayload, seq int64) error {
	service := payload.Service
	health, err := json.Marshal(service.Health)
	if err != nil {
		return err
	}
	provenance, err := json.Marshal(service.Provenance)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO services (
		id, name, command, dir, health, log_path, provenance, pid, started_at,
		status, auto_restart, restart_count, created_seq, updated_seq
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		service.ID, service.Name, service.Command, service.Dir, string(health), service.LogPath,
		string(provenance), service.PID, formatTime(service.StartedAt), ServiceRunning,
		service.AutoRestart, service.RestartCount, seq, seq)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO services_fts (service_id, name, command) VALUES (?, ?, ?)`,
		service.ID, service.Name, service.Command)
	return err
}

func (s *Store) AdoptService(id string) error {
	return s.adoptService(id, serviceAdoptedPayload{})
}

func (s *Store) SetServiceAutoRestart(id string, enabled bool) error {
	return s.adoptService(id, serviceAdoptedPayload{AutoRestart: &enabled})
}

func (s *Store) adoptService(id string, payload serviceAdoptedPayload) error {
	return s.transitionService(id, EventServiceAdopted, payload, func(tx *sql.Tx, seq int64) error {
		return applyServiceAdoptedPayload(tx, id, payload, seq)
	})
}

func applyServiceAdopted(tx *sql.Tx, id string, seq int64) error {
	return applyServiceAdoptedPayload(tx, id, serviceAdoptedPayload{}, seq)
}

func applyServiceAdoptedPayload(tx *sql.Tx, id string, payload serviceAdoptedPayload, seq int64) error {
	var result sql.Result
	var err error
	if payload.AutoRestart == nil {
		result, err = tx.Exec(`UPDATE services SET updated_seq=? WHERE id=?`, seq, id)
	} else {
		result, err = tx.Exec(`UPDATE services SET auto_restart=?, updated_seq=? WHERE id=?`, *payload.AutoRestart, seq, id)
	}
	return requireServiceChange(result, err, id)
}

func (s *Store) StopService(id, reason string) error {
	payload := serviceStoppedPayload{Reason: bounded(strings.TrimSpace(reason), MaxDigestBytes)}
	return s.transitionService(id, EventServiceStopped, payload, func(tx *sql.Tx, seq int64) error {
		return applyServiceStatus(tx, id, ServiceStopped, seq, 0)
	})
}

func (s *Store) FailService(id, reason string, restartCount int) error {
	if restartCount < 0 {
		return fmt.Errorf("fail service: %w: negative restart count", ErrInvalid)
	}
	payload := serviceFailedPayload{Reason: bounded(strings.TrimSpace(reason), MaxDigestBytes), RestartCount: restartCount}
	return s.transitionService(id, EventServiceFailed, payload, func(tx *sql.Tx, seq int64) error {
		return applyServiceStatus(tx, id, ServiceFailed, seq, restartCount)
	})
}

func (s *Store) RestartService(id string, pid int, startedAt time.Time, restartCount int) error {
	if pid <= 0 || startedAt.IsZero() || restartCount < 0 {
		return fmt.Errorf("restart service: %w: invalid process identity", ErrInvalid)
	}
	payload := serviceRestartedPayload{PID: pid, StartedAt: startedAt, RestartCount: restartCount}
	return s.transitionService(id, EventServiceRestarted, payload, func(tx *sql.Tx, seq int64) error {
		return applyServiceRestarted(tx, id, payload, seq)
	})
}

func (s *Store) RestService(id, reason string, restartCount int) error {
	payload := serviceFailedPayload{Reason: bounded(strings.TrimSpace(reason), MaxDigestBytes), RestartCount: restartCount}
	return s.transitionService(id, EventServiceRested, payload, func(tx *sql.Tx, seq int64) error {
		return applyServiceStatus(tx, id, ServiceResting, seq, restartCount)
	})
}

func (s *Store) transitionService(id string, kind EventKind, payload any, apply func(*sql.Tx, int64) error) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("service transition: %w: empty id", ErrInvalid)
	}
	tx, err := s.beginWrite()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists bool
	if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM services WHERE id=?)`, id).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("service transition: %w: %q", ErrNotFound, id)
	}
	seq, _, err := appendEvent(tx, id, kind, payload)
	if err != nil {
		return err
	}
	if err := apply(tx, seq); err != nil {
		return err
	}
	return tx.Commit()
}

func applyServiceStatus(tx *sql.Tx, id string, status ServiceStatus, seq int64, restartCount int) error {
	result, err := tx.Exec(`UPDATE services SET status=?, restart_count=?, updated_seq=? WHERE id=?`,
		status, restartCount, seq, id)
	return requireServiceChange(result, err, id)
}

func applyServiceRestarted(tx *sql.Tx, id string, payload serviceRestartedPayload, seq int64) error {
	result, err := tx.Exec(`UPDATE services SET status=?, pid=?, started_at=?, restart_count=?, updated_seq=? WHERE id=?`,
		ServiceRunning, payload.PID, formatTime(payload.StartedAt), payload.RestartCount, seq, id)
	return requireServiceChange(result, err, id)
}

func requireServiceChange(result sql.Result, err error, id string) error {
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("service %q is missing", id)
	}
	return nil
}

func (s *Store) Service(id string) (Service, bool, error) {
	row := s.db.QueryRow(`SELECT `+serviceColumns+` FROM services WHERE id=?`, id)
	service, err := scanService(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Service{}, false, nil
	}
	if err != nil {
		return Service{}, false, fmt.Errorf("read service %q: %w", id, err)
	}
	return service, true, nil
}

func (s *Store) ActiveServices() ([]Service, error) {
	return s.queryServices(`WHERE status != ? ORDER BY name COLLATE NOCASE, created_seq`, []any{ServiceStopped})
}

func (s *Store) ServiceByName(name string) (Service, bool, error) {
	services, err := s.queryServices(`WHERE name = ? COLLATE NOCASE AND status != ? ORDER BY created_seq DESC LIMIT 1`,
		[]any{strings.TrimSpace(name), ServiceStopped})
	if err != nil || len(services) == 0 {
		return Service{}, false, err
	}
	return services[0], true, nil
}

func (s *Store) SearchServices(reference string) ([]Service, error) {
	terms := serviceSearchTerms(reference)
	if len(terms) == 0 {
		return s.ActiveServices()
	}
	quoted := make([]string, 0, len(terms))
	for _, term := range terms {
		quoted = append(quoted, `"`+strings.ReplaceAll(term, `"`, `""`)+`"`)
	}
	return s.queryServices(`JOIN services_fts ON services_fts.service_id=services.id
		WHERE services.status != ? AND services_fts MATCH ? ORDER BY bm25(services_fts), services.created_seq DESC`,
		[]any{ServiceStopped, strings.Join(quoted, " OR ")})
}

// SearchRestartableServices includes stopped history so the startup receipt's
// “start it again” instruction has a real conversational target. The newest
// incarnation of each name wins when a stopped name was later reused.
func (s *Store) SearchRestartableServices(reference string) ([]Service, error) {
	terms := serviceSearchTerms(reference)
	var services []Service
	var err error
	if len(terms) == 0 {
		services, err = s.queryServices(`ORDER BY created_seq DESC`, nil)
	} else {
		quoted := make([]string, 0, len(terms))
		for _, term := range terms {
			quoted = append(quoted, `"`+strings.ReplaceAll(term, `"`, `""`)+`"`)
		}
		services, err = s.queryServices(`JOIN services_fts ON services_fts.service_id=services.id
			WHERE services_fts MATCH ? ORDER BY bm25(services_fts), services.created_seq DESC`,
			[]any{strings.Join(quoted, " OR ")})
	}
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	result := make([]Service, 0, len(services))
	for _, service := range services {
		key := strings.ToLower(service.Name)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, service)
	}
	if len(terms) == 0 {
		// With no reference words there is no relevance to honor, so the list
		// the user sees is ordered exactly like the rail: by name.
		sort.SliceStable(result, func(first, second int) bool {
			return strings.ToLower(result[first].Name) < strings.ToLower(result[second].Name)
		})
	}
	return result, nil
}

// ServiceHygieneAsked reports whether the once-per-service hygiene nudge has
// already been journaled. The durable question is the memory; asking twice
// about the same service is the thing this prevents.
func (s *Store) ServiceHygieneAsked(serviceID string) (bool, error) {
	var asked bool
	err := s.db.QueryRow(`SELECT EXISTS(
		SELECT 1 FROM agent_questions, json_each(agent_questions.options)
		WHERE json_extract(json_each.value, '$.value') = ?)`,
		ServiceHygieneKeepValue(strings.TrimSpace(serviceID))).Scan(&asked)
	if err != nil {
		return false, fmt.Errorf("read service hygiene memory: %w", err)
	}
	return asked, nil
}

// ServiceHygieneKeepValue is the durable option value that marks a service as
// already nudged. It is shared by the asker and the answer router.
func ServiceHygieneKeepValue(serviceID string) string {
	return "service:hygiene-keep:" + serviceID
}

// ServiceHygieneStopValue is the answering half of the same nudge.
func ServiceHygieneStopValue(serviceID string) string {
	return "service:hygiene-stop:" + serviceID
}

// SessionQuietSince reports whether a session has carried no user message
// since the given moment — "no activity near this service" in one query.
func (s *Store) SessionQuietSince(sessionID string, since time.Time) (bool, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return true, nil
	}
	var spoke bool
	err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM messages
		WHERE session_id=? AND role=? AND ts > ?)`, sessionID, RoleUser, formatTime(since)).Scan(&spoke)
	if err != nil {
		return false, fmt.Errorf("read session quiet: %w", err)
	}
	return !spoke, nil
}

func serviceSearchTerms(reference string) []string {
	stop := map[string]bool{"the": true, "a": true, "an": true, "it": true, "service": true, "server": true,
		"please": true, "stop": true, "restart": true, "start": true, "running": true, "auto": true, "enable": true}
	seen := map[string]bool{}
	var terms []string
	for _, term := range strings.FieldsFunc(strings.ToLower(reference), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	}) {
		if len(term) < 2 || stop[term] || seen[term] {
			continue
		}
		seen[term] = true
		terms = append(terms, term)
	}
	return terms
}

const serviceColumns = `services.id, services.name, services.command, services.dir, services.health,
	services.log_path, services.provenance, services.pid, services.started_at, services.status,
	services.auto_restart, services.restart_count, services.created_seq, services.updated_seq`

func (s *Store) queryServices(clause string, args []any) ([]Service, error) {
	rows, err := s.db.Query(`SELECT `+serviceColumns+` FROM services `+clause, args...)
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}
	defer rows.Close()
	var services []Service
	for rows.Next() {
		service, err := scanService(rows)
		if err != nil {
			return nil, fmt.Errorf("list services: %w", err)
		}
		services = append(services, service)
	}
	return services, rows.Err()
}

func scanService(scanner rowScanner) (Service, error) {
	var service Service
	var health, provenance, started string
	if err := scanner.Scan(&service.ID, &service.Name, &service.Command, &service.Dir, &health,
		&service.LogPath, &provenance, &service.PID, &started, &service.Status,
		&service.AutoRestart, &service.RestartCount, &service.CreatedSeq, &service.UpdatedSeq); err != nil {
		return Service{}, err
	}
	if err := json.Unmarshal([]byte(health), &service.Health); err != nil {
		return Service{}, err
	}
	if err := json.Unmarshal([]byte(provenance), &service.Provenance); err != nil {
		return Service{}, err
	}
	parsed, err := parseTime(started)
	if err != nil {
		return Service{}, err
	}
	service.StartedAt = parsed
	return service, nil
}
