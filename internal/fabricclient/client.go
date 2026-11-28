// Package fabricclient calls the signed client-fabric routes that a full
// computer uses when admitting or inspecting paired phones. Every request is
// stamped with reqsign so the sync service authenticates both the identity
// namespace and the acting computer, and every wire error code is translated
// into a typed error whose text never exposes raw wire codes to a person.
package fabricclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/clientproto"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// codedError pairs a person-facing sentence with an underlying wireauth
// sentinel so callers can match either package's error while never printing a
// raw wire code.
type codedError struct {
	sentence string
	kind     error
}

func (e *codedError) Error() string { return e.sentence }
func (e *codedError) Unwrap() error { return e.kind }

// Sentinel errors mapped from the fabric wire's {"err":"<code>"} responses.
var (
	// ErrStale means the sync service already holds an equal or newer enrollment version for this phone.
	ErrStale = errors.New("that phone enrollment is older than the one already stored")
	// ErrExists means a device record with that id already exists in the directory.
	ErrExists = errors.New("that device is already registered")
	// ErrBadRequest means the request body or parameters violated the route contract.
	ErrBadRequest = errors.New("the relay refused an invalid phone request")
	// ErrUnauthorized means the caller is not permitted to use this route.
	ErrUnauthorized error = &codedError{"this computer is not authorized for that request", wireauth.ErrUnauthorized}
	// ErrRevoked means the calling device has been removed from the identity.
	ErrRevoked error = &codedError{"this computer was removed and can no longer reach your chats", wireauth.ErrRevoked}
	// ErrSkew means the caller's clock differs too much from the relay's clock.
	ErrSkew error = &codedError{"this computer's clock is too far from the relay's clock", wireauth.ErrSkew}
	// ErrMethodDenied means a client attempted an action outside its enrolled method list.
	ErrMethodDenied = errors.New("that action is not enabled for this phone")
	// ErrStorageOff means storage is disabled on the sync service.
	ErrStorageOff error = &codedError{"storage is turned off on this relay", wireauth.ErrStorageOff}
	// ErrNotFound means the target client or resource does not exist.
	ErrNotFound = errors.New("that phone is not registered on the relay")
	// ErrClaimed means another computer or token already owns the queue entry.
	ErrClaimed = errors.New("that phone call is already claimed")
	// ErrSequence means a queue entry number was out of sequence.
	ErrSequence = errors.New("that phone call number is out of order")
	// ErrNotEnrolled means a client-class approval was attempted before enrollment and registration completed.
	ErrNotEnrolled = errors.New("that phone has not finished enrolling yet")
	// ErrAlreadyDecided means the pairing request was already answered.
	ErrAlreadyDecided = errors.New("that request was already answered")
	// ErrRotated means the identity is being replaced and refuses writes.
	ErrRotated error = &codedError{"this identity is being replaced", wireauth.ErrRotated}
	// ErrGone means the identity was replaced and deleted.
	ErrGone error = &codedError{"this identity was replaced and removed", wireauth.ErrGone}
	// ErrTooLarge means the request body exceeded the route's size cap.
	ErrTooLarge = errors.New("that phone record is too large for the relay")
	// ErrFull means the identity has reached its phone or slot capacity on the sync service.
	ErrFull = errors.New("this account has reached the phone limit on the relay")
	// ErrRateLimited means the caller must wait before retrying.
	ErrRateLimited error = &codedError{"the relay is busy right now; try again in a moment", wireauth.ErrRateLimited}
	// ErrUnavailable means the sync service is temporarily unable to serve the request.
	ErrUnavailable error = &codedError{"the relay is not available right now", wireauth.ErrUnavailable}
	// ErrUnreachable means the HTTP request could not reach the sync service.
	ErrUnreachable = errors.New("cannot reach the relay")
)

// EnrollmentRow is one stored phone enrollment returned by GET /v1/clients.
type EnrollmentRow struct {
	Device string                 `json:"device"`
	Enroll clientproto.Enrollment `json:"enroll"`
}

// Client speaks the signed fabric routes as a full device of an identity.
type Client struct {
	BaseURL string
	Signer  reqsign.Signer
	HTTP    *http.Client
	Now     func() time.Time
}

// New returns a signed fabric client for the sync service at baseURL.
func New(baseURL string, signer reqsign.Signer, hc *http.Client) *Client {
	return &Client{
		BaseURL: baseURL,
		Signer:  signer,
		HTTP:    hc,
		Now:     time.Now,
	}
}

// Base returns the sync address with any trailing slash removed.
func (c *Client) Base() string {
	return strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// PutEnrollment stores a signed phone enrollment at PUT /v1/clients/{device}/enroll.
// An identical re-send succeeds with 204; a lower version or a different
// statement at the same version fails with ErrStale.
func (c *Client) PutEnrollment(ctx context.Context, e clientproto.Enrollment) error {
	if strings.TrimSpace(e.Device) == "" {
		return ErrBadRequest
	}
	path := "/v1/clients/" + url.PathEscape(e.Device) + "/enroll"
	return c.do(ctx, http.MethodPut, path, e, nil)
}

type registerRequest struct {
	Cert    identity.Cert `json:"cert"`
	Methods []string      `json:"methods"`
	Name    string        `json:"name"`
}

// RegisterClient writes the client device record at PUT /v1/clients/{device}.
// When the device is already in the directory, the sync service answers 409
// exists and RegisterClient returns ErrExists so the caller can treat a retry
// for the same client as idempotent.
func (c *Client) RegisterClient(ctx context.Context, cert identity.Cert, methods []string, sealedName string) error {
	device := cert.DeviceID()
	if strings.TrimSpace(device) == "" {
		return ErrBadRequest
	}
	if methods == nil {
		methods = []string{}
	}
	path := "/v1/clients/" + url.PathEscape(device)
	body := registerRequest{
		Cert:    cert,
		Methods: methods,
		Name:    sealedName,
	}
	return c.do(ctx, http.MethodPut, path, body, nil)
}

// ListEnrollments fetches every stored phone enrollment for this identity from GET /v1/clients.
func (c *Client) ListEnrollments(ctx context.Context) ([]EnrollmentRow, error) {
	var out struct {
		Clients []EnrollmentRow `json:"clients"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/clients", nil, &out); err != nil {
		return nil, err
	}
	return out.Clients, nil
}

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	if c == nil || c.Signer == nil {
		return ErrUnauthorized
	}
	var raw []byte
	var bodyReader io.Reader
	if in != nil {
		var err error
		raw, err = json.Marshal(in)
		if err != nil {
			return err
		}
		bodyReader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, wireauth.Endpoint(c.BaseURL, path), bodyReader)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	reqsign.Sign(req, raw, c.Signer, c.now())
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnreachable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return decodeError(resp)
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(io.LimitReader(resp.Body, clientproto.MaxFabricBody)).Decode(out)
}

type errBody struct {
	Err string `json:"err"`
}

func decodeError(resp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, clientproto.MaxFabricBody))
	var b errBody
	if json.Unmarshal(raw, &b) == nil && b.Err != "" {
		if err := mapErrorCode(resp.StatusCode, b.Err); err != nil {
			return wireauth.Wait(err, resp.Header)
		}
	}
	switch resp.StatusCode {
	case http.StatusBadRequest:
		return ErrBadRequest
	case http.StatusUnauthorized:
		return ErrUnauthorized
	case http.StatusForbidden:
		return ErrMethodDenied
	case http.StatusNotFound:
		return ErrNotFound
	case http.StatusGone:
		return ErrGone
	case http.StatusRequestEntityTooLarge:
		return ErrTooLarge
	case http.StatusTooManyRequests, http.StatusInsufficientStorage:
		return wireauth.Wait(ErrFull, resp.Header)
	}
	if resp.StatusCode >= http.StatusInternalServerError {
		return ErrUnavailable
	}
	return ErrBadRequest
}

func mapErrorCode(status int, code string) error {
	switch code {
	case "version":
		return ErrStale
	case "exists":
		return ErrExists
	case "bad_request", "self_revoke", "bad_grace":
		return ErrBadRequest
	case "unauthorized":
		return ErrUnauthorized
	case "revoked":
		return ErrRevoked
	case "skew":
		return ErrSkew
	case "method_denied":
		return ErrMethodDenied
	case "storage_off":
		return ErrStorageOff
	case "not_found":
		return ErrNotFound
	case "claimed":
		return ErrClaimed
	case "sequence":
		return ErrSequence
	case "not_enrolled":
		return ErrNotEnrolled
	case "already_decided":
		return ErrAlreadyDecided
	case "rotated":
		return ErrRotated
	case "gone":
		if status == http.StatusNotFound {
			return ErrNotFound
		}
		return ErrGone
	case "too_large", "too_big":
		return ErrTooLarge
	case "full", "relay_storage_full":
		return ErrFull
	case "rate_limited", "relay_admission_full", "too_many_identities", "too_many_watchers":
		return ErrRateLimited
	case "unavailable", "unreachable", "internal":
		return ErrUnavailable
	}
	return nil
}
