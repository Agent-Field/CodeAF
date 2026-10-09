import { useState } from 'react';
import { EarlierRow, SummaryDivider } from '../EarlierTurns';
import { TurnViewV2 } from '../blocks/TurnViewV2';
import { isFolded } from '../folding';
import type { TurnV2 } from '../types';

const turn = (id: string, user: string, digest: string): TurnV2 => ({ id, user, attachments: [], steer: [], blocks: [], state: 'done', digest });

const turns = [
  turn('h:0', 'Why does CI fail on configs?', 'The shared fixture has a trailing comma.'),
  turn('h:1', 'Fix it in the lexer', 'Done. Accepted outside strict mode.'),
  turn('h:2', 'Does JSON5 handle this?', 'Yes, but it also allows comments.'),
];

/** Long history: an "earlier turns" group, the divider, then folded turns. */
export function FoldingSpecimen() {
  const [open, setOpen] = useState(false);
  const [chosen, setChosen] = useState<Record<string, boolean>>({});
  return (
    <div className="conversation-column">
      <EarlierRow count={18} open={open} onToggle={() => setOpen(!open)} />
      <SummaryDivider />
      {turns.map((item, index) => {
        const folded = isFolded(item, index, turns.length + 1, chosen);
        return (
          <TurnViewV2
            key={item.id}
            turn={item}
            folded={folded}
            onToggleFold={() => setChosen({ ...chosen, [item.id]: !folded })}
            renderBlock={() => null}
            renderAttachment={() => null}
          />
        );
      })}
    </div>
  );
}
