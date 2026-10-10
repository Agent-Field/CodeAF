import { useCallback, useEffect, useRef, useState, type KeyboardEvent, type RefObject } from 'react';
import design from '../../design/tokens.json';
import { Button } from './Button';
import '../../styles/tokens.css';
import '../../styles/ui.css';
import './filter-tabs.css';

export type FilterTabOption<T extends string> = { value: T; label: string; disabled?: boolean };

const narrowQuery = `(max-width: ${design.breakpoints.small}px)`;

/**
 * A fade belongs only on an edge that still has pills past it, and only while the
 * window is at the small breakpoint, which is when this row scrolls.
 */
function useFilterTabEdges(ref: RefObject<HTMLDivElement | null>) {
 const [edges, setEdges] = useState({ start: false, end: false });
 const measure = useCallback(() => {
  const element = ref.current;
  if (!element) return;
  const narrow = window.matchMedia(narrowQuery).matches;
  const start = narrow && element.scrollLeft > 1;
  const end = narrow && element.scrollLeft + element.clientWidth < element.scrollWidth - 1;
  setEdges(current => current.start === start && current.end === end ? current : { start, end });
 }, [ref]);
 useEffect(() => {
  measure();
  const element = ref.current;
  if (!element || typeof ResizeObserver === 'undefined') return;
  const watch = new ResizeObserver(measure);
  watch.observe(element);
  const query = window.matchMedia(narrowQuery);
  query.addEventListener('change', measure);
  window.addEventListener('resize', measure);
  return () => {
   watch.disconnect();
   query.removeEventListener('change', measure);
   window.removeEventListener('resize', measure);
  };
 }, [measure, ref]);
 return { edges, measure };
}

/** Keep a pill the keyboard just chose in the readable part of the row. The page does not move, and a narrow row leaves the pill clear of the edge mask. */
function reveal(group: HTMLElement, button: HTMLElement) {
 const inset = window.matchMedia(narrowQuery).matches ? Number.parseFloat(getComputedStyle(group).getPropertyValue('--filter-tab-mask')) || 0 : 0;
 const groupRect = group.getBoundingClientRect();
 const buttonRect = button.getBoundingClientRect();
 const left = groupRect.left + (group.scrollLeft > 0 ? inset : 0);
 const right = groupRect.right - inset;
 if (buttonRect.left < left) group.scrollLeft -= left - buttonRect.left;
 else if (buttonRect.right > right) group.scrollLeft += buttonRect.right - right;
}

/**
 * Exclusive filter pills. One tab stop, and arrows, Home and End move and select,
 * the same keyboard model as Segmented. A disabled pill is skipped.
 */
export function FilterTabs<T extends string>({ label, options, value, onChange, disabled = false }: { label: string; options: FilterTabOption<T>[]; value: T; onChange: (value: T) => void; disabled?: boolean }) {
 const group = useRef<HTMLDivElement>(null);
 const { edges, measure } = useFilterTabEdges(group);
 const enabled = (index: number) => !disabled && !options[index]?.disabled;
 const choose = (index: number) => {
  if (!enabled(index)) return;
  onChange(options[index].value);
  const button = group.current?.querySelectorAll<HTMLButtonElement>('[role="radio"]')[index];
  button?.focus();
  if (button && group.current) reveal(group.current, button);
 };
 const neighbor = (from: number, direction: 1 | -1) => {
  for (let step = 1; step <= options.length; step++) {
   const index = (from + direction * step + options.length) % options.length;
   if (enabled(index)) return index;
  }
  return -1;
 };
 const onKeyDown = (event: KeyboardEvent, index: number) => {
  const direction = event.key === 'ArrowRight' || event.key === 'ArrowDown' ? 1 : event.key === 'ArrowLeft' || event.key === 'ArrowUp' ? -1 : 0;
  let next = -1;
  if (direction) next = neighbor(index, direction);
  else if (event.key === 'Home') next = options.findIndex((_, item) => enabled(item));
  else if (event.key === 'End') {
   next = -1;
   for (let item = options.length - 1; item >= 0; item--) if (enabled(item)) { next = item; break; }
  } else return;
  event.preventDefault();
  if (next >= 0) choose(next);
 };
 const selected = options.findIndex((option, index) => option.value === value && enabled(index));
 const tabStop = selected >= 0 ? selected : options.findIndex((_, index) => enabled(index));
 return <div ref={group} className="filter-tabs" role="radiogroup" aria-label={label} aria-disabled={disabled || undefined} data-fade-start={edges.start ? '' : undefined} data-fade-end={edges.end ? '' : undefined} onScroll={measure}>
  {options.map((option, index) => <button key={option.value} type="button" role="radio" aria-checked={option.value === value} tabIndex={index === tabStop ? 0 : -1} className="filter-tab-button" disabled={disabled || option.disabled} onClick={() => choose(index)} onKeyDown={event => onKeyDown(event, index)}>{option.label}</button>)}
 </div>;
}

const specimenOptions: FilterTabOption<string>[] = [
 { value: 'all', label: 'All' },
 { value: 'decisions', label: 'Decisions' },
 { value: 'files', label: 'Files' },
 { value: 'tasks', label: 'Tasks' },
 { value: 'archived', label: 'Archived', disabled: true },
 { value: 'open', label: 'Open' },
 { value: 'places', label: 'Places' },
 { value: 'history', label: 'History' },
 { value: 'terminal', label: 'Terminal' },
];

/** Measured on its own page so the shell's other controls are not in the axe run. The product never navigates here. */
export function FilterTabsSpecimen() {
 const [value, setValue] = useState('all');
 return <div className="filter-tabs-specimen">
  <FilterTabs label="Filter tabs" options={specimenOptions} value={value} onChange={setValue}/>
  <Button variant="quiet">After filter tabs</Button>
  <FilterTabs label="Disabled filter tabs" options={[{ value: 'all', label: 'All' }, { value: 'files', label: 'Files' }]} value="all" onChange={() => {}} disabled/>
 </div>;
}
