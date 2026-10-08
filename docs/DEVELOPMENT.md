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

In a second Mac terminal, clone this private repo and prepare the native checkout:

```sh
git clone git@github.com:santoshkumarradha/codeaf-app.git
cd codeaf-app
npm ci
npm run desktop:remote
```

Install Node 24, Go 1.26, Rust stable, and Xcode command line tools first. `desktop:remote` builds the local Go sidecar and launches the Mac shell against forwarded localhost without starting another Vite server. UI edits on Linux hot-reload on the Mac. Default Vite HMR shares port 1420, so one tunnel is sufficient. Keep both terminals running.

Use Git to sync Rust/Go/native configuration changes to the Mac; restart the Mac shell afterward. Avoid copying node_modules, target, dist, or generated sidecars between machines. These are platform-specific. This setup keeps the local engine on the Mac. Running tasks remotely on Linux would require a separate authenticated engine transport, which is not implemented.

## Simpler fallback

Commit and pull changes, then run `npm run desktop:dev` on the Mac. It starts Vite, the native shell, and the local Go health command from the same checkout. This is also the recommended way to verify production behavior before packaging.

## Verification boundaries

A browser on Linux can verify layout and React interactions. It cannot validate macOS WebView rendering, native menus, permissions, signing, or sidecar packaging. Run native checks on both OSes; macOS distribution needs signing/notarization configuration before public release.

## This workspace

The scaffold is at `/home/santosh/codeaf-app` on Spark and `/Users/santoshkumar/codeaf-app` on Blackmac. The Mac copy was transferred over SSH because its GitHub SSH credential cannot currently clone this private repository; it is not a Git checkout yet. Once repository access is configured, clone into a fresh directory and use that checkout for ongoing work.

With `npm run dev` running on Spark, open a second Spark terminal:

```sh
ssh -N -R 1420:127.0.0.1:1420 blackmac
```

Then in a Blackmac terminal:

```sh
export PATH=/opt/homebrew/bin:/usr/local/go/bin:$HOME/.cargo/bin:$PATH
cd ~/codeaf-app
npm run desktop:remote
```

This reverse tunnel avoids requiring inbound SSH from Blackmac to Spark. If Mac port 1420 is already occupied, stop that development server or choose another forwarded port and set `CODEAF_DEV_URL` accordingly. First native launch can take 1–3 minutes; frontend edits then hot-reload. The Go toolchain is selected automatically from `engine/go.mod`.
