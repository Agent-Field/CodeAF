import { Button } from '../../../components/ui';
import type { EngineQuestion } from '../../chat/engine-client';
import { orderQuestions } from './layout';
import './pane-mini-tray.css';

/** An unfocused split pane keeps its standing question reachable without opening the full tray. */
export function PaneMiniTray({ questions, onReview }: { questions: EngineQuestion[]; onReview?: () => void }) {
  const question = orderQuestions(questions)[0];
  if (!question) return null;

  // Review runs before native focus lets the pane replace this card with the full tray.
  return (
    <section className="split-tray pane-mini-tray" aria-label="Needs you">
      <span className="pane-mini-tray-dot" aria-hidden="true" />
      <span className="pane-mini-tray-title">{question.head}</span>
      <Button variant="quiet" className="pane-mini-tray-review" data-pane-review onPointerDown={onReview} onFocus={onReview} onClick={onReview}>Review</Button>
    </section>
  );
}
