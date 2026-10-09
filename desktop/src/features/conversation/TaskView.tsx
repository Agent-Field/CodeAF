import { useCallback, useEffect, useRef, useState } from 'react';
import { readEngineFile, readTaskPage, taskAction, type TaskAction } from '../chat/engine-client';
import { Button, Text } from '../../components/ui';
import type { ReadFile } from './tasks/LogStep';
import { TaskComposer } from './tasks/TaskComposer';
import { TaskHead, type HeadActions } from './tasks/TaskHead';
import { TaskLinks } from './tasks/TaskLinks';
import { TaskNotes, type Outbox } from './tasks/TaskNotes';
import { Checks, ChangedFiles, Instructions, LastWords, Result, type RenderFile } from './tasks/TaskSections';
import type { TaskPage } from './tasks/taskTypes';
import { useNow } from './tasks/useNow';
import { WorkLog } from './tasks/WorkLog';
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
};

/** A task is a conversation: result first, then the work, the notes, the brief. */
export function TaskPageBody({ page, now, onOpenTask, actions = {}, actionError, renderFile, readFile, outbox }: TaskPageBodyProps) {
  const model = taskPageModel(page, now);
  return (
    <article className="task-view-body">
      <TaskHead model={model} actions={actions} actionError={actionError} />
      <Result text={model.result} />
      <LastWords text={model.lastWords} shown={model.ended && !model.result} />
      <ChangedFiles paths={model.changed} renderFile={renderFile} />
      <WorkLog steps={model.steps} live={model.live} ended={model.ended} now={now} readFile={readFile} />
      <TaskNotes notes={model.notes} outbox={outbox} />
      <Instructions text={model.instructions} />
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

/** Full command output lives in a file the engine wrote; read it through the engine's file door. */
async function readEngineText(sessionId: string, path: string): Promise<string> {
  const file = await readEngineFile(sessionId, path);
  const bytes = Uint8Array.from(atob(file.dataBase64), (char) => char.charCodeAt(0));
  return new TextDecoder().decode(bytes);
}

export type TaskViewProps = {
  sessionId: string;
  taskId: string;
  onOpenTask: OpenTask;
  /** Draws a changed file (the file chip); without it the path is shown as text. */
  renderFile?: RenderFile;
  readFile?: ReadFile;
  /** The way back from a finished task to the conversation. */
  onMessageConversation?: () => void;
  now?: number;
};

export function TaskView({ sessionId, taskId, onOpenTask, renderFile, readFile, onMessageConversation, now: given }: TaskViewProps) {
  const { load, retry, refresh } = useTaskPage(sessionId, taskId);
  const [outbox, setOutbox] = useState<Outbox>();
  const [actionError, setActionError] = useState('');
  const now = useNow(isTaskRunning(load.page), given);
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
    } finally {
      setOutbox(undefined);
    }
  }

  const actions: HeadActions = { onPause: control('pause'), onResume: control('resume'), onStop: control('cancel') };
  const model = load.page ? taskPageModel(load.page, now) : undefined;
  return (
    <div className="task-view">
      {load.loading && !load.page && <Text className="task-view-quiet">Loading…</Text>}
      {load.error && (
        <div className="task-view-error" role="alert">
          <Text>{load.error}</Text>
          <Button onClick={retry}>Retry</Button>
        </div>
      )}
      {load.page && (
        <>
          <TaskPageBody {...{ page: load.page, now, onOpenTask, actions, actionError, renderFile, outbox, readFile: read }} />
          <TaskComposer
            ended={Boolean(model?.ended)}
            paused={Boolean(model?.controls.resume)}
            onNote={note}
            onAmend={(text) => act('amend', text)}
            onPause={actions.onPause}
            onResume={actions.onResume}
            onStop={actions.onStop}
            onMessageConversation={onMessageConversation}
          />
        </>
      )}
    </div>
  );
}
