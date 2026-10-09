import './turnViewV2.specimen.css';
import { useState } from 'react';
import { Icon } from '../../../components/ui';
import { QueuedRows, type QueuedItem } from '../blocks/QueuedRows';
import { TurnFooter } from '../blocks/TurnFooter';
import { TurnViewV2 } from '../blocks/TurnViewV2';
import type { FileRef, TurnBlock, TurnV2 } from '../types';

const work = (id: string, steps: number): TurnBlock => ({
  kind: 'work',
  id,
  steps: [],
  notes: [],
  live: false,
  summary: { seconds: 42, steps, calls: steps * 2, failed: 0 },
});

const planAnswer = `The login test was failing because the clock was never faked.

- Pinned the clock in \`auth_test.go\`
- Re-ran the suite: **all 41 pass**

Nothing else needed to change.`;

const file = (path: string): FileRef => ({ path, source: 'attachment' });

const turns: TurnV2[] = [
  {
    id: 'v2:0',
    user: 'Why is the login test flaky? Please fix it.',
    attachments: [file('shots/failure-1.png'), file('shots/failure-2.png'), file('logs/ci-run.txt')],
    steer: [],
    blocks: [
      { kind: 'update', id: 'u1', text: 'Found it: the test reads the real clock. I am pinning it now.', cut: false, streaming: false },
      work('w1', 4),
      { kind: 'answer', id: 'a1', text: planAnswer, streaming: false },
    ],
    state: 'done',
    digest: 'The login test was failing because the clock was never faked.',
  },
  {
    id: 'v2:1',
    user: 'Now check the signup flow too',
    attachments: [],
    steer: [],
    blocks: [
      { kind: 'update', id: 'u2', text: 'The signup flow shares the same helper, so I will read it first and then', cut: true, streaming: false },
    ],
    state: 'stopped',
    digest: '',
  },
  {
    id: 'v2:2',
    user: 'Add retries to the uploader',
    attachments: [],
    steer: [
      { id: 's1', text: 'also check the tests', landing: 'waiting for the running step', consumed: false },
      { id: 's2', text: 'and keep the old timeout', landing: 'stopped the reply here', consumed: true },
    ],
    blocks: [work('w2', 2)],
    state: 'working',
    digest: '',
  },
  {
    id: 'v2:3',
    user: 'Rename the helper everywhere',
    attachments: [],
    steer: [],
    blocks: [{ kind: 'error', id: 'e1', text: 'The model stopped responding.' }],
    state: 'failed',
    digest: '',
  },
  {
    id: 'v2:4',
    user: 'Summarise yesterday’s release notes',
    attachments: [],
    steer: [],
    blocks: [],
    state: 'failed',
    digest: '',
  },
];

const queued: QueuedItem[] = [
  { id: 'q1', text: 'After that, update the changelog' },
  { id: 'q3', text: 'And bump the patch version' },
  { id: 'q4', text: 'Tag the release as v2.4.1' },
  { id: 'q2', text: 'A much longer queued message that should be cut to a single line instead of wrapping under the remove button' },
];

const renderBlock = (block: TurnBlock) =>
  block.kind === 'work' ? (
    <p className="specimen-work">{`Worked ${block.summary.seconds}s · ${block.summary.steps} steps`}</p>
  ) : null;

const renderAttachment = (f: FileRef) =>
  /\.png$/.test(f.path) ? (
    <span className="specimen-thumb" role="img" aria-label={f.path} />
  ) : (
    <span className="specimen-chip">
      <Icon name="file" size="sm" />
      {f.path}
    </span>
  );

function moved(items: QueuedItem[], id: string, to: number): QueuedItem[] {
  const item = items.find((candidate) => candidate.id === id);
  if (!item) return items;
  const rest = items.filter((candidate) => candidate.id !== id);
  return [...rest.slice(0, to), item, ...rest.slice(to)];
}

export function TurnViewV2Specimen() {
  const [folded, setFolded] = useState<Record<string, boolean>>({ 'v2:1': true });
  const [items, setItems] = useState(queued);
  return (
    <div className="conversation-column">
      {turns.map((turn) => (
        <TurnViewV2
          key={turn.id}
          turn={turn}
          folded={!!folded[turn.id]}
          onToggleFold={() => setFolded({ ...folded, [turn.id]: !folded[turn.id] })}
          renderBlock={renderBlock}
          renderAttachment={renderAttachment}
          onRetry={() => undefined}
          onOpenWork={() => undefined}
        />
      ))}
      <TurnViewV2
        turn={{ ...turns[1], id: 'v2:cut' }}
        folded={false}
        onToggleFold={() => undefined}
        renderBlock={renderBlock}
        renderAttachment={renderAttachment}
      />
      <TurnFooter state="done" retrying />
      <QueuedRows
        items={items}
        onRemove={(id) => setItems(items.filter((item) => item.id !== id))}
        onEdit={(id, text) => setItems(items.map((item) => (item.id === id ? { ...item, text } : item)))}
        onMove={(id, to) => setItems(moved(items, id, to))}
      />
    </div>
  );
}
