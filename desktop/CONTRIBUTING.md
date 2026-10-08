# Contributing to codeaf desktop

Read [the repository contract](AGENTS.md) and [the visual standard](docs/DESIGN.md) before implementation. Humans and coding agents use the same rules.

1. Build from shared UI primitives and the semantic Icon registry.
2. Adjust a design value once in `src/design/tokens.json`, then run `npm run design:generate`. Extend central components rather than copying CSS into a screen.
3. Update the Design system screen and standard for any intentional visual change. Regenerate native icons with `npm run brand:icons` if brand geometry/colors changed.
4. Run `npm run check`. For native changes validate both Linux and macOS. Include concise validation evidence and honest limits in review.
5. Keep review images/video out of source commits; attach to the PR. Do not change the style gate solely to permit an ad hoc feature.

## Commands

| Command | Purpose |
| --- | --- |
| `npm run design:generate` | Generate CSS, HTML metadata, favicon, native source and window geometry |
| `npm run brand:icons` | Generate native icon formats and fingerprint manifest |
| `npm run design:check` | Enforce shared design boundaries and generated outputs |
| `npm run check` | Required build, policy and contract verification |

GitHub CI runs the same checks. Configure this workflow as a required branch check when repository administration/billing permits; a committed workflow alone does not enforce branch protection.

Run commands from desktop/. Use [the monorepo workflow](../docs/DESKTOP.md) to merge upstream/dev and validate before pushing the integration branch. The root contributor contract also applies.
