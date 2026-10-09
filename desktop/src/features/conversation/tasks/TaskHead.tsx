import { Button, Icon, PageHeading, type IconName } from '../../../components/ui';
import type { TaskPageModel } from '../taskPageTurn';

export type HeadActions = {
  onPause?: () => void;
  onResume?: () => void;
  onStop?: () => void;
};

/** "● Running · step 7 · 2m 14s · Deepseek v4.1 Flash": only the parts the engine knows. */
function StateLine({ model }: { model: TaskPageModel }) {
  const words = [model.mark.label, ...model.stateParts];
  return (
    <div className="task-view-status" data-tone={model.mark.tone}>
      <span className="state-mark" data-tone={model.mark.tone} role="img" aria-label={model.mark.label}>
        <Icon name={model.mark.icon} size="xs" />
      </span>
      <span>{words.join(' · ')}</span>
    </div>
  );
}

type Control = { id: string; label: string; icon: IconName; run: () => void };

function controlsOf(model: TaskPageModel, actions: HeadActions): Control[] {
  const { pause, resume, stop } = model.controls;
  const all: Control[] = [
    { id: 'pause', label: 'Pause', icon: 'pause', run: actions.onPause ?? noop },
    { id: 'resume', label: 'Resume', icon: 'play', run: actions.onResume ?? noop },
    { id: 'stop', label: 'Stop', icon: 'stop', run: actions.onStop ?? noop },
  ];
  const wanted = [pause && actions.onPause, resume && actions.onResume, stop && actions.onStop];
  return all.filter((_, index) => Boolean(wanted[index]));
}

function noop() {}

function Controls({ model, actions }: { model: TaskPageModel; actions: HeadActions }) {
  const buttons = controlsOf(model, actions);
  if (buttons.length === 0) return null;
  return (
    <div className="task-view-actions">
      {buttons.map((button) => (
        <Button key={button.id} className="task-view-action" onClick={button.run}>
          <Icon name={button.icon} size="xs" />
          <span>{button.label}</span>
        </Button>
      ))}
    </div>
  );
}

export function TaskHead({ model, actions, actionError }: { model: TaskPageModel; actions: HeadActions; actionError?: string }) {
  return (
    <header className="task-view-head">
      <PageHeading className="task-view-title">{model.title}</PageHeading>
      <StateLine model={model} />
      {model.endReason && <p className="task-view-reason">{model.endReason}</p>}
      <Controls model={model} actions={actions} />
      {actionError && <p className="task-view-action-error" role="alert">{actionError}</p>}
    </header>
  );
}
