import { emptyDocument, type WorkDocument } from '../chat/work-model';
import { capturedTaskPages, graphFixture } from './graph-fixture';
import { taskStanding, type PlanTaskRow } from './plan-model';

export function taskDocumentKey(origin:string,id:string){return `${origin}:task:${id}`;}
/** One captured task identity feeds both the reading pane and its tab previews. */
export function capturedTaskDocument(row:PlanTaskRow):WorkDocument{
 const page=capturedTaskPages[row.ID];
 return {...emptyDocument(),taskDraft:'',activity:{phase:taskStanding(row).phase,recordedAt:Date.parse(graphFixture.capturedAt),sample:true},notice:'Captured task preview · no AI or tools are running.',sections:[{id:`captured-${row.ID}`,title:row.Title,originalFormat:'markdown',original:page?.Description??'No task instruction was captured.',digest:page?.Result??'No result was recorded in this capture.',blocks:[{kind:'paragraph',text:page?.Result??'No result was recorded in this capture.'}],amendments:[],folded:false,originalOpen:true,stepsOpen:false,sample:false}]};
}
