import type { EngineEntry, EngineQuestion } from '../../../src/features/chat/engine-client';
import type { MockFile, Scenario } from './mock-engine';

// Record shapes follow session.DisplayEntry, including the fields the client
// type names only on the v2 model (Took, Attachments).
type WireEntry = EngineEntry & { Took?: number; Attachments?: { Path: string; Name?: string; MIME?: string }[] };

const user = (Text: string, extra: Partial<WireEntry> = {}): WireEntry => ({ Role: 'user', Text, ...extra });
const narrate = (Text: string): WireEntry => ({ Role: 'assistant', Text, Answer: false });
const update = (Text: string): WireEntry => ({ Role: 'assistant', Text, Answer: true, Addressed: true });
const answer = (Text: string): WireEntry => ({ Role: 'assistant', Text, Answer: true });
const call = (CallID: string, Tool: string, args: object, Output: string, extra: Partial<WireEntry> = {}): WireEntry => ({
  Role: 'tool', Text: '', Tool, Hint: Tool, CallID, Answered: true, Args: JSON.stringify(args), Output, Took: 400_000_000, ...extra,
});

export const PNG_1X1 = 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==';
const text = (body: string): MockFile => ({ mime: 'text/plain', dataBase64: Buffer.from(body).toString('base64') });
const png: MockFile = { mime: 'image/png', dataBase64: PNG_1X1 };

const TEST_FILE = 'internal/auth/auth_test.go';
const BEFORE = 'func TestLogin(t *testing.T) {\n\tnow := time.Now()\n\tcheck(t, now)\n}';
const AFTER = 'func TestLogin(t *testing.T) {\n\tnow := fixedClock()\n\tcheck(t, now)\n}';
const LONG_OUTPUT = Array.from({ length: 60 }, (_, i) => `line ${i + 1}: test output`).join('\n');

export const RICH_ANSWER = [
  `Pinned the clock in \`${TEST_FILE}\`, so the login test no longer reads the real time.`,
  '',
  'The fix follows https://pkg.go.dev/time and the suite passes.',
].join('\n');

/** One full turn: an interim update, narrated steps (read, edit, bash, fetch), a generated image and the answer. */
export function richReply(prompt = 'Fix the flaky login test and draw a logo'): Scenario {
  const reply: WireEntry[] = [
    update('Found it: the test reads the real clock. Pinning it now.'),
    narrate('Reading the login test.'),
    call('r1', 'read', { path: TEST_FILE }, BEFORE),
    narrate('Pinning the clock.'),
    call('e1', 'edit', { path: TEST_FILE, oldText: '\tnow := time.Now()', newText: '\tnow := fixedClock()' }, 'Edited 1 place.'),
    call('b1', 'bash', { command: 'go test ./internal/auth' }, LONG_OUTPUT.slice(0, 300)),
    call('w1', 'web_fetch', { url: 'https://pkg.go.dev/time' }, 'time package'),
    call('g1', 'generate_image', { prompt: 'a small lighthouse logo', path: 'out/logo.png' }, 'out/logo.png — 1024×1024 png, 2KB'),
    answer(RICH_ANSWER),
  ];
  return {
    initial: { title: 'Login test', entries: [] },
    turns: [{ entries: reply }],
    tools: { b1: { output: LONG_OUTPUT, full: true } },
    files: { [TEST_FILE]: text(AFTER), 'out/logo.png': png, '.codeaf/attachments/failure.png': png },
  };
}

const ago = (seconds: number) => new Date(Date.now() - seconds * 1000).toISOString();
const later = (seconds: number) => new Date(Date.now() + seconds * 1000).toISOString();

const consent = (id: number, head: string, extra: Partial<EngineQuestion> = {}): EngineQuestion => ({
  id,
  kind: 'consent',
  ask: 'permission',
  head,
  options: [
    { key: '1', label: 'allow once' },
    { key: '2', label: 'always', widening: true },
    { key: '3', label: 'deny', safe: true },
  ],
  scope: ['once', 'always'],
  stakes: 'costly',
  blocking: { turn: true },
  asked: ago(30),
  ...extra,
});

export const choice: EngineQuestion = {
  id: 7,
  kind: 'ask',
  ask: 'choice',
  head: 'Pick a database',
  options: [
    { key: 'sqlite', label: 'SQLite', body: 'One file, no server.' },
    { key: 'postgres', label: 'Postgres', body: 'Shared server.' },
  ],
  asked: ago(40),
};

/** Questions the tray holds at once: a choice (non-blocking) and a set of three permissions. */
export function trayQuestions(): EngineQuestion[] {
  const heads = ['Run `go test ./...`', 'Edit `main.go`', 'Fetch `pkg.go.dev`'];
  return [choice, ...heads.map((head, n) => consent(n + 1, head, { batch: 'step:4', asked: ago(30 - n) }))];
}

/** A task proposal that starts by itself in two minutes. */
export function timedProposal(): EngineQuestion {
  return {
    id: 8,
    kind: 'task',
    ask: 'confirmation',
    head: 'Start a task: add retries to the uploader',
    deadline: later(120),
    pick: { key: '1' },
    options: [{ key: '1', label: 'start it' }, { key: '2', label: 'no', safe: true }],
  };
}

/** A consent question about call c1 that the scripted turn is waiting on. */
export function waitingConsent(): EngineQuestion {
  return consent(4, 'Allow `rm -rf build`?', { subject: { kind: 'call', callId: 'c1' } });
}

export const waitingTurn: WireEntry[] = [narrate('Removing the old build.'), call('c1', 'bash', { command: 'rm -rf build' }, '', { Answered: false })];

// The notes internal/session/readhandoff.go writes when a batch of reads goes to a quick task.
const HANDED =
  "[This reading was handed to quick task 1, which finished it and returned the distilled answer below — the raw reads never entered this conversation. If you need one file's exact text to defend the answer, read that one file directly.]";
const COVERED = '[Covered by the handoff to quick task 1 — the distilled answer is on the first call of this batch.]';
export const HANDOFF_ANSWER = "What I've learned: README.md says the folder is a scratch workspace for the codeaf desktop app.";

/** A read batch the engine handed to a quick task: the answer rides on the first call, the list call is only covered. */
export function handoffReply(): Scenario {
  const reply: WireEntry[] = [
    narrate('Reading the sandbox.'),
    call('h1', 'read', { path: '/home/santosh/sandbox/README.md' }, `${HANDED}\n\n${HANDOFF_ANSWER}`, { Hint: 'handed to quick task 1' }),
    call('h2', 'ls', { path: '/home/santosh/sandbox/notes' }, COVERED, { Hint: 'handed to quick task 1' }),
    call('h3', 'mystery_probe', { path: '/home/santosh/sandbox/notes', depth: 2 }, 'probed 3 entries'),
    answer('The sandbox is a scratch workspace.'),
  ];
  return { initial: { title: 'Sandbox', entries: [] }, turns: [{ entries: reply }], tools: {}, files: {} };
}

export { user };
