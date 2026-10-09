import type { WorkActivity } from './activity';
export type WorkBlock = { kind: 'paragraph'; text: string } | { kind: 'code'; text: string } | { kind: 'list'; items: string[] } | { kind: 'table'; headers: string[]; rows: string[][] };
export type WorkTimelineItem = { kind: 'text' | 'tool' | 'note'; index: number };
export type WorkSection = { originalFormat?:'literal'|'markdown'; toolGroupsOpen?: string[]; timeline?: WorkTimelineItem[]; recordedSteps?: {callId?:string;tool:string;hint:string;args:string;output:string;answered:boolean}[]; engineNotes?: string[]; id: string; title: string; original: string; digest: string; blocks: WorkBlock[]; amendments: { text: string; receipt: string }[]; folded: boolean; originalOpen: boolean; stepsOpen: boolean; sample: boolean; context?: { name: string; text?: string }[] };
export type EngineBinding = { sessionFile: string; workspace: string; model: string };
export type PlanNavigation = { ids: (string|null)[]; index: number };
export type WorkDocument = { taskDraft?:string; planOriginTabId?:string; planNavigation?: PlanNavigation; planWorkspacePreview?: boolean; planWorkspaceView?: 'conversation'|'plan'; engineTitle?: string; engine?: EngineBinding; taskInspectorOpen?: boolean; taskPreview?: boolean; activity?: WorkActivity; sections: WorkSection[]; running: boolean; stopped: boolean; decision: boolean; preset: 'auto'|'fast'|'thorough'|'custom'; rememberPreset: boolean; model: string; effort: string; context: {id:string;name:string;kind:'file'|'paste';text?:string}[]; queue:string[]; scroll: number; notice: string };
export const documentKey = 'codeaf.desktop.work-documents.v1';
export function emptyDocument(): WorkDocument { return { sections: [], running:false,stopped:false,decision:false,preset:'auto',rememberPreset:true,model:'codeaf',effort:'auto',context:[],queue:[],scroll:0,notice:'' }; }
export function readDocuments(): Record<string,WorkDocument> {
 try {
  const value:unknown=JSON.parse(localStorage.getItem(documentKey)??'{}');
  if (!value || typeof value!=='object' || Array.isArray(value)) return {};
  const result:Record<string,WorkDocument>={};
  for (const [id, raw] of Object.entries(value)) {
   if (!raw || typeof raw!=='object') continue;
   const d=raw as WorkDocument;
   if (!Array.isArray(d.sections) || !Array.isArray(d.context) || !Array.isArray(d.queue) || !d.queue.every(x=>typeof x==='string')) continue;
   if (!['auto','fast','thorough','custom'].includes(d.preset)||typeof d.rememberPreset!=='boolean'||typeof d.model!=='string'||typeof d.effort!=='string'||typeof d.running!=='boolean'||typeof d.stopped!=='boolean'||typeof d.decision!=='boolean'||!Number.isFinite(d.scroll)||d.scroll<0||typeof d.notice!=='string') continue;
   if (d.planWorkspacePreview !== undefined && typeof d.planWorkspacePreview !== 'boolean' || d.planWorkspaceView !== undefined && !['conversation','plan'].includes(d.planWorkspaceView)) continue;
   if (d.taskDraft!==undefined && typeof d.taskDraft!=='string' || d.planOriginTabId!==undefined && typeof d.planOriginTabId!=='string') continue;
   if (d.planNavigation && (!Array.isArray(d.planNavigation.ids) || !d.planNavigation.ids.length || d.planNavigation.ids.some(id=>id!==null && typeof id!=='string') || !Number.isInteger(d.planNavigation.index) || d.planNavigation.index<0 || d.planNavigation.index>=d.planNavigation.ids.length)) continue;
   if (d.engineTitle !== undefined && typeof d.engineTitle !== 'string') continue;
   if (d.sections.some(s=>s.originalFormat!==undefined && !['literal','markdown'].includes(s.originalFormat))) continue;
   if (d.sections.some(s=>s.toolGroupsOpen && (!Array.isArray(s.toolGroupsOpen) || !s.toolGroupsOpen.every(key=>typeof key==='string')))) continue;
   if (d.sections.some(s=>s.timeline && (!Array.isArray(s.timeline) || !s.timeline.every(item=>item && ['text','tool','note'].includes(item.kind) && Number.isSafeInteger(item.index) && item.index >= 0)))) continue;
   if (d.engine && (typeof d.engine.sessionFile !== 'string' || !d.engine.sessionFile || typeof d.engine.workspace !== 'string' || d.engine.model !== 'deepseek/deepseek-v4.1-flash')) continue;
   if (d.taskInspectorOpen !== undefined && typeof d.taskInspectorOpen !== 'boolean' || d.taskPreview !== undefined && typeof d.taskPreview !== 'boolean') continue;
   if (d.activity && (!['streaming','working','waiting','completed','stopped','failed','staged'].includes(d.activity.phase) || !Number.isFinite(d.activity.recordedAt) || d.activity.recordedAt < 0 || typeof d.activity.sample !== 'boolean')) continue;
   if (d.sections.some(s=>s.recordedSteps && (!Array.isArray(s.recordedSteps) || !s.recordedSteps.every(step=>step && (step.callId===undefined||typeof step.callId==='string') && typeof step.tool==='string' && typeof step.hint==='string' && typeof step.args==='string' && typeof step.output==='string' && typeof step.answered==='boolean')) || s.engineNotes && (!Array.isArray(s.engineNotes) || !s.engineNotes.every(note=>typeof note==='string')))) continue;
   if (!d.sections.every(s=>s&&typeof s.id==='string'&&typeof s.title==='string'&&typeof s.original==='string'&&typeof s.digest==='string'&&typeof s.folded==='boolean'&&typeof s.originalOpen==='boolean'&&typeof s.stepsOpen==='boolean'&&typeof s.sample==='boolean'&&Array.isArray(s.amendments)&&s.amendments.every(a=>a&&typeof a.text==='string'&&typeof a.receipt==='string')&&Array.isArray(s.blocks)&&s.blocks.every(b=>b&&(['paragraph','code'].includes(b.kind)?'text' in b&&typeof b.text==='string':b.kind==='list'?Array.isArray(b.items)&&b.items.every(x=>typeof x==='string'):b.kind==='table'&&Array.isArray(b.headers)&&b.headers.every(x=>typeof x==='string')&&Array.isArray(b.rows)&&b.rows.every(row=>Array.isArray(row)&&row.every(x=>typeof x==='string'))))&&(!s.context||Array.isArray(s.context)&&s.context.every(c=>c&&typeof c.name==='string'&&(c.text===undefined||typeof c.text==='string'))))) continue;
   if (!d.context.every(c=>c&&typeof c.id==='string'&&typeof c.name==='string'&&['file','paste'].includes(c.kind)&&(c.text===undefined||typeof c.text==='string'))) continue;
   if (new Set(d.sections.map(section=>section.id)).size!==d.sections.length || ['__proto__','constructor','prototype'].includes(id)) continue;
   result[id]=d;
  }
  return result;
 } catch { return {}; }
}
function section(id:string,title:string,original:string,digest:string,blocks:WorkBlock[],folded=false):WorkSection { return {id,title,original,digest,blocks,amendments:[],folded,originalOpen:false,stepsOpen:false,sample:true}; }
export function pricingSample():WorkDocument {
 return {...emptyDocument(), sections:[
  section('sample-shortlist','Shortlist of direct competitors','Help me set Q4 pricing tiers. Start with who we compete with.','14 vendors with self-serve plans; Plane swapped for ClickUp.',[{kind:'paragraph',text:'14 vendors with self-serve plans; Plane swapped for ClickUp.'}],true),
  {...section('sample-table','Public pricing table','Pull everyone’s public pricing into one table. Use monthly list prices.','13 of 14 priced, 1 login-gated skipped.',[{kind:'paragraph',text:'Done: 13 of 14 priced. Monday is login-gated, so it is skipped and marked. Everything is normalised to per-seat monthly from the list price.'}],true),amendments:[{text:'Skip login-gated prices.',receipt:'Sample amendment retained'}]},
  {...section('sample-bands','Price bands in EUR','Chart the price bands, and tell me where we sit. Use list prices only, ignore annual discounts. We’re at $12/seat on Pro; don’t count the free tier. If a vendor has usage-based pricing, estimate at 10 seats and flag it as an estimate.','You’re €11, mid-band, in the most crowded set.',[
   {kind:'paragraph',text:'Three bands. Under €8/seat: 5 vendors. €8–15: 6. Above €15: 3. You’re at €11, mid-band, in the most crowded set. Both usage-based vendors, estimated at 25 seats, land mid-band.'},
   {kind:'table',headers:['Price band','Vendors','Your position'],rows:[['Under €8','5',''],['€8–15','6','Pro · €11'],['Above €15','3','']]},
   {kind:'paragraph',text:'The estimates don’t change the picture. Both land mid-band.'}
  ]),amendments:[{text:'actually, 25 seats',receipt:'Sample estimate updated'},{text:'and show it in EUR',receipt:'Sample currency updated'}]}
 ]};
}
export function vendorSample():WorkDocument { return {...emptyDocument(),sections:[section('sample-reconcile','Reconcile vendor invoices against POs','Reconcile this week’s vendor invoices against POs. Flag anything without a purchase order.','All 23 matched except three.',[
 {kind:'paragraph',text:'All 23 invoices matched except three. Two are quantity mismatches under $50; one (Northwind, $4,120) has no PO at all.'},
 {kind:'table',headers:['Vendor','Exception','Amount'],rows:[['Northwind','Missing PO','$4,120'],['Contoso','Quantity mismatch','$34'],['Fabrikam','Quantity mismatch','$48']]}
 ])]}; }
