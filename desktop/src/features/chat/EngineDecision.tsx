import { useState } from 'react';
import { Button, SectionHeading, Select, Text, TextInput } from '../../components/ui';
import type { EngineAnswer, EngineQuestion, EngineQuestionBlock } from './engine-client';
export type EngineDecisionProps = { question: EngineQuestion; busy?: boolean; error?: string; onAnswer: (answer: EngineAnswer) => Promise<boolean> };
function Evidence({ block }: { block: EngineQuestionBlock }) {
 return <div className="engine-question-evidence">{block.title && <Text tone="default">{block.title}</Text>}{block.body && (['diff','diagram','layout'].includes(block.kind) ? <pre className="engine-question-code">{block.body}</pre> : <Text>{block.body}</Text>)}{block.kind === 'table' && block.rows?.length && <div className="engine-question-table-scroll"><table><thead><tr>{block.rows[0].map((cell,index)=><th key={index}>{cell}</th>)}</tr></thead><tbody>{block.rows.slice(1).map((row,index)=><tr key={index}>{row.map((cell,column)=><td key={column}>{cell}</td>)}</tr>)}</tbody></table></div>}{block.path && <Text>Evidence: {block.path}</Text>}</div>;
}
/** The engine supplies every choice and its identity. Rendering never grants approval automatically. */
export function EngineDecision({ question, busy = false, error, onAnswer }: EngineDecisionProps) {
 const [change, setChange] = useState('');
 const [picked, setPicked] = useState<string[]>([]);
 const [scope, setScope] = useState('once');
 const [blanks, setBlanks] = useState<Record<string,string>>(() => Object.fromEntries((question.input?.blanks ?? []).map(blank => [blank.label, blank.default ?? ''])));
 const [submitting, setSubmitting] = useState(false);
 const [localError, setLocalError] = useState('');
 const options = question.options ?? [];
 const unsupported = ['pairs','dial'].includes(question.input?.kind ?? '') || [...(question.attach ?? []), ...options.flatMap(option => option.blocks ?? [])].some(block => !['text','diagram','diff','layout','table'].includes(block.kind));
 const checklist = question.input?.kind === 'checklist';
 async function answer(key: string) {
  if (busy || submitting || unsupported) return;
  const keys = checklist ? picked : key ? [key] : [];
  setSubmitting(true); setLocalError('');
  try {
   await onAnswer({ kind:question.kind, id:question.id, ...(question.ref ? {ref:question.ref} : {}), key:keys[0] ?? '', ...(keys.length ? {picked:keys} : {}), ...(change ? {change} : {}), ...(question.input?.kind === 'blanks' ? {blanks} : {}), ...(scope && scope !== 'once' ? {scope} : {}) });
  } catch { setLocalError('The answer could not be sent. Your words are preserved.'); }
  finally { setSubmitting(false); }
 }
 const locked = busy || submitting || unsupported;
 return <section className="engine-decision" aria-label={question.head} aria-busy={busy || submitting || undefined}>
  <SectionHeading>{question.head}</SectionHeading>{question.reason && <Text>{question.reason}</Text>}
  {question.attach?.map((block,index)=><Evidence key={index} block={block}/>)}
  {question.input?.kind === 'blanks' && question.input.blanks?.map(blank => <label key={blank.label} className="engine-question-field"><Text tone="default">{blank.label}</Text>{blank.kind === 'choice' ? <Select disabled={locked} label={blank.label} value={blanks[blank.label] ?? ''} onValueChange={value=>setBlanks(previous=>({...previous,[blank.label]:value}))} options={(blank.choices ?? []).map(value=>({value,label:value}))}/> : <TextInput aria-label={blank.label} type={blank.kind === 'number' ? 'number' : question.input?.secret ? 'password' : 'text'} value={blanks[blank.label] ?? ''} disabled={locked} onChange={event=>setBlanks(previous=>({...previous,[blank.label]:event.target.value}))}/>}</label>)}
  {options.map(option=><div key={option.key} className="engine-question-option" data-widening={option.widening || undefined}>
   {option.body && <Text>{option.body}</Text>}{option.consequence && <Text>{option.consequence}</Text>}{option.blocks?.map((block,index)=><Evidence key={index} block={block}/>)}
   {option.widening && <Text className="engine-question-widening">Broader permission</Text>}
   <Button variant="secondary" disabled={locked} aria-pressed={checklist ? picked.includes(option.key) : undefined} onClick={()=>checklist ? setPicked(previous=>previous.includes(option.key) ? previous.filter(key=>key!==option.key) : [...previous,option.key]) : void answer(option.key)}>{option.label}</Button>
  </div>)}
  {!unsupported && <div className="engine-question-input"><TextInput aria-label={question.input?.prompt || 'Add words to your answer'} placeholder={question.input?.prompt || 'Add context, if needed…'} type={question.input?.secret ? 'password' : 'text'} autoComplete="off" value={change} disabled={locked} onChange={event=>setChange(event.target.value)} onKeyDown={event=>{if(event.key==='Enter'&&!event.nativeEvent.isComposing&&!options.length){event.preventDefault();void answer('');}}}/>{(checklist || !options.length) && <Button variant="secondary" disabled={locked || (checklist ? !picked.length : !change.trim() && question.input?.kind !== 'blanks')} onClick={()=>void answer('')}>Send answer</Button>}</div>}
  {!!question.scope?.length && <Select disabled={locked} label="Answer scope" value={scope} onValueChange={setScope} options={[{value:'once',label:'This request'},...question.scope.filter(value=>value!=='once').map(value=>({value,label:value}))]}/>}
  {unsupported && <Text role="status">This question needs evidence or controls not yet available here. Open this conversation in the TUI to answer it.</Text>}
  {(error || localError) && <Text role="status">{error || localError}</Text>}
 </section>;
}
