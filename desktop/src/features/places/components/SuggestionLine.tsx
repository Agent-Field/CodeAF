import { Button, Icon } from '../../../components/ui';
import type { SuggestionModel } from '../stale-model';
import '../home.css';

type SuggestionLineProps = SuggestionModel & {
  /** Runs one verb. The owner supplies how a refusal is shown (the Home banner, the chooser's alert); the default just runs it. */
  run?: (action: () => void | Promise<void>) => unknown;
  /** True while a verb is in flight: every button holds still so a second click cannot repeat it. */
  busy?: boolean;
  className?: string;
};

/** The one quiet suggestion line (design 6d and 8c): a sparkle, a sentence in muted words, then plain verbs. It is the same line the Home
 * draws for the engine's offers; a surface that has no suggestion draws nothing, never an empty line. */
export function SuggestionLine({ text, actions, run = action => action(), busy, className }: SuggestionLineProps) {
  if (!actions.length) return null;
  return <div className={className ? `home-suggestion ${className}` : 'home-suggestion'} role="group" aria-label="Suggestion" data-suggestion>
    <Icon name="sparkles" size="micro"/>
    <span className="home-suggestion-text">{text}</span>
    {actions.map(action => <Button key={action.id} variant="ghost" className="home-suggestion-action" disabled={busy || action.disabled}
      onClick={() => void run(action.onSelect)}>{action.label}</Button>)}
  </div>;
}
