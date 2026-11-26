package connections

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func request(ctx context.Context, c Config, token, method, path string, query url.Values, body []byte, key string) ([]byte, error) {
	address := strings.TrimSuffix(c.Origin, "/") + path
	if len(query) != 0 {
		address += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, address, bytes.NewReader(body))
	if err != nil {
		return nil, ErrUnavailable
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	req.Header.Set("Accept", "application/json")
	if method == http.MethodPost {
		req.Header.Set("Idempotency-Key", key)
		req.Header.Set("Content-Type", "application/json")
		req.GetBody = nil
	}
	// A fixed origin keeps the bearer at the configured endpoint. A fresh
	// transport avoids automatic POST replay on a reused connection.
	transport := &http.Transport{
		DialContext:            (&net.Dialer{Timeout: deadline}).DialContext,
		DisableKeepAlives:      true,
		MaxResponseHeaderBytes: 64 << 10,
		TLSHandshakeTimeout:    deadline,
		IdleConnTimeout:        time.Second,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Do(req)
	if err != nil {
		if method == http.MethodPost {
			return nil, ErrUncertain
		}
		return nil, ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 || resp.StatusCode == http.StatusAccepted {
		if method == http.MethodPost {
			if resp.StatusCode == http.StatusConflict || resp.StatusCode == http.StatusPreconditionFailed {
				return nil, ErrConflict
			}
			// Only explicit client-side rejections can be reported as refused.
			// A server error can arrive after the remote operation committed.
			if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusRequestTimeout {
				return nil, ErrRejected
			}
			return nil, ErrUncertain
		}
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return nil, ErrSourcePermission
		}
		return nil, ErrUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		if method == http.MethodPost {
			return nil, ErrUncertain
		}
		return nil, ErrUnavailable
	}
	if len(raw) > maxBytes {
		return nil, ErrTooLarge
	}
	return raw, nil
}

func decodeJSON(body []byte, dest any) error {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(dest); err != nil {
		return ErrInvalidRows
	}
	if err := dec.Decode(new(any)); !errors.Is(err, io.EOF) {
		return ErrInvalidRows
	}
	return nil
}

func httpRows(ctx context.Context, c Config, token string, read Read, args map[string]any) ([]map[string]any, error) {
	query := url.Values{}
	for name, value := range args {
		query.Set(name, fmt.Sprint(value))
	}
	var missing bool
	path := pathParameter.ReplaceAllStringFunc(read.Path, func(marker string) string {
		name := marker[1 : len(marker)-1]
		value := query.Get(name)
		if !pathValue.MatchString(value) {
			missing = true
		}
		return value
	})
	if missing || !fixedPath(path) {
		return nil, errors.New("a path argument needs letters, digits, hyphens or underscores")
	}
	for _, param := range pathParameter.FindAllStringSubmatch(read.Path, -1) {
		query.Del(param[1])
	}
	raw, err := request(ctx, c, token, http.MethodGet, path, query, nil, "")
	if err != nil {
		return nil, err
	}
	if read.Rows != "" {
		for _, property := range strings.Split(read.Rows, ".") {
			var object map[string]json.RawMessage
			if err := decodeJSON(raw, &object); err != nil {
				return nil, err
			}
			raw = object[property]
		}
	}
	if read.Columnar {
		return columnRows(raw, read)
	}
	var rows []map[string]any
	if err := decodeJSON(raw, &rows); err != nil || rows == nil {
		return nil, ErrInvalidRows
	}
	return rows, nil
}

// columnRows transposes only the declared arrays. The remote body is already
// size-limited and the row cap bounds the temporary maps made from it.
func columnRows(raw []byte, read Read) ([]map[string]any, error) {
	const maxColumnRows = 10_000
	var object map[string]json.RawMessage
	if err := decodeJSON(raw, &object); err != nil {
		return nil, err
	}
	names := append([]string{read.Key}, columnNames(read.Fields)...)
	if read.Version != "" {
		names = append(names, read.Version)
	}
	var rows []map[string]any
	for _, name := range names {
		var column []any
		if err := decodeJSON(object[name], &column); err != nil || column == nil || len(column) > maxColumnRows {
			return nil, ErrInvalidRows
		}
		if rows == nil {
			rows = make([]map[string]any, len(column))
			for i := range rows {
				rows[i] = make(map[string]any, len(names))
			}
		} else if len(rows) != len(column) {
			return nil, ErrInvalidRows
		}
		for i, cell := range column {
			rows[i][name] = cell
		}
	}
	return rows, nil
}

func containsCredential(value any, credential string) bool {
	text, ok := value.(string)
	return credential != "" && ok && strings.Contains(text, credential)
}
