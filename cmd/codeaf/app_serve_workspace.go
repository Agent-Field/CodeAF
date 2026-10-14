package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps"
	"github.com/Agent-Field/codeaf/internal/apps/space"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/home"
)

func workspaceRequested(args []string) bool {
	for _, arg := range args {
		if arg == "--workspace" || arg == "--workspace=true" {
			return true
		}
	}
	return false
}

// serveWorkspace is the one --workspace branch of `codeaf app serve`.
// TLS is terminated in front of this process; the listener itself is plain HTTP.
func serveWorkspace(args []string) (bool, error) {
	if !workspaceRequested(args) {
		return false, nil
	}
	flags := commandFlags("app serve --workspace")
	var name, listen, homeDir, publicURL, signIn string
	flags.Bool("workspace", false, "serve a workspace instead of Personal")
	flags.StringVar(&name, "name", "", "workspace name, such as acme")
	flags.StringVar(&listen, "listen", ":8443", "address to listen on")
	flags.StringVar(&homeDir, "home", "", "home directory for this workspace (space.db lives here)")
	flags.StringVar(&publicURL, "public-url", "", "address invite links use (default: http://127.0.0.1 and the listen port)")
	flags.StringVar(&signIn, "signin", "email", "how members sign in: email, google, or github")
	if err := parseCommandFlags(flags, args); err != nil {
		return true, err
	}
	if flags.NArg() != 0 || name == "" || homeDir == "" {
		fmt.Fprintln(usageErr, "error: --workspace needs --name and --home")
		return true, exitCannotRun
	}
	root, err := filepath.Abs(homeDir)
	if err != nil {
		return true, err
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return true, err
	}
	if err := os.Setenv(home.EnvVar, root); err != nil {
		return true, err
	}
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		return true, err
	}
	if publicURL == "" {
		publicURL = workspacePublicURL(listener.Addr())
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	all := apps.Wire(ctx, root)
	ws, err := space.OpenWorkspace(svc.Config{Root: filepath.Join(root, "apps"), Ctx: ctx}, all, space.WorkspaceOptions{
		Name: name, Home: root, PublicURL: publicURL, SignIn: signIn,
		Mailer: space.MailerFromEnv(), Providers: space.ProvidersFromEnv(publicURL), SetupOut: os.Stdout,
	})
	if err != nil {
		_ = listener.Close()
		return true, err
	}
	all.Spaces = ws
	all.Members = ws.Members()
	server := &http.Server{
		Handler: ws.Mux(ctx, all), ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout: 60 * time.Second, BaseContext: func(net.Listener) context.Context { return ctx },
	}
	shutdownDone := make(chan struct{})
	guard.Go("workspace/shutdown", func() {
		defer close(shutdownDone)
		<-ctx.Done()
		deadline, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := server.Shutdown(deadline); err != nil {
			_ = server.Close()
		}
	})
	fmt.Fprintf(os.Stdout, "workspace %s listening on %s\n", name, listener.Addr())
	err = server.Serve(listener)
	stop()
	<-shutdownDone
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return true, err
	}
	return true, nil
}

func workspacePublicURL(addr net.Addr) string {
	host, port, err := net.SplitHostPort(addr.String())
	if err != nil {
		return "http://" + addr.String()
	}
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}
