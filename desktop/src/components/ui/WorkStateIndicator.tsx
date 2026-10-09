import { Icon, type IconName } from './Icon';
export type WorkIndicatorPhase = 'streaming' | 'working' | 'waiting' | 'completed' | 'stopped' | 'failed' | 'staged';
const glyphs: Record<WorkIndicatorPhase, IconName> = { streaming: 'activity', working: 'activity', waiting: 'more', completed: 'check', stopped: 'close', failed: 'activity', staged: 'tab' };
/** A still state marker; activity is announced in text, never by color alone. */
export function WorkStateIndicator({ phase, label }: { phase: WorkIndicatorPhase; label: string }) {
 return <span className="work-state-indicator" data-phase={phase} role="img" aria-label={label}><Icon name={glyphs[phase]} size="xs"/></span>;
}
