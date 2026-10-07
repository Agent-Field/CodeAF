# Background update screens

Captured from `3a99582adbc2a001193ec3db4cd58a132fe00830` with `make build` and `scripts/update-drive.sh` on Spark. Each state is shown at 80 and 120 columns, 40 rows. PNGs were rendered from the captured ANSI cells with agg and ffmpeg using JetBrainsMono Nerd Font Mono; no UI text was added to the images.

The seven update states use deterministic, explicitly labelled render fixtures and synthetic release labels. They do not contact a provider, download an update or prove runtime continuity. The two settings states operate the actual settings page and saved setting in isolated profiles. Every capture uses an isolated home and private terminal server.

| State | 80 columns | 120 columns |
| --- | --- | --- |
| Launch offer | [View](offer-80c.png) | [View](offer-120c.png) |
| Background download | [View](downloading-80c.png) | [View](downloading-120c.png) |
| Installed | [View](ready-80c.png) | [View](ready-120c.png) |
| Installed while work continues | [View](ready-working-80c.png) | [View](ready-working-120c.png) |
| Manual update | [View](manual-80c.png) | [View](manual-120c.png) |
| Automatic installation off | [View](off-80c.png) | [View](off-120c.png) |
| Failed install | [View](failure-80c.png) | [View](failure-120c.png) |
| Workspace setting on | [View](settings-auto-on-80c.png) | [View](settings-auto-on-120c.png) |
| Workspace setting off | [View](settings-auto-off-80c.png) | [View](settings-auto-off-120c.png) |

The `.txt` and `.ansi` siblings preserve the terminal capture. Live model continuity is validated separately; screenshots alone do not establish that work survives installation.
