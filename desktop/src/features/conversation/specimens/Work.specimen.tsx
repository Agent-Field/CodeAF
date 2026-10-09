import { useState } from 'react';
import { Text } from '../../../components/ui';
import type { ToolStep, WorkBlock, WorkStep } from '../types';
import { ThinkingView } from '../work/ThinkingView';
import { ToolCallRow } from '../work/ToolCallRow';
import { WorkBlockView } from '../work/WorkBlockView';
import '../work/work.css';

// A fixed clock keeps the specimen still: elapsed times never drift between screenshots.
const NOW = 1_800_000_000_000;

const call = (id: string, tool: string, args: object, state: ToolStep['state'], output = '', extra: Partial<ToolStep> = {}): ToolStep => ({
  id,
  callId: id,
  tool,
  hint: '',
  args: JSON.stringify(args),
  output,
  state,
  ...extra,
});

const step = (id: string, title: string, category: string, state: WorkStep['state'], calls: ToolStep[], tookMs?: number): WorkStep => ({
  id,
  title,
  titleSource: 'caption',
  category,
  calls,
  state,
  tookMs,
});

const editArgs = {
  path: 'internal/session/agent.go',
  edits: [
    {
      oldText: 'func (a *Agent) shapeEntries(raw []Entry) []DisplayEntry {\n\tout := make([]DisplayEntry, 0)\n\tfor _, e := range raw {\n\t\tout = append(out, shape(e))\n\t}\n\treturn out\n}',
      newText: 'func (a *Agent) shapeEntries(raw []Entry) []DisplayEntry {\n\tout := make([]DisplayEntry, 0, len(raw))\n\tfor _, e := range raw {\n\t\tif e.Role == "note" {\n\t\t\tcontinue\n\t\t}\n\t\tout = append(out, shape(e))\n\t}\n\treturn out\n}',
    },
  ],
};

const goTest = 'ok  \tcodeaf/internal/session\t4.812s\n--- FAIL: TestShapeEntriesKeepsNotes (0.00s)\n    agent_test.go:212: want 3 entries, got 2\nFAIL\nFAIL\tcodeaf/internal/tui3\t1.904s\n\nCommand exited with code 1';

const running: WorkBlock = {
  kind: 'work',
  id: 'running',
  live: true,
  startedAt: NOW - 42_000,
  notes: [],
  thinking: {
    streaming: true,
    text: 'The caption only rides on the first call of a batch.\nSo the step title has to come from the anchor, not the later calls.\nNarration before the batch wins when it exists.\nI should check how an empty narration is skipped.',
  },
  summary: { steps: 2, calls: 4, failed: 0 },
  steps: [
    step('s1', 'Read the display record shaping', 'read', 'done', [
      call('c1', 'read', { path: 'internal/session/agent.go' }, 'done', 'func (a *Agent) shapeEntries...\n[Showing lines 4940-5100 of 5400]', { tookMs: 120 }),
      call('c2', 'grep', { pattern: 'CaptionCategory' }, 'done', 'internal/session/agent.go:4702\ninternal/session/caption.go:31', { tookMs: 80 }),
    ], 200),
    step('s2', 'Running the session tests', 'test', 'running', [
      call('c3', 'bash', { command: 'go test ./internal/session -run TestShape' }, 'running', '', { startedAt: NOW - 12_000 }),
      call('c4', 'write', { path: 'internal/session/shape_test.go' }, 'running', '', {}),
    ]),
  ],
};

const forming: WorkBlock = {
  ...running,
  id: 'forming',
  thinking: undefined,
  steps: [
    running.steps[0],
    step('s3', 'Editing the shaping loop', 'edit', 'preparing', [call('c5', 'edit', { path: 'internal/session/agent.go' }, 'running', '')]),
  ],
};

const settled: WorkBlock = {
  kind: 'work',
  id: 'settled',
  live: false,
  notes: [],
  thinking: { text: 'Notes must survive shaping, so the filter is wrong.', streaming: false, seconds: 6 },
  summary: { seconds: 42, thoughtSeconds: 6, steps: 4, calls: 6, failed: 1 },
  steps: [
    step('t1', 'Searched for where notes are dropped', 'search', 'done', [call('d1', 'grep', { pattern: 'Role == "note"' }, 'done', 'internal/session/agent.go:4961', { tookMs: 880 })], 880),
    step('t2', 'Edited the shaping loop', 'edit', 'done', [call('d2', 'edit', editArgs, 'done', 'Successfully replaced 1 block(s) in internal/session/agent.go.', { tookMs: 340 })], 340),
    step('t3', 'Ran the tests', 'test', 'failed', [
      call('d3', 'bash', { command: 'go test ./internal/session ./internal/tui3' }, 'failed', goTest, { tookMs: 18_400 }),
      call('d4', 'bash', { command: 'go vet ./...' }, 'stopped', '', { tookMs: 2_100 }),
    ], 20_500),
    step('t4', 'Fetched the Go testing docs', 'browse', 'stopped', [call('d5', 'web_fetch', { url: 'https://pkg.go.dev/testing' }, 'stopped', '')]),
  ],
};

function Labelled({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <section className="work-specimen-section">
      <Text className="work-specimen-label">{label}</Text>
      {children}
    </section>
  );
}

function OpenCall({ call: sample }: { call: ToolStep }) {
  const [open, setOpen] = useState(true);
  return <ToolCallRow call={sample} open={open} onToggle={() => setOpen(!open)} now={NOW} readFull={async () => ({ output: sample.output, full: true })} />;
}

const writeCall = call('w1', 'write', { path: 'internal/session/shape_test.go', content: 'package session\n\nfunc TestShapeEntriesKeepsNotes(t *testing.T) {\n\tgot := shape(notes)\n\tif len(got) != 3 {\n\t\tt.Fatalf("want 3, got %d", len(got))\n\t}\n}\n' }, 'done', 'Successfully wrote 156 bytes', { tookMs: 210 });

/** The call row in every state, one line each, as components §3.3 lists them. */
function CallStates() {
  const row = (sample: ToolStep, phase?: 'forming' | 'waiting' | 'refused') => (
    <ToolCallRow key={sample.id} call={sample} phase={phase} open={false} onToggle={() => undefined} now={NOW} />
  );
  return (
    <div className="work-calls">
      {row(call('t1', 'bash', { command: 'go build ./...' }, 'running'), 'forming')}
      {row(call('t2', 'bash', { command: 'rm -rf build' }, 'running'), 'waiting')}
      {row(call('t3', 'bash', { command: 'go test ./...' }, 'running', '', { startedAt: NOW - 12_000 }))}
      {row(call('t4', 'bash', { command: 'make build' }, 'done', '', { tookMs: 1200 }))}
      {row(call('t5', 'bash', { command: 'make build' }, 'failed', 'Command exited with code 2', { tookMs: 4100 }))}
      {row(call('t6', 'bash', { command: 'go generate ./...' }, 'stopped'))}
      {row(call('t7', 'bash', { command: 'git push --force' }, 'running'), 'refused')}
    </div>
  );
}

/** The work rhythm with realistic engine data (research notes); the clock is fixed. */
export function WorkSpecimen() {
  return (
    <div className="work-specimen">
      <Labelled label="Running: a finished step, then tests in flight"><WorkBlockView block={running} now={NOW} /></Labelled>
      <Labelled label="Running: a call still forming"><WorkBlockView block={forming} now={NOW} /></Labelled>
      <Labelled label="Settled, opened: failed and stopped steps"><WorkBlockView block={settled} open onToggle={() => undefined} /></Labelled>
      <Labelled label="Settled, folded"><WorkBlockView block={{ ...settled, id: 'folded' }} /></Labelled>
      <Labelled label="Edit as a diff"><OpenCall call={settled.steps[1].calls[0]} /></Labelled>
      <Labelled label="Write preview"><OpenCall call={writeCall} /></Labelled>
      <Labelled label="Tool call row states"><CallStates /></Labelled>
      <Labelled label="Bash output"><OpenCall call={settled.steps[2].calls[0]} /></Labelled>
      <Labelled label="Thinking, live and settled">
        <ThinkingView streaming text={running.thinking?.text ?? ''} />
        <ThinkingView streaming={false} text="Notes must survive shaping, so the filter is wrong." seconds={6} />
      </Labelled>
    </div>
  );
}
