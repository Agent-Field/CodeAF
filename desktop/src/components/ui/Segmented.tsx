import { useRef, type KeyboardEvent } from 'react';

export type SegmentedOption<T extends string> = { value: T; label: string };

/** A small set of exclusive choices: radio semantics, arrow keys move and select, one tab stop. */
export function Segmented<T extends string>({ label, options, value, onChange }: { label: string; options: SegmentedOption<T>[]; value: T; onChange: (value: T) => void }) {
 const group = useRef<HTMLDivElement>(null);
 const move = (event: KeyboardEvent, index: number) => {
  const step = event.key === 'ArrowRight' || event.key === 'ArrowDown' ? 1 : event.key === 'ArrowLeft' || event.key === 'ArrowUp' ? -1 : 0;
  if (!step) return;
  event.preventDefault();
  const next = (index + step + options.length) % options.length;
  onChange(options[next].value);
  group.current?.querySelectorAll<HTMLButtonElement>('[role="radio"]')[next]?.focus();
 };
 return <div ref={group} className="segmented" role="radiogroup" aria-label={label}>
  {options.map((option, index) => <button key={option.value} type="button" role="radio" aria-checked={option.value === value} tabIndex={option.value === value ? 0 : -1} className="segmented-option" onClick={() => onChange(option.value)} onKeyDown={event => move(event, index)}>{option.label}</button>)}
 </div>;
}
