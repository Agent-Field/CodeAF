import { Button, Icon } from '../../../components/ui';
import type { SuggestionAction } from '../stale-model';
import '../home.css';
import './suggestion.css';

type Tone = 'field' | 'quiet';

type SuggestionProps = {
  layout: 'line' | 'card';
  /** The sparkles sentence. A line with no sentence draws nothing. */
  text?: string;
  /** Card only: "Suggested place". */
  kicker?: string;
  title?: string;
  detail?: string;
  actions: readonly (SuggestionAction & { tone?: Tone })[];
  /** Accessible name of the group. The line is "Suggestion"; the card is "Suggested place". */
  label?: string;
  /** True while a verb is in flight, so a second click cannot repeat it. */
  busy?: boolean;
  /** Runs one verb. The owner supplies how a refusal is shown; the default just runs it. */
  run?: (action: () => void | Promise<void>) => unknown;
  className?: string;
};

/**
 * One suggestion, as a sparkles line or the Suggested place card (Components).
 * It draws only the words and verbs it was given. An empty action list is not a
 * suggestion, so nothing is drawn.
 */
export function Suggestion({ layout, text, kicker, title, detail, actions, label, busy, run = action => action(), className }: SuggestionProps) {
  if (!actions.length) return null;
  const press = (action: SuggestionAction) => { void run(action.onSelect); };
  if (layout === 'card') {
    const name = title?.trim();
    if (!name) return null;
    return <div className={className ? `suggestion-card ${className}` : 'suggestion-card'} role="group" aria-label={label ?? kicker ?? 'Suggested place'} data-suggestion data-layout="card">
      <span className="suggestion-kicker"><Icon name="sparkles" size="micro"/>{kicker || 'Suggested place'}</span>
      <span className="suggestion-title">{name}</span>
      {detail?.trim() ? <span className="suggestion-detail">{detail}</span> : null}
      <span className="suggestion-actions">
        {actions.map(action => <Button key={action.id} variant={action.tone === 'quiet' ? 'ghost' : 'quiet'}
          className={action.tone === 'quiet' ? 'suggestion-action suggestion-action-quiet' : 'suggestion-action suggestion-action-field'}
          disabled={busy || action.disabled} onClick={() => press(action)}>{action.label}</Button>)}
      </span>
    </div>;
  }
  if (!text?.trim()) return null;
  return <div className={className ? `home-suggestion ${className}` : 'home-suggestion'} role="group" aria-label={label ?? 'Suggestion'} data-suggestion data-layout="line">
    <Icon name="sparkles" size="tiny"/>
    <span className="home-suggestion-text">{text}</span>
    {actions.map(action => <Button key={action.id} variant="ghost" className="home-suggestion-action" disabled={busy || action.disabled} onClick={() => press(action)}>{action.label}</Button>)}
  </div>;
}
