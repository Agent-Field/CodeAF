import { useEffect, useRef, useState } from 'react';
import { Button, CodeText, Icon, Text } from '../../components/ui';
import type { WorkSection } from './work-model';
import { readToolResult } from './engine-client';

type Step = NonNullable<WorkSection['recordedSteps']>[number];
function readableName(tool: string) {
 return tool.replace(/_/g, ' ').replace(/^./, letter => letter.toUpperCase());
}
function preview(step: Step): {text:string;code:boolean} {
 try {
  const args: unknown = JSON.parse(step.args);
  if (args && typeof args === 'object') {
   const values = args as Record<string, unknown>;
   for (const key of ['query','command','cmd','url','path','file_path','task','title']) {
    if (typeof values[key] === 'string') return {text:values[key],code:['command','cmd','path','file_path'].includes(key)};
   }
  }
 } catch { /* Arguments can be incomplete while a call is being recorded. */ }
 return {text:step.hint && step.hint !== step.tool ? step.hint : '',code:false};
}
function argumentsText(args: string) {
 try { return JSON.stringify(JSON.parse(args), null, 2); } catch { return args; }
}
function ToolPreview({step}:{step:Step}){const value=preview(step);return value.code?<CodeText>{value.text}</CodeText>:<>{value.text}</>;}
export function ToolActivity({ steps, open, onOpen, sessionId }: { steps: Step[]; open: boolean; onOpen: () => void; sessionId?:string }) {
 const [copied, setCopied] = useState<number>();
 const [results,setResults]=useState<Record<string,{output?:string;error?:string;loading?:boolean}>>({});
 const loading=useRef(new Set<string>());
 const currentSession=useRef(sessionId);currentSession.current=sessionId;
 useEffect(()=>{setResults({});loading.current.clear();setCopied(undefined);},[sessionId]);
 async function expand(step:Step) {
  const id=step.callId;
  if(!id || !sessionId || !step.answered || results[id]?.output !== undefined || loading.current.has(id))return;
  const ownSession=sessionId;loading.current.add(id);setResults(value=>({...value,[id]:{loading:true}}));
  try{const result=await readToolResult(sessionId,id);if(currentSession.current===ownSession)setResults(value=>({...value,[id]:{output:result.output}}));}
  catch(error){if(currentSession.current===ownSession)setResults(value=>({...value,[id]:{error:error instanceof Error?error.message:'Could not read the full result.'}}));}
  finally{if(currentSession.current===ownSession)loading.current.delete(id);}
 }
 const pending = steps.filter(step=>!step.answered).length;
 return <div className="tool-activity">
  <Button className="tool-activity-toggle" aria-expanded={open} onClick={onOpen}>
   <Icon name="code" size="xs"/><span>{steps.length} tool {steps.length === 1 ? 'call' : 'calls'}{pending ? ` · ${pending} awaiting result` : ''}</span><Icon name="chevron" size="xs" motion="disclosure"/>
  </Button>
  {open && <div className="tool-activity-list">{steps.map((step,index)=><details className="tool-call" key={step.callId??`${step.tool}-${index}`} onToggle={event=>{if(event.currentTarget.open)void expand(step);}}>
   <summary><Icon name={step.answered ? 'check' : 'code'} size="xs"/><span className="tool-call-name">{readableName(step.tool)}</span><span className="tool-call-preview">{<ToolPreview step={step}/>}</span><Icon name="chevron" size="xs" motion="disclosure"/></summary>
   <div className="tool-call-body">
    {step.args && <details className="tool-call-arguments"><summary>Arguments</summary><pre className="work-code"><CodeText>{argumentsText(step.args)}</CodeText></pre></details>}
    <div className="tool-call-result-heading"><Text>{results[step.callId??'']?.loading?'Reading full result…':step.answered?'Result':'Awaiting result'}</Text>{step.output && <Button onClick={()=>void navigator.clipboard.writeText(results[step.callId??'']?.output??step.output).then(()=>setCopied(index)).catch(()=>setCopied(undefined))}>{copied===index?'Copied':'Copy result'}</Button>}</div>
    {results[step.callId??'']?.error && <Text role="status">{results[step.callId??''].error} Showing the recorded summary.</Text>}
    {(results[step.callId??'']?.output??step.output) ? <pre className="work-code tool-call-result"><CodeText>{results[step.callId??'']?.output??step.output}</CodeText></pre> : <Text>{step.answered?'Finished without text output.':'The engine has not recorded a result yet.'}</Text>}
   </div>
  </details>)}</div>}
 </div>;
}
