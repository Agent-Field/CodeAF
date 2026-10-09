import { Icon } from '../../../components/ui';
import type { TurnItem, TurnV2 } from '../types';
import './steer.css';

type SteerEntry = TurnV2['steer'][number];

/** A message typed into a running turn: a small elbowed line on the right, not a bubble.
 * Pending, it leads with where it will land; consumed, only the elbow and the words remain. */
export function Steer({ entry }: { entry: Pick<SteerEntry, 'text' | 'landing' | 'consumed'> }) {
  const pending = !entry.consumed;
  return (
    <div className="steer" data-consumed={entry.consumed}>
      {pending && entry.landing && <span className="steer-landing">{entry.landing}</span>}
      <span className="steer-elbow" aria-hidden="true">
        <Icon name="cornerDownRight" size="xs" />
      </span>
      <span className="steer-text">{entry.text}</span>
    </div>
  );
}

/** Steers that no work block holds (typed before any work began) stand on their own under the message. */
export function Steers({ steer, inWork }: { steer: TurnV2['steer']; inWork: TurnItem[] }) {
  const held = new Set(inWork.map((item) => item.id));
  const loose = steer.filter((entry) => !held.has(entry.id));
  if (loose.length === 0) return null;
  return (
    <div className="steers">
      {loose.map((entry) => (
        <Steer key={entry.id} entry={entry} />
      ))}
    </div>
  );
}
