import { chatIdFromSessionFile } from '../places/client';
import { nextUpWalk } from '../nextup/useNextUpWalk';
import { worldStore } from '../chat/world-store';
// One tab's attachment to its engine session. Attaching only reads: no session
// is created until the person sends, and detaching never stops the work.

import { useEffect, useMemo, useRef, useState } from 'react';
import type { BeforeFirstTurn } from './firstTurn';
import {
  answerEngine,
  connectEngine,
  editQueued,
  EngineError,
  holdQuestion,
  moveQueued,
  readEngine,
  readToolResult,
  removeQueued,
  sendEngine,
  sendEngineWithFiles,
  sendQueuedNow,
  stopEngine,
  taskAction,
  watchEngine,
  type EngineAnswer,
  type EngineEvent,
  type EngineSnapshot,
  type OutgoingFile,
  type TaskAction,
} from '../chat/engine-client';
import type { SendMode } from './Composer';
import { emptyLive, projectTurnsV2, reduceLive, type LiveOverlayV2, type ReceiptPlaces } from './model';
import { questionKey } from './model/entry';
import { blocksComposer } from './tray/layout';
import type { ConversationModel } from './types';

export type FailedSend = { text: string; mode: SendMode; message: string; files?: OutgoingFile[] };

type Options = { sessionFile?: string; onSessionFile: (sessionFile: string) => void; beforeFirstTurn?: BeforeFirstTurn; newConversationPlace?: string };

const BACKOFF_MS = [1000, 2000, 5000, 10000];

export const emptyModel: ConversationModel = { title: '', turns: [], preface: [], running: false, questions: [], tasks: [] };

const isUnreachable = (reason: unknown) => reason instanceof EngineError && reason.unreachable;
const messageOf = (reason: unknown) => (reason instanceof Error ? reason.message : 'The engine did not accept this.');

function lastUserText(snapshot?: EngineSnapshot): string {
  const entry = snapshot?.entries.filter((candidate) => candidate.Role === 'user').pop();
  return entry?.Text ?? '';
}

function buildModel(snapshot: EngineSnapshot, live: LiveOverlayV2, places: ReceiptPlaces): ConversationModel {
  const { turns, preface } = projectTurnsV2(snapshot, live, places);
  const { title, running, questions = [], tasks, planError } = snapshot;
  return { title, turns, preface, running, questions, tasks, planError };
}

function deliver(id: string, text: string, mode: SendMode, files?: OutgoingFile[]) {
  return files?.length ? sendEngineWithFiles(id, text, files) : sendEngine(id, text, mode);
}

export function useConversation({ sessionFile, onSessionFile, beforeFirstTurn, newConversationPlace }: Options) {
  const [snapshot, setSnapshot] = useState<EngineSnapshot>();
  const [live, setLive] = useState<LiveOverlayV2>(emptyLive());
  const [online, setOnline] = useState(false);
  const [connecting, setConnecting] = useState(false);
  const [unreachable, setUnreachable] = useState(false);
  const [failed, setFailed] = useState<FailedSend>();
  // The person's words between pressing Send and the engine recording them (design: Sending, optimistic at 60%).
  const [writing, setWriting] = useState<{ text: string; at: number }>();
  const [busyKey, setBusyKey] = useState<string | null>(null);
  const generation = useRef(0);
  const current = useRef<EngineSnapshot | undefined>(undefined);
  const reader = useRef<AbortController | undefined>(undefined);
  const retryTimer = useRef<number | undefined>(undefined);
  const attempts = useRef(0);
  const places = useRef<ReceiptPlaces>(new Map());
  const file = useRef(sessionFile);
  file.current = sessionFile;
  const announce = useRef(onSessionFile);
  announce.current = onSessionFile;
  const firstTurn = useRef(beforeFirstTurn);
  firstTurn.current = beforeFirstTurn;
  // Once the first-turn step has succeeded for this conversation it is never repeated.
  const firstTurnDone = useRef(false);

  function receive(value: EngineSnapshot) {
    current.current = value;
    setSnapshot(value);
    if (!value.running) setLive(emptyLive(value.entries.length));
  }

  function onEvent(event: EngineEvent) {
    const entries = current.current?.entries.length ?? 0;
    if (event.kind === 'error') {
      const message = event.error || event.text || 'The engine reported an error.';
      setFailed({ text: lastUserText(current.current), mode: 'submit', message });
    }
    // Errors surface once, as the retryable item after the turn, not inside it.
    setLive((before) => ({ ...reduceLive(before, event, entries), error: undefined }));
  }

  function reconnectLater() {
    const own = generation.current;
    const delay = BACKOFF_MS[Math.min(attempts.current, BACKOFF_MS.length - 1)];
    attempts.current += 1;
    window.clearTimeout(retryTimer.current);
    retryTimer.current = window.setTimeout(() => {
      const saved = current.current?.sessionFile ?? file.current;
      if (own === generation.current && saved) void attach(saved).catch(reconnectLater);
    }, delay);
  }

  function watch(value: EngineSnapshot) {
    const own = generation.current;
    reader.current?.abort();
    const controller = new AbortController();
    reader.current = controller;
    watchEngine(value, receive, onEvent, controller.signal, () => current.current).catch(() => {
      if (controller.signal.aborted || own !== generation.current) return;
      setOnline(false);
      reconnectLater();
    });
  }

  async function attach(saved?: string): Promise<EngineSnapshot | undefined> {
    const own = generation.current;
    setConnecting(true);
    try {
      const value = await connectEngine(saved, newConversationPlace);
      if (own !== generation.current) return undefined;
      attempts.current = 0;
      setOnline(true);
      setUnreachable(false);
      receive(value);
      watch(value);
      if (value.sessionFile !== saved) announce.current(value.sessionFile);
      return value;
    } catch (reason) {
      if (own === generation.current) {
        setOnline(false);
        setUnreachable(isUnreachable(reason));
      }
      throw reason;
    } finally {
      if (own === generation.current) setConnecting(false);
    }
  }

  useEffect(() => {
    if (file.current) void attach(file.current).catch(reconnectLater);
    return () => {
      generation.current += 1;
      reader.current?.abort();
      window.clearTimeout(retryTimer.current);
    };
  }, []);

  async function attached(): Promise<EngineSnapshot | undefined> {
    if (online && current.current) return current.current;
    return attach(current.current?.sessionFile ?? file.current);
  }

  async function send(text: string, mode: SendMode, files?: OutgoingFile[]): Promise<boolean> {
    const own = generation.current;
    setFailed(undefined);
    try {
      const target = await attached();
      if (!target || own !== generation.current) return false;
      if (blocksComposer(target.questions ?? [])) throw new Error('Answer the question above first. Your message is kept.');
      if (firstTurn.current && !firstTurnDone.current && target.entries.length === 0) {
        await firstTurn.current(target.sessionFile);
        firstTurnDone.current = true;
        if (own !== generation.current) return false;
      }
      if (mode !== 'queue') setLive(emptyLive(target.entries.length));
      if (mode === 'submit' && text.trim()) setWriting({ text, at: target.entries.length });
      const value = await deliver(target.id, text, mode, files);
      if (own !== generation.current) return false;
      receive(value);
      return true;
    } catch (reason) {
      if (own !== generation.current) return false;
      if (isUnreachable(reason)) setUnreachable(true);
      setFailed({ text, mode, message: messageOf(reason), files });
      return false;
    } finally {
      if (own === generation.current) setWriting(undefined);
    }
  }

  async function stop() {
    const target = current.current;
    if (!target) return;
    try {
      receive(await stopEngine(target.id));
    } catch (reason) {
      // Nothing to resend: a failed Stop offers no Retry.
      setFailed({ text: '', mode: 'submit', message: messageOf(reason) });
    }
  }

  async function answer(value: EngineAnswer): Promise<boolean> {
    const target = current.current;
    if (!target || busyKey) return false;
    setBusyKey(questionKey(value));
    try {
      receive(await answerEngine(target.id, value));
      nextUpWalk.acknowledge(target.sessionFile ? chatIdFromSessionFile(target.sessionFile) : target.id, value, worldStore.getState().items);
      return true;
    } catch {
      // The card says the answer did not go through and keeps it.
      return false;
    } finally {
      setBusyKey(null);
    }
  }

  /** Stops a question's clock while the person reads it; a refused hold leaves the clock as the engine has it. */
  function hold(question: { kind: string; id: number; ref?: string }) {
    const target = current.current;
    if (target) void holdQuestion(target.id, question).catch(() => undefined);
  }

  /**
   * One change to a queued message. A refusal is the race with delivery (the turn
   * already started): the snapshot is re-read so the row shows what really happened,
   * and the engine's sentence is said once.
   */
  async function changeQueue(change: (id: string) => Promise<EngineSnapshot>) {
    const target = current.current;
    if (!target) return;
    try {
      receive(await change(target.id));
    } catch (reason) {
      await readEngine(target.id, target.entries.length, target).then(receive, () => undefined);
      setFailed({ text: '', mode: 'submit', message: messageOf(reason) });
    }
  }
  const editQueue = (queued: string, text: string) => changeQueue((id) => editQueued(id, queued, text));
  const moveQueue = (queued: string, to: number) => changeQueue((id) => moveQueued(id, queued, to));
  const removeQueue = (queued: string) => changeQueue((id) => removeQueued(id, queued));

  async function sendQueueNow(queued: string) {
    const target = current.current;
    const own = generation.current;
    if (!target) return;
    setFailed(undefined);
    try {
      const value = await sendQueuedNow(target.id, queued);
      if (own === generation.current) receive(value);
    } catch (reason) {
      if (own !== generation.current) return;
      const alreadySent = reason instanceof EngineError && reason.status === 409;
      // A conflict proves delivery even when the refresh fails, so the stale row must leave immediately.
      if (alreadySent && current.current) {
        receive({ ...current.current, queue: current.current.queue?.filter((row) => row.id !== queued) });
      }
      setFailed({ text: '', mode: 'submit', message: alreadySent ? 'that message has already been sent' : messageOf(reason) });
      const held = current.current;
      if (!held) return;
      await readEngine(target.id, held.entries.length, held).then((value) => {
        if (own !== generation.current) return;
        receive(alreadySent ? { ...value, queue: value.queue?.filter((row) => row.id !== queued) } : value);
      }, () => undefined);
    }
  }

  async function controlTask(taskId: string, action: TaskAction) {
    const target = current.current;
    if (!target) return;
    try {
      await taskAction(target.id, taskId, action);
    } catch (reason) {
      setFailed({ text: '', mode: 'submit', message: messageOf(reason) });
    }
  }

  /** Retry what failed: the unsent message if there is one, otherwise the attachment. */
  async function retry(): Promise<boolean> {
    attempts.current = 0;
    window.clearTimeout(retryTimer.current);
    if (failed?.text || failed?.files?.length) return send(failed.text, failed.mode, failed.files);
    const saved = current.current?.sessionFile ?? file.current;
    if (saved) await attach(saved).catch(() => undefined);
    return false;
  }

  function readFull(callId: string) {
    const target = current.current;
    if (!target) return Promise.reject(new Error('This conversation is not attached.'));
    return readToolResult(target.id, callId);
  }

  const model = useMemo(() => (snapshot ? buildModel(snapshot, live, places.current) : emptyModel), [snapshot, live]);

  // Once the engine has recorded anything newer than the send, the real message takes over.
  const sending = writing && (snapshot?.entries.length ?? 0) <= writing.at ? writing.text : undefined;

  return { model, snapshot, sending, online, connecting, unreachable, failed, busyKey, send, stop, answer, hold, controlTask, retry, readFull, editQueue, moveQueue, removeQueue, sendQueueNow };
}
