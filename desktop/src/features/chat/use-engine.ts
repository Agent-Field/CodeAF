import { useEffect, useRef, useState } from 'react';
import { answerEngine, connectEngine, sendEngine, stopEngine, watchEngine, type EngineAnswer, type EngineSnapshot, type EngineEvent } from './engine-client';
import type { WorkDocument } from './work-model';
import { engineDocumentChange } from './engine-document';
import { withStreamingText } from './engine-stream';

/** A view attachment. Aborting its stream never stops the persistent engine. */
export function useEngine(tabId: string, document: WorkDocument, update: (change: Partial<WorkDocument>) => void) {
 const [snapshot, setSnapshot] = useState<EngineSnapshot>();
 const [connecting, setConnecting] = useState(false);
 const [online, setOnline] = useState(false);
 const [error, setError] = useState('');
 const [answering, setAnswering] = useState(false);
 const controller = useRef<AbortController | undefined>(undefined);
 const current = useRef(document); current.current = document;
 const publish = useRef(update); publish.current = update;
 const generation = useRef(0);
 const streaming = useRef('');
 const segment = useRef<{id:string; baseBlockCount:number}|undefined>(undefined);
 function apply(value: EngineSnapshot) {
  setSnapshot(value);
  const before = current.current;
  const change = engineDocumentChange(value, before);
  if (streaming.current && segment.current && change.sections && value.running) {
   const active = segment.current;
   change.sections = change.sections.map(section => section.id === active.id ? withStreamingText(section, streaming.current, active.baseBlockCount) : section);
  }
  current.current = { ...before, ...change };
  publish.current(change);
 }
 function event(value: EngineEvent) {
  if (value.kind === 'text' && value.text) {
   if (!streaming.current) { const last = current.current.sections[current.current.sections.length - 1]; segment.current = last ? { id:last.id, baseBlockCount:last.blocks.length } : undefined; }
   streaming.current += value.text;
   const before = current.current;
   const last = before.sections[before.sections.length - 1];
   if (last) {
    const sections = before.sections.map(section => section.id === last.id ? withStreamingText(section, streaming.current, segment.current?.baseBlockCount ?? section.blocks.length) : section);
    const change = { sections, running: true, activity: { phase: 'streaming' as const, recordedAt: Date.now(), sample: false } };
    current.current = { ...before, ...change }; publish.current(change);
   }
  }
  if (value.kind === 'assistantDone' || value.kind === 'turnDone') { streaming.current = ''; segment.current = undefined; }
  if (value.kind === 'error') {
   const message = value.error || value.text || 'The engine reported an error. Your draft is preserved.';
   setError(message);
   const change = { notice: message, running: false, activity: { phase: 'failed' as const, recordedAt: Date.now(), sample: false } };
   current.current = { ...current.current, ...change }; publish.current(change);
  }
 }
 async function connect() {
  if (connecting) return;
  if (!current.current.engine && current.current.sections.length) {
   setError('Connect a fresh tab to the engine. This local preview stays here.'); return;
  }
  const ownGeneration = generation.current;
  setConnecting(true); setError('');
  try {
   const value = await connectEngine(current.current.engine?.sessionFile);
   if (ownGeneration !== generation.current) return;
   controller.current?.abort(); const next = new AbortController(); controller.current = next;
   streaming.current = ''; segment.current = undefined; setOnline(true); apply(value);
   void watchEngine(value, apply, event, next.signal).catch(reason => {
    if (next.signal.aborted) return;
    setOnline(false); setError(reason instanceof Error ? reason.message : 'The engine connection closed.');
   });
  } catch (reason) {
   if (ownGeneration === generation.current) { setOnline(false); setError(reason instanceof Error ? reason.message : 'Cannot connect to the engine.'); }
  } finally { if (ownGeneration === generation.current) setConnecting(false); }
 }
 useEffect(() => {
  generation.current++; setSnapshot(undefined); setOnline(false); setError(''); setConnecting(false); setAnswering(false);
  if (current.current.engine) void connect();
  return () => { generation.current++; controller.current?.abort(); };
 }, [tabId]);
 async function send(text: string, mode: 'submit'|'steer'|'queue') {
  if (!snapshot || !online) { setError('Reconnect this saved conversation before sending.'); return false; }
  if (snapshot.needsPerson) { setError(snapshot.questions?.length ? 'Answer the pending question below before continuing. Your draft is preserved.' : 'Answer the pending approval or question in the TUI first. Your draft is preserved.'); return false; }
  const ownGeneration = generation.current;
  try { streaming.current = ''; segment.current = undefined; const value = await sendEngine(snapshot.id, text, mode); if (ownGeneration !== generation.current) return false; apply(value); setError(''); return true; }
  catch (reason) { if (ownGeneration === generation.current) setError(reason instanceof Error ? reason.message : 'The engine did not accept the instruction.'); return false; }
 }
 async function stop() {
  if (!snapshot || !online) { setError('Reconnect before stopping this work.'); return; }
  const ownGeneration = generation.current;
  try { const value = await stopEngine(snapshot.id); if (ownGeneration !== generation.current) return; apply(value); const change = { stopped: true, running: false, activity: { phase: 'stopped' as const, recordedAt: Date.now(), sample: false } }; current.current = { ...current.current, ...change }; publish.current(change); }
  catch (reason) { if (ownGeneration === generation.current) setError(reason instanceof Error ? reason.message : 'The engine did not confirm Stop.'); }
 }
 async function answer(value: EngineAnswer) {
  if (!snapshot || !online || answering) return false;
  const question = snapshot.questions?.find(q => q.kind === value.kind && q.id === value.id && (q.ref ?? '') === (value.ref ?? ''));
  if (!question) { setError('This question is no longer pending. Your words are preserved.'); return false; }
  const ownGeneration = generation.current; setAnswering(true);
  try { const next = await answerEngine(snapshot.id, value); if (ownGeneration !== generation.current) return false; apply(next); setError(''); return true; }
  catch (reason) { if (ownGeneration === generation.current) setError(reason instanceof Error ? reason.message : 'The engine did not accept the answer.'); return false; }
  finally { if (ownGeneration === generation.current) setAnswering(false); }
 }
 return { snapshot, connecting, online, error, answering, connect, send, stop, answer };
}
