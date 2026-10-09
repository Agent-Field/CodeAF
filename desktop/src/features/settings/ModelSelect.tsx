import { useEffect, useId, useMemo, useRef, useState, type KeyboardEvent } from 'react';
import { Button, Icon, TextInput } from '../../components/ui';
import type { CatalogModel } from '../chat/engine-client';

/** How many matches the list draws at once; the catalog is hundreds long and the search narrows it. */
const SHOWN = 60;

/** The name a person reads for a model: the provider's name for it, else its id. */
export const modelName = (catalog: readonly CatalogModel[], id: string): string => catalog.find(model => model.id === id)?.name || id;

/** Case-blind match of every word against the id and the name. */
export function matchModels(catalog: readonly CatalogModel[], query: string): CatalogModel[] {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean);
  return catalog.filter(model => words.every(word => `${model.id} ${model.name}`.toLowerCase().includes(word)));
}

type ModelSelectProps = {
  /** The accessible name, e.g. "Model for Tasks". */
  label: string;
  value: string;
  catalog: readonly CatalogModel[];
  onChange: (model: string) => void;
};

/**
 * A model choice with search, drawn like the composer's model popover: surface paper, sh-2, rows that
 * lift on hover, the chosen model checked. Esc and a press outside close it; focus returns to the button.
 */
export function ModelSelect({ label, value, catalog, onChange }: ModelSelectProps) {
  const root = useRef<HTMLDivElement>(null);
  const search = useRef<HTMLInputElement>(null);
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState('');
  const list = useId();
  const matches = useMemo(() => matchModels(catalog, query).slice(0, SHOWN), [catalog, query]);

  const close = (restore: boolean) => { setOpen(false); setQuery(''); if (restore) root.current?.querySelector('button')?.focus(); };
  const pick = (id: string) => { close(true); if (id !== value) onChange(id); };

  useEffect(() => {
    if (!open) return;
    search.current?.focus();
    const outside = (event: PointerEvent) => { if (!root.current?.contains(event.target as Node)) close(false); };
    document.addEventListener('pointerdown', outside, true);
    return () => document.removeEventListener('pointerdown', outside, true);
  }, [open]);

  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); close(true); return; }
    const step = event.key === 'ArrowDown' ? 1 : event.key === 'ArrowUp' ? -1 : 0;
    if (!step) return;
    event.preventDefault();
    const stops = [...event.currentTarget.querySelectorAll<HTMLElement>('input, [data-nav]')];
    const at = stops.indexOf(document.activeElement as HTMLElement);
    stops[(at + step + stops.length) % stops.length]?.focus();
  };

  return (
    <div ref={root} className="model-select">
      <Button className="model-select-trigger" aria-label={label} aria-haspopup="dialog" aria-expanded={open} data-state={open ? 'open' : 'closed'} onClick={() => setOpen(value => !value)}>
        <span className="model-select-value">{modelName(catalog, value)}</span>
        <Icon name="chevron" size="xs" />
      </Button>
      {open && (
        <div className="model-select-panel" role="dialog" aria-label={label} onKeyDown={onKeyDown}>
          <TextInput ref={search} appearance="field" role="combobox" aria-expanded aria-controls={list} aria-label="Search models" placeholder="Search models" value={query} onChange={event => setQuery(event.target.value)} onKeyDown={event => { if (event.key === 'Enter' && matches[0]) { event.preventDefault(); pick(matches[0].id); } }} />
          <div id={list} role="listbox" aria-label="Models" className="model-select-list">
            {matches.map(model => (
              <Button key={model.id} role="option" data-nav aria-selected={model.id === value} className="model-select-row" onClick={() => pick(model.id)}>
                <span className="model-select-mark">{model.id === value && <Icon name="check" size="xs" />}</span>
                <span className="model-select-name">{model.name || model.id}</span>
              </Button>
            ))}
            {matches.length === 0 && <p className="model-select-empty">No model matches.</p>}
          </div>
        </div>
      )}
    </div>
  );
}
