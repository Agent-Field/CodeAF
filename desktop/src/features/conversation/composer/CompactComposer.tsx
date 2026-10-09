import type { ReactNode } from 'react';
import { Button, TextInput } from '../../../components/ui';
import type { EngineQuestion } from '../../chat/engine-client';
import { orderQuestions } from '../tray/layout';
import './compact-composer.css';

/** The one line a pane that needs you shows above its compact field: the question's head and "Review". */
export function SplitTray({ head, count = 1 }: { head: string; count?: number }) {
  return (
    <section className="split-tray" aria-label="Needs you">
      <span className="split-tray-dot" aria-hidden="true" />
      <span className="split-tray-head">{count > 1 ? `${count} need you: ${head}` : head}</span>
      <Button className="split-tray-review">Review</Button>
    </section>
  );
}

/** The tray line for what the engine is asking; nothing at all when nothing is standing. */
export function SplitTrayLine({ questions }: { questions: EngineQuestion[] }) {
  const standing = orderQuestions(questions);
  return standing.length ? <SplitTray head={standing[0].head} count={standing.length} /> : null;
}

/**
 * Shell 3b: an unfocused pane of a split keeps a 36px field in place of the full composer, and it never hides, so
 * nothing shifts under the cursor. It is a real field: clicking it or typing in it focuses the pane (the pane
 * grid listens for that), which swaps in the full composer with the same draft.
 */
export function CompactComposer({ label, draft, onDraft, children }: { label: string; draft: string; onDraft: (draft: string) => void; children?: ReactNode }) {
  const text = `Reply to ${label}`;
  return (
    <div className="compact-composer">
      {children}
      <TextInput className="compact-composer-field" aria-label={text} placeholder={text} value={draft} onChange={(event) => onDraft(event.target.value)} />
    </div>
  );
}
