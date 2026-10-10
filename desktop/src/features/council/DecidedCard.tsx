import { Icon } from '../../components/ui';
import './council.css';

export type CouncilDecision = {
  /** The sentence the places settled on, without the "Decided:" lead. */
  decision: string;
  /** Names of the places that agreed. Omitted when the engine did not record it. */
  agreedBy?: string[];
  turns?: number;
  /** Total spend in dollars. Unknown renders as nothing, never $0.00. */
  costUsd?: number;
  /** Places the decision was filed into. */
  filedIn?: string[];
};

/** "Marketing and Config parser", "A, B and C": the way the design writes a list of places. */
function joinNames(names: string[]): string {
  return names.length < 2 ? names.join('') : `${names.slice(0, -1).join(', ')} and ${names[names.length - 1]}`;
}

/** The facts line under the decision. Each segment is dropped when its fact is unknown (emptiness law). */
export function decidedFacts({ agreedBy, turns, costUsd, filedIn }: CouncilDecision): string {
  const segments = [
    agreedBy && agreedBy.length > 1 ? 'Agreed by both places' : undefined,
    turns ? `${turns} ${turns === 1 ? 'turn' : 'turns'}` : undefined,
    costUsd ? `$${costUsd.toFixed(2)}` : undefined,
    filedIn?.length ? `filed in ${joinNames(filedIn)}` : undefined,
  ];
  return segments.filter(Boolean).join(' · ');
}

/** The receipt a council leaves when it settles: what was decided, then who agreed, what it cost and where it was filed. */
export function DecidedCard(props: { decision: CouncilDecision }) {
  const facts = decidedFacts(props.decision);
  return (
    <div className="council-decided">
      <span className="council-decided-title"><Icon name="check" size="tiny"/>Decided: {props.decision.decision}</span>
      {facts && <span className="council-decided-facts">{facts}</span>}
    </div>
  );
}

/** Says what the person may do here, and what writing costs the council. */
export function CouncilFooter() {
  return <span className="council-footer">You can read and steer a council like any chat. Writing here pauses it until you send.</span>;
}
