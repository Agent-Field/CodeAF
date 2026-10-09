import { useEffect, useRef, useState, type RefObject, type ReactNode } from 'react';
import { Button, HoverPreview, IconButton, SectionHeading, Text, WorkStateIndicator } from '../../components/ui';
import { planForest, taskStanding, type PlanTaskPage, type PlanTaskRow } from './plan-model';
import './tasks.css';
export type TaskInspectorProps = {
 conversationId: string; open: boolean; rows: readonly PlanTaskRow[]; onClose: () => void;
 presentation?: 'inline'|'drawer'; returnFocus?: RefObject<HTMLElement|null>;
 sourceLabel?: string; planError?: string; page?: PlanTaskPage|null; pageLoading?: boolean; pageError?: string;
 onSelectTask?: (id: string) => void;
};
/** Chat-scoped read-only plan: parent connectors describe containment, never dependency edges. */
export function TaskInspector({ conversationId, open, rows, onClose, presentation = 'inline', returnFocus, sourceLabel, planError, page, pageLoading, pageError, onSelectTask }: TaskInspectorProps) {
 const dialog = useRef<HTMLDialogElement>(null);
 const close = useRef<HTMLButtonElement>(null);
 const [selected, setSelected] = useState<string>();
 const [collapsed, setCollapsed] = useState<Set<string>>(() => new Set(rows.filter(row => row.Status === 'done').map(row => row.ID)));
 const previousFocus = useRef<HTMLElement|null>(null);
 const seenRows = useRef(new Set<string>());
 useEffect(() => { setSelected(undefined); seenRows.current = new Set(); setCollapsed(new Set(rows.filter(row => row.Status === 'done').map(row => row.ID))); }, [conversationId]);
 useEffect(() => {
  // Fold already-completed branches on their first read, never on live status changes.
  const completedNewRows = rows.filter(row => !seenRows.current.has(row.ID) && row.Status === 'done');
  rows.forEach(row => seenRows.current.add(row.ID));
  if (completedNewRows.length) setCollapsed(value => new Set([...value, ...completedNewRows.map(row => row.ID)]));
 }, [rows]);
 useEffect(() => {
  if (presentation !== 'drawer') return;
  if (open && !dialog.current?.open) { previousFocus.current = document.activeElement instanceof HTMLElement ? document.activeElement : null; dialog.current?.showModal(); close.current?.focus(); }
  else if (!open) dialog.current?.close();
 }, [open, presentation]);
 const { roots, children } = planForest(rows);
 const leaves = rows.filter(row => !children.has(row.ID));
 const completed = leaves.filter(row => row.Status === 'done').length;
 const attention = rows.filter(row => taskStanding(row).attention).length;
 const current = rows.find(row => row.ID === selected);
 const detail = page?.Row.ID === selected ? page : null;
 function dismiss() { onClose(); if (presentation === 'inline') returnFocus?.current?.focus(); }
 function branch(row: PlanTaskRow, ancestors: ReadonlySet<string>): ReactNode {
  if (ancestors.has(row.ID)) return null;
  const descendants = children.get(row.ID) ?? []; const folded = collapsed.has(row.ID); const state = taskStanding(row);
  const next = new Set([...ancestors, row.ID]);
  return <li key={row.ID} className="task-branch"><div className="task-row">
   {descendants.length ? <IconButton className="task-disclosure" label={`${folded ? 'Expand' : 'Collapse'} ${row.Title}`} icon={folded ? 'chevronRight' : 'chevron'} iconSize="xs" aria-expanded={!folded} onClick={() => setCollapsed(value => { const changed = new Set(value); if (folded) changed.delete(row.ID); else changed.add(row.ID); return changed; })}/> : <span className="task-leaf-space" aria-hidden="true"/>}
   <HoverPreview disabled={selected === row.ID} title={row.Title} description={row.Hold || row.Live?.Command || row.Stage || row.Note || ''} meta={`${state.label}${row.Program ? ` · ${row.Program}` : ''}${row.Archived ? ' · previous run' : ''}`}><Button className="task-select" aria-label={`${row.Title}, ${state.label}`} aria-pressed={selected === row.ID} onClick={() => { setSelected(row.ID); onSelectTask?.(row.ID); }}><WorkStateIndicator phase={state.phase} label={state.label}/><span className="task-title">{row.Title}</span></Button></HoverPreview>
  </div>{!!row.Total && <Text className="task-root-summary">{row.Done ?? 0} of {row.Total} tasks completed{row.Running ? ` · ${row.Running} active` : ''}{row.Queued ? ` · ${row.Queued} queued` : ''}{row.Failed ? ` · ${row.Failed} incomplete` : ''}</Text>}{!!descendants.length && !folded && <ul className="task-outline task-children">{descendants.map(child => branch(child, next))}</ul>}</li>;
 }
 const body = <><div className="task-inspector-header"><SectionHeading>Plan</SectionHeading><IconButton ref={close} label="Close task plan" icon="close" onClick={dismiss}/></div><div className="task-inspector-body">
  {sourceLabel && <Text className="task-inspector-source">{sourceLabel}</Text>}
  {planError && <Text role="status">{planError}</Text>}
  {rows.length ? <Text className="task-inspector-summary">{completed} of {leaves.length} leaf tasks completed{attention ? ` · ${attention} need attention` : ''}</Text> : <div className="task-empty"><Text tone="default">{planError ? 'The plan could not be read.' : 'No tasks in the current plan.'}</Text><Text>When codeaf creates a plan, its tasks and progress appear here.</Text></div>}
  <ul className="task-outline" aria-label="Conversation tasks">{roots.map(row => branch(row, new Set()))}</ul>
  {current && <section className="task-detail" aria-label={`Details for ${current.Title}`} aria-busy={pageLoading || undefined}><div className="task-detail-header"><SectionHeading>{current.Title}</SectionHeading><IconButton label="Close task details" icon="close" iconSize="xs" onClick={() => setSelected(undefined)}/></div><Text tone="default">{taskStanding(current).label}{current.Archived ? ' · previous run' : ''}</Text>
   {current.Hold && <Text>{current.Hold}</Text>}{current.Live?.Command && <Text className="task-live-step">{current.Live.Command}</Text>}{current.Note && <Text>{current.Note}</Text>}
   {!!current.Waits?.length && <><Text tone="default">Waiting on</Text><ul className="task-detail-list">{current.Waits.map(id => <li key={id}>{rows.find(row => row.ID === id)?.Title ?? id}</li>)}</ul></>}
   {rows.some(row => row.Waits?.includes(current.ID)) && <><Text tone="default">Needed by</Text><ul className="task-detail-list">{rows.filter(row => row.Waits?.includes(current.ID)).map(row => <li key={row.ID}>{row.Title}</li>)}</ul></>}
   {pageLoading && <Text>Reading task details…</Text>}{pageError && <Text role="status">{pageError}</Text>}{detail?.Description && <Text>{detail.Description}</Text>}
   {detail?.Result && <><Text tone="default">Result</Text><Text>{detail.Result}</Text></>}
   {!!detail?.Checks?.length && <><Text tone="default">Checks</Text><ul className="task-detail-list">{detail.Checks.map((check, index) => <li key={index}>{check}</li>)}</ul></>}
   {!!detail?.Notes?.length && <><Text tone="default">Notes</Text>{detail.Notes.map((note, index) => <Text key={index}>{note.Person ? 'You' : note.Author || 'Worker'}: {note.Body}</Text>)}</>}
   {!!detail?.Steps?.length && <><Text tone="default">Recorded steps</Text><ol className="task-detail-list">{detail.Steps.map((step, index) => <li key={index}>{step.command}{step.observation && <Text>{step.observation}</Text>}</li>)}</ol></>}
  </section>}
 </div></>;
 if (presentation === 'drawer') return <dialog ref={dialog} className="task-inspector-drawer" aria-label="Conversation task plan" onCancel={dismiss} onClose={() => { onClose(); const target = returnFocus?.current ?? previousFocus.current; if (target?.isConnected) target.focus(); }} onClick={event => { if (event.target !== event.currentTarget) return; const bounds = event.currentTarget.getBoundingClientRect(); if (event.clientX < bounds.left || event.clientX > bounds.right || event.clientY < bounds.top || event.clientY > bounds.bottom) dismiss(); }}>{open && body}</dialog>;
 return open ? <aside className="task-inspector" aria-label="Conversation task plan">{body}</aside> : null;
}
