package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/Agent-Field/codeaf/internal/factory"
	"net"
	"net/url"
	"strings"
	"time"
)

const keyAppMCPServers = "app.mcp.servers"

// AppMCPServer holds public connection facts; bearer tokens come from the app's pipe.
type AppMCPServer struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Address string `json:"address"`
}

// AppMCPAddress refuses credentials in addresses before any bytes can reach disk.
func AppMCPAddress(address string) (string, error) {
	address = strings.TrimSpace(address)
	if !strings.Contains(address, "://") {
		scheme := "https://"
		host := strings.Split(address, "/")[0]
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		ip := net.ParseIP(strings.Trim(host, "[]"))
		if host == "localhost" || ip != nil && ip.IsLoopback() {
			scheme = "http://"
		}
		address = scheme + address
	}
	u, err := url.Parse(address)
	if err != nil || len(address) > 2048 || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || strings.ContainsAny(address, " \t\r\n\x00") || (u.Scheme != "http" && u.Scheme != "https") {
		return "", errors.New("use an HTTP or HTTPS server address without credentials, query or fragment")
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme == "http" && u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return "", errors.New("remote servers require HTTPS")
	}
	u.Host = strings.ToLower(u.Host)
	return u.String(), nil
}

func appMCPRows(held map[string]json.RawMessage) ([]AppMCPServer, error) {
	rows := []AppMCPServer{}
	if raw, ok := held[keyAppMCPServers]; ok && json.Unmarshal(raw, &rows) != nil {
		return nil, errors.New("could not read saved servers")
	}
	for _, row := range rows {
		address, err := AppMCPAddress(row.Address)
		if err != nil || address != row.Address || row.ID != appMCPID(address) {
			return nil, errors.New("could not read saved servers")
		}
	}
	return rows, nil
}
func appMCPID(address string) string {
	sum := sha256.Sum256([]byte(address))
	return "mcp_" + hex.EncodeToString(sum[:8])
}

func AppMCPServersAt(profile string) ([]AppMCPServer, error) {
	held, err := readProfileConfig(profile)
	if err != nil {
		return nil, err
	}
	return appMCPRows(held)
}

// AddAppMCPServer shares the profile lock, so independent app launches cannot lose a server.
func AddAppMCPServer(profile, address string) (AppMCPServer, error) {
	address, err := AppMCPAddress(address)
	if err != nil {
		return AppMCPServer{}, err
	}
	u, _ := url.Parse(address)
	row := AppMCPServer{ID: appMCPID(address), Name: u.Hostname(), Address: address}
	err = transformProfileValues(profile, func(held map[string]json.RawMessage) (map[string]any, error) {
		rows, err := appMCPRows(held)
		if err != nil {
			return nil, err
		}
		for _, old := range rows {
			if old.ID == row.ID {
				row = old
				return nil, nil
			}
		}
		if len(rows) >= 64 {
			return nil, errors.New("at most 64 servers may be saved")
		}
		return map[string]any{keyAppMCPServers: append(rows, row)}, nil
	})
	return row, err
}

func RemoveAppMCPServer(profile, id string) error {
	return transformProfileValues(profile, func(held map[string]json.RawMessage) (map[string]any, error) {
		rows, err := appMCPRows(held)
		if err != nil {
			return nil, err
		}
		for i, row := range rows {
			if row.ID == id {
				return map[string]any{keyAppMCPServers: append(rows[:i], rows[i+1:]...), "app.accounts.mcp:" + id: removeProfileKey}, nil
			}
		}
		return nil, errors.New("server not found")
	})
}

// Account action ids are bounded and public; the settings namespace can never carry tokens.
func appAccountActionKey(key string) bool {
	parts := strings.Split(key, ".")
	if len(parts) != 4 || parts[0] != "accounts" || parts[2] != "actions" {
		return false
	}
	allowed := map[string]string{"google": "mail-read mail-send calendar-read calendar-write", "slack": "messages-read messages-send"}
	for _, id := range strings.Fields(allowed[parts[1]]) {
		if parts[3] == id {
			return true
		}
	}
	return false
}

// AppOutputPolicyAt resolves this Mac's saved window and age at launch or sweep.
func AppOutputPolicyAt(profile string) (bytes int64, age time.Duration) {
	bytes, age = int64(factory.DefaultKeepOutputMB)*(1<<20), time.Duration(factory.DefaultKeepOutputDays)*24*time.Hour
	values, _, err := AppSettingsAt(profile)
	if err != nil {
		return
	}
	number := func(key string) int64 {
		switch n := values[key].(type) {
		case int:
			return int64(n)
		case float64:
			return int64(n)
		}
		return 0
	}
	bytes = number("factory.keep_output_mb") * (1 << 20)
	age = time.Duration(number("factory.keep_output_days")) * 24 * time.Hour
	return
}
