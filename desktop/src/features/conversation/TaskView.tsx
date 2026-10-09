import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
import { readTaskPage, type EngineTaskPage } from '../chat/engine-client';
import { Button, Icon, Markdown, PageHeading, Text } from '../../components/ui';
import { StateMark } from './StateMark';
import { TaskNotice } from './TaskNotice';
import { isTaskRunning, taskPageModel, type TaskPageModel } from './taskPageTurn';
import { taskMark } from './taskState';
import type { TurnItem } from './types';
import './task-view.css';

const REFRESH_MS = 3000;

type RenderItem = (item: TurnItem) => ReactNode;
type OpenTask = (taskId: string, background: boolean) => void;

function StatusLine({ model }: { model: TaskPageModel }) {
  const label = taskMark(model.status, model.stopped).label;
  return (
    <div className="task-view-status">
      <StateMark status={model.status} stopped={model.stopped} />
      <span>{model.detail ? `${label} \u00b7 ${model.detail}` : label}</span>
    </div>
  );
}

/** The worker brief is for the engine; it stays one quiet step away. */
function Instructions({ text }: { text: string }) {
  const [open, setOpen] = useState(false);
  if (!text.trim()) return null;
  return (
    <div className="task-view-instructions">
      <Button className="task-view-disclosure" aria-expanded={open} onClick={() => setOpen(!open)}>
        <Icon name="chevron" size="xs" motion="disclosure" />
        <span>Instructions</span>
      </Button>
      {open && (
        <div className="task-view-brief">
          <Markdown>{text}</Markdown>
        </div>
      )}
    </div>
  );
}

function Checks({ checks }: { checks: string[] }) {
  if (checks.length === 0) return null;
  return (
    <ul className="task-view-list" aria-label="Checks">
      {checks.map((check, index) => (
        <li key={index} className="task-view-check">
          <Icon name="checklist" size="xs" />
          <span>{check}</span>
        </li>
      ))}
    </ul>
  );
}

type BodyProps = { page: EngineTaskPage; renderItem: RenderItem; onOpenTask: OpenTask };

export function TaskPageBody({ page, renderItem, onOpenTask }: BodyProps) {
  const model = taskPageModel(page);
  return (
    <article className="task-view-body">
      <header className="task-view-head">
        <PageHeading className="task-view-title">{model.title}</PageHeading>
        <StatusLine model={model} />
      </header>
      {model.result && <Markdown>{model.result}</Markdown>}
      {model.worked && <div className="task-view-worked">{renderItem(model.worked)}</div>}
      <Instructions text={model.instructions} />
      <Checks checks={model.checks} />
      {model.notes.map((note, index) => (
        <Text key={index} className="task-view-note">{note}</Text>
      ))}
      {model.children.length > 0 && (
        <div className="task-view-children">
          {model.children.map((child) => (
            <TaskNotice key={child.id} item={child} open={false} onToggle={() => undefined} onOpenTask={onOpenTask} />
          ))}
        </div>
      )}
    </article>
  );
}

type Load = { page?: EngineTaskPage; error?: string; loading: boolean };

function useTaskPage(sessionId: string, taskId: string) {
  const [load, setLoad] = useState<Load>({ loading: true });
  const generation = useRef(0);
  const timer = useRef<number | undefined>(undefined);

  const fetchPage = useCallback(async () => {
    const mine = ++generation.current;
    try {
      const page = await readTaskPage(sessionId, taskId);
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
    window.clearTimeout(timer.current);
    setLoad((previous) => ({ ...previous, error: undefined }));
    void fetchPage();
  }, [fetchPage]);
  return { load, retry };
}

export function TaskView({ sessionId, taskId, renderItem, onOpenTask }: { sessionId: string; taskId: string; renderItem: RenderItem; onOpenTask: OpenTask }) {
  const { load, retry } = useTaskPage(sessionId, taskId);
  return (
    <div className="task-view">
      {load.loading && !load.page && <Text className="task-view-quiet">Loading…</Text>}
      {load.error && (
        <div className="task-view-error" role="alert">
          <Text>{load.error}</Text>
          <Button onClick={retry}>Retry</Button>
        </div>
      )}
      {load.page && <TaskPageBody page={load.page} renderItem={renderItem} onOpenTask={onOpenTask} />}
    </div>
  );
}
