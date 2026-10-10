# Desktop forwarded engine

## Can the desktop use a remote engine forwarded to this computer

A development desktop can attach to an existing engine through a saved connection record. `CODEAF_DESKTOP_CONNECTION` points to that record on disk; it contains the engine's loopback HTTP URL, token and model. A forwarded engine remains running when the desktop quits.

## Why does a forwarded localhost engine work with the desktop security policy

The desktop changes the connection's `http://localhost:` address to `http://127.0.0.1:` before sharing it with the window. The port, engine token and model stay the same. The app's security policy allows this numeric loopback address without allowing connections to other computers.

## Why is my forwarded desktop engine connection refused

The connection must use HTTP on localhost or 127.0.0.1 with a nonzero port and a nonempty token. Credentials in the URL, a non-root path, a query or a fragment are refused with "The forwarded engine connection must be an authenticated loopback URL". An unreadable record says "The forwarded engine connection is unreadable"; invalid JSON says "The forwarded engine connection is invalid".
