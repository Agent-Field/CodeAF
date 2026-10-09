// The preview body of each tab kind (Shell 3k): kind, state, title, then the one piece that matters.
// Conversation and task are live from the engine's summary; file, diff and terminal read through the engine
// client while the card is open; web, settings and history draw what the tab itself knows.
import { engineFileDiff, readEngineText, readTerminalOutput, terminalStateWords } from '../../chat/engine-client';
import type { PreviewRenderProps } from '../kinds/slots';
import { routeTask } from '../view-state';
import { askOf, changedLines, headLines, questionsFor, stateOf, tailLines, type Ask } from './content';
import { PreviewButtons, PreviewCard, PreviewError, PreviewField, PreviewShot, PreviewState, PreviewText } from './PreviewCard';
import { addressOf, targetOf } from './target';
import { useLoaded } from './useLoaded';

/** "Allow all" and "Review" when permissions wait; "Review" alone for any other question. */
function Actions({ ask, act }: { ask: Ask; act: PreviewRenderProps['act'] }) {
  return <PreviewButtons busy={act.busy} primary={ask.permissions ? { label: ask.count > 1 ? 'Allow all' : 'Allow', onClick: act.allowAll } : undefined} secondary={{ label: 'Review', onClick: act.review }}/>;
}

/** A conversation or a task: the state, then what it asks or its last reply. A card that needs you carries its primary action. */
export function SessionPreview({ pane, title, summary, act }: PreviewRenderProps) {
  const taskId = pane.kind === 'task' && pane.route ? routeTask(pane.route) : undefined;
  const state = stateOf(summary, taskId);
  const ask = askOf(questionsFor(summary, taskId));
  const draft = pane.draft.trim();
  const body = ask?.text ?? (summary?.digest ? `Last reply: “${summary.digest}”` : draft ? `Draft: ${draft}` : '');
  return (
    <PreviewCard kind={pane.kind} lead={state.lead} title={title} state={state.words && <PreviewState dot={state.dot}>{state.words}</PreviewState>} actions={ask && <Actions ask={ask} act={act}/>}>
      {body && <PreviewText>{body}</PreviewText>}
      {act.error && <PreviewError>{act.error}</PreviewError>}
    </PreviewCard>
  );
}

export function TerminalPreview({ pane, title, summary, now }: PreviewRenderProps) {
  const target = targetOf(pane);
  const session = target.sessionId ?? summary?.sessionId;
  const output = useLoaded(session && target.terminalId ? `${session}/${target.terminalId}` : undefined, () => readTerminalOutput(session!, target.terminalId!, 4096));
  return (
    <PreviewCard kind="terminal" title={title} state={output && terminalStateWords(output.info, now)}>
      {output && <PreviewField label="Last output" lines={tailLines(output.text)}/>}
    </PreviewCard>
  );
}

export function DiffPreview({ pane, title, summary }: PreviewRenderProps) {
  const target = targetOf(pane);
  const session = target.sessionId ?? summary?.sessionId;
  const diff = useLoaded(session && target.path ? `${session}/${target.path}` : undefined, () => engineFileDiff(session!, target.path!));
  return (
    <PreviewCard kind="diff" title={title} state={diff && <><span className="preview-diff-add">+{diff.added}</span><span className="preview-diff-del">−{diff.deleted}</span></>}>
      {diff && <PreviewField label="First changes" lines={changedLines(diff)}/>}
    </PreviewCard>
  );
}

export function FilePreview({ pane, title, summary }: PreviewRenderProps) {
  const target = targetOf(pane);
  const session = target.sessionId ?? summary?.sessionId;
  const file = useLoaded(session && target.path ? `${session}/${target.path}` : undefined, () => readEngineText(session!, target.path!));
  return (
    <PreviewCard kind="file" title={title} state={file && !file.refusal && `${file.lines} ${file.lines === 1 ? 'line' : 'lines'}`}>
      {file?.refusal ? <PreviewText>{file.message}</PreviewText> : file && <PreviewField label="First lines" lines={headLines(file.text)}/>}
    </PreviewCard>
  );
}

export function WebPreview({ pane, title }: PreviewRenderProps) {
  const target = targetOf(pane);
  return <PreviewShot title={title} address={target.url ? addressOf(target.url) : ''} image={target.shot}/>;
}

/** Settings, history, the inbox and a new tab have nothing to quote: kind and title, and a draft if one is typed. */
export function PlainPreview({ pane, title }: PreviewRenderProps) {
  const draft = pane.draft.trim();
  return <PreviewCard kind={pane.kind} title={title}>{draft && <PreviewText>{`Draft: ${draft}`}</PreviewText>}</PreviewCard>;
}
