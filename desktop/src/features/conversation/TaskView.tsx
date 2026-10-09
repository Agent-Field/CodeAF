import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
import { readTaskPage, type EngineTaskPage } from '../chat/engine-client';
import { Button, Icon, Text } from '../../components/ui';
import { isTaskRunning, taskPageModel } from './taskPageTurn';
import type { Turn } from './types';
import './task-view.css';

const REFRESH_MS = 3000;

type RenderTurn = (turn: Turn, opts: { userFormat: 'literal' | 'markdown' }) => ReactNode;
type OpenTask = (taskId: string, background: boolean) => void;

export function TaskPageBody({ page, renderTurn, onOpenTask }: { page: EngineTaskPage; renderTurn: RenderTurn; onOpenTask: OpenTask }) {
  const model = taskPageModel(page);
  return (
    <div className="task-view-body">
      {renderTurn(model.turn, { userFormat: model.userFormat })}
      {model.checks.length > 0 && (
        <section className="task-view-checks" aria-label="Checks">
          <Text className="task-view-heading">Checks</Text>
          <ul className="task-view-list">
            {model.checks.map((check, index) => (
              <li key={index} className="task-view-check">
                <Icon name="checklist" size="sm" />
                <span>{check}</span>
              </li>
            ))}
          </ul>
        </section>
      )}
      {model.notes.map((note, index) => (
        <Text key={index} className="task-view-note">{note}</Text>
      ))}
      {model.children.length > 0 && (
        <ul className="task-view-list task-view-children">
          {model.children.map((child) => (
            <li key={child.id}>
              <Button
                className="task-view-child"
                disabled={!child.taskId}
                onClick={(event) => child.taskId && onOpenTask(child.taskId, event.metaKey || event.ctrlKey)}
              >
                <span className="task-view-child-title">{child.title}</span>
                <span className="task-view-child-state">{child.status}</span>
              </Button>
            </li>
          ))}
        </ul>
      )}
    </div>
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

export function TaskView({ sessionId, taskId, renderTurn, onOpenTask }: { sessionId: string; taskId: string; renderTurn: RenderTurn; onOpenTask: OpenTask }) {
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
      {load.page && <TaskPageBody page={load.page} renderTurn={renderTurn} onOpenTask={onOpenTask} />}
    </div>
  );
}
