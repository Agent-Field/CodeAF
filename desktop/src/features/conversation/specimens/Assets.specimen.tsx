import { Markdown, Text } from '../../../components/ui';
import { AssetProvider, ChangesSummary, DeliverableView, FileChip, ImageFigure, ImageGrid, LinkChip, noEngineAssets, useAssetMarkdownHooks } from '../assets';
import type { FileRef } from '../types';
import { fakeAssets } from './assets-fixtures';

const answer = `The total lives in [cart/total.go](cart/total.go) and \`cart/total.go\` is covered by tests.
Details are in \`README.md\`; \`cart/missing.go\` does not exist, so it stays code.
Read the [Go blog post](https://go.dev/blog/errors) first, or just https://github.com/golang/go/issues/1.
Compare with ![the sunrise render](out/hero-sunrise.svg) and ![remote](https://example.com/a.png).`;

const changed: FileRef[] = [
  { path: 'cart/total.go', added: 31, removed: 4, source: 'edit' },
  { path: 'cart/total_test.go', added: 11, removed: 3, source: 'write' },
  { path: 'web/checkout/summary.tsx', source: 'edit' },
  { path: 'cart/removed.go', added: 0, removed: 9, source: 'edit' },
];

const grid = ['sunrise', 'dusk', 'fog'].map(name => ({ path: `out/hero-${name}.svg`, caption: `Hero, ${name} palette`, meta: 'generated · 640×480' }));

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="asset-specimen-row">
      <Text>{label}</Text>
      <div>{children}</div>
    </div>
  );
}

function AnswerWithAssets() {
  return <Markdown {...useAssetMarkdownHooks()}>{answer}</Markdown>;
}

export function AssetsSpecimen() {
  return (
    <div className="asset-specimen">
      <AssetProvider value={fakeAssets}>
        <Row label="File chips: exists, directory, missing, outside, with stat">
          <FileChip path="cart/total.go" source="edit" added={31} removed={4} />{' '}
          <FileChip path="docs/pricing.pdf" source="read" />{' '}
          <FileChip path="out/hero-dusk.svg" source="image" />{' '}
          <FileChip path="cart" source="task" />{' '}
          <FileChip path="cart/old/legacy_totals.go" source="read" />{' '}
          <FileChip path="/etc/hosts" source="read" />{' '}
          <FileChip path="internal/payments/providers/stripe/webhooks/handler_test.go" source="edit" added={8} />
        </Row>
        <Row label="Link chips and text links">
          <LinkChip href="https://github.com/golang/go/issues/1" title="Go issue 1" />{' '}
          <LinkChip href="https://go.dev/blog/errors" />{' '}
          <LinkChip href="https://docs.python.org/3/library/asyncio.html" title="asyncio, asynchronous I/O" />{' '}
          <LinkChip href="https://news.ycombinator.com/" />{' '}
          <LinkChip href="https://www.rust-lang.org/tools" />{' '}
          <LinkChip href="https://developer.mozilla.org/en-US/docs/Web" title="MDN Web Docs" />
        </Row>
        <Row label="Assistant reply (Markdown hooks)">
          <AnswerWithAssets />
        </Row>
        <Row label="Image figure">
          <ImageFigure path="out/hero-sunrise.svg" caption="A warm sunrise over low hills" meta="generated · 640×480" />
          <ImageFigure path="out/missing.svg" caption="Never written" meta="generated · 640×480" />
        </Row>
        <Row label="Image grid">
          <ImageGrid items={grid.slice(0, 2)} />
        </Row>
        <Row label="Changes summary">
          <ChangesSummary files={changed} renderDiff={path => <Text>Diff for {path} is drawn by the work lane.</Text>} />
        </Row>
        <Row label="Deliverables">
          <DeliverableView deliverable={{ kind: 'media', id: 'm1', path: 'out/welcome.wav', media: 'audio', caption: 'Spoken welcome message' }} />
        </Row>
      </AssetProvider>
      <Row label="No engine attached: plain text">
        <AssetProvider value={noEngineAssets}>
          <FileChip path="cart/total.go" source="edit" />
        </AssetProvider>
      </Row>
    </div>
  );
}
