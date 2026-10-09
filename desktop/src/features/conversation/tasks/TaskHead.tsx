import { DropdownMenu, Icon, IconButton, PageHeading, type MenuEntry } from '../../../components/ui';
import type { TaskPageModel } from '../taskPageTurn';
import './task-head.css';

export type HeadActions = {
  onPause?: () => void;
  onResume?: () => void;
  onStop?: () => void;
};

/** A running task is a calm accent dot, a waiting one amber, a failed one red; the rest keep their quiet icon. */
const DOT_KINDS: ReadonlySet<string> = new Set(['running', 'yourcall', 'incomplete']);

function Mark({ model }: { model: TaskPageModel }) {
  const { mark } = model;
  return (
    <span className="task-view-mark" data-kind={mark.kind} role="img" aria-label={mark.label}>
      {DOT_KINDS.has(mark.kind) ? <span className="task-view-dot" /> : <Icon name={mark.icon} size="xs" />}
    </span>
  );
}

/** "● Running  step 7 · 2m 15s · Flash · $0.06": the state word at ink-2, then only the parts the engine knows. */
function StateLine({ model }: { model: TaskPageModel }) {
  return (
    <div className="task-view-status" data-tone={model.mark.tone}>
      <Mark model={model} />
      <span className="task-view-state-word">{model.mark.label}</span>
      {model.stateParts.length > 0 && <span className="task-view-state-parts">{model.stateParts.join(' · ')}</span>}
    </div>
  );
}

/** The one pause-or-resume verb the task offers right now. */
function PauseButton({ model, actions }: { model: TaskPageModel; actions: HeadActions }) {
  const { pause, resume } = model.controls;
  if (pause && actions.onPause) return <IconButton className="task-view-action" label="Pause" icon="pause" iconSize="sm" onClick={actions.onPause} />;
  if (resume && actions.onResume) return <IconButton className="task-view-action" label="Resume" icon="play" iconSize="sm" onClick={actions.onResume} />;
  return null;
}

function moreEntries(model: TaskPageModel, actions: HeadActions): MenuEntry[] {
  if (!model.controls.stop || !actions.onStop) return [];
  const stop = actions.onStop;
  return [{ id: 'stop', label: 'Stop', icon: 'stop', danger: true, onSelect: stop }];
}

function RowActions({ model, actions }: { model: TaskPageModel; actions: HeadActions }) {
  const entries = moreEntries(model, actions);
  return (
    <span className="task-view-actions">
      <PauseButton model={model} actions={actions} />
      {entries.length > 0 && (
        <DropdownMenu label="Task actions" items={entries}>
          <IconButton className="task-view-action" label="More task actions" icon="more" iconSize="sm" />
        </DropdownMenu>
      )}
    </span>
  );
}

export function TaskHead({ model, actions, actionError }: { model: TaskPageModel; actions: HeadActions; actionError?: string }) {
  return (
    <header className="task-view-head">
      <div className="task-view-titlebar">
        <PageHeading className="task-view-title">{model.title}</PageHeading>
        <RowActions model={model} actions={actions} />
      </div>
      <StateLine model={model} />
      {model.endReason && <p className="task-view-reason">{model.endReason}</p>}
      {actionError && <p className="task-view-action-error" role="alert">{actionError}</p>}
    </header>
  );
}
