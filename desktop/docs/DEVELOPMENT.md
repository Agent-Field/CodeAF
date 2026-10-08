# Development across machines

## Recommended: local native shell, remote UI

Keep the primary working checkout on your Linux development machine. Run the frontend there:

```sh
npm ci
npm run dev
```

On your Mac, forward the development port through SSH (replace `YOUR_DEV_HOST` with the reachable SSH alias):

```sh
ssh -N -L 1420:127.0.0.1:1420 YOUR_DEV_HOST
```

In a second Mac terminal, clone the integration branch and prepare the native checkout:

```sh
git clone --branch work/8f3c2a9d git@github.com:Agent-Field/CodeAF.git codeaf
cd codeaf/desktop
npm ci
npm run desktop:remote
```

Install Node 24, Go 1.26, Rust stable, and Xcode command line tools first. `desktop:remote` builds the local Go sidecar and launches the Mac shell against forwarded localhost without starting another Vite server. UI edits on Linux hot-reload on the Mac. Default Vite HMR shares port 1420, so one tunnel is sufficient. Keep both terminals running.

Use Git to sync Rust/Go/native configuration changes to the Mac; restart the Mac shell afterward. Avoid copying node_modules, target, dist, or generated sidecars between machines. These are platform-specific. This setup keeps the local engine on the Mac. Running tasks remotely on Linux would require a separate authenticated engine transport, which is not implemented.

## Simpler fallback

Commit and pull changes, then run `npm run desktop:dev` on the Mac. It starts Vite, the native shell, and the local Go health command from the same checkout. This is also the recommended way to verify production behavior before packaging.

## Verification boundaries

A browser on Linux can verify layout and React interactions. It cannot validate macOS WebView rendering, native menus, permissions, signing, or sidecar packaging. Run native checks on both OSes; macOS distribution needs signing/notarization configuration before public release.

## Monorepo workflow

The desktop is in desktop/ on public integration branch work/8f3c2a9d.
Use the same branch on both machines. Run commands from desktop/; merge
upstream/dev with npm run upstream:sync from a clean checkout. This checks
the desktop and root gates before you review and push. No automatic rebase
or publication occurs. See ../../docs/DESKTOP.md for the engine boundaries.
