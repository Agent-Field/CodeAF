// One overview card (design 3h): kind and state line, title, the one piece that matters, footer. Readable text,
// never a miniature screenshot. The body is the kind's preview content (last reply, terminal lines, diff head).
// A running task quotes the live command the engine sent. A card that needs you carries Allow all / Review.
import { useContext, useState, type DragEvent, type MouseEvent } from 'react';
import { Button, ContextMenu, Icon, IconButton, type MenuEntry } from '../../components/ui';
import { answerEngine } from '../chat/engine-client';
import { isMac } from '../../design/keyboard';
import { relativeTime, summarize, type TabMark, type TabSummary } from '../conversation/tabSummary';
import { bulkAnswers } from '../conversation/tray/answers';
import { TabsApiContext } from './context';
import { tabDragType } from './hosts/dragHost';
import { kindDef } from './kinds/registry';
import type { PreviewActions } from './kinds/slots';
import { focusedPane, panesOf, type Tab } from './model';
import { isBackgroundPress, liveCommandLines, overviewCardState } from './overview-model';
import { askOf, questionsFor } from './preview/content';
import { PreviewButtons, PreviewContents, PreviewField } from './preview/PreviewCard';
import { routeTask } from './view-state';

const markRank: TabMark[] = ['waiting', 'failed', 'working'];

/** The state a tab shows: the most urgent mark among its panes (needs you, then failed, then working). */
export function tabMark(tab: Tab, summaries: Readonly<Record<string, TabSummary>>): TabMark | undefined {
  const marks = panesOf(tab).map(pane => summaries[pane.id]?.mark);
  return markRank.find(mark => marks.includes(mark));
}

/** What a card is searched by: the draft, the question, the live command, else the latest answer. Empty when unknown. */
export function cardText(tab: Tab, summaries: Readonly<Record<string, TabSummary>>): string {
  const pane = focusedPane(tab);
  const known = summaries[pane.id];
  const taskId = pane.kind === 'task' && pane.route ? routeTask(pane.route) : undefined;
  const ask = askOf(questionsFor(known, taskId))?.text;
  const command = liveCommandLines(tab, summaries)[0]?.text;
  return pane.draft.trim() || ask || command || known?.digest || known?.firstLine || '';
}

export const kindLabel = (tab: Tab) => (tab.split ? 'Split' : kindDef(tab.kind).label);
export const kindIcon = (tab: Tab) => {
  if (tab.split) return 'split' as const;
  const pane = focusedPane(tab);
  const picked = kindDef(pane.kind).iconFor?.(pane);
  // A file tab's type comes from its path. A title-only glyph remains for a kind that has not filled iconFor.
  if (typeof picked === 'string') return picked;
  return kindDef(pane.kind).glyph?.(pane.title) ?? kindDef(pane.kind).icon;
};

/** The 6px state dot, shared by the card head and the filmstrip label. Running is accent, needs you amber, failed red. */
export function StateDot({ mark }: { mark: TabMark }) {
  return <span className="overview-dot" data-state={mark} aria-hidden="true"/>;
}

/**
 * The press handlers of a card or filmstrip item: a plain click opens, a ⌘/Ctrl-click or a middle-click is the
 * background press. Cancel middle-button autoscroll and guard the ensuing PRIMARY paste separately:
 * Chromium can still send paste to a different focused field after canceled mouse events.
 */
export function backgroundPress(open: () => void, background: () => void) {
  return {
    onClick: (event: MouseEvent) => (isBackgroundPress(event, isMac) ? background() : open()),
    onAuxClick: (event: MouseEvent) => { if (event.button === 1) {
      event.preventDefault();
      // Chromium can paste PRIMARY into the focused filter after a canceled auxclick.
      // Suppress only the paste caused by this background press, then release the guard.
      const doc = event.currentTarget.ownerDocument;
      const preventPaste = (paste: Event) => paste.preventDefault();
      doc.addEventListener('paste', preventPaste, { capture: true, once: true });
      setTimeout(() => doc.removeEventListener('paste', preventPaste, true), 0);
      background();
    } },
    onMouseDown: (event: MouseEvent) => { if (event.button === 1) event.preventDefault(); },
  };
}

/** What went wrong, in the same words the hover preview uses. The buttons stay so the person can try again. */
const failureWords = (error: unknown) => `Not sent. ${error instanceof Error && error.message ? error.message : 'The engine did not answer.'}`;

/**
 * Allow all answers the questions this card shows, on the engine session the summary came from. Review opens the tab.
 * With no workspace api (the Design system specimen) the buttons still draw and Allow all does nothing.
 */
function useCardActions(tab: Tab, onReview: () => void): PreviewActions {
  const api = useContext(TabsApiContext);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const pane = focusedPane(tab);
  const summary = api?.summaries[pane.id];
  const taskId = pane.kind === 'task' && pane.route ? routeTask(pane.route) : undefined;
  return {
    busy,
    error,
    review: onReview,
    allowAll: () => {
      const session = summary?.sessionId;
      if (!api || !session || busy) return;
      setBusy(true);
      setError(undefined);
      void (async () => {
        let latest = summary;
        try {
          for (const answer of bulkAnswers(questionsFor(summary, taskId), 'allow')) {
            latest = summarize(await answerEngine(session, answer));
            api.receiveSummary(pane.id, latest);
          }
          if (questionsFor(latest, taskId).length) setError('Some questions still need you. Review them.');
        } catch (failure) {
          setError(failureWords(failure));
        } finally {
          setBusy(false);
        }
      })();
    },
  };
}

function CardBody({ tab, summaries, now, act }: { tab: Tab; summaries: Readonly<Record<string, TabSummary>>; now: number; act: PreviewActions }) {
  if (tab.split) return <span className="overview-card-panes">{tab.split.panes.map(pane => <span key={pane.id} className="overview-card-pane">{pane.title}</span>)}</span>;
  const pane = focusedPane(tab);
  const summary = summaries[pane.id];
  const taskId = pane.kind === 'task' && pane.route ? routeTask(pane.route) : undefined;
  const ask = askOf(questionsFor(summary, taskId));
  const commands = liveCommandLines(tab, summaries);
  // PreviewContents keeps the overview's own chrome and drops the preview's. The actions live on that dropped
  // chrome, so a card that needs you draws them again here, with the same engine calls.
  const actions = ask && (
    <div className="overview-card-actions preview-actions" onMouseDown={event => event.stopPropagation()} onClick={event => event.stopPropagation()}>
      <PreviewButtons busy={act.busy} primary={ask.permissions ? { label: ask.count > 1 ? 'Allow all' : 'Allow', onClick: act.allowAll } : undefined} secondary={{ label: 'Review', onClick: act.review }}/>
    </div>
  );
  // A running task's one piece is the command in progress. A question outranks it, the same way the hover card does.
  if (!ask && commands.length) return <PreviewField label="Live command" lines={commands}/>;
  const Preview = kindDef(pane.kind).preview;
  if (Preview) return <><PreviewContents><Preview pane={pane} title={tab.title} summary={summary} now={now} act={act}/></PreviewContents>{actions}</>;
  const text = cardText(tab, summaries);
  return text ? <span className="overview-card-text">{text}</span> : null;
}

type Props = {
  modelNames?: Readonly<Record<string, string>>;
  tab: Tab; summaries: Readonly<Record<string, TabSummary>>; now: number;
  active: boolean; cursor: boolean;
  /** The tab's own menu, built by the shared tab menu builder; absent for a tab that has none (the Inbox). */
  menu?: MenuEntry[];
  /** A plain press. */
  onOpen: () => void;
  /** ⌘-click or middle-click: Interactions "Background tab". The tab is already open, so this opens nothing. */
  onBackground: () => void;
  onClose?: () => void;
  /** A card dragged onto this one lands before it (left half) or after it (right half), in this card's section. */
  onDropTab?: (id: string, after: boolean) => void;
};

/** Which half of the card a drag is over: the left half drops before the card, the right half after it. */
const halfOf = (event: DragEvent<HTMLElement>) => { const box = event.currentTarget.getBoundingClientRect(); return event.clientX - box.left > box.width / 2 ? 'after' : 'before'; };

export function OverviewCard({ tab, summaries, now, active, cursor, menu, onOpen, onBackground, onClose, onDropTab, modelNames }: Props) {
  const act = useCardActions(tab, onOpen);
  const drop = onDropTab && {
    onDragOver: (event: DragEvent<HTMLElement>) => {
      if (!event.dataTransfer.types.includes(tabDragType)) return;
      event.preventDefault();
      event.dataTransfer.dropEffect = 'move';
      event.currentTarget.dataset.drop = halfOf(event);
    },
    onDragLeave: (event: DragEvent<HTMLElement>) => { if (!event.currentTarget.contains(event.relatedTarget as Node | null)) delete event.currentTarget.dataset.drop; },
    onDrop: (event: DragEvent<HTMLElement>) => {
      const id = event.dataTransfer.getData(tabDragType);
      const after = halfOf(event) === 'after';
      delete event.currentTarget.dataset.drop;
      if (!id) return;
      // The section under the card sees the drop too (it clears its own highlight) and leaves a handled drop alone.
      event.preventDefault();
      if (id !== tab.id) onDropTab(id, after);
    },
  };
  const state = overviewCardState(tab, summaries);
  const dot = state.lead === 'amber' ? 'waiting' : state.lead === 'danger' ? 'failed' : state.dot ? 'working' : undefined;
  const pane = focusedPane(tab);
  const summary = summaries[pane.id];
  const updated = summary?.updatedAt;
  const model = pane.kind === 'conversation' ? summary?.model : pane.kind === 'task' && pane.route?.taskId ? summary?.taskModels?.[pane.route?.taskId] : undefined;
  const footer = [updated === undefined ? undefined : relativeTime(updated, now), model ? modelNames?.[model] || model : undefined].filter(Boolean).join(' · ');
  const card = (
    <article className="overview-card" draggable {...drop} onDragStart={event => { event.dataTransfer.setData(tabDragType, tab.id); event.dataTransfer.effectAllowed = 'move'; }} data-active={active} data-cursor={cursor} data-card-id={tab.id} data-kind={tab.split ? 'split' : tab.kind}>
      <Button className="overview-card-open" aria-label={`Open ${tab.title}`} aria-current={active || undefined} {...backgroundPress(onOpen, onBackground)}/>
      <div className="overview-card-face">
        <div className="overview-card-head">
          <Icon name={kindIcon(tab)} size="micro"/><span>{kindLabel(tab)}</span>
          {state.words && <span className="overview-card-state">{dot && <StateDot mark={dot}/>}{state.words}</span>}
        </div>
        <span className="overview-card-title">{tab.title}</span>
        <div className="overview-card-body"><CardBody tab={tab} summaries={summaries} now={now} act={act}/></div>
        {footer && <span className="overview-card-foot">{footer}</span>}
      </div>
      {onClose && <IconButton className="overview-card-close" label={`Close ${tab.title}`} icon="close" iconSize="micro" onClick={onClose}/>}
    </article>
  );
  return menu ? <ContextMenu label={`Actions for ${tab.title}`} items={menu}>{card}</ContextMenu> : card;
}
