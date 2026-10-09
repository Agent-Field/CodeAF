import { useEffect, useRef, useState, type ReactNode } from 'react';
import { Button, ContextMenu, HoverPreview, IconButton, SectionHeading, Text, TextInput, WorkStateIndicator } from '../../components/ui';
import { planForest, taskStanding, type PlanDependency, type PlanTaskRow } from './plan-model';
import './plan-outline.css';
import '../../styles/plan-outline.css';
export type PlanOutlineProps = {
 rows:readonly PlanTaskRow[]; dependencies:readonly PlanDependency[]; selectedId?:string; scopeId?:string;
 onNavigate:(id:string,newTab:boolean)=>void; onClose:()=>void; sourceLabel?:string;
};
/** Containment is the navigation hierarchy. Dependency metadata never invents a parent. */
export function PlanOutline({rows:allRows,dependencies,selectedId,scopeId,onNavigate,onClose,sourceLabel}:PlanOutlineProps){
 const fullPlan=planForest(allRows);
 const included=new Set<string>();
 if(scopeId){const stack=allRows.filter(row=>row.ID===scopeId);while(stack.length){const row=stack.pop()!;if(included.has(row.ID))continue;included.add(row.ID);stack.push(...(fullPlan.children.get(row.ID)??[]));}}
 const rows=scopeId?allRows.filter(row=>included.has(row.ID)):allRows;
 const [collapsed,setCollapsed]=useState(new Set<string>());
 const [searchOpen,setSearchOpen]=useState(false);
 const [query,setQuery]=useState('');
 const search=useRef<HTMLInputElement>(null);const searchToggle=useRef<HTMLButtonElement>(null);
 useEffect(()=>{if(searchOpen)search.current?.focus();},[searchOpen]);
 const {roots,children}=planForest(rows);
 const byId=new Map(rows.map(row=>[row.ID,row]));
 const selectedAncestors=new Set<string>();let selected=byId.get(selectedId??'');while(selected?.Parent&&!selectedAncestors.has(selected.Parent)){selectedAncestors.add(selected.Parent);selected=byId.get(selected.Parent);}
 const leaves=rows.filter(row=>!children.has(row.ID));const completed=leaves.filter(row=>row.Status==='done').length;const cancelled=leaves.filter(row=>row.Status==='cancelled').length;
 const incoming=new Map<string,number>(),outgoing=new Map<string,number>();
 dependencies.filter(edge=>edge.kind!=='suggests').forEach(edge=>{incoming.set(edge.to,(incoming.get(edge.to)??0)+1);outgoing.set(edge.from,(outgoing.get(edge.from)??0)+1);});
 const needle=query.trim().toLocaleLowerCase();const visible=new Set<string>();
 if(needle)for(const row of rows){if(!row.Title.toLocaleLowerCase().includes(needle))continue;let current:PlanTaskRow|undefined=row;const seen=new Set<string>();while(current&&!seen.has(current.ID)){seen.add(current.ID);visible.add(current.ID);current=byId.get(current.Parent??'');}}
 function dismissSearch(){setSearchOpen(false);setQuery('');requestAnimationFrame(()=>searchToggle.current?.focus());}
 function branch(row:PlanTaskRow,ancestors:ReadonlySet<string>):ReactNode{
  if(ancestors.has(row.ID)||needle&&!visible.has(row.ID))return null;
  const descendants=children.get(row.ID)??[];const folded=!needle&&collapsed.has(row.ID);const state=taskStanding(row);
  const waits=incoming.get(row.ID)??0,dependents=outgoing.get(row.ID)??0;
  const relations=[waits?`${waits} prerequisite${waits===1?'':'s'}`:'',dependents?`${dependents} dependent${dependents===1?'':'s'}`:''].filter(Boolean).join(' · ');
  const next=new Set([...ancestors,row.ID]);
  return <li className="plan-outline-branch" data-parent={descendants.length>0} key={row.ID}><ContextMenu label={`Task actions for ${row.Title}`} items={[
   {id:'open',label:'Open task',onSelect:()=>onNavigate(row.ID,false)},
   {id:'new-tab',label:'Open task in new tab',onSelect:()=>onNavigate(row.ID,true)},
  ]}><div className="plan-outline-row" data-selected={selectedId===row.ID} data-active-branch={folded&&selectedAncestors.has(row.ID)||undefined}>
   {descendants.length?<IconButton className="plan-outline-disclosure" label={`${folded?'Expand':'Collapse'} ${row.Title}`} icon={folded?'chevronRight':'chevron'} iconSize="xs" aria-expanded={!folded} disabled={!!needle} onClick={()=>setCollapsed(previous=>{const changed=new Set(previous);if(folded)changed.delete(row.ID);else changed.add(row.ID);return changed;})}/>:<span className="plan-outline-indent" aria-hidden="true"/>}
   <HoverPreview disabled={selectedId===row.ID} title={row.Title} description={row.Hold || row.Live?.Command || row.Note || ''} meta={[state.label,relations].filter(Boolean).join(' · ')}><Button className="plan-outline-task" aria-label={`${row.Title}, ${state.label}`} aria-current={selectedId===row.ID?'page':undefined} aria-description={descendants.length?`${descendants.length} child tasks`:undefined} onClick={event=>onNavigate(row.ID,event.metaKey||event.ctrlKey)}><WorkStateIndicator phase={state.phase} label={state.label}/><span className="plan-outline-title">{row.Title}</span>{descendants.length>0&&<span className="plan-outline-count" aria-hidden="true">{descendants.length}</span>}</Button></HoverPreview>
  </div></ContextMenu>{descendants.length>0&&!folded&&<ul className="plan-outline-list plan-outline-children">{descendants.map(child=>branch(child,next))}</ul>}</li>;
 }
 return <aside className="plan-outline" aria-label="Plan hierarchy"><div className="plan-outline-header"><div className="plan-outline-heading"><SectionHeading>Plan</SectionHeading><Text className="plan-outline-progress">{completed} of {leaves.length} completed{cancelled?` · ${cancelled} cancelled`:''}</Text></div><div className="plan-outline-actions"><IconButton ref={searchToggle} label={searchOpen?'Close task search':'Find tasks'} icon="search" iconSize="sm" aria-expanded={searchOpen} disabled={!rows.length} onClick={()=>searchOpen?dismissSearch():setSearchOpen(true)}/><IconButton label="Close task hierarchy" icon="close" iconSize="sm" onClick={onClose}/></div></div>
  {sourceLabel&&<Text className="plan-outline-source">{sourceLabel}</Text>}
  {searchOpen&&<div className="plan-outline-search"><TextInput ref={search} aria-label="Filter plan tasks" placeholder="Find a task…" value={query} onChange={event=>setQuery(event.target.value)} onKeyDown={event=>{if(event.key==='Escape'){event.preventDefault();event.stopPropagation();dismissSearch();}}}/></div>}
  <div className="plan-outline-scroll">{rows.length?<ul className="plan-outline-list" aria-label="Plan tasks">{roots.map(root=>branch(root,new Set()))}</ul>:<Text className="plan-outline-empty">No tasks in this plan.</Text>}{scopeId&&rows.length===1&&!needle&&<Text className="plan-outline-empty">No subtasks.</Text>}{needle&&!visible.size&&<Text className="plan-outline-empty">No matching tasks.</Text>}</div>
 </aside>;
}
