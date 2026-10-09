import { useCallback, useEffect, useRef, useState } from 'react';
import { readTaskPage, taskAction, type EngineTaskRow, type TaskAction } from '../chat/engine-client';
import { readEngineText } from './tasks/readText';
import { Button, Text } from '../../components/ui';
import type { ReadFile } from './tasks/LogStep';
import { TaskComposer } from './tasks/TaskComposer';
import { TaskHead, type HeadActions } from './tasks/TaskHead';
import { TaskLinks } from './tasks/TaskLinks';
import { TaskNotes, type Outbox } from './tasks/TaskNotes';
import { InstructionsCard } from './tasks/InstructionsCard';
import { Checks, ChangedFiles, LastWords, Result, type RenderFile } from './tasks/TaskSections';
import type { TaskPage } from './tasks/taskTypes';
import { useNow } from './tasks/useNow';
import { WorkLog } from './tasks/WorkLog';
import { isEnded, rowKind } from './taskState';
import { isTaskRunning, taskPageModel } from './taskPageTurn';
import './task-view.css';

const REFRESH_MS = 3000;

type OpenTask = (taskId: string, background: boolean) => void;

export type TaskPageBodyProps = {
  page: TaskPage;
  now: number;
  onOpenTask: OpenTask;
  actions?: HeadActions;
  actionError?: string;
  renderFile?: RenderFile;
  readFile?: ReadFile;
  /** A note being sent, drawn as a bubble until the engine's record replaces it. */
  outbox?: Outbox;
  /** Changes the brief; offered only while the task can still hear it. */
  onAmend?: (text: string) => Promise<void> | void;
};

/** A task is a conversation: result first, then the work, the notes, the brief. */
export function TaskPageBody({ page, now, onOpenTask, actions = {}, actionError, renderFile, readFile, outbox, onAmend }: TaskPageBodyProps) {
  const model = taskPageModel(page, now);
  return (
    <article className="task-view-body">
      <TaskHead model={model} actions={actions} actionError={actionError} />
      <Result text={model.result} />
      <LastWords text={model.lastWords} shown={model.ended && !model.result} />
      <ChangedFiles paths={model.changed} renderFile={renderFile} />
      <InstructionsCard text={model.instructions} onAmend={model.ended ? undefined : onAmend} />
      <WorkLog steps={model.steps} live={model.live} ended={model.ended} now={now} readFile={readFile} />
      <TaskNotes notes={model.notes} outbox={outbox} />
      <Checks checks={model.checks} />
      <TaskLinks label="Tasks inside this one" links={model.children} onOpenTask={onOpenTask} />
      <TaskLinks label="Waits on" links={model.waits} onOpenTask={onOpenTask} />
    </article>
  );
}

type Load = { page?: TaskPage; error?: string; loading: boolean };

function useTaskPage(sessionId: string, taskId: string) {
  const [load, setLoad] = useState<Load>({ loading: true });
  const generation = useRef(0);
  const timer = useRef<number | undefined>(undefined);

  const fetchPage = useCallback(async () => {
    window.clearTimeout(timer.current);
    const mine = ++generation.current;
    try {
      const page = (await readTaskPage(sessionId, taskId)) as TaskPage;
      if (mine !== generation.current) return;
      setLoad({ page, loading: false });
      if (isTaskRunning(page)) timer.current = window.setTimeout(fetchPage, REFRESH_MS);
    } catch (error) {
      if (mine !== generation.current) return;
      const message = error instanceof Error ? error.message : 'Could not load this task.';
      setLoad((previous) => ({ page: previous.page, error: message, loading: false }));
    }
  }, [sessionId, taskId]);

  useEffect(() => {
    setLoad({ loading: true });
    void fetchPage();
    return () => {
      generation.current += 1;
      window.clearTimeout(timer.current);
    };
  }, [fetchPage]);

  const retry = useCallback(() => {
    setLoad((previous) => ({ ...previous, error: undefined }));
    void fetchPage();
  }, [fetchPage]);
  return { load, retry, refresh: fetchPage };
}

const messageOf = (error: unknown) => (error instanceof Error ? error.message : 'That did not go through.');

export type TaskViewProps = {
  sessionId: string;
  taskId: string;
  onOpenTask: OpenTask;
  /** Draws a changed file (the file chip); without it the path is shown as text. */
  renderFile?: RenderFile;
  readFile?: ReadFile;
  /** The conversation's own row for this task: the same live state the task panel draws, so the two cannot disagree. */
  liveRow?: EngineTaskRow;
  /** The way back from a finished task to the conversation. */
  onMessageConversation?: () => void;
  now?: number;
};

/** The page read, with its row replaced by the live one: the panel and this view then say one thing. */
function withLiveRow(page: TaskPage | undefined, liveRow: EngineTaskRow | undefined): TaskPage | undefined {
  return page && liveRow ? { ...page, Row: { ...page.Row, ...liveRow } } : page;
}

export function TaskView({ sessionId, taskId, liveRow, onOpenTask, renderFile, readFile, onMessageConversation, now: given }: TaskViewProps) {
  const { load, retry, refresh } = useTaskPage(sessionId, taskId);
  const page = withLiveRow(load.page, liveRow);
  const liveEnded = liveRow ? isEnded(rowKind(liveRow)) : undefined;
  const pageEnded = load.page ? isEnded(rowKind(load.page.Row)) : undefined;
  // The live row says the task is over before the page read knows it: read the page again, for its result.
  useEffect(() => {
    if (liveEnded !== undefined && pageEnded !== undefined && liveEnded !== pageEnded) void refresh();
  }, [liveEnded, pageEnded, refresh]);
  const [outbox, setOutbox] = useState<Outbox>();
  const [actionError, setActionError] = useState('');
  const now = useNow(isTaskRunning(page), given);
  const read = readFile ?? ((path: string) => readEngineText(sessionId, path));

  async function act(action: TaskAction, text?: string) {
    await taskAction(sessionId, taskId, action, text);
    await refresh();
  }
  const control = (action: TaskAction) => async () => {
    setActionError('');
    try {
      await act(action);
    } catch (error) {
      setActionError(messageOf(error));
    }
  };
  async function note(text: string) {
    setOutbox({ text });
    try {
      await act('note', text);
    } catch (error) {
      // The engine refused (a task that has ended answers 409): read the page again so the view catches up.
      void refresh();
      throw error;
    } finally {
      setOutbox(undefined);
    }
  }

  const actions: HeadActions = { onPause: control('pause'), onResume: control('resume'), onStop: control('cancel') };
  const model = page ? taskPageModel(page, now) : undefined;
  return (
    <div className="task-view">
      {load.loading && !load.page && <Text className="task-view-quiet">Loading…</Text>}
      {load.error && (
        <div className="task-view-error" role="alert">
          <Text>{load.error}</Text>
          <Button onClick={retry}>Retry</Button>
        </div>
      )}
      {page && (
        <>
          <TaskPageBody {...{ page, now, onOpenTask, actions, actionError, renderFile, outbox, readFile: read, onAmend: (text: string) => act('amend', text) }} />
          <TaskComposer
            ended={Boolean(model?.ended)}
            onNote={note}
            onMessageConversation={onMessageConversation}
          />
        </>
      )}
    </div>
  );
}
