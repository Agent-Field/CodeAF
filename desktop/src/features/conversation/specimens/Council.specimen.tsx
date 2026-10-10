import { CouncilFooter, DecidedCard } from '../../council/DecidedCard';
import { PlaceMessage } from '../../council/PlaceMessage';

/** The council from Decisions 12d: two places settling a question, then the receipt. Fixture names only. */
export function CouncilSpecimen() {
  return (
    <div className="council-stack">
      <PlaceMessage speaker={{ name: 'Marketing', tint: 'rose' }} text={'Then I\'ll write "in v2.4.1 and later, outside strict mode". Does that hold?'}/>
      <PlaceMessage speaker={{ name: 'Config parser', tint: 'tide' }} text="Yes. Both drafts I checked are accurate."/>
      <DecidedCard decision={{ decision: 'promise it for v2.4.1+, outside strict mode', agreedBy: ['Marketing', 'Config parser'], turns: 2, costUsd: 0.01, filedIn: ['Marketing', 'Config parser'] }}/>
      <CouncilFooter/>
    </div>
  );
}
