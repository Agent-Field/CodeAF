import { useState } from 'react';
import { Button, DropdownMenu, Icon, IconButton, Text } from '../../components/ui';
import type { PlanNavigation } from '../chat/work-model';
import { graphFixture } from './graph-fixture';
import { type PlanDependency, type PlanTaskRow } from './plan-model';
import './task-conversation.css';
import '../../styles/task-conversation.css';

/** Explicit captured-record navigation; no second engine or reconstructed assistant history. */
export function TaskConversation({task,navigation,onNavigate,onHistory,onShowPlan}:{task?:PlanTaskRow;navigation:PlanNavigation;onNavigate:(id:string|null,newTab?:boolean)=>void;onHistory:(index:number)=>void;onShowPlan?:()=>void}){
 const [relationsOpen,setRelationsOpen]=useState(false);
 const ancestors:PlanTaskRow[]=[];const visited=new Set<string>();let parent=task?.Parent;
 while(parent&&!visited.has(parent)){visited.add(parent);const row=graphFixture.rows.find(row=>row.ID===parent);if(!row)break;ancestors.unshift(row);parent=row.Parent;}
 const edges:readonly PlanDependency[]=task?graphFixture.dependencies.filter(edge=>edge.from===task.ID||edge.to===task.ID):[];
 return <>
  <nav className="task-location" aria-label="Task conversation navigation">
   <div className="task-history-controls"><IconButton label="Back to previous task" icon="chevronLeft" iconSize="xs" disabled={navigation.index===0} onClick={()=>onHistory(navigation.index-1)}/><IconButton label="Forward to next task" icon="chevronRight" iconSize="xs" disabled={navigation.index===navigation.ids.length-1} onClick={()=>onHistory(navigation.index+1)}/></div>
   {task&&<div className="task-breadcrumb-menu"><DropdownMenu label="Conversation ancestors" items={[{id:'root-conversation',label:graphFixture.title,onSelect:()=>onNavigate(null)},...ancestors.filter(row=>row.Parent).map(row=>({id:row.ID,label:row.Title,onSelect:()=>onNavigate(row.ID)}))]}><IconButton label="Conversation ancestors" icon="more" iconSize="xs"/></DropdownMenu></div>}
   <ol className="task-breadcrumbs" data-nested={!!task} aria-label="Conversation breadcrumb"><li className="task-breadcrumb-ancestor"><Button aria-current={!task?'page':undefined} onClick={()=>onNavigate(null)}>{graphFixture.title}</Button></li>{ancestors.filter(row=>row.Parent).map(row=><li className="task-breadcrumb-ancestor" key={row.ID}><Icon name="chevronRight" size="xs"/><Button onClick={event=>onNavigate(row.ID,event.metaKey||event.ctrlKey)}>{row.Title}</Button></li>)}{task&&<li><Icon name="chevronRight" size="xs"/><span aria-current="page">{task.Title}</span></li>}</ol>
   {onShowPlan&&<IconButton className="task-show-plan" label="Show task hierarchy" icon="sidebar" iconSize="xs" onClick={onShowPlan}/>}
  </nav>
  {task&&<div className="task-conversation-context">
    {!!edges.length&&<div className="task-record-relations"><Button aria-expanded={relationsOpen} onClick={()=>setRelationsOpen(open=>!open)}><Icon name={relationsOpen?'chevron':'chevronRight'} size="xs"/>Connections · {edges.length}</Button>{relationsOpen&&<ul aria-label="Task dependencies">{edges.map(edge=>{const incoming=edge.to===task.ID;const id=incoming?edge.from:edge.to;const row=graphFixture.rows.find(row=>row.ID===id);return <li key={`${edge.from}-${edge.to}-${edge.kind}`}><Text>{edge.kind==='suggests'?(incoming?'Suggested by':'Suggests'):edge.kind==='blocks'?(incoming?'Blocked by':'Blocks'):(incoming?'Receives from':'Feeds into')}</Text><Button onClick={event=>onNavigate(id,event.metaKey||event.ctrlKey)}>{row?.Title??id}<Icon name="arrow" size="xs"/></Button></li>;})}</ul>}</div>}
  </div>}
 </>;
}
