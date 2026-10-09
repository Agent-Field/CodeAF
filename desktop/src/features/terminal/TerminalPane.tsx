import { useEffect, useRef, useState } from 'react';
import { Button, Text } from '../../components/ui';
import { askAboutTerminalOutput, closeTerminal, readTerminalOutput, removeTerminal, terminalStateWords } from '../chat/engine-client';
import { labelLine } from '../conversation/tabSummary';
import type { PaneRenderProps } from '../tabs/kinds/slots';
import { AskField } from './AskField';
import { bindingOf, unbind } from './bindings';
import { announceClosePane, announceOpenConversation, announceOpenTerminal } from './events';
import { startFor, startSentence } from './open';
import { metaLine, removeLabel, toneOf } from './state';
import { TerminalHeader } from './TerminalHeader';
import { TerminalScreen, type ScreenHandle } from './TerminalScreen';
import { useTerminalFeed, type Note } from './useTerminalFeed';
import './terminal.css';

/** Plain output sent along with a question: the most recent 64KB, the same window the engine reads. */
const askTailBytes = 64 << 10;
const tickMs = 1000;

function useNow(live: boolean) {
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    if (!live) return;
    setNow(Date.now());
    const timer = window.setInterval(() => setNow(Date.now()), tickMs);
    return () => window.clearInterval(timer);
  }, [live]);
  return now;
}

/** Design 3c: a terminal or job tab. Output on the terminal field, live state in the header, "Ask codeaf about this output" below. */
export function TerminalPane({ pane, focused, actions }: PaneRenderProps) {
  const [binding, setBinding] = useState(() => bindingOf(pane.id));
  const [screen, setScreen] = useState<ScreenHandle | null>(null);
  const [startNote, setStartNote] = useState<string>();
  const feed = useTerminalFeed(binding, screen);
  const { info, target } = feed;
  const running = info?.state === 'running';
  const now = useNow(running);
  // The tab's glyph (3j): a terminal that ended with a non-zero exit shows the red failed dot in place of its icon; running stays silent.
  const failed = info ? toneOf(info) === 'failed' : false;
  // The workspace builds its actions afresh each render, so the latest one is read through a ref and the effect runs only when what it reports changes.
  const summarize = useRef(actions.onSummary);
  summarize.current = actions.onSummary;
  const title = info?.title; const endedAt = info?.endedAt;
  useEffect(() => {
    if (title) summarize.current({ title, firstLine: '', digest: '', mark: failed ? 'failed' : undefined, updatedAt: endedAt ? Date.parse(endedAt) : undefined });
  }, [title, endedAt, failed]);
  // A live terminal takes the keyboard when its tab is the one in front; a finished one has nothing to type into.
  useEffect(() => { if (focused && running) screen?.focus(); }, [focused, running, screen]);

  const refusal: Note | undefined = !binding?.terminalId
    ? { text: startNote ?? binding?.refused ?? 'This terminal is gone.', retry: binding?.refused ? startAgain : undefined }
    : feed.note;
  async function startAgain() {
    try { await startFor(pane.id, { sessionFile: binding?.sessionFile }); setStartNote(undefined); setBinding(bindingOf(pane.id)); }
    catch (failure) { setStartNote(startSentence(failure)); }
  }

  async function stop() {
    if (target) feed.setInfo(await closeTerminal(target.sessionId, target.terminalId).catch(() => info!));
  }
  async function remove() {
    if (target) await removeTerminal(target.sessionId, target.terminalId).catch(() => undefined);
    unbind(pane.id);
    announceClosePane({ paneId: pane.id });
  }
  /** A finished job again: the same command, as a new job tab under the same conversation (Components, "Finished job · tab menu"). */
  const command = info?.kind === 'job' && !running ? info.command : undefined;
  const rerun = command && info ? () => announceOpenTerminal({ sessionFile: binding?.sessionFile, command, title: info.title }) : undefined;
  async function readOutput() {
    const selected = screen?.selection() ?? '';
    if (selected.trim()) return selected;
    if (!target) return '';
    return (await readTerminalOutput(target.sessionId, target.terminalId, askTailBytes)).text;
  }
  async function ask(question: string) {
    if (!target || !info) throw new Error('There is no output to ask about yet.');
    const snapshot = await askAboutTerminalOutput({ sessionId: target.sessionId, id: target.terminalId }, question, { selection: screen?.selection(), tailBytes: askTailBytes });
    announceOpenConversation({ sessionFile: snapshot.sessionFile, title: question.trim() ? labelLine(question) : `About ${info.title}` });
  }

  return <div className="terminal-pane">
    <TerminalHeader
      title={info?.title ?? pane.title} meta={info ? metaLine(info) : ''} words={info ? terminalStateWords(info, now) : ''} tone={info ? toneOf(info) : undefined}
      canStop={running} onRerun={rerun} removeLabel={removeLabel(info?.kind ?? 'terminal')} onStop={() => void stop()} readOutput={readOutput} onRemove={() => void remove()}/>
    <div className="terminal-field" data-note={refusal ? '' : undefined}>
      {refusal
        ? <div className="terminal-note"><Text>{refusal.text}</Text>{refusal.retry && <Button onClick={refusal.retry}>Try again</Button>}</div>
        : <TerminalScreen label={`${info?.title ?? pane.title} terminal`} interactive={running} cursor={running} onData={feed.send} onResize={feed.resize} onReady={setScreen}/>}
    </div>
    {!refusal && <AskField onAsk={ask}/>}
  </div>;
}
