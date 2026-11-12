package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/Agent-Field/codeaf/internal/guard"
)

// The solo host shares its launch bearer only through a private runtime socket.
// serve.json remains a portable discovery record without an app credential.
func serveAppFactoryDiscovery(root string, target factoryTarget) (func(), error) {
	path, err := factoryRunnerSocket(root)
	if err != nil {
		return nil, err
	}
	path += ".solo"
	// The caller holds serve.lock, so the previous socket has no serving owner.
	_ = os.Remove(path)
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0600); err != nil {
		listener.Close()
		return nil, err
	}
	server := &http.Server{ReadHeaderTimeout: time.Second, WriteTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/solo" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(factoryDiscovery{URL: target.url, Token: target.token, PID: os.Getpid(), App: true})
	})}
	guard.Go("app/factory discovery", func() { _ = server.Serve(listener) })
	return func() { _ = server.Close(); _ = os.Remove(path) }, nil
}

func appFactoryDiscovery(root string) (factoryDiscovery, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		path, err := factoryRunnerSocket(root)
		if err != nil {
			return nil, err
		}
		return (&net.Dialer{}).DialContext(ctx, "unix", path+".solo")
	}}
	defer transport.CloseIdleConnections()
	r, err := http.NewRequestWithContext(ctx, "GET", "http://solo/solo", nil)
	if err != nil {
		return factoryDiscovery{}, err
	}
	response, err := (&http.Client{Transport: transport}).Do(r)
	if err != nil {
		return factoryDiscovery{}, err
	}
	defer response.Body.Close()
	var target factoryDiscovery
	err = json.NewDecoder(response.Body).Decode(&target)
	return target, err
}
