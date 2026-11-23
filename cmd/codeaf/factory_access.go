package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/Agent-Field/codeaf/internal/processgroup"
)

// revokeFactorySessions reaches the running service through its private
// discovery receipt, so the command clears the map that browsers actually use.
func revokeFactorySessions(out io.Writer) error {
	body, err := os.ReadFile(filepath.Join(v3FactoryRoot(), "serve.json"))
	if err != nil {
		return fmt.Errorf("cannot find the running factory service: %w", err)
	}
	var discovery factoryDiscovery
	if err := json.Unmarshal(body, &discovery); err != nil {
		return fmt.Errorf("cannot read factory service discovery: %w", err)
	}
	if discovery.PID <= 0 || !processgroup.ProcessAlive(discovery.PID) {
		return fmt.Errorf("the factory service is not running; start codeaf factory serve")
	}
	address, err := url.Parse(discovery.URL)
	if err != nil || address.Scheme != "http" || address.User != nil || address.Port() == "" {
		return fmt.Errorf("invalid factory service discovery address")
	}
	ip := net.ParseIP(address.Hostname())
	if ip == nil {
		return fmt.Errorf("invalid factory service discovery address")
	}
	// A wildcard listener is where the server binds, not where this local
	// command should connect. Keep revocation on the matching loopback family.
	if ip.IsUnspecified() {
		peer := "::1"
		if ip.To4() != nil {
			peer = "127.0.0.1"
		}
		address.Host = net.JoinHostPort(peer, address.Port())
	}
	address.Path, address.RawPath, address.RawQuery, address.Fragment = "/v1/access/revoke-sessions", "", "", ""
	req, err := http.NewRequest(http.MethodPost, address.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+discovery.Token)
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	response, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("cannot revoke factory sessions: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("factory session revocation returned HTTP %d", response.StatusCode)
	}
	_, err = fmt.Fprintln(out, "factory sessions revoked")
	return err
}
