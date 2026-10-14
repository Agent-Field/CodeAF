// Package space is the personal sidecar's memory of workspaces and, when
// started with --workspace, the workspace server itself.
//
// Personal keeps working the way it did: space.info is this Mac, one member
// "me". Joined workspaces and their member tokens live in the OS keychain
// (Secret Service on Linux, Keychain on macOS). A file keyring exists for
// tests and is not a fallback the server uses.
//
// # Running a workspace locally
//
// Two homes, two ports. Nothing here touches the other home.
//
//	# terminal 1 — the acme server. The first start prints a setup code.
//	codeaf app serve --workspace --name acme --listen 127.0.0.1:8443 \
//	  --home /tmp/acme-home --public-url http://127.0.0.1:8443 --signin email
//
//	# terminal 2 — the personal sidecar the desk already talks to
//	codeaf app serve --home /tmp/personal-home --listen 127.0.0.1:8765
//
// From Personal, spaces.new with that setup code and address
// http://127.0.0.1:8443 makes you Owner. Invite from the workspace, then
// spaces.join the codeaf://join/… link. The email code is printed in the
// workspace log. spaces.signin submits it, and spaces.connect returns the
// bridge url and the member token.
//
// Invite links to a loopback host use http. Every other host uses https,
// because TLS terminates in front of this server and is not terminated here.
// A restart before anyone claims the setup code still accepts the same code;
// it is printed only on the first start.
package space
