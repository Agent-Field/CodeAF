# Wave 2 progress — integrate candidate (not verified)

**Integrate SHA:** `f2de6878de6ae54d60ff4f98d97cb9b8f766e416`  
**Branch:** `feat/collaborative-workspace-0918` (pushed; no merge to `dev`)  
**Updated:** 2026-09-19T04:07Z

Wave 1 remains the only owner-verified binary. Do not treat this SHA as `ready.json`.

## Wave 1 owner launch (copy-paste)

```
ssh -t spark '/home/santosh/src/codeaf-workspace-0918-control/releases/wave-1/4b3b407a676efca3282b9834f05ea956c98a89cd/launch.sh'
```

## Wave 2 integrate

Merged onto the feature branch: bind `7e110d49`, session `3c4c6e4e`, tui `c1433bab`, tick `644f326a`, proof `e51e50e0`.

Focused tests passed on this SHA: `./internal/workspace`, `./internal/wsapi`, `./internal/wsdiscover`, `./internal/embed`, `./internal/e2e`, `./internal/config`, session Organize/Guidance/Hybrid, tui3 Folder, cmd/codeaf Folder|Organize, `./internal/manual`.

`t-w2-integrate` is done. `t-wave2` stays unclaimed until `releases/wave-2/ready.json`.

Running: independent reviews (storage + J09–J18) and Spark `make pr-ready BASE=610a32ba` with `PLANDB_DB` unset. Live J09–J18 follows affected.

The standing-tick organizer runner is still unbound (`v3Organizer` returns nil): pending `observe_and_organize` jobs stay pending rather than inventing membership. Reviews/live will decide whether that must be wired before ready.json.
