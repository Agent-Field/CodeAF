import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { Button, Text } from '../../components/ui';
import { askAboutTerminalOutput, closeTerminal, readTerminalOutput, removeTerminal, terminalStateWords } from '../chat/engine-client';
import { labelLine } from '../conversation/tabSummary';
import type { PaneRenderProps } from '../tabs/kinds/slots';
import { AskField } from './AskField';
import { unbind } from './bindings';
import { announceClosePane, announceOpenConversation, announceOpenTerminal } from './events';
import { startFor, startSentence } from './open';
import { bindingFor, legacyTerminalView, terminalView } from './target';
import { metaLine, removeLabel, toneOf } from './state';
import { offerTerminalTabMeta, type TabMetaSource } from './tabMeta';
import { offerTerminalTabMenu, type FinishedJobOffer } from './tabMenu';
import type { TerminalBinding } from './bindings';
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
  // The pane's own target wins; the legacy per-pane binding only speaks for a pane that has none yet.
  const [started, setStarted] = useState<TerminalBinding>();
  const binding = started ?? bindingFor(pane);
  const [screen, setScreen] = useState<ScreenHandle | null>(null);
  const [startNote, setStartNote] = useState<string>();
  const [actionError, setActionError] = useState<string>();
  const feed = useTerminalFeed(binding, screen);
  const { info, target } = feed;
  const running = info?.state === 'running';
  const now = useNow(running);
  // The tab's glyph (3j): a terminal that ended with a non-zero exit shows the red failed dot in place of its icon; running stays silent.
  const failed = info ? toneOf(info) === 'failed' : false;
  // The workspace builds its actions afresh each render, so the latest one is read through a ref and the effect runs only when what it reports changes.
  const summarize = useRef(actions.onSummary);
  summarize.current = actions.onSummary;
  const menuFacts = useRef<FinishedJobOffer>({ finished: false, onRemove: () => {} });
  const metaFacts = useRef<TabMetaSource | undefined>(undefined);
  const title = info?.title; const endedAt = info?.endedAt;
  // Before paint, so the tab right-click, the exit words and the header agree in the same frame.
  // Kind, state and exit code are deps because `exit 0` changes none of the title, the end time or the failed mark.
  useLayoutEffect(() => {
    if (title) summarize.current({ title, firstLine: '', digest: '', mark: failed ? 'failed' : undefined, updatedAt: endedAt ? Date.parse(endedAt) : undefined });
  }, [title, endedAt, failed, info?.kind, info?.state, info?.exitCode]);
  // A tab saved before the durable target carried none: write it onto the pane through the tab action, once.
  const hydrate = useRef(actions.onView);
  hydrate.current = actions.onView;
  const legacy = legacyTerminalView(pane);
  const legacyKey = legacy ? `${legacy.sessionFile}\n${legacy.target?.terminalId}` : '';
  useEffect(() => { if (legacy) hydrate.current(legacy); }, [legacyKey]); // eslint-disable-line react-hooks/exhaustive-deps
  // A live terminal takes the keyboard when its tab is the one in front; a finished one has nothing to type into.
  useEffect(() => { if (focused && running) screen?.focus(); }, [focused, running, screen]);

  const refusal: Note | undefined = !binding?.terminalId
    ? { text: startNote ?? binding?.refused ?? 'This terminal is gone.', retry: binding?.refused ? startAgain : undefined }
    : feed.note;
  async function startAgain() {
    try {
      const { target: next } = await startFor(pane.id, { sessionFile: binding?.sessionFile });
      setStartNote(undefined); setStarted(next); actions.onView(terminalView(next));
    }
    catch (failure) { setStartNote(startSentence(failure)); }
  }

  async function stop() {
    if (!target) return;
    setActionError(undefined);
    try { feed.setInfo(await closeTerminal(target.sessionId, target.terminalId)); }
    catch (error) { setActionError(`Could not stop: ${startSentence(error)}`); }
  }
  async function remove() {
    setActionError(undefined);
    if (target) {
      try { await removeTerminal(target.sessionId, target.terminalId); }
      catch (error) { setActionError(`Could not remove: ${startSentence(error)}`); return; }
    }
    unbind(pane.id);
    announceClosePane({ paneId: pane.id });
  }
  /** A finished job again: the same command, as a new job tab under the same conversation (Components, "Finished job · tab menu"). */
  const finishedJob = info?.kind === 'job' && !running;
  const command = finishedJob ? info?.command : undefined;
  const rerun = command && info ? () => announceOpenTerminal({ sessionFile: binding?.sessionFile, command, title: info.title }) : undefined;
  const onRemove = () => { void remove(); };
  const onClose = () => announceClosePane({ paneId: pane.id });
  // The same functions the header receives. Published during render, not in an effect: the summary
  // layout effect above re-renders the strip before a later effect would run, and that render is the one that builds the tab menu.
  menuFacts.current = { finished: finishedJob, onRerun: rerun, onRemove };
  offerTerminalTabMenu(pane.id, () => menuFacts.current);
  // Published during render, like the menu: the summary effect above re-renders the strip, and that render reads the words.
  metaFacts.current = info ? { kind: info.kind, state: info.state, exitCode: info.exitCode } : undefined;
  offerTerminalTabMeta(pane.id, () => metaFacts.current);
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
      canStop={running} finishedJob={finishedJob} onClose={onClose} onRerun={rerun} removeLabel={removeLabel(info?.kind ?? 'terminal')} onStop={() => void stop()} readOutput={readOutput} onRemove={onRemove}/>
    {actionError && <Text className="terminal-action-error" role="alert">{actionError}</Text>}
    <div className="terminal-field" data-kind={info?.kind} data-note={refusal ? '' : undefined}>
      {refusal
        ? <div className="terminal-note"><Text>{refusal.text}</Text>{refusal.retry && <Button onClick={refusal.retry}>Try again</Button>}</div>
        : <TerminalScreen label={`${info?.title ?? pane.title} terminal`} interactive={running} cursor={running} onData={feed.send} onResize={feed.resize} onReady={setScreen}/>}
    </div>
    {!refusal && <AskField onAsk={ask}/>}
  </div>;
}
