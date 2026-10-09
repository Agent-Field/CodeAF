import { useEffect, useLayoutEffect, useRef, useState, type RefObject } from 'react';
import { Button, DropdownMenu, Icon, IconButton, KeyboardShortcut, Text, TextArea, TextInput, type MenuEntry } from '../../components/ui';
import { composerActionShortcut, composerShortcuts, formatShortcut, isMac } from '../../design/keyboard';
import design from '../../design/tokens.json';
import { ModelPicker, previewModels } from './ModelPicker';
import './work-composer.css';
export type WorkPreset = 'auto' | 'fast' | 'thorough' | 'custom';
export type WorkComposerAction = 'send' | 'steer' | 'queue' | 'restart';
export type WorkContext = { id: string; name: string; kind: 'file' | 'paste'; text?: string };
export type WorkComposerProps = {
 tabId: string; title: string; draft: string; onDraft: (value: string) => void;
 fixedModelLabel?: string; status?: string; running: boolean; fresh: boolean; preset: WorkPreset; onPreset: (value: WorkPreset) => void;
 rememberPreset: boolean; onRememberPreset: (value: boolean) => void;
 model: string; effort: string; onModel: (value: string) => void; onEffort: (value: string) => void;
 context: WorkContext[]; onContext: (value: WorkContext[]) => void;
 onAction: (action: WorkComposerAction) => boolean | void; onStop: () => void;
 suggestions?: string[]; editorRef?: RefObject<HTMLTextAreaElement | null>; fileInputRef?: RefObject<HTMLInputElement | null>; disabled?: boolean;
};
const presets: { value: WorkPreset; label: string; detail: string }[] = [
 { value: 'auto', label: 'Auto', detail: 'Choose per request · preview' },
 { value: 'fast', label: 'Fast', detail: 'Quick iteration · preview' },
 { value: 'thorough', label: 'Thorough', detail: 'Deeper reasoning · preview' },
];
const longPasteCharacters = 1200;
export function WorkComposer(props: WorkComposerProps) {
 const { tabId, title, draft, onDraft, running, fresh, preset, onPreset, rememberPreset, onRememberPreset, model, effort, onModel, onEffort, context, onContext, onAction, onStop, suggestions = [], disabled = false } = props;
 const internalEditor = useRef<HTMLTextAreaElement>(null);
 const editor = props.editorRef ?? internalEditor;
 const internalFileInput = useRef<HTMLInputElement>(null);
 const fileInput = props.fileInputRef ?? internalFileInput;
 const contextDialog = useRef<HTMLDialogElement>(null);
 const contextTrigger = useRef<HTMLButtonElement | null>(null);
 const [focused, setFocused] = useState(false);
 const [advancedOpen, setAdvancedOpen] = useState(false);
 const [menuOpen, setMenuOpen] = useState(false);
 const [inspecting, setInspecting] = useState<WorkContext | null>(null);
 const [notice, setNotice] = useState('');
 const [primaryHeld, setPrimaryHeld] = useState(false);
 const ready = !!draft.trim() || context.length > 0;
 const expanded = fresh || focused || menuOpen || advancedOpen || !!inspecting || ready;
 const label = preset === 'custom' ? previewModels.find(choice => choice.id === model)?.name ?? 'Custom' : presets.find(choice => choice.value === preset)?.label ?? 'Auto';
 useEffect(() => { setFocused(window.document.activeElement === editor.current); setAdvancedOpen(false); setMenuOpen(false); setInspecting(null); setNotice(''); }, [tabId]);
 useEffect(() => {
  const sync = (event: KeyboardEvent) => setPrimaryHeld(isMac ? event.metaKey : event.ctrlKey);
  const clear = () => setPrimaryHeld(false);
  window.addEventListener('keydown', sync); window.addEventListener('keyup', sync); window.addEventListener('blur', clear);
  return () => { window.removeEventListener('keydown', sync); window.removeEventListener('keyup', sync); window.removeEventListener('blur', clear); };
 }, []);
 useEffect(() => {
  if (inspecting && !contextDialog.current?.open) contextDialog.current?.showModal();
  else if (!inspecting) contextDialog.current?.close();
 }, [inspecting]);
 useLayoutEffect(() => {
  const field = editor.current;
  if (!field) return;
  function resize() {
   if (!field) return;
   field.rows = design.interaction.composerMinRows;
   const css = getComputedStyle(field), line = parseFloat(css.lineHeight);
   if (!line) return;
   const height = field.scrollHeight - parseFloat(css.paddingTop) - parseFloat(css.paddingBottom);
   field.rows = Math.min(design.interaction.composerMaxRows, Math.max(design.interaction.composerMinRows, Math.ceil((height - parseFloat(design.foundation['border-width'])) / line)));
  }
  resize(); const observer = new ResizeObserver(resize); observer.observe(field);
  return () => observer.disconnect();
 }, [draft, tabId, expanded, editor]);
 function choose(value: WorkPreset) { onPreset(value); setNotice(`${value === 'custom' ? 'Custom model' : presets.find(choice => choice.value === value)?.label} selected for ${rememberPreset ? 'this tab' : 'one submission'}.`); }
 function cycle() { choose(presets[(presets.findIndex(choice => choice.value === preset) + 1) % presets.length].value); }
 function act(action: WorkComposerAction) {
  if (!ready || disabled) return;
  if (action === 'queue' && context.length) { setNotice('Queue accepts text only. Your draft and context are preserved.'); return; }
  if (onAction(action) === false) return;
  if (!rememberPreset) onPreset('auto');
  if (fresh) { editor.current?.blur(); setFocused(false); }
  else editor.current?.focus();
 }
 const pickerEntries: MenuEntry[] = [
  ...presets.map(choice => ({ id: choice.value, label: `${choice.label} · ${choice.detail}`, checked: preset === choice.value, onSelect: () => choose(choice.value) })),
  { kind: 'separator', id: 'advanced-separator' },
  { id: 'advanced', label: 'Choose model & effort…', icon: 'chevronRight', onSelect: () => setAdvancedOpen(true) },
  { kind: 'separator', id: 'remember-separator' },
  { id: 'remember', label: 'Remember for this tab', checked: rememberPreset, onSelect: () => onRememberPreset(!rememberPreset) },
 ];
 const presetChip = props.fixedModelLabel ? <Text className="work-fixed-model">{props.fixedModelLabel}</Text> : <div className="work-preset-chip" data-once={!rememberPreset}>
  <Button className="work-preset-cycle" disabled={disabled} onClick={cycle} title="Cycle model preset" aria-label={`Model preset: ${label}. Click to cycle.`}>{label}{!rememberPreset && <span> · once</span>}</Button>
  <DropdownMenu label="Model presets" items={pickerEntries} onOpenChange={setMenuOpen}><IconButton className="work-preset-menu" label="Choose model preset" icon="chevron" iconSize="xs" disabled={disabled}/></DropdownMenu>
 </div>;
 return <div className="work-composer" data-expanded={expanded} data-fresh={fresh} data-running={running} onFocusCapture={() => setFocused(true)} onBlurCapture={event => { if (!event.currentTarget.contains(event.relatedTarget)) setFocused(false); }} onKeyDown={event => {
  const primary = isMac ? event.metaKey && !event.ctrlKey : event.ctrlKey && !event.metaKey;
  if (primary && !event.altKey && !event.shiftKey && event.key === '.') { event.preventDefault(); cycle(); }
 }}>
  {primaryHeld && !menuOpen && !advancedOpen && !inspecting && <div className="work-shortcut-hints" role="group" aria-label="Work keyboard shortcuts">
   <span><KeyboardShortcut label={formatShortcut('⌘/Ctrl L')}/> focus line</span>
   <span><KeyboardShortcut label={composerShortcuts.newSection}/> new section</span>
   <span><KeyboardShortcut label={formatShortcut('⌘/Ctrl .')}/> cycle preset</span>
   {suggestions.length > 0 && <span><KeyboardShortcut label="Tab"/> accept suggestion</span>}
   {running && <span><KeyboardShortcut label="Esc"/> stop preview</span>}
   {!fresh && <span><KeyboardShortcut label={formatShortcut('⌘/Ctrl ⇧ F')}/> fold all</span>}
  </div>}
  {context.length > 0 && <div className="work-contexts" role="group" aria-label="Attached context">{context.map(item => <div className="work-context-chip" key={item.id}>
   <Button onClick={event => { contextTrigger.current = event.currentTarget; setInspecting(item); }} aria-label={`Inspect ${item.name}`}><Icon name="code" size="xs"/>{item.name}</Button>
   <IconButton label={`Remove ${item.name}`} icon="close" iconSize="xs" onClick={() => onContext(context.filter(value => value.id !== item.id))}/>
  </div>)}</div>}
  <div className="work-writing-line">
   <TextArea ref={editor} aria-label={`Draft for ${title}`} aria-describedby={`work-composer-help-${tabId}`} placeholder={fresh ? 'What’s on your mind?' : !focused && !ready && suggestions[0] ? suggestions[0] : !ready ? 'What next?' : running ? 'Direct this work…' : 'Continue, or start another section…'} value={draft} disabled={disabled} rows={design.interaction.composerMinRows} onChange={event => onDraft(event.target.value)} onPaste={event => {
    const text = event.clipboardData.getData('text/plain'); if (text.length < longPasteCharacters) return;
    event.preventDefault(); const count = text.trim().split(/\s+/).filter(Boolean).length;
    onContext([...context, { id: crypto.randomUUID(), kind: 'paste', name: `Pasted text · ${count.toLocaleString()} words`, text }]);
    setNotice('Pasted text attached. Open its chip to inspect the full text.');
   }} onKeyDown={event => {
    if (event.nativeEvent.isComposing || event.nativeEvent.keyCode === 229) return;
    if (event.key === 'Tab' && !event.shiftKey && !event.metaKey && !event.ctrlKey && !draft && suggestions[0]) { event.preventDefault(); onDraft(suggestions[0]); return; }
    const action = composerActionShortcut(event, running); if (action) { event.preventDefault(); act(action); }
   }}/>
   {fresh && !ready && presetChip}
   {!fresh && !focused && !ready && suggestions.length > 0 && <span className="work-suggestion-hint" aria-hidden="true">Tab</span>}
   {fresh && ready && <IconButton className="work-quick-send" label="Send" icon="arrow" iconSize="xs" disabled={!ready || disabled} title={`Send (${composerShortcuts.send})`} onClick={() => act('send')}/>}
  </div>
  {ready && <div className="work-composer-controls">
   <div className="work-composer-tools">{presetChip}<Button className="work-attach" disabled={disabled} onClick={() => fileInput.current?.click()}><Icon name="plus" size="xs"/>Attach</Button></div>
   {ready && !fresh && <div className="work-composer-actions">
    <Button className="work-new-section" variant="secondary" disabled={disabled} onClick={() => act('restart')}>New section</Button>
    {running && <DropdownMenu label="Submission actions" onOpenChange={setMenuOpen} items={[{ id: 'queue', label: 'Queue after this turn', shortcut: composerShortcuts.queue, disabled: context.length > 0, onSelect: () => act('queue') }]}><IconButton label="Submission actions" icon="more" iconSize="xs" disabled={disabled}/></DropdownMenu>}
    <Button className="work-continue" variant="primary" disabled={disabled} onClick={() => act(running ? 'steer' : 'send')}>{running ? 'Steer' : 'Continue'}<Icon name="arrow" size="xs"/></Button>
   </div>}
  </div>}
  {focused && !ready && !fresh && suggestions.length > 0 && <div className="work-next-suggestions">{suggestions.map(suggestion => <Button className="work-next-suggestion" key={suggestion} onClick={() => { onDraft(suggestion); editor.current?.focus(); }}>{suggestion}</Button>)}</div>}
  <div className="work-composer-caption">{props.status && <Text className="work-status" role="status">{props.status}</Text>}{running && <Button className="work-stop" title={props.fixedModelLabel?"Stop engine work":"Stop this preview"} onClick={onStop}>Stop</Button>}</div>
  <TextInput ref={fileInput} type="file" multiple hidden aria-label="Attach files to preview" onChange={event => {
   const files = Array.from(event.currentTarget.files ?? []); onContext([...context, ...files.map(file => ({ id: crypto.randomUUID(), kind: 'file' as const, name: file.name }))]);
   event.currentTarget.value = ''; setNotice('Files attached to this local preview. No upload or request was made.'); editor.current?.focus();
  }}/>
  <Text className="work-composer-help" id={`work-composer-help-${tabId}`}>{running ? `Enter to steer. ${composerShortcuts.queue} to queue.` : 'Enter to continue.'} {composerShortcuts.newSection} starts a new section. Shift Enter adds a line. Large pasted text becomes inspectable context.</Text>
  <Text className="work-composer-notice" role="status" hidden={!notice}>{notice}</Text>
  {!props.fixedModelLabel&&<ModelPicker value={model} effort={effort} onSelect={value => { onModel(value); onPreset('custom'); }} onEffort={onEffort} editor={editor} externalOpen={advancedOpen} onExternalOpenChange={setAdvancedOpen} hideTrigger/>}
  <dialog ref={contextDialog} className="work-context-dialog" aria-label="Attached context preview" onCancel={() => setInspecting(null)} onClose={() => { setInspecting(null); contextTrigger.current?.focus(); }}>
   <div className="work-context-dialog-heading"><Text>{inspecting?.name}</Text><IconButton label="Close attached context" icon="close" iconSize="xs" onClick={() => setInspecting(null)}/></div>
   <Text className="work-context-content">{inspecting?.text ?? 'File selected for this local UI preview. File contents have not been uploaded or read.'}</Text>
  </dialog>
 </div>;
}
