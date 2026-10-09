import { Markdown } from '../../../components/ui';

const sample = `# Release notes

A **quiet** summary with \`inline code\`, a [source link](https://example.com/reference) and _emphasis_.

## What changed

- Faster startup
  - Lazy plugin loading
  - Smaller bundle
- Clearer errors

1. Install
2. Run the check

> Measure first, then change the code.

\`\`\`ts
export function add(a: number, b: number): number {
  return a + b;
}
\`\`\`

| Item | Result |
| --- | --- |
| Engine | Shared |
| Renderer | Single |

---

- [x] Verified
- [ ] Pending
`;

export function MarkdownSpecimen() {
 return <Markdown>{sample}</Markdown>;
}
