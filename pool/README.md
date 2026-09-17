# model-pool

This orphan branch holds the Model Pool's published index document and
its detached signature, and nothing derived from them.

- `pool/index.json` — the signed index document.
- `pool/index.json.sig` — the detached Ed25519 signature (standard
  base64) over the exact bytes of `pool/index.json`.

Both files are copies of what the relay serves; there is no count, no
leaderboard and no extracted statistic here. Verify them with:

```
node relay/verify.mjs pool/index.json pool/index.json.sig <public-key-base64>
```
