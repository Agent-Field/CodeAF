// One tab's attachment to its engine session. Attaching only reads: no session
// is created until the person sends, and detaching never stops the work.

import { useEffect, useMemo, useRef, useState } from 'react';
import {
  answerEngine,
  connectEngine,
  EngineError,
  readToolResult,
  sendEngine,
  stopEngine,
  watchEngine,
  type EngineAnswer,
  type EngineEvent,
  type EngineSnapshot,
} from '../chat/engine-client';
import type { SendMode } from './Composer';
import { emptyOverlay, projectConversation, reduceLiveEvent, type LiveOverlay } from './transcript';
import type { ConversationModel } from './types';

export type FailedSend = { text: string; mode: SendMode; message: string };

type Options = { sessionFile?: string; onSessionFile: (sessionFile: string) => void };

const BACKOFF_MS = [1000, 2000, 5000, 10000];

export const emptyModel: ConversationModel = { title: '', turns: [], preface: [], running: false, questions: [], tasks: [] };

const isUnreachable = (reason: unknown) => reason instanceof EngineError && reason.unreachable;
const messageOf = (reason: unknown) => (reason instanceof Error ? reason.message : 'The engine did not accept this.');

function lastUserText(snapshot?: EngineSnapshot): string {
  const entry = snapshot?.entries.filter((candidate) => candidate.Role === 'user').pop();
  return entry?.Text ?? '';
}

export function useConversation({ sessionFile, onSessionFile }: Options) {
  const [snapshot, setSnapshot] = useState<EngineSnapshot>();
  const [overlay, setOverlay] = useState<LiveOverlay>(emptyOverlay());
  const [online, setOnline] = useState(false);
  const [connecting, setConnecting] = useState(false);
  const [unreachable, setUnreachable] = useState(false);
  const [failed, setFailed] = useState<FailedSend>();
  const [answering, setAnswering] = useState(false);
  const generation = useRef(0);
  const current = useRef<EngineSnapshot | undefined>(undefined);
  const reader = useRef<AbortController | undefined>(undefined);
  const retryTimer = useRef<number | undefined>(undefined);
  const attempts = useRef(0);
  const file = useRef(sessionFile);
  file.current = sessionFile;
  const announce = useRef(onSessionFile);
  announce.current = onSessionFile;

  function receive(value: EngineSnapshot) {
    current.current = value;
    setSnapshot(value);
    if (!value.running) setOverlay(emptyOverlay(value.entries.length));
  }

  function onEvent(event: EngineEvent) {
    const entries = current.current?.entries.length ?? 0;
    if (event.kind === 'error') {
      const message = event.error || event.text || 'The engine reported an error.';
      setFailed({ text: lastUserText(current.current), mode: 'submit', message });
    }
    // Errors surface once, as the retryable item after the turn, not inside it.
    setOverlay((before) => ({ ...reduceLiveEvent(before, event, entries), error: undefined }));
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
    watchEngine(value, receive, onEvent, controller.signal).catch(() => {
      if (controller.signal.aborted || own !== generation.current) return;
      setOnline(false);
      reconnectLater();
    });
  }

  async function attach(saved?: string): Promise<EngineSnapshot | undefined> {
    const own = generation.current;
    setConnecting(true);
    try {
      const value = await connectEngine(saved);
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

  async function send(text: string, mode: SendMode): Promise<boolean> {
    const own = generation.current;
    setFailed(undefined);
    try {
      const target = await attached();
      if (!target || own !== generation.current) return false;
      if (target.needsPerson) throw new Error('Answer the question above first. Your message is kept.');
      setOverlay(emptyOverlay(target.entries.length));
      const value = await sendEngine(target.id, text, mode);
      if (own !== generation.current) return false;
      receive(value);
      return true;
    } catch (reason) {
      if (own !== generation.current) return false;
      if (isUnreachable(reason)) setUnreachable(true);
      setFailed({ text, mode, message: messageOf(reason) });
      return false;
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
    if (!target || answering) return false;
    setAnswering(true);
    try {
      receive(await answerEngine(target.id, value));
      return true;
    } catch {
      return false;
    } finally {
      setAnswering(false);
    }
  }

  /** Retry what failed: the unsent message if there is one, otherwise the attachment. */
  async function retry(): Promise<boolean> {
    attempts.current = 0;
    window.clearTimeout(retryTimer.current);
    if (failed?.text) return send(failed.text, failed.mode);
    const saved = current.current?.sessionFile ?? file.current;
    if (saved) await attach(saved).catch(() => undefined);
    return false;
  }

  function readFull(callId: string) {
    const target = current.current;
    if (!target) return Promise.reject(new Error('This conversation is not attached.'));
    return readToolResult(target.id, callId);
  }

  const model = useMemo(() => (snapshot ? projectConversation(snapshot, overlay) : emptyModel), [snapshot, overlay]);

  return { model, snapshot, online, connecting, unreachable, failed, answering, send, stop, answer, retry, readFull };
}
