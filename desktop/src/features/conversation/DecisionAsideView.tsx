import { useState } from 'react';
import type { ConversationPlan, DecisionAside, DecisionWhy } from '../chat/engine-client';
import { GroupedReceipt } from '../decisions/GroupedReceipt';
import { ReceiptLine } from '../decisions/ReceiptLine';
import { PlanCard } from '../decisions/PlanCard';
import { createPlanClient } from '../decisions/client';
import { NoteItem } from './NoteItem';
import { SystemNote } from './SystemNote';

/** Explanations come from the journal, so reopening a conversation needs no invented reason. */
export function DecisionAsideView({ decision, onOpen }: { decision: DecisionAside; onOpen: (key: string) => void }) {
  const [why, setWhy] = useState<DecisionWhy>();
  const children = decision.children ?? [];
  const actions = {
    onOpen: (key: string) => { if (key) onOpen(key); },
    onWhy: (key: string) => setWhy(children.find(row => `${row.question.kind}:${row.question.token}` === key)?.why ?? decision.why),
  };
  return <>
    {children.length ? <GroupedReceipt actions={children.map(row => ({ questionId: `${row.question.kind}:${row.question.token}`, placeName: row.why.by, reason: row.why.because, text: row.text, summary: row.text }))} {...actions}/> : decision.why ? <ReceiptLine text={decision.text} questionId="" placeName={decision.why.by} reason={decision.why.because} {...actions}/> : <NoteItem text={decision.text}/>}
    {why && <SystemNote kind="info" action={{ label: 'Close', onClick: () => setWhy(undefined) }}>{[why.by, why.because, why.percent ? `${why.percent}%` : ''].filter(Boolean).join(' · ')}</SystemNote>}
  </>;
}

const plans = createPlanClient();

/** A failed write keeps the card available; only the engine's acknowledgement removes it. */
export function TranscriptPlan({ plan, sessionId }: { plan: ConversationPlan; sessionId: string }) {
  const [settled, setSettled] = useState(false);
  const [failure, setFailure] = useState('');
  const [lines, setLines] = useState<string[]>([]);
  async function write(action: () => Promise<void>) {
    setFailure('');
    try { await action(); } catch (error) {
      setFailure(error instanceof Error ? error.message : 'That did not go through. Try again.');
      throw error;
    }
  }
  return <>
    {!settled && <PlanCard steps={plan.steps}
      onEdit={steps => write(async () => { await plans.edit(sessionId, plan.id, steps); })}
      onGo={() => write(async () => { const receipt = await plans.go(sessionId, plan.id); setLines(receipt.results.map(row => row.line)); setSettled(true); }).catch(() => undefined)}
      onCancel={() => { void write(async () => { await plans.cancel(sessionId, plan.id); setSettled(true); }).catch(() => undefined); }}/ >}
    {lines.map((line, index) => <NoteItem key={index} text={line}/>)}
    {failure && <SystemNote kind="failure">{failure}</SystemNote>}
  </>;
}
