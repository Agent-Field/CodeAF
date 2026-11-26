# First-party app releases

Each directory under `definitions/` is an ordinary app definition. Changing
the definition requires a new release archive at the matching path under
`firstparty/`. `TestShippedDefinitionsMatchSignedReleases` compares every
source file with its signed release.

Sign a checked definition with the release key on a trusted machine:

```sh
go run ./internal/apps/apppackage/cmd/seal DEFINITION_DIR VERSION \
  < RELEASE_SIGNING_KEY.pem > NEW_ARCHIVE.codeaf-app
```

Keep the signing key outside the repository. The verifier distributes only
the public key. The archive signature authenticates the publisher and covers
the manifest and definition. Desktop editor declarations still compile to
fixed renderers with scoped records and registered Actions.
