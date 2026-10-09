import { useCallback, useEffect, useRef, useState } from 'react';
import { EngineError, readTerminal, resizeTerminal, watchTerminal, writeTerminal, type TerminalInfo } from '../chat/engine-client';
import { sessionFor } from './open';
import type { ScreenHandle } from './TerminalScreen';

/** What the pane draws instead of the screen, when there is no terminal to draw. */
export type Note = { text: string; /** Present when asking again can help. */ retry?: () => void };
export type Feed = {
  info?: TerminalInfo;
  /** The engine session and terminal ids once attached; actions need both. */
  target?: { sessionId: string; terminalId: string };
  note?: Note;
  send: (data: string) => void;
  resize: (cols: number, rows: number) => void;
  setInfo: (info: TerminalInfo) => void;
};

/** How long to wait before following a stream again after it ended or broke while the program still runs. */
const reconnectMs = 250;
const resizeDebounceMs = 120;
const gone = 'This terminal is gone.';
/** What the engine keeps of one terminal's output (desktopbridge `scrollbackBytes`); older bytes are dropped, and the log says so (Components, edge states). */
export const keptKilobytes = 512;
export const trimmedLine = `… earlier output trimmed to the last ${keptKilobytes} KB`;

const wait = (ms: number, signal: AbortSignal) => new Promise<void>(resolve => { const timer = setTimeout(resolve, ms); signal.addEventListener('abort', () => { clearTimeout(timer); resolve(); }, { once: true }); });
const isGone = (failure: unknown) => failure instanceof EngineError && failure.status === 404;
const sentence = (failure: unknown) => (failure instanceof Error && failure.message ? failure.message : 'The terminal could not be reached.');

/**
 * Follows one engine terminal into a screen: replays the kept scrollback from byte 0, then the live output,
 * and keeps the state record current. Holds exactly one stream (browsers allow six connections per origin).
 * `screen` is null until the screen is open; the feed starts when it is.
 */
export function useTerminalFeed(binding: { sessionFile?: string; terminalId?: string } | undefined, screen: ScreenHandle | null): Feed {
  const [info, setInfo] = useState<TerminalInfo>();
  const [target, setTarget] = useState<Feed['target']>();
  const [note, setNote] = useState<Note>();
  const [attempt, setAttempt] = useState(0);
  const targetRef = useRef<Feed['target']>(undefined);
  const infoRef = useRef<TerminalInfo | undefined>(undefined);
  infoRef.current = info;

  useEffect(() => {
    const terminalId = binding?.terminalId;
    if (!screen || !terminalId) return;
    const abort = new AbortController();
    const { signal } = abort;
    (async () => {
      let sessionId: string;
      try {
        sessionId = (await sessionFor(binding?.sessionFile)).id;
        if (signal.aborted) return;
        const record = await readTerminal(sessionId, terminalId);
        if (signal.aborted) return;
        targetRef.current = { sessionId, terminalId }; setTarget(targetRef.current);
        setInfo(record); setNote(undefined);
        // The engine started the program at its own default size; tell it the size the screen really has.
        const { cols, rows } = screen.size();
        if (record.state === 'running' && (record.cols !== cols || record.rows !== rows)) resizeTerminal(sessionId, terminalId, cols, rows).catch(() => undefined);
      } catch (failure) {
        if (!signal.aborted) setNote(isGone(failure) ? { text: gone } : { text: sentence(failure), retry: () => setAttempt(n => n + 1) });
        return;
      }
      let after = 0; let first = true; let exited = false;
      while (!signal.aborted && !exited) {
        try {
          await watchTerminal(sessionId, terminalId, after, (bytes, end, cut) => {
            if (cut) { screen.reset(); first = true; }
            if (first && infoRef.current?.kind === 'job') screen.pad();
            if (cut) screen.write(`\x1b[2m${trimmedLine}\x1b[0m\r\n`);
            first = false;
            screen.write(bytes); after = end;
          }, final => { exited = true; setInfo(final); }, signal);
        } catch (failure) {
          if (signal.aborted) return;
          if (isGone(failure)) { setNote({ text: gone }); return; }
        }
        if (!exited) await wait(reconnectMs, signal);
      }
    })();
    return () => abort.abort();
  }, [screen, binding?.sessionFile, binding?.terminalId, attempt]);

  // Keystrokes go out in order, one request at a time, so fast typing never arrives shuffled.
  const queue = useRef(Promise.resolve());
  const send = useCallback((data: string) => {
    const t = targetRef.current;
    if (!t) return;
    queue.current = queue.current.then(() => writeTerminal(t.sessionId, t.terminalId, data)).catch(() => undefined);
  }, []);
  const resizeTimer = useRef(0);
  useEffect(() => () => window.clearTimeout(resizeTimer.current), []);
  const resize = useCallback((cols: number, rows: number) => {
    window.clearTimeout(resizeTimer.current);
    resizeTimer.current = window.setTimeout(() => {
      const t = targetRef.current;
      if (t && infoRef.current?.state === 'running') resizeTerminal(t.sessionId, t.terminalId, cols, rows).catch(() => undefined);
    }, resizeDebounceMs);
  }, []);
  return { info, target, note, send, resize, setInfo };
}
