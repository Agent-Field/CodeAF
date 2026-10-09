// Fixture questions shaped like the engine's own (research notes, Questions).
// They live only in the specimen; nothing here ever reaches a live tray.

import type { EngineQuestion } from '../../../chat/engine-client';

type Case = { title: string; questions: EngineQuestion[] };

const ago = (now: number, seconds: number) => new Date(now - seconds * 1000).toISOString();
const later = (now: number, seconds: number) => new Date(now + seconds * 1000).toISOString();

const consent = (id: number, head: string, extra: Partial<EngineQuestion> = {}): EngineQuestion => ({
  id,
  kind: 'consent',
  ask: 'permission',
  head,
  reason: 'It writes outside the folder you opened.',
  options: [
    { key: '1', label: 'allow once' },
    { key: '2', label: 'always', widening: true },
    { key: '3', label: 'deny', safe: true },
  ],
  scope: ['once', 'task', 'always'],
  stakes: 'costly',
  asker: { kind: 'engine' },
  blocking: { turn: true },
  ...extra,
});

export function fixtures(now: number): Case[] {
  return [
    { title: 'One consent', questions: [consent(1, 'Run `rm -rf build`?', { stakes: 'irreversible' })] },
    {
      title: 'Batch of three permissions',
      questions: [1, 2, 3].map((n) => consent(n, ['Run `go test ./...`', 'Edit `main.go`', 'Fetch `pkg.go.dev`'][n - 1], { batch: 'step:4', asked: ago(now, 30 - n) })),
    },
    {
      title: 'Mixed set with review, a later chip and a task asker',
      questions: [
        consent(4, 'Run `make build`', { batch: 'step:5', asked: ago(now, 40) }),
        {
          id: 5,
          kind: 'ask',
          ask: 'choice',
          head: 'Which database should the report read from?',
          reason: 'Both have the rows; they differ in how fresh they are.',
          batch: 'step:5',
          asked: ago(now, 39),
          pick: { key: 'b', reason: 'The replica is fresh enough and costs nothing.', confidence: 'fairly' },
          options: [
            { key: 'a', label: 'primary', body: 'Live data.', consequence: 'Adds load while people work.', dimensions: { fresh: 'seconds', cost: 'high' } },
            { key: 'b', label: 'replica', body: 'A few minutes behind.', consequence: 'No load on the primary.', dimensions: { fresh: 'minutes', cost: 'none' } },
            { key: 'c', label: 'skip the report', safe: true, dimensions: { fresh: 'n/a', cost: 'none' } },
          ],
          blocking: { turn: true },
        },
        { id: 6, kind: 'ask', ask: 'clarification', head: 'Tidy the old branches?', later: true, asked: ago(now, 20), options: [{ key: '1', label: 'yes' }, { key: '2', label: 'not now', safe: true }] },
      ],
    },
    {
      title: 'Landing with Tell it',
      questions: [
        {
          id: 7,
          kind: 'landing',
          ask: 'landing',
          head: 'The importer is finished. Does it match what you wanted?',
          reason: 'It reads CSV and JSON, and skips rows it cannot parse.',
          asker: { kind: 'task', name: 'Build the importer' },
          attach: [{ kind: 'diff', title: 'Summary of the change', body: '- rows = parse(file)\n+ rows = parse(file, lenient=True)' }],
          options: [
            { key: 'a', label: 'accept' },
            { key: 'n', label: 'not right', safe: true },
            { key: 's', label: 'tell it' },
          ],
          blocking: { tasks: ['Build the importer'] },
        },
      ],
    },
    {
      title: 'Task proposal with countdown and a model blank',
      questions: [
        {
          id: 8,
          kind: 'task',
          ask: 'confirmation',
          head: 'Start a task: add retries to the uploader',
          reason: 'It touches two files and runs the uploader tests.',
          deadline: later(now, 15),
          pick: { key: '1' },
          options: [{ key: '1', label: 'start it' }, { key: '2', label: 'no', safe: true }],
          input: { kind: 'blanks', blanks: [{ label: 'Model', kind: 'choice', default: 'deepseek/deepseek-v4.1-flash', choices: ['deepseek/deepseek-v4.1-flash', 'another/model'] }] },
        },
      ],
    },
    {
      title: 'Checklist',
      questions: [
        { id: 9, kind: 'ask', ask: 'choice', head: 'Which folders should the audit cover?', input: { kind: 'checklist' }, options: [{ key: 'src', label: 'src', body: 'The application.' }, { key: 'docs', label: 'docs' }, { key: 'tests', label: 'tests' }] },
      ],
    },
    {
      title: 'Pairs',
      questions: [
        {
          id: 10,
          kind: 'ask',
          ask: 'judgement',
          head: 'Which matters more here?',
          input: { kind: 'pairs', blanks: [{ label: 'Speed or cost', choices: ['Speed', 'Cost'] }, { label: 'New or proven', choices: ['New', 'Proven'] }] },
        },
      ],
    },
    {
      title: 'Dial',
      questions: [
        { id: 11, kind: 'ask', ask: 'judgement', head: 'How careful should the migration be?', input: { kind: 'dial', prompt: 'Carefulness', dial: { min: 0, max: 10, default: 6, labels: ['Fast', 'Balanced', 'Very careful'] } } },
      ],
    },
    {
      title: 'Clarification',
      questions: [
        { id: 14, kind: 'ask', ask: 'clarification', head: 'Which customers still run v1?', input: { kind: 'text', prompt: 'Your answer' }, blocking: { turn: true } },
      ],
    },
    {
      title: 'Secret key',
      questions: [
        { id: 12, kind: 'connect', ask: 'clarification', head: 'Paste the API key for the weather service', reason: 'It is kept on this machine and never shown again.', input: { kind: 'text', prompt: 'API key', secret: true }, stakes: 'reversible' },
      ],
    },
    {
      title: 'Compare, with a picture',
      questions: [
        {
          id: 13,
          kind: 'ask',
          ask: 'choice',
          head: 'Which layout should the home page use?',
          pick: { key: 'b', reason: 'Fewer clicks to the thing people open most.', confidence: 'unsure' },
          attach: [{ kind: 'image', title: 'Current home page', path: '/favicon.svg' }],
          options: [
            { key: 'a', label: 'grid', dimensions: { 'Time to build': 'a day', 'Fits on a phone': 'yes' } },
            { key: 'b', label: 'list', dimensions: { 'Time to build': 'an hour', 'Fits on a phone': 'yes' } },
          ],
        },
      ],
    },
  ];
}
