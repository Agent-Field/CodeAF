import { useState } from 'react';
import type { Turn, TurnItem } from '../types';
import { TurnView } from '../TurnView';

const markdown = `Here is the plan.

- Read the files
- Count the words
- Write a summary

\`\`\`ts
const total = words.length;
\`\`\`

| File | Words |
| --- | --- |
| a.md | 120 |
| b.md | 340 |

A very long address: https://example.com/a/really/long/path/that/keeps/going/and/going/without/any/break/at/all`;

const longUser = Array.from({ length: 14 }, (_, i) => `Line ${i + 1} of a long pasted message.`).join('\n');

const turns: Turn[] = [
  {
    id: 'specimen:0',
    user: 'List the files in this folder',
    items: [{ kind: 'text', id: 'a', text: 'There are four files.', streaming: false }],
    state: 'done',
    digest: 'There are four files.',
  },
  {
    id: 'specimen:1',
    user: 'Make a plan',
    items: [
      { kind: 'text', id: 'b', text: markdown, streaming: false },
      { kind: 'note', id: 'c', text: 'Conversation compacted' },
      { kind: 'error', id: 'd', text: 'The model stopped responding.' },
    ],
    state: 'failed',
    digest: 'Here is the plan.',
  },
  { id: 'specimen:2', user: longUser, items: [], state: 'working', digest: '' },
];

const stubItem = (item: TurnItem) => <p>{item.kind}</p>;

export function TurnViewSpecimen() {
  const [folded, setFolded] = useState<Record<string, boolean>>({ 'specimen:0': true });
  return (
    <div className="conversation-column">
      {turns.map((turn) => (
        <TurnView
          key={turn.id}
          turn={turn}
          folded={!!folded[turn.id]}
          onToggleFold={() => setFolded({ ...folded, [turn.id]: !folded[turn.id] })}
          renderItem={stubItem}
        />
      ))}
    </div>
  );
}
