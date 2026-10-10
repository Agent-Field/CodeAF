# Engine wire goldens

These files are generated and checked by
`TestTypedDecisionClientWireFixtures` in
`internal/desktopbridge/typed_clients_contract_test.go`.

Regenerate from the repository root:

```sh
UPDATE_DECISIONS_FIXTURES=1 make test-focus PKGS=./internal/desktopbridge RUN='^TestTypedDecisionClientWireFixtures$'
```

Knows, plan, refusal and empty council fixtures are actual HTTP responses.
The nonempty council fixture uses the same Go wire adapter as both council
routes, without a temporary transcript path. Decisions success fixtures are
absent because the five reserved routes still answer 501.
