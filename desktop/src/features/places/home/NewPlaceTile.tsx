import { useLayoutEffect, useRef, useState } from 'react';
import { PlaceTile } from '../components/PlaceTile';
import type { TintName } from '../components/PlaceSwatch';
import { nameProblem, type HomeChild } from '../home-model';

// Places 8f orders the inline palette independently of the shared menu palette.
const choices: readonly TintName[] = ['tide', 'rose', 'sage', 'sand', 'iris'];
type Draft = { name: string; tint: TintName; parent?: string };
type Props = {
  label?: string;
  places: readonly HomeChild[];
  siblings: readonly string[];
  parentId?: string;
  disabled?: boolean;
  onCreate: (draft: Draft) => Promise<boolean>;
};

/** The draft stays local until the engine accepts it, so a failed write never discards the person's name. */
export function NewPlaceTile({ label, places, siblings, parentId, disabled, onCreate }: Props) {
  const [draft, setDraft] = useState<Draft>();
  const button = useRef<HTMLButtonElement>(null);
  const restoreFocus = useRef(false);
  const submitting = useRef(false);
  useLayoutEffect(() => {
    if (!draft && restoreFocus.current) {
      restoreFocus.current = false;
      button.current?.focus();
    }
  }, [draft]);
  const start = () => {
    const uses = (tint: TintName) => places.filter(place => !place.archived && !place.path?.length && place.tint === tint).length;
    const tint = choices.reduce((best, tint) => uses(tint) < uses(best) ? tint : best);
    setDraft({ name: '', tint, parent: parentId });
  };
  const problem = draft ? nameProblem(draft.name, siblings) : undefined;
  const submit = async () => {
    if (!draft || problem || disabled || submitting.current) return;
    submitting.current = true;
    try {
      if (await onCreate({ ...draft, name: draft.name.trim() })) setDraft(undefined);
    } finally { submitting.current = false; }
  };
  return draft
    ? <PlaceTile mode="creating" name={draft.name} tint={draft.tint} choices={choices} disabled={disabled}
        invalid={!!problem && !!draft.name.trim()} hint={draft.name.trim() && problem ? problem : '↵ create · Esc cancel'}
        onNameChange={name => setDraft(previous => previous && { ...previous, name })}
        onTintChange={tint => setDraft(previous => previous && { ...previous, tint })}
        onSubmit={() => void submit()} onCancel={() => { restoreFocus.current = true; setDraft(undefined); }}/>
    : <PlaceTile mode="new" label={label} buttonRef={button} disabled={disabled} onCreate={start}/>;
}
