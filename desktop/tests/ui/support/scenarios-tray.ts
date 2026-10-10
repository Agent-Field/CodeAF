import type { EngineQuestion } from '../../../src/features/chat/engine-client';

const asked = (second: number) => `2026-10-09T10:00:${String(second).padStart(2, '0')}Z`;

const soon = () => new Date(Date.now() + 45_000).toISOString();

export const permissionOptions = [
  { key: '1', label: 'allow once' },
  { key: '2', label: 'always', widening: true },
  { key: '3', label: 'deny', safe: true },
];

/** One permission: Allow once, Deny, Always allow…. Holds two tasks up. */
export function singlePermission(): EngineQuestion {
  return {
    id: 1,
    kind: 'consent',
    ask: 'permission',
    head: 'Allow `rm -rf build`?',
    reason: 'It deletes the build folder.',
    options: permissionOptions,
    scope: ['once', 'task', 'always'],
    stakes: 'costly',
    blocking: { tasks: ['Update fixtures', 'Port fix to v1'] },
    asked: asked(1),
  };
}

/** Three pages so ← → and the disabled end arrow can be measured. Oldest first. */
export function pagerQuestions(): EngineQuestion[] {
  return [
    {
      id: 1, kind: 'ask', ask: 'choice', head: 'Pick a database', asked: asked(1),
      options: [
        { key: 'sqlite', label: 'SQLite', body: 'One file.' },
        { key: 'postgres', label: 'Postgres', body: 'A server.' },
      ],
    },
    { id: 2, kind: 'ask', ask: 'clarification', head: 'Name the cache', asked: asked(2), input: { kind: 'text', prompt: 'Your answer' } },
    {
      id: 3, kind: 'ask', ask: 'choice', head: 'Pick a region', asked: asked(3),
      options: [{ key: 'e', label: 'East' }, { key: 'w', label: 'West' }],
    },
  ];
}

/** Choice cards with a deadline and a note field, so typing can hold the clock. */
export function clockedChoice(): EngineQuestion {
  return {
    id: 14,
    kind: 'ask',
    ask: 'choice',
    head: 'Should trailing commas be on by default?',
    deadline: soon(),
    pick: { key: 'strict', reason: 'No surprise for existing users.' },
    input: { kind: 'text', prompt: 'Add a note' },
    options: [
      { key: 'strict', label: 'Keep strict', body: 'Opt in with Strict: false.' },
      { key: 'tolerant', label: 'Tolerant by default', body: 'Friendlier, and files may break other tools.' },
    ],
  };
}

/** Irreversible card: Keep it, Remove, Tell it…. No clock, no Always, no reason field. */
export function irreversibleChoice(): EngineQuestion {
  return {
    id: 5,
    kind: 'ask',
    ask: 'choice',
    head: 'Remove the legacy YAML loader?',
    reason: 'This deletes loader_yaml.go and its 31 tests. It can\'t be undone from here.',
    stakes: 'irreversible',
    deadline: soon(),
    asker: { kind: 'task', name: 'Audit config loaders' },
    scope: ['once', 'always'],
    options: [
      { key: 'remove', label: 'Remove' },
      { key: 'always', label: 'always', widening: true },
      { key: 'keep', label: 'Keep it', safe: true },
      { key: 'tell', label: 'Tell it' },
    ],
  };
}

/** Clarification that needs words. The pick is what You decide sends back to the asker. */
export function clarification(id = 8): EngineQuestion {
  return {
    id,
    kind: 'ask',
    ask: 'clarification',
    head: id === 8 ? 'Which customers still run v1?' : 'Which region should the cache use?',
    asked: asked(id),
    input: { kind: 'text', prompt: 'Your answer' },
    pick: { key: id === 8 ? 'northwind' : 'east' },
    blocking: { turn: true },
  };
}

/** A second page so Later has a header count to fold into. */
export function clarificationPair(): EngineQuestion[] {
  return [clarification(8), clarification(9)];
}

const gitHead = ['git checkout release/v1', 'git cherry-pick 3f2a1c', 'git push origin release/v1'];

/** One card for three git permissions. */
export function gitBatch(): EngineQuestion[] {
  return gitHead.map((head, index) => ({
    id: index + 1,
    kind: 'consent' as const,
    ask: 'permission' as const,
    head,
    batch: 'step:4',
    asked: asked(index + 1),
    options: permissionOptions,
    scope: ['once', 'always'],
    stakes: 'costly' as const,
    blocking: { tasks: ['Port fix to v1 branch'] },
  }));
}

/** A long reason, so the tray body has to scroll inside its 40% cap. */
export function tallPermission(): EngineQuestion {
  const reason = Array.from({ length: 40 }, (_, index) => `Line ${index + 1} of the command explanation stays in the scrolling middle.`).join(' ');
  return { ...singlePermission(), reason, blocking: { turn: true } };
}
