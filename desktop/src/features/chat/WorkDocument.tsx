import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { Button, DropdownMenu, WorkStateIndicator, Icon, IconButton, Text, CodeText, Markdown, type MenuEntry } from '../../components/ui';
import { EngineDecision } from './EngineDecision';
import { ToolActivity } from './ToolActivity';
import { useEngine } from './use-engine';
import { readTaskPage } from './engine-client';
import { WorkComposer } from './WorkComposer';
import { emptyDocument, pricingSample, vendorSample, type WorkBlock, type WorkDocument, type WorkSection } from './work-model';
import { isWorkShortcut } from '../../design/keyboard';
import design from '../../design/tokens.json';
import './work-document.css';
import { PlanOutline } from '../tasks/PlanOutline';
import { capturedTaskDocument, taskDocumentKey } from '../tasks/captured-task-document';
import { TaskConversation } from '../tasks/TaskConversation';
import { graphFixture } from '../tasks/graph-fixture';
import { TaskInspector } from '../tasks/TaskInspector';
import { planPreviewRows, planPreviewPage } from '../tasks/fixtures';
import { taskStanding, type PlanTaskRow, type PlanTaskPage } from '../tasks/plan-model';
import { activityLabel, phaseFor, type WorkPhase } from './activity';
type Props = {taskDocuments?:Record<string,WorkDocument>;onTaskDocumentChange?:(id:string,change:Partial<WorkDocument>,initial:WorkDocument)=>void;capturedTask?:PlanTaskRow;onOpenTaskTab:(id:string)=>void;prototypeRequested?:boolean;tabId:string;title:string;draft:string;onDraft:(value:string)=>void;onExplore:()=>void;resumeTabs:{id:string;title:string;draft:string}[];onResume:(id:string)=>void;document:WorkDocument;onDocumentChange:(change:Partial<WorkDocument>)=>void};
function Blocks({blocks}:{blocks:WorkBlock[]}) {return <>{blocks.map((block,index)=> block.kind==='paragraph'? <Markdown key={index}>{block.text}</Markdown>:block.kind==='code'?<pre className="work-code" key={index}><CodeText>{block.text}</CodeText></pre>:block.kind==='list'?<ul key={index}>{block.items.map((item,i)=><li key={i}>{item}</li>)}</ul>:<div className="work-table-scroll" key={index} role="region" aria-label="Result table" tabIndex={0}><table className="work-table"><thead><tr>{block.headers.map((header,i)=><th key={i}>{header}</th>)}</tr></thead><tbody>{block.rows.map((row,i)=><tr key={i}>{row.map((cell,j)=><td key={j}>{cell}</td>)}</tr>)}</tbody></table></div>)}</>;}
export function WorkDocumentView({taskDocuments,onTaskDocumentChange,capturedTask,onOpenTaskTab,prototypeRequested,tabId,title,draft,onDraft,onExplore,resumeTabs,onResume,document,onDocumentChange}:Props) {
 const viewport=useRef<HTMLDivElement>(null);
 const [planSingle,setPlanSingle]=useState(()=>window.matchMedia(`(max-width: ${design.breakpoints.planSplit}px)`).matches);
 useEffect(()=>{const query=window.matchMedia(`(max-width: ${design.breakpoints.planSplit}px)`);const sync=()=>setPlanSingle(query.matches);query.addEventListener('change',sync);return()=>query.removeEventListener('change',sync);},[]);
 const planTrigger=useRef<HTMLButtonElement>(null);
 const [taskPage,setTaskPage]=useState<PlanTaskPage|null>(null);
 const [pageLoading,setPageLoading]=useState(false);
 const [pageError,setPageError]=useState("");
 const engine=useEngine(tabId,document,onDocumentChange);
 const taskSelection=useRef(0);
 async function selectTask(id:string){
  const selection=++taskSelection.current; setPageError("");
  if(document.taskPreview){setTaskPage(planPreviewPage(id));return;}
  if(!engine.snapshot)return;setPageLoading(true);setTaskPage(null);
  try { const page=await readTaskPage(engine.snapshot.id,id); if(selection===taskSelection.current)setTaskPage(page); }
  catch(error){if(selection===taskSelection.current)setPageError(error instanceof Error?error.message:"Could not read task details.");}
  finally {if(selection===taskSelection.current)setPageLoading(false);}
 }
 const [taskDrawer,setTaskDrawer]=useState(()=>window.matchMedia(`(max-width: ${design.breakpoints.compact}px)`).matches);
 useEffect(()=>{ const query=window.matchMedia(`(max-width: ${design.breakpoints.compact}px)`); const sync=()=>setTaskDrawer(query.matches); query.addEventListener("change",sync); return()=>query.removeEventListener("change",sync); },[]);
 useEffect(()=>{taskSelection.current++;setTaskPage(null);setPageLoading(false);setPageError("");},[tabId]);
 const editor=useRef<HTMLTextAreaElement>(null);
 const fileInput=useRef<HTMLInputElement>(null);
 const dock=useRef<HTMLDivElement>(null);
 const freshDockTop=useRef<number|null>(null);
 const glideAfterSend=useRef(false);
 const documentRef=useRef(document);documentRef.current=document;
 const update=onDocumentChange;
 function activity(phase:WorkPhase,sample=true){return {phase,recordedAt:Date.now(),sample};}
 function stop(){if(document.engine){void engine.stop();return;}update({running:false,stopped:true,activity:activity('stopped'),notice:''});}
 useLayoutEffect(()=>{if(viewport.current) viewport.current.scrollTop=documentRef.current.scroll;},[tabId]);
 const fresh=!document.sections.length;
 useLayoutEffect(()=>{
  const element=dock.current;if(!element)return;
  if(fresh){freshDockTop.current=element.getBoundingClientRect().top;return;}
  if(!glideAfterSend.current)return;
  glideAfterSend.current=false;
  if(window.matchMedia('(prefers-reduced-motion: reduce)').matches||freshDockTop.current===null)return;
  const offset=freshDockTop.current-element.getBoundingClientRect().top;
  const css=getComputedStyle(element);
  const animation=element.animate([{transform:`translateY(${offset}px)`},{transform:`translateY(${design.foundation['motion-rest']})`}],{duration:parseFloat(css.getPropertyValue('--duration-work')),easing:css.getPropertyValue('--ease-layout').trim()});
  return()=>animation.cancel();
 },[fresh,tabId]);
 function amend(sectionId:string,change:Partial<WorkSection>){update({sections:document.sections.map(section=>section.id===sectionId?{...section,...change}:section)});}
 function foldAll(){update({sections:document.sections.map(section=>({...section,folded:true,originalOpen:false}))});}
 useEffect(()=>{
  const onKey=(event:KeyboardEvent)=>{
   if(event.defaultPrevented||event.isComposing||window.document.querySelector('dialog[open],.app-menu[data-state="open"],.select-menu[data-state="open"]')||editor.current?.closest('[hidden]'))return;
   const action=isWorkShortcut(event);
   if(event.key==='Escape'&&document.running){event.preventDefault();stop();return;}
   if(action==='focus'){event.preventDefault();editor.current?.focus();}
   if(action==='fold'){event.preventDefault();foldAll();}
   if(action==='preset'){event.preventDefault();update({preset:document.preset==='auto'?'fast':document.preset==='fast'?'thorough':'auto'});}
  };
  window.addEventListener('keydown',onKey);return()=>window.removeEventListener('keydown',onKey);
 },[tabId,document]);
 function submit(action:'send'|'steer'|'queue'|'restart'):boolean {
  if(!draft.trim()&&!document.context.length)return false;
  if(document.engine){
   if(document.context.some(item=>item.text===undefined)){update({notice:'Only text context is supported by the engine bridge. Your draft and attachments are preserved.'});return false;}
   const text=[draft,...document.context.map(item=>`Attached context: ${item.name}\n${item.text}`)].filter(Boolean).join('\n\n');
   const submittedDraft=draft;glideAfterSend.current=fresh;
   void engine.send(text,action==='queue'?'queue':action==='steer'?'steer':'submit').then(accepted=>{if(accepted){if(editor.current?.value===submittedDraft)onDraft('');update({context:[]});}});
   return false;
  }
  if(action==='queue'){
   if(document.context.length){update({notice:'Queue accepts words alone. Your draft and context are preserved.'});return false;}
   update({queue:[...document.queue,draft]});onDraft('');return true;
  }
  const current=document.sections[document.sections.length-1];
  if((action==='steer'||action==='send')&&current){
   update({sections:document.sections.map(section=>section.id===current.id?{...section,folded:false,amendments:[...section.amendments,{text:draft,receipt:'Staged locally · no request made'}],context:[...(section.context??[]),...document.context.map(c=>({name:c.name,text:c.text}))]}:section),context:[],decision:false,stopped:false,activity:document.running?document.activity:activity('staged',false),notice:''});
  }else{
   glideAfterSend.current=fresh;
   const section:WorkSection={id:crypto.randomUUID(),title:`Instruction ${document.sections.length+1}`,original:draft,digest:'Instruction staged locally; no request made.',blocks:[{kind:'paragraph',text:'Instruction staged locally; no request made.'}],amendments:[],folded:false,originalOpen:false,stepsOpen:false,sample:false,context:document.context.map(c=>({name:c.name,text:c.text}))};
   update({sections:[...document.sections.map(s=>({...s,folded:true,originalOpen:false})),section],context:[],decision:false,stopped:false,activity:document.running?document.activity:activity('staged',false),notice:''});
  }
  onDraft('');return true;
 }
 function load(sample:WorkDocument){update({...sample,scroll:0,preset:document.preset,rememberPreset:document.rememberPreset,model:document.model,effort:document.effort,context:document.context,activity:activity('completed'),notice:'Named sample · no AI or tools are running.'});if(viewport.current)viewport.current.scrollTop=0;}
 function preview(phase:WorkPhase){
  const base=fresh?pricingSample():document;
  update({...base,preset:document.preset,rememberPreset:document.rememberPreset,model:document.model,effort:document.effort,context:document.context,
   running:phase==='working'||phase==='streaming',decision:phase==='waiting',stopped:phase==='stopped',activity:activity(phase),
   notice:phase==='failed'?'Failure sample · no external request failed.':phase==='streaming'?'Receiving reply sample · no stream is connected.':'Named state sample · no AI or tools are running.'});
 }
 function loadPlanWorkspace(){load({...pricingSample(),planWorkspacePreview:true,planWorkspaceView:'conversation',planNavigation:{ids:[null],index:0},taskInspectorOpen:true,sections:[{id:'plan-conversation',title:'Review the execution plan',original:'Review this captured plan and its tasks.',digest:graphFixture.description,blocks:[{kind:'paragraph',text:`### ${graphFixture.title}\n\n${graphFixture.description}\n\nOpen a task in the plan to read its captured record. Breadcrumbs return to the parent conversation; Command-click on Mac or Control-click on Linux opens a separate tab.`}],amendments:[],folded:false,originalOpen:false,stepsOpen:false,sample:false}]});}
 useEffect(()=>{if(prototypeRequested&&fresh&&!document.engine&&!document.planWorkspacePreview)loadPlanWorkspace();},[tabId,prototypeRequested]);
 const actions:MenuEntry[]=[
  {id:'plan-workspace',label:'Preview plan workspace',onSelect:loadPlanWorkspace},
  {id:'pricing',label:'Load Pricing research sample',onSelect:()=>load(pricingSample())},
  {id:'vendor',label:'Load Vendor reconcile sample',onSelect:()=>load(vendorSample())},
  {kind:'separator',id:'states'},
  {id:'streaming',label:'Preview receiving reply',checked:phaseFor(document)==='streaming',onSelect:()=>preview('streaming')},
  {id:'running',label:'Preview working state',checked:phaseFor(document)==='working',onSelect:()=>preview('working')},
  {id:'decision',label:'Preview decision state',checked:document.decision,onSelect:()=>preview('waiting')},
  {id:'completed',label:'Preview completed state',checked:phaseFor(document)==='completed',onSelect:()=>preview('completed')},
  {id:'failed',label:'Preview failed state',checked:phaseFor(document)==='failed',onSelect:()=>preview('failed')},
  {id:'ready',label:'Preview ready state',onSelect:()=>update({running:false,stopped:false,decision:false,activity:document.sections.length?activity('completed'):undefined,notice:''})},
  {id:'fold',label:'Fold all sections',shortcut:'⌘/Ctrl ⇧ F',disabled:fresh,onSelect:foldAll},
  {id:'task-plan',label:'Preview task plan',onSelect:()=>update({taskPreview:true,taskInspectorOpen:true})},
  {id:'foundation',label:'Explore the foundation',onSelect:onExplore}
 ];
 const planRows=document.taskPreview?planPreviewRows:(engine.snapshot?.tasks.filter(row=>!row.Archived)??[]);
 const ledger=document.sections.filter(section=>section.folded).length>=design.interaction.workLedgerThreshold;
 const suggestions=document.sections.some(section=>section.id==='sample-reconcile')?['Ask Northwind for the PO number','Review the exceptions']:document.sections.some(section=>section.sample)?['Break down by plan tier','Export the table','Propose tiers']:[];
 const navigation=document.planNavigation??{ids:[null],index:0};
 const selectedTask=document.planWorkspacePreview?graphFixture.rows.find(row=>row.ID===navigation.ids[navigation.index]):undefined;
 function navigateTask(id:string|null,newTab=false){
  if(newTab&&id){onOpenTaskTab(id);return;}
  if(id===navigation.ids[navigation.index])return;
  update({planNavigation:{ids:[...navigation.ids.slice(0,navigation.index+1),id],index:navigation.index+1},planWorkspaceView:'conversation'});
 }
 const taskDocumentId=selectedTask?taskDocumentKey(document.planOriginTabId??tabId,selectedTask.ID):'';
 const capturedDocument=selectedTask?capturedTaskDocument(selectedTask):emptyDocument();
 const taskDocument=taskDocuments?.[taskDocumentId]??capturedDocument;
 const showGraph=!!document.planWorkspacePreview&&!!document.taskInspectorOpen;
 const graphOnly=showGraph&&planSingle&&(document.planWorkspaceView??'plan')==='plan';
 return <div className="work-conversation" data-plan-workspace={!!document.planWorkspacePreview}>
 {document.planWorkspacePreview&&planSingle&&<div className="work-plan-views" role="group" aria-label="Plan workspace views"><Button aria-pressed={!graphOnly} onClick={()=>update({planWorkspaceView:'conversation'})}>Conversation</Button><Button aria-pressed={graphOnly} onClick={()=>update({planWorkspaceView:'plan',taskInspectorOpen:true})}>Plan</Button></div>}
 <div className="work-document" data-fresh={fresh&&!selectedTask} hidden={graphOnly}>
 {document.planWorkspacePreview&&<TaskConversation key={`${tabId}:${selectedTask?.ID??"root"}`} onShowPlan={!planSingle&&!showGraph?()=>update({taskInspectorOpen:true}):undefined} task={selectedTask} navigation={navigation} onNavigate={navigateTask} onHistory={index=>update({planNavigation:{...navigation,index},planWorkspaceView:'conversation'})}/>}
 {selectedTask&&<div className="work-task-conversation" role="region" aria-label="Captured task conversation"><WorkDocumentView key={taskDocumentId} capturedTask={selectedTask} onOpenTaskTab={onOpenTaskTab} tabId={taskDocumentId} title={selectedTask.Title} draft={taskDocument.taskDraft??''} onDraft={taskDraft=>onTaskDocumentChange?.(taskDocumentId,{taskDraft},capturedDocument)} document={taskDocument} onDocumentChange={change=>onTaskDocumentChange?.(taskDocumentId,change,capturedDocument)} onExplore={onExplore} resumeTabs={[]} onResume={onResume}/></div>}
 <div className="work-root-conversation" hidden={!!selectedTask}>
  <div className="work-document-scroll" key={tabId} ref={viewport} role="region" aria-label="Work document" tabIndex={0} onScroll={event=>{const scroll=event.currentTarget.scrollTop;if(Math.abs(documentRef.current.scroll-scroll)>parseFloat(design.foundation['border-width']))update({scroll});}}>
   {capturedTask&&<div className="work-captured-state"><WorkStateIndicator phase={taskStanding(capturedTask).phase} label={taskStanding(capturedTask).label}/><Text>{taskStanding(capturedTask).label} · captured task record</Text></div>}
   {document.sections.map(section=><section className="work-section" key={section.id} aria-label={`Work section ${section.title}`} data-folded={section.folded} data-ledger={ledger&&section.folded}>
    <div className="work-ask-row"><Button className="work-ask" aria-expanded={!section.folded} aria-controls={`result-${section.id}`} aria-label={`${section.folded?'Open':'Fold'} ${section.title}`} onClick={()=>amend(section.id,{folded:!section.folded})}><Icon name="chevronRight" size="xs"/><span>{section.title}</span></Button>{section.amendments.length>0&&<Text className="work-amended">amended ×{section.amendments.length}</Text>}</div>
    {section.folded&&<Text className="work-digest">{section.digest}</Text>}<div className="work-section-body" id={`result-${section.id}`} hidden={section.folded}>
     <Button className="work-provenance-toggle" aria-expanded={section.originalOpen} aria-controls={`instruction-${section.id}`} aria-label={`${section.originalOpen?'Hide':'Show'} original instruction for ${section.title}`} onClick={()=>amend(section.id,{originalOpen:!section.originalOpen})}>{section.originalOpen?'Hide instruction':section.originalFormat==='markdown'?'Task instruction':'Original instruction'}</Button>
     <div className="work-provenance" role="group" hidden={!section.originalOpen} aria-label={`Original instructions for ${section.title}`} id={`instruction-${section.id}`}>{section.originalFormat==='markdown'?<Markdown>{section.original}</Markdown>:<Text>{section.original}</Text>}{section.context?.map((context,i)=><details key={i}><summary>{context.name}</summary><Text>{context.text??'File metadata retained in this local preview.'}</Text></details>)}{section.amendments.map((amendment,i)=><Text key={i}>{amendment.text}</Text>)}</div>
     <div className="work-output" aria-busy={document.running&&section.id===document.sections[document.sections.length-1]?.id || undefined}>
      {section.timeline?.length ? section.timeline.map((item,index)=>{
       if(item.kind==='tool') {
        if(index>0&&section.timeline?.[index-1].kind==='tool')return null;
        const steps=[];for(let position=index;position<(section.timeline?.length??0)&&section.timeline?.[position].kind==='tool';position++){const step=section.recordedSteps?.[section.timeline[position].index];if(step)steps.push(step);}
        const group=steps[0]?.callId??`group-${index}`;const opened=section.toolGroupsOpen??[];
        return <ToolActivity key={`tools-${index}`} sessionId={engine.snapshot?.id} steps={steps} open={opened.includes(group)} onOpen={()=>amend(section.id,{toolGroupsOpen:opened.includes(group)?opened.filter(key=>key!==group):[...opened,group]})}/>;
       }
       if(item.kind==='note')return <Text className="work-engine-note" key={`note-${index}`}>{section.engineNotes?.[item.index]}</Text>;
       const block=section.blocks[item.index];return block?<Blocks key={`reply-${index}`} blocks={[block]}/>:null;
      }) : <><Blocks blocks={section.blocks}/>{section.engineNotes?.map((note,index)=><Text className="work-engine-note" key={`note-${index}`}>{note}</Text>)}{!!section.recordedSteps?.length&&<ToolActivity sessionId={engine.snapshot?.id} steps={section.recordedSteps} open={section.stepsOpen} onOpen={()=>amend(section.id,{stepsOpen:!section.stepsOpen})}/>}</>}
     </div>
     {section.amendments.length>0&&<div className="work-amendments">{section.amendments.map((amendment,i)=><Text key={i}><span className="work-amendment-word">{amendment.text}</span><span> — {amendment.receipt}</span></Text>)}</div>}
     {section.sample&&<><Button className="work-steps-toggle" aria-expanded={section.stepsOpen} onClick={()=>amend(section.id,{stepsOpen:!section.stepsOpen})}>steps</Button>{section.stepsOpen&&<div className="work-steps"><Text>Sample trace</Text><ol><li>Read source material</li><li>Compare the findings</li><li>Prepare the result</li></ol></div>}</>}
    </div>
   </section>)}
  </div>
  <div className="work-document-dock" ref={dock}>

   {document.queue.length>0&&<div className="work-queue" role="group" aria-label="Queued preview messages">{document.queue.map((message,index)=><div key={index}><Text>{message}</Text><Button onClick={()=>{onDraft(draft?`${draft}\n${message}`:message);update({queue:document.queue.filter((_,i)=>i!==index)});editor.current?.focus();}}>Edit</Button><IconButton label={`Remove queued message ${index+1}`} icon="close" iconSize="xs" onClick={()=>update({queue:document.queue.filter((_,i)=>i!==index)})}/></div>)}</div>}
   {engine.snapshot?.questions?.map(question=><EngineDecision key={`${tabId}:${question.kind}:${question.id}:${question.ref??''}`} question={question} busy={engine.answering} error={engine.error} onAnswer={engine.answer}/>)}
   {document.decision&&<div className="work-decision"><Text tone="default">Which currency should the table and chart use?</Text><div>{['USD','EUR','Show both'].map(answer=><Button key={answer} variant="secondary" onClick={()=>update({decision:false,activity:activity('completed'),notice:`Decision recorded in this sample: ${answer}. No request made.`})}>{answer}</Button>)}</div><Text>or type · waiting on you · sample</Text></div>}
   {document.stopped&&!document.engine&&<div className="work-recovery"><Text>Stopped in this preview · partial result kept</Text><Button onClick={()=>update({running:true,stopped:false,activity:activity('working'),notice:'Resume preview · no job started.'})}>Resume</Button><Button onClick={()=>update({stopped:false,activity:activity('stopped'),notice:'Partial sample kept.'})}>Keep the result</Button></div>}
   {phaseFor(document)==='failed'&&!document.engine&&<div className="work-recovery" role="status"><Text>Sample request failed · your draft and partial result are kept</Text><Button onClick={()=>update({running:true,activity:activity('working'),notice:'Retry preview · no request made.'})}>Retry sample</Button></div>}
   {document.engine&&document.stopped&&<Text className="work-status">Stopped · partial result kept. Send an instruction to continue.</Text>}
   <WorkComposer status={document.running?`${activityLabel(document)} · type to steer`:undefined} tabId={tabId} title={title} draft={draft} onDraft={onDraft} running={document.running} fresh={fresh} preset={document.preset} onPreset={preset=>update({preset})} rememberPreset={document.rememberPreset} onRememberPreset={rememberPreset=>update({rememberPreset})} model={document.model} effort={document.effort} onModel={model=>update({model})} onEffort={effort=>update({effort})} context={document.context} onContext={context=>update({context})} onAction={submit} onStop={stop} fixedModelLabel={document.engine?"DeepSeek v4.1 Flash":undefined} disabled={engine.connecting} suggestions={document.decision?[]:suggestions} editorRef={editor} fileInputRef={fileInput}/>
   {fresh&&<div className="work-resume" role="group" aria-label="Resumable local tabs">{resumeTabs.filter(tab=>tab.draft.trim()).slice(0,3).map(tab=><Button key={tab.id} onClick={()=>onResume(tab.id)}>Continue “{tab.title}”<Icon name="arrow" size="xs"/></Button>)}<Button onClick={()=>fileInput.current?.click()}>Start from a file…<Icon name="plus" size="xs"/></Button></div>}
   <div className="work-preview-footer">{!capturedTask&&<Button className="work-plan-trigger" aria-expanded={!!document.taskInspectorOpen} aria-label="Plan" aria-description={planRows.length?`${planRows.length} tasks`:undefined} onClick={event=>{planTrigger.current=event.currentTarget;update({taskInspectorOpen:!document.taskInspectorOpen,...(document.planWorkspacePreview?{planWorkspaceView:'plan' as const}: {})});}}>Plan{planRows.length?` · ${planRows.length}`:""}<Icon name="sidebar" size="xs"/></Button>}<Text className="work-preview-notice" role="status">{engine.error || document.notice}</Text>{!engine.online&&!document.planWorkspacePreview&&!capturedTask&&<Button className="work-engine-connect" loading={engine.connecting} onClick={()=>void engine.connect()}>{engine.connecting?"Connecting…":document.engine?"Reconnect engine":"Connect engine"}</Button>}{engine.online&&<Text className="work-engine-status">Engine connected</Text>}{!document.engine&&!capturedTask&&<DropdownMenu label="Work preview" items={actions}><Button className="work-preview-label">UI preview<Icon name="chevron" size="xs"/></Button></DropdownMenu>}</div>
  </div>
 </div></div>{document.planWorkspacePreview?<div className="work-plan-outline" hidden={!showGraph||(planSingle&&!graphOnly)}><PlanOutline key={tabId} scopeId={selectedTask?.ID} rows={graphFixture.rows} dependencies={graphFixture.dependencies} selectedId={selectedTask?.ID} sourceLabel="Captured plan · read-only" onClose={()=>update({taskInspectorOpen:false,planWorkspaceView:'conversation'})} onNavigate={navigateTask}/></div>:<TaskInspector conversationId={tabId} open={!!document.taskInspectorOpen} rows={planRows} planError={engine.snapshot?.planError} sourceLabel={document.taskPreview?"Task plan preview · no work is running":document.engine?"Current conversation plan":undefined} page={taskPage} pageLoading={pageLoading} pageError={pageError} onSelectTask={id=>void selectTask(id)} presentation={taskDrawer?"drawer":"inline"} returnFocus={planTrigger} onClose={()=>update({taskInspectorOpen:false})}/>}</div>;
}
