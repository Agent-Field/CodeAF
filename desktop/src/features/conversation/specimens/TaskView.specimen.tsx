import { useState } from 'react';
import { CodeText } from '../../../components/ui';
import { Breadcrumb } from '../Breadcrumb';
import { TaskComposer } from '../tasks/TaskComposer';
import type { TaskPage } from '../tasks/taskTypes';
import { TaskPageBody } from '../TaskView';
import { SPECIMEN_NOW } from './TaskPanel.specimen';

const at = (secondsAgo: number) => new Date(SPECIMEN_NOW - secondsAgo * 1000).toISOString();

export const runningPage: TaskPage = {
  Row: { ID: 't-3', Title: 'Count words in every Markdown file', Status: 'running', Parent: 't-1', Started: at(134), Model: 'deepseek/deepseek-v4.1-flash' },
  Description: 'Count the words in every **Markdown** file under `docs/`, skip generated pages, and report the total.',
  Steps: [
    { step: 1, command: 'cd /run/copy && ls docs', observation: 'ARCHITECTURE.md\nCHAT.md\nELEMENTS.md', parts: [{ Command: 'cd /run/copy', Separator: '&&', Start: 0, End: 12, SepEnd: 15, RunCopyPrefix: true }, { Command: 'ls docs', Start: 16, End: 23 }], took: 100_000_000 },
    { step: 2, command: 'rm -rf docs', refused: true, not_run: true },
    { step: 3, command: 'go test ./...', observation: 'ok  \tinternal/words\t0.412s\nok  \tinternal/walk\t1.208s', full_output: 'tmp/step-3.txt', took: 3_100_000_000 },
    { step: 4, command: 'sed -n 1,40p docs/CHAT.md', observation: '# Conversation spec', took: 100_000_000 },
  ],
  Live: { Step: 5, Command: 'make build', Since: at(12) },
  Notes: [
    { Author: 'chat', Body: 'Started from the conversation: skip anything under docs/generated.', At: at(130) },
    { Person: true, Body: 'also cover the empty case', At: at(70) },
    { Person: true, Body: 'and keep the output short', At: at(5) },
  ],
  Children: [{ ID: 't-3a', Title: 'Skip generated pages', Status: 'running', Depth: 1 }],
  WaitRows: [{ ID: 't-2', Title: 'Load the schema', Status: 'done' }],
};

export const donePage: TaskPage = {
  Row: { ID: 't-2', Title: 'Tighten the retry loop', Status: 'done', Parent: 't-1', Started: at(300), Ended: at(166), Model: 'deepseek/deepseek-v4.1-flash' },
  Description: 'Make the **retry loop** back off, and cap it at five tries.',
  Result: 'Retries now back off exponentially and stop after **five tries**. The client tests pass.',
  Changed: ['internal/client/retry.go', 'internal/client/retry_test.go'],
  Steps: [
    { step: 1, command: 'rg retry internal/client', observation: 'client.go:41', took: 200_000_000 },
    { step: 2, command: 'rm -rf internal/client', refused: true, not_run: true },
    { step: 3, command: 'go test ./internal/client', observation: 'ok', took: 4_200_000_000 },
  ],
  Checks: ['Retry tests pass', 'Backoff is capped'],
  Notes: [{ Person: true, Body: 'Leave the timeout alone.', At: at(250) }],
};

function File({ path }: { path: string }) {
  return <CodeText>{path}</CodeText>;
}

function Frame({ page, last }: { page: TaskPage; last: string }) {
  const [sent, setSent] = useState('');
  return (
    <div className="specimen-task">
      <Breadcrumb
        segments={[
          { id: 't-1', label: 'Reliability' },
          { id: page.Row.ID, label: page.Row.Title },
        ]}
        onNavigate={() => undefined}
        canBack
        canForward={false}
        onBack={() => undefined}
        onForward={() => undefined}
      />
      <TaskPageBody
        page={page}
        now={SPECIMEN_NOW}
        onOpenTask={() => undefined}
        actions={{ onPause: () => undefined, onStop: () => undefined }}
        onAmend={(text) => setSent(text)}
        renderFile={(path) => <File path={path} />}
        readFile={async () => 'the complete output of the command'}
      />
      <TaskComposer
        ended={page.Row.Status === 'done'}
        onNote={(text) => setSent(text)}
        onMessageConversation={() => setSent(last)}
      />
      {sent && <p>{sent}</p>}
    </div>
  );
}

export function TaskViewSpecimen() {
  return (
    <div className="specimen-stack">
      <Frame page={runningPage} last="" />
      <Frame page={donePage} last="back to the conversation" />
    </div>
  );
}
