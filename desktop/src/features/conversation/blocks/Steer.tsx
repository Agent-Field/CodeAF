import type { TurnV2 } from '../types';

type SteerEntry = TurnV2['steer'][number];

/** A message typed into a running turn: an elbow line, with the landing clause until it is consumed. */
export function Steer({ entry }: { entry: SteerEntry }) {
  return (
    <p className="steer" data-consumed={entry.consumed}>
      <span className="steer-elbow" aria-hidden="true">
        {'↳'}
      </span>
      <span className="steer-text">{entry.text}</span>
      {entry.landing && <span className="steer-landing">{entry.landing}</span>}
    </p>
  );
}

export function Steers({ steer }: { steer: TurnV2['steer'] }) {
  if (steer.length === 0) return null;
  return (
    <div className="steers">
      {steer.map((entry) => (
        <Steer key={entry.id} entry={entry} />
      ))}
    </div>
  );
}
