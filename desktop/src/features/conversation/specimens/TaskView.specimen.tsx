import { useState } from 'react';
import type { EngineTaskPage } from '../../chat/engine-client';
import { Breadcrumb } from '../Breadcrumb';
import { TaskPageBody } from '../TaskView';

const page: EngineTaskPage = {
  Row: { ID: 't-2', Title: 'Tighten the retry loop', Status: 'done' },
  Description: 'Make the **retry loop** back off, and cap it at five tries.',
  Steps: [
    { step: 1, command: 'rg retry internal/client', observation: 'client.go:41' },
    { step: 2, command: 'go test ./internal/client', observation: 'ok' },
  ],
  Result: 'Retries now back off and stop after five tries.',
  Checks: ['Retry tests pass', 'Backoff is capped'],
  Notes: [{ Body: 'Left the timeout alone.' }],
  Children: [{ ID: 't-3', Title: 'Add a retry test', Status: 'done' }],
};


export function TaskViewSpecimen() {
  const [opened, setOpened] = useState('');
  return (
    <div>
      <Breadcrumb
        segments={[
          { id: null, label: 'Fix the client' },
          { id: 't-1', label: 'Reliability' },
          { id: 't-2', label: page.Row.Title },
        ]}
        onNavigate={(id) => setOpened(id ?? 'conversation')}
        canBack
        canForward={false}
        onBack={() => setOpened('back')}
        onForward={() => setOpened('forward')}
      />
      <TaskPageBody page={page} renderItem={(item) => <p>{item.kind}</p>} onOpenTask={(id) => setOpened(id)} />
      {opened && <p>{opened}</p>}
    </div>
  );
}
