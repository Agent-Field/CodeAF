import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { Text } from '../../components/ui';
import { connectEngine, sendEngineWithFiles } from '../chat/engine-client';
import { labelLine } from '../conversation/tabSummary';
import type { PaneRenderProps } from '../tabs/kinds/slots';
import { AskField } from '../terminal/AskField';
import { announceClosePane, announceOpenConversation, announceOpenTerminal } from '../terminal/events';
import { offerTerminalTabMenu, type FinishedJobOffer } from '../terminal/tabMenu';
import { offerTerminalTabMeta, type TabMetaSource } from '../terminal/tabMeta';
import { TerminalHeader } from '../terminal/TerminalHeader';
import { TerminalScreen, type ScreenHandle } from '../terminal/TerminalScreen';
import '../terminal/terminal.css';
import './jobs.css';
import { stopJob } from './client';
import { jobMeta, jobTabMeta, jobTone, jobWords, toScreen } from './words';
import { useJobLog } from './useJobLog';

const tickMs = 1000;
const sentence = (failure: unknown) => (failure instanceof Error ? failure.message : 'The engine did not answer.');

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

/**
 * Feeds the read-only screen: an appended tail is written as the new suffix, anything else (the front was cut,
 * the log restarted) redraws. A first draw pushes a short log to the bottom edge, the way the design shows it.
 */
function useLogScreen(screen: ScreenHandle | null, text: string) {
  const drawn = useRef<string>(undefined);
  useEffect(() => {
    if (!screen) return;
    const before = drawn.current;
    if (before === text) return;
    if (before !== undefined && text.startsWith(before)) screen.write(toScreen(text.slice(before.length)));
    else { screen.reset(); if (text) { screen.pad(); screen.write(toScreen(text)); } }
    drawn.current = text;
  }, [screen, text]);
}

/** Design 3c for an engine job: header, the log on the terminal field, "Ask codeaf about this output" below. Nothing here accepts input. */
export function JobPane({ pane, actions }: PaneRenderProps) {
  const jobId = pane.job?.jobId ?? '';
  const log = useJobLog(pane.sessionFile, jobId);
  const { job } = log;
  const running = job?.state === 'running';
  const now = useNow(running);
  const [screen, setScreen] = useState<ScreenHandle | null>(null);
  const [actionError, setActionError] = useState<string>();
  useLogScreen(screen, log.text);

  const failed = job?.state === 'failed';
  const summarize = useRef(actions.onSummary);
  summarize.current = actions.onSummary;
  const title = job?.name || pane.title;
  useLayoutEffect(() => {
    if (job) summarize.current({ title, firstLine: '', digest: '', mark: failed ? 'failed' : undefined });
  }, [job, title, failed]);

  const menuFacts = useRef<FinishedJobOffer>({ finished: false, onRemove: () => {} });
  const metaFacts = useRef<TabMetaSource | undefined>(undefined);
  const finished = !!job && !running;
  const command = finished ? job.command : undefined;
  const rerun = command ? () => announceOpenTerminal({ sessionFile: pane.sessionFile, command, title }) : undefined;
  // The engine has no route that deletes a job record, so Remove job closes this tab; the record stays until the engine drops it.
  const onRemove = () => announceClosePane({ paneId: pane.id });
  // Published during render, like the shell pane: the strip reads these in the render the summary effect triggers.
  menuFacts.current = { finished, onRerun: rerun, onRemove };
  offerTerminalTabMenu(pane.id, () => menuFacts.current);
  metaFacts.current = jobTabMeta(job);
  offerTerminalTabMeta(pane.id, () => metaFacts.current);

  async function stop() {
    if (!log.sessionId) return;
    setActionError(undefined);
    try { await stopJob(log.sessionId, jobId); log.refresh(); }
    catch (error) { setActionError(`Could not stop: ${sentence(error)}`); }
  }
  async function readOutput() {
    const selected = screen?.selection() ?? '';
    return selected.trim() ? selected : log.text;
  }
  /** Q5: the question opens a NEW conversation with the log (or the selection) attached as a file. */
  async function ask(question: string) {
    const picked = screen?.selection().trim();
    const text = picked || log.text;
    if (!text.trim()) throw new Error('There is no output to ask about yet.');
    const target = (await connectEngine()).id;
    const name = `${title.replace(/[^\w.-]+/g, '-')}-${picked ? 'selection' : 'output'}.txt`;
    const bytes = new TextEncoder().encode(text);
    let binary = '';
    for (const byte of bytes) binary += String.fromCharCode(byte);
    const snapshot = await sendEngineWithFiles(target, question.trim() || 'What is going on in this output?', [{ name, mime: 'text/plain', dataBase64: btoa(binary) }]);
    announceOpenConversation({ sessionFile: snapshot.sessionFile, title: question.trim() ? labelLine(question) : `About ${title}` });
  }

  const note = log.gone ? 'The engine no longer has this job.' : !job ? log.error : undefined;
  return <div className="terminal-pane job-pane">
    <TerminalHeader
      title={title} meta={job ? jobMeta() : ''} words={job ? jobWords(job, now, log.readAt) : ''} tone={job ? jobTone(job) : undefined}
      canStop={running} finishedJob={finished} onClose={() => announceClosePane({ paneId: pane.id })} onRerun={rerun} removeLabel="Remove job"
      onStop={() => void stop()} readOutput={readOutput} onRemove={onRemove}/>
    {actionError && <Text className="terminal-action-error" role="alert">{actionError}</Text>}
    {log.truncated && <Text className="job-trim">Earlier output was trimmed. Showing the end of the log.</Text>}
    <div className="terminal-field" data-kind="job" data-note={note ? '' : undefined}>
      {note
        ? <div className="terminal-note"><Text>{note}</Text></div>
        : <TerminalScreen label={`${title} log`} interactive={false} cursor={false} onReady={setScreen}/>}
    </div>
    {!note && <AskField onAsk={ask}/>}
  </div>;
}
