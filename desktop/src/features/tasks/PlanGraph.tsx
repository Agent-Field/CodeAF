import { useRef, useState } from 'react';
import { Button, HoverPreview, IconButton, SectionHeading, Text, WorkStateIndicator } from '../../components/ui';
import { DependencyMap } from '../../components/ui/DependencyMap';
import { planForest, taskStanding, type PlanDependency, type PlanTaskRow } from './plan-model';
import { dependencyStages, dependencyWords } from './graph-model';
import './plan-graph.css';
import '../../styles/plan-graph.css';
export type PlanGraphProps = { rows:readonly PlanTaskRow[]; dependencies:readonly PlanDependency[]; sourceLabel:string; onClose?:()=>void; onOpenConversation?:(taskId:string)=>void };
export function PlanGraph({rows,dependencies,sourceLabel,onClose,onOpenConversation}:PlanGraphProps){
 const {roots,children}=planForest(rows);
 const host=useRef<HTMLElement>(null);
 const [showIndependent,setShowIndependent]=useState(dependencies.length===0);
 const connected=new Set(dependencies.flatMap(edge=>[edge.from,edge.to]));
 const independent=rows.filter(row=>!children.has(row.ID)&&!connected.has(row.ID));
 const [selected,setSelected]=useState<string|undefined>(()=>rows.find(row=>!children.has(row.ID)&&row.Status==='running')?.ID ?? rows.find(row=>!children.has(row.ID))?.ID);
 const [collapsed,setCollapsed]=useState(new Set<string>());
 const byId=new Map(rows.map(row=>[row.ID,row]));
 const levels=dependencyStages(rows,dependencies);
 const leaves=rows.filter(row=>!children.has(row.ID));
 const completed=leaves.filter(row=>row.Status==='done').length;
 function visibleId(id:string):string|undefined{
  let current=byId.get(id);let result=current?.ID;const seen=new Set<string>();
  while(current?.Parent&&!seen.has(current.ID)){seen.add(current.ID);current=byId.get(current.Parent);if(current&&collapsed.has(current.ID))result=current.ID;}
  return result;
 }
 function revealTask(id:string){
  if(!connected.has(id))setShowIndependent(true);
  const parents=new Set<string>();let row=byId.get(id);while(row?.Parent&&!parents.has(row.Parent)){parents.add(row.Parent);row=byId.get(row.Parent);}
  setCollapsed(previous=>new Set([...previous].filter(parent=>!parents.has(parent))));setSelected(id);
  requestAnimationFrame(()=>{const node=Array.from(host.current?.querySelectorAll<HTMLElement>('[data-graph-node]')??[]).find(candidate=>candidate.dataset.graphNode===id);node?.scrollIntoView({behavior:'auto',block:'nearest',inline:'nearest'});});
 }
 function toggle(row:PlanTaskRow){
  const folding=!collapsed.has(row.ID);
  if(folding&&selected){let current=byId.get(selected);const seen=new Set<string>();while(current?.Parent&&!seen.has(current.ID)){seen.add(current.ID);if(current.Parent===row.ID){setSelected(row.ID);break;}current=byId.get(current.Parent);}}
  setCollapsed(previous=>{const next=new Set(previous);if(folding)next.add(row.ID);else next.delete(row.ID);return next;});
 }
 const edges=dependencies.filter(edge=>edge.kind!=='suggests'||edge.from===selected||edge.to===selected).flatMap(edge=>{const from=visibleId(edge.from),to=visibleId(edge.to);return from&&to&&from!==to?[{from,to,kind:edge.kind,selected:edge.from===selected||edge.to===selected}]:[];});
 const current=byId.get(selected??'');
 function taskNode(row:PlanTaskRow){const state=taskStanding(row);const childCount=children.get(row.ID)?.length??0;
  return <div className="plan-graph-node-shell" key={row.ID}>
   <HoverPreview disabled={selected===row.ID} title={row.Title} description={row.Note || row.Live?.Command || ''} meta={state.label}><Button className="plan-graph-node" data-graph-node={row.ID} aria-label={`${row.Title}, ${state.label}`} aria-pressed={selected===row.ID} onClick={()=>setSelected(row.ID)}><WorkStateIndicator phase={state.phase} label={state.label}/><span className="plan-graph-node-content"><span className="plan-graph-node-title">{row.Title}</span><span className="plan-graph-node-state">{state.label}</span>{childCount>0&&<span className="plan-graph-node-count">{childCount} parts</span>}</span></Button></HoverPreview>
   {childCount>0&&<IconButton className="plan-graph-node-disclosure" label={`${collapsed.has(row.ID)?'Expand':'Collapse'} subtree ${row.Title}`} icon={collapsed.has(row.ID)?'chevronRight':'chevron'} iconSize="xs" aria-expanded={!collapsed.has(row.ID)} onClick={()=>toggle(row)}/>}
  </div>;
 }
 return <section ref={host} className="plan-graph" aria-label="Task dependency graph"><div className="plan-graph-header"><div className="plan-graph-heading"><SectionHeading>Plan</SectionHeading><Text className="plan-graph-progress">{completed} of {leaves.length} tasks completed</Text></div>{onClose&&<IconButton label="Close plan workspace" icon="close" onClick={onClose}/>}</div><div className="plan-graph-meta"><Text className="plan-graph-source">{sourceLabel}</Text>{independent.length>0&&<Button className="plan-graph-independent" aria-expanded={showIndependent} onClick={()=>setShowIndependent(value=>!value)}>{showIndependent?'Hide':'Show'} {independent.length} independent tasks</Button>}</div>
  <div className="plan-graph-scroll" role="region" aria-label="Pan task graph" tabIndex={0}>
   {!rows.length&&<Text className="plan-graph-empty">No tasks in this plan.</Text>}
   <DependencyMap edges={edges}>{roots.map(root=>{
    const members:PlanTaskRow[]=[];const seen=new Set<string>();const visit=(row:PlanTaskRow)=>{if(seen.has(row.ID))return;seen.add(row.ID);if(row.ID!==root.ID&&visibleId(row.ID)===row.ID&&(showIndependent||connected.has(row.ID)||children.has(row.ID)))members.push(row);(children.get(row.ID)??[]).forEach(visit);};visit(root);
    const stages=[...new Set(members.map(row=>levels.get(row.ID)??0))].sort((a,b)=>a-b);
    return <section className="plan-graph-group" key={root.ID} aria-label={`Task group ${root.Title}`}><div className="plan-graph-group-header">{children.has(root.ID)&&<IconButton label={`${collapsed.has(root.ID)?'Expand':'Collapse'} ${root.Title}`} icon={collapsed.has(root.ID)?'chevronRight':'chevron'} iconSize="xs" aria-expanded={!collapsed.has(root.ID)} onClick={()=>toggle(root)}/>}<Button className="plan-graph-group-title" data-graph-node={root.ID} aria-label={`${root.Title}, ${taskStanding(root).label}`} aria-pressed={selected===root.ID} onClick={()=>setSelected(root.ID)}><WorkStateIndicator phase={taskStanding(root).phase} label={taskStanding(root).label}/>{root.Title}</Button></div><div className="plan-graph-stages">{stages.map(stage=><div className="plan-graph-stage" key={stage}>{members.filter(row=>(levels.get(row.ID)??0)===stage).map(taskNode)}</div>)}</div></section>;
   })}</DependencyMap>
  </div>
  {current&&<section className="plan-graph-detail" aria-label={`Graph details for ${current.Title}`}><div className="plan-graph-detail-header"><SectionHeading>{current.Title}</SectionHeading>{onOpenConversation&&<Button className="plan-graph-open-task" onClick={()=>onOpenConversation(current.ID)}>Ask about this task</Button>}</div><Text>{taskStanding(current).label}{current.Parent&&byId.has(current.Parent)?` · Part of ${byId.get(current.Parent)?.Title}`:''}</Text>{current.Note&&<Text>{current.Note}</Text>}<div className="plan-graph-relations">{(['Prerequisites','Dependents'] as const).map(label=>{const incoming=label==='Prerequisites';const relations=dependencies.filter(edge=>edge.kind!=='suggests'&&(incoming?edge.to===current.ID:edge.from===current.ID));return relations.length?<section key={label} aria-label={label}><Text tone="default">{label}</Text><ul>{relations.map((edge,index)=><li key={index}><Button className="plan-graph-relation" onClick={()=>revealTask(incoming?edge.from:edge.to)}>{byId.get(incoming?edge.from:edge.to)?.Title??(incoming?edge.from:edge.to)}</Button><span className="plan-graph-edge-word">{dependencyWords[edge.kind]}</span></li>)}</ul></section>:null;})}{dependencies.some(edge=>edge.kind==='suggests'&&(edge.from===current.ID||edge.to===current.ID))&&<section aria-label="Suggestions"><Text tone="default">Suggestions</Text><ul>{dependencies.filter(edge=>edge.kind==='suggests'&&(edge.from===current.ID||edge.to===current.ID)).map((edge,index)=><li key={index}>{byId.get(edge.from===current.ID?edge.to:edge.from)?.Title}<span className="plan-graph-edge-word">Advisory</span></li>)}</ul></section>}</div></section>}
 </section>;
}
