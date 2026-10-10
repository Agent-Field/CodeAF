import { Button, IconButton } from '../../../components/ui';
import type { Question } from './form';

/** What the amber head says: the blocked reply first, then the asking task, else a plain nudge. */
export function crumbOf(question: Question | undefined): { title: string; kind: string } {
  if (question?.learning) {
    const { place, agreed, of } = question.learning;
    return { title: place ? `${place} is learning` : 'Learning', kind: of ? `· ${agreed} of ${of} agreed` : '' };
  }
  if (question?.blocking?.turn) return { title: 'The reply is waiting on this', kind: '' };
  const task = question?.asker?.kind === 'task' ? question.asker.name : '';
  return task ? { title: task, kind: '· task' } : { title: 'Waiting on you', kind: '' };
}

type HeaderProps = {
  question: Question | undefined;
  /** 1-based place of the shown page; the pager draws only when there is more than one. */
  index: number;
  count: number;
  laterCount: number;
  laterOpen: boolean;
  onPage: (step: -1 | 1) => void;
  onToggleLater: () => void;
};

/** The pinned head of the card: amber mark, crumb, and "2 of 5" with its two arrows.
 * The count's accessible name is "Question N of M", the sentence a screen reader hears. */
export function TrayHeader({ question, index, count, laterCount, laterOpen, onPage, onToggleLater }: HeaderProps) {
  const { title, kind } = crumbOf(question);
  return (
    <header className="tray-header">
      <span className="tray-mark" aria-hidden="true" />
      <span className="tray-crumb">{title}</span>
      {kind && <span className="tray-crumb-kind">{kind}</span>}
      <div className="tray-pager">
        {laterCount > 0 && (
          <Button className="tray-later" aria-expanded={laterOpen} onClick={onToggleLater}>
            {`${laterCount} later`}
          </Button>
        )}
        {count > 1 && (
          <>
            <span className="tray-count" role="status" aria-atomic="true" aria-label={`Question ${index} of ${count}`}>{`${index} of ${count}`}</span>
            <IconButton className="tray-arrow" label="Previous question" icon="chevronLeft" iconSize="xs" disabled={index <= 1} onClick={() => onPage(-1)} />
            <IconButton className="tray-arrow" label="Next question" icon="chevronRight" iconSize="xs" disabled={index >= count} onClick={() => onPage(1)} />
          </>
        )}
      </div>
    </header>
  );
}
