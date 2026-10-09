import { useEffect, useLayoutEffect, useRef, useState, type RefObject } from 'react';
import { Button, Icon, IconButton, Select, Text, TextInput } from '../../components/ui';
export const previewModels = [
 { id: 'codeaf', name: 'CodeAF AI', provider: 'Preview', detail: 'Balanced' },
 { id: 'fast', name: 'Fast', provider: 'Preview', detail: 'Quick iteration' },
 { id: 'reasoning', name: 'Reasoning', provider: 'Preview', detail: 'Deeper thinking' },
];
type Props = { value: string; effort: string; onSelect: (id: string) => void; onEffort: (value: string) => void; editor: RefObject<HTMLTextAreaElement | null>; externalOpen?: boolean; onExternalOpenChange?: (open: boolean) => void; hideTrigger?: boolean };
export function ModelPicker({ value, effort, onSelect, onEffort, editor, externalOpen, onExternalOpenChange, hideTrigger = false }: Props) {
 const [localOpen, setLocalOpen] = useState(false);
 const open = externalOpen ?? localOpen;
 function setOpen(value: boolean) { setLocalOpen(value); onExternalOpenChange?.(value); }
 const [query, setQuery] = useState('');
 const [pinned, setPinned] = useState(['codeaf']);
 const [recent, setRecent] = useState<string[]>([]);
 const dialog = useRef<HTMLDialogElement>(null);
 const trigger = useRef<HTMLButtonElement>(null);
 const search = useRef<HTMLInputElement>(null);
 const selected = useRef(false);
 const pinFocus = useRef<string | null>(null);
 useLayoutEffect(() => {
  if (!pinFocus.current) return;
  dialog.current?.querySelector<HTMLButtonElement>(`[data-model-pin="${pinFocus.current}"]`)?.focus();
  pinFocus.current = null;
 }, [pinned]);
 useEffect(() => {
  if (open && !dialog.current?.open) { selected.current = false; dialog.current?.showModal(); search.current?.focus(); }
  else if (!open) dialog.current?.close();
 }, [open]);
 const matches = previewModels.filter(model => `${model.name} ${model.provider} ${model.detail}`.toLowerCase().includes(query.trim().toLowerCase()));
 const groups = [
  { name: 'Pinned', models: matches.filter(model => pinned.includes(model.id)) },
  { name: 'Recent', models: recent.filter(id => !pinned.includes(id)).flatMap(id => matches.filter(model => model.id === id)) },
  { name: 'Models', models: matches.filter(model => !pinned.includes(model.id) && !recent.includes(model.id)) },
 ];
 function close() { setOpen(false); setQuery(''); }
 return <>
  {!hideTrigger && <Button className="model-picker-trigger" aria-label="Choose conversation model" aria-haspopup="dialog" aria-expanded={open} onClick={event => { trigger.current = event.currentTarget; setOpen(true); }}>{previewModels.find(model => model.id === value)?.name ?? 'Choose model'}<Icon name="chevron" size="xs"/></Button>}
  <dialog ref={dialog} className="model-picker" aria-label="Choose preview model" onCancel={close} onClose={() => { close(); (selected.current || hideTrigger ? editor.current : trigger.current)?.focus(); }} onClick={event => { if (event.target !== event.currentTarget) return; const bounds = event.currentTarget.getBoundingClientRect(); if (event.clientX < bounds.left || event.clientX > bounds.right || event.clientY < bounds.top || event.clientY > bounds.bottom) close(); }}>
   <div className="model-picker-search"><Icon name="search" size="sm"/><TextInput ref={search} aria-label="Search models" placeholder="Search models…" value={query} onChange={event => setQuery(event.target.value)} onKeyDown={event => { if (event.key === 'ArrowDown') { event.preventDefault(); dialog.current?.querySelector<HTMLButtonElement>('.model-picker-select')?.focus(); } }}/><IconButton label="Close model picker" icon="close" iconSize="xs" onClick={close}/></div>
   <div className="model-picker-results" aria-label="Preview model choices">{groups.filter(group => group.models.length).map(group => <div key={group.name}><Text className="model-picker-heading">{group.name}</Text>{group.models.map(model => <div className="model-picker-row" key={model.id}>
    <Button className="model-picker-select" aria-label={`Use ${model.name}`} aria-pressed={value === model.id} onKeyDown={event => {
     const rows = Array.from(dialog.current?.querySelectorAll<HTMLButtonElement>('.model-picker-select') ?? []);
     const index = rows.indexOf(event.currentTarget);
     const target = event.key === 'ArrowDown' ? rows[(index + 1) % rows.length] : event.key === 'ArrowUp' ? rows[(index - 1 + rows.length) % rows.length] : event.key === 'Home' ? rows[0] : event.key === 'End' ? rows[rows.length - 1] : undefined;
     if (target) { event.preventDefault(); target.focus(); }
    }} onClick={() => { selected.current = true; onSelect(model.id); setRecent(current => [model.id, ...current.filter(id => id !== model.id)]); close(); }}><span><span className="model-picker-name">{model.name}</span><span className="model-picker-detail">{model.detail} · {model.provider}</span></span>{value === model.id && <Icon name="check" size="xs"/>}</Button>
    <IconButton className="model-picker-pin" data-model-pin={model.id} label={`${pinned.includes(model.id) ? 'Unpin' : 'Pin'} ${model.name}`} aria-pressed={pinned.includes(model.id)} icon="pin" iconSize="xs" onClick={() => { pinFocus.current = model.id; setPinned(current => current.includes(model.id) ? current.filter(id => id !== model.id) : [...current, model.id]); }}/>
   </div>)}</div>)}{!matches.length && <Text className="model-picker-empty" role="status">No matching models</Text>}</div>
   <div className="model-picker-footer"><Select label="Preview reasoning effort" value={effort} onValueChange={onEffort} options={[{value:'auto',label:'Auto effort'},{value:'low',label:'Low effort'},{value:'medium',label:'Medium effort'},{value:'high',label:'High effort'}]}/><Text>Preview choices</Text></div>
  </dialog>
 </>;
}
