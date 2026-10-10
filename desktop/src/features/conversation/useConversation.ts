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
import { beginPost, holdSend, restoreFront, type HeldSend } from './offline/outbox';
import { questionKey } from './model/entry';
import { blocksComposer } from './tray/layout';
import type { ConversationModel } from './types';

export type FailedSend = { text: string; mode: SendMode; message: string; files?: OutgoingFile[]; /** Set when this text was taken back out of the pane outbox. */ fromOutbox?: boolean };

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

function rememberHeld(box: { current: HeldSend[] }, held: readonly HeldSend[], setHeldTexts: (texts: string[]) => void) {
  const next = [...held];
  box.current = next;
  setHeldTexts(next.map((item) => item.text));
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
  // Plain-text sends held in this pane while the engine is not answering. Oldest first.
  const [heldTexts, setHeldTexts] = useState<string[]>([]);
  // A refused post is put back into an empty composer. The view applies it once.
  const [draftToRestore, setDraftToRestore] = useState<string>();
  const box = useRef<HeldSend[]>([]);
  const onlineRef = useRef(false);
  const unreachableRef = useRef(false);
  const flushing = useRef(false);
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
      onlineRef.current = false;
      setOnline(false);
      reconnectLater();
    });
  }

  function remember(held: readonly HeldSend[]) {
    rememberHeld(box, held, setHeldTexts);
  }

  /** The engine is answering again. Held sends may post; the unreachable line comes down. */
  function markOnline() {
    onlineRef.current = true;
    setOnline(true);
    unreachableRef.current = false;
    setUnreachable(false);
  }

  /** Nothing answered. Held sends stay put until a later attach succeeds. */
  function markUnreachable() {
    unreachableRef.current = true;
    setUnreachable(true);
    onlineRef.current = false;
    setOnline(false);
  }

  /**
   * Post held plain text oldest first. A refusal puts that one message back
   * for the composer and leaves anything sent after it still held. A lost
   * connection puts the message back at the front so it is not skipped.
   */
  async function flushOutbox() {
    if (flushing.current) return;
    flushing.current = true;
    const own = generation.current;
    let paused = false;
    try {
      while (own === generation.current && onlineRef.current && box.current.length > 0) {
        const target = current.current;
        if (!target) { paused = true; break; }
        if (firstTurn.current && !firstTurnDone.current && target.entries.length === 0) {
          try {
            await firstTurn.current(target.sessionFile);
            if (own !== generation.current) return;
            firstTurnDone.current = true;
          } catch (reason) {
            if (own !== generation.current) return;
            if (isUnreachable(reason)) markUnreachable();
            else refuseHeld(messageOf(reason));
            paused = true;
            break;
          }
        }
        const step = beginPost(box.current);
        if (!step.posting) break;
        remember(step.held);
        if (step.posting.mode !== 'queue') setLive(emptyLive(target.entries.length));
        if (step.posting.mode === 'submit' && step.posting.text.trim()) setWriting({ text: step.posting.text, at: target.entries.length });
        try {
          const value = await deliver(target.id, step.posting.text, step.posting.mode);
          if (own !== generation.current) return;
          receive(value);
        } catch (reason) {
          if (own !== generation.current) return;
          if (isUnreachable(reason)) {
            remember(restoreFront(box.current, step.posting));
            markUnreachable();
          } else {
            setFailed({ text: step.posting.text, mode: step.posting.mode, message: messageOf(reason), fromOutbox: true });
            setDraftToRestore(step.posting.text);
          }
          paused = true;
          break;
        } finally {
          if (own === generation.current) setWriting(undefined);
        }
      }
    } finally {
      flushing.current = false;
    }
    if (!paused && own === generation.current && onlineRef.current && box.current.length > 0) void flushOutbox();
  }

  /** The oldest held send could not be posted. It becomes the draft again; the rest stay held. */
  function refuseHeld(message: string) {
    const step = beginPost(box.current);
    if (!step.posting) return;
    remember(step.held);
    setFailed({ text: step.posting.text, mode: step.posting.mode, message, fromOutbox: true });
    setDraftToRestore(step.posting.text);
  }

  async function attach(saved?: string): Promise<EngineSnapshot | undefined> {
    const own = generation.current;
    setConnecting(true);
    try {
      const value = await connectEngine(saved, newConversationPlace);
      if (own !== generation.current) return undefined;
      attempts.current = 0;
      markOnline();
      receive(value);
      watch(value);
      if (value.sessionFile !== saved) announce.current(value.sessionFile);
      await flushOutbox();
      return value;
    } catch (reason) {
      if (own === generation.current) {
        onlineRef.current = false;
        setOnline(false);
        unreachableRef.current = isUnreachable(reason);
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
    if (onlineRef.current && current.current) return current.current;
    return attach(current.current?.sessionFile ?? file.current);
  }

  /** Plain text joins the pane outbox. Files and a blank do not. */
  function holdLocally(text: string, mode: SendMode, files?: OutgoingFile[]): boolean {
    const step = holdSend(box.current, { text, mode, files });
    if (!step.accepted) return false;
    remember(step.held);
    return true;
  }

  async function send(text: string, mode: SendMode, files?: OutgoingFile[]): Promise<boolean> {
    const own = generation.current;
    setFailed(undefined);
    // Already unreachable: hold plain text here. A send with files stays in the composer.
    if (unreachableRef.current) {
      if (holdLocally(text, mode, files)) return true;
      setFailed({ text, mode, message: '', files });
      return false;
    }
    // A send already waiting goes out first. This one lines up behind it.
    if (!files?.length && (box.current.length > 0 || flushing.current)) {
      if (!holdLocally(text, mode)) return false;
      if (onlineRef.current) void flushOutbox();
      return true;
    }
    let optimistic = false;
    try {
      const target = await attached();
      if (!target || own !== generation.current) return false;
      if (!files?.length && (box.current.length > 0 || flushing.current)) {
        if (!holdLocally(text, mode)) return false;
        if (onlineRef.current) void flushOutbox();
        return true;
      }
      if (blocksComposer(target.questions ?? [])) throw new Error('Answer the question above first. Your message is kept.');
      if (firstTurn.current && !firstTurnDone.current && target.entries.length === 0) {
        await firstTurn.current(target.sessionFile);
        firstTurnDone.current = true;
        if (own !== generation.current) return false;
      }
      if (mode !== 'queue') setLive(emptyLive(target.entries.length));
      if (mode === 'submit' && text.trim()) {
        optimistic = true;
        setWriting({ text, at: target.entries.length });
      }
      const value = await deliver(target.id, text, mode, files);
      if (own !== generation.current) return false;
      receive(value);
      return true;
    } catch (reason) {
      if (own !== generation.current) return false;
      // The header line is the only sentence for a quiet engine. Plain text
      // waits in this pane; a send with files keeps the draft and draws no second notice.
      if (isUnreachable(reason)) {
        markUnreachable();
        if (holdLocally(text, mode, files)) return true;
        setFailed({ text, mode, message: '', files });
        return false;
      }
      setFailed({ text, mode, message: messageOf(reason), files });
      return false;
    } finally {
      if (optimistic && own === generation.current) setWriting(undefined);
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

  /**
   * Ask the engine again without sending the composer's draft. Held plain-text
   * sends post in order once that ask succeeds. The header line's probes and
   * its Retry button use this. A success drops an empty failure that existed
   * only because nothing answered.
   */
  async function reprobe(): Promise<boolean> {
    const own = generation.current;
    window.clearTimeout(retryTimer.current);
    const saved = current.current?.sessionFile ?? file.current;
    if (!saved) return false;
    try {
      const value = await attach(saved);
      if (own !== generation.current || !value) return false;
      setFailed(current => (current && current.message === '' ? undefined : current));
      return true;
    } catch {
      return false;
    }
  }

  /** Retry what failed: the unsent message if there is one, otherwise the attachment. */
  async function retry(): Promise<boolean> {
    attempts.current = 0;
    window.clearTimeout(retryTimer.current);
    // A refused outbox send is the next one out, ahead of anything still held.
    if (failed?.fromOutbox && failed.text && !failed.files?.length) {
      const text = failed.text;
      const mode = failed.mode;
      setFailed(undefined);
      remember(restoreFront(box.current, { text, mode }));
      if (unreachableRef.current) return true;
      try {
        const target = await attached();
        if (target) await flushOutbox();
      } catch {
        // The words are held again. They go out when the engine next answers.
      }
      return true;
    }
    if (failed?.text || failed?.files?.length) return send(failed.text, failed.mode, failed.files);
    const saved = current.current?.sessionFile ?? file.current;
    if (saved) await attach(saved).catch(() => undefined);
    return false;
  }

  function ackDraftRestore() {
    setDraftToRestore(undefined);
  }

  function readFull(callId: string) {
    const target = current.current;
    if (!target) return Promise.reject(new Error('This conversation is not attached.'));
    return readToolResult(target.id, callId);
  }

  const model = useMemo(() => (snapshot ? buildModel(snapshot, live, places.current) : emptyModel), [snapshot, live]);

  // Once the engine has recorded anything newer than the send, the real message takes over.
  // Held sends stay listed until they are the one being posted, so two copies of the same words both stay visible.
  const sendingNow = writing && (snapshot?.entries.length ?? 0) <= writing.at ? writing.text : undefined;
  const pendingSends = sendingNow ? [...heldTexts, sendingNow] : heldTexts;

  return { model, snapshot, pendingSends, draftToRestore, ackDraftRestore, online, connecting, unreachable, failed, busyKey, send, stop, answer, hold, controlTask, retry, reprobe, readFull, editQueue, moveQueue, removeQueue, sendQueueNow };
}
