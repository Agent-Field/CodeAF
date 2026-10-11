import { Markdown, TextArea } from '../../../components/ui';
import type { TabSummary } from '../../conversation/tabSummary';
import type { Pane } from '../model';
import { kindDef } from '../kinds/registry';
import type { PreviewActions } from '../kinds/slots';
import { routeTask } from '../view-state';
import { askOf, questionsFor } from './content';
import { PreviewContents } from './PreviewCard';
import '../../conversation/conversation-view.css';
import '../../conversation/composer.css';

const idle: PreviewActions = { busy: false, allowAll: () => {}, review: () => {} };

/**
 * Half-scale panes quote the same real summary as the grid, inside the workspace's reading column.
 * They never attach a session, start a terminal, mount a native webview or load a full transcript.
 */
export function FilmstripPane({ pane, summary, now }: { pane: Pane; summary?: TabSummary; now: number }) {
  const session = pane.kind === 'conversation' || pane.kind === 'task' || pane.kind === 'newtab';
  const Preview = kindDef(pane.kind).preview;
  if (!session) return <div className="overview-preview-body"><PreviewContents><Preview pane={pane} title={pane.title} summary={summary} now={now} act={idle}/></PreviewContents></div>;
  const task = pane.kind === 'task' && pane.route ? routeTask(pane.route) : undefined;
  const question = askOf(questionsFor(summary, task));
  const command = task ? summary?.taskLive?.[task]?.command : undefined;
  const text = question?.text ?? command ?? summary?.digest;
  return <div className="conversation-view"><div className="conversation-main">
    <div className="conversation-scroll"><div className="conversation-column">{text && <Markdown>{text}</Markdown>}</div></div>
    <div className="conversation-footer conversation-column"><div className="composer"><TextArea className="composer-field" aria-label="Draft" value={pane.draft} readOnly/></div></div>
  </div></div>;
}
