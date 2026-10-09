import { useEffect } from 'react';
import { Button } from '../../../components/ui';
import { needsWords, optionLabel, permissionRoles, visibleOptions, type Option, type Question } from './form';
import './answer-forms.css';

type FormProps = {
  question: Question;
  locked: boolean;
  /** Only one question is waiting: no pager, so Enter chooses the primary. */
  single?: boolean;
  onPress: (option: Option) => void;
};

const TYPING = 'input, textarea, select, button, a, [contenteditable="true"]';

/** Enter presses the primary, unless the person is typing or on another control. */
function useEnterChooses(option: Option | undefined, enabled: boolean, onPress: (option: Option) => void) {
  useEffect(() => {
    if (!enabled || !option) return;
    function onKey(event: KeyboardEvent) {
      const target = event.target as Element | null;
      if (event.key !== 'Enter' || event.defaultPrevented || event.isComposing) return;
      if (event.metaKey || event.ctrlKey || event.altKey || event.shiftKey) return;
      if (target?.closest?.(TYPING)) return;
      event.preventDefault();
      onPress(option!);
    }
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [enabled, option, onPress]);
}

/** The "and say why" hint after Deny (Components, Decision tray): it opens the optional reason field. */
export type WhyToggle = { open: boolean; toggle: () => void };

type AnswerProps = { kind: 'primary' | 'field' | 'danger' | 'ghost'; locked: boolean; onClick: () => void; children: string };

/** Fill, hover and press come from the shared Button variants. */
const ANSWER_VARIANT = { primary: 'primary', field: 'quiet', danger: 'danger', ghost: 'ghost' } as const;

function Answer({ kind, locked, onClick, children }: AnswerProps) {
  return (
    <Button variant={ANSWER_VARIANT[kind]} className={`answer answer-${kind}`} disabled={locked} onClick={onClick}>
      {children}
    </Button>
  );
}

/** Allow once is the loud answer; Deny sits beside it; "Always allow…" stays quiet. */
export function PermissionAnswers({ question, locked, single, why, onPress }: FormProps & { why: WhyToggle }) {
  const { allow, deny, always } = permissionRoles(question);
  useEnterChooses(allow, Boolean(single) && !locked, onPress);
  const answer = (option: Option | undefined, kind: AnswerProps['kind'], label?: string) =>
    option && (
      <Answer kind={kind} locked={locked} onClick={() => onPress(option)}>
        {label ?? optionLabel(question, option)}
      </Answer>
    );
  return (
    <div className="answer-row">
      {answer(allow, 'primary')}
      {answer(deny, 'field')}
      {answer(always, 'ghost', 'Always allow…')}
      {deny && <Button variant="ghost" className="answer-why" aria-expanded={why.open} disabled={locked} onClick={why.toggle}>and say why</Button>}
    </div>
  );
}

/** An answer that needs words ("Tell it…") is quiet; the safe one is a field; the rest is the danger. */
function irreversibleKind(question: Question, option: Option): AnswerProps['kind'] {
  if (needsWords(question, option)) return 'ghost';
  return option.safe ? 'field' : 'danger';
}

const KIND_ORDER = ['field', 'danger', 'ghost'];

/** Cannot be undone: no Always, no clock, and the destructive answer is never the primary. */
export function IrreversibleAnswers({ question, locked, onPress }: FormProps) {
  const rows = visibleOptions(question).map((option) => ({ option, kind: irreversibleKind(question, option) }));
  rows.sort((a, b) => KIND_ORDER.indexOf(a.kind) - KIND_ORDER.indexOf(b.kind));
  return (
    <div className="answer-row">
      {rows.map(({ option, kind }) => (
        <Answer key={option.key} kind={kind} locked={locked} onClick={() => onPress(option)}>
          {optionLabel(question, option)}
        </Answer>
      ))}
    </div>
  );
}
