import type { MockConversation } from './history-engine';
import type { Scenario } from './mock-engine';
import { plainReply } from './scenarios';

/** The clock every History spec fixes: Friday 9 October 2026, 15:00 local. Fixtures are dated against it. */
export const NOW = new Date(2026, 9, 9, 15, 0);
const at = (month: number, day: number, hour: number, minute = 0) => new Date(2026, month, day, hour, minute).toISOString();

const lexerMessages: NonNullable<MockConversation['messages']> = [
  { role: 'user', text: 'CI is failing on the config tests. Can you look?' },
  { role: 'assistant', text: 'A shared fixture has a trailing comma, and the error points at the wrong file.' },
  { role: 'user', text: 'Should we fix it in the parser or the lexer?' },
  { role: 'assistant', text: "The lexer's error recovery reports position 1:1, so a fix there gives the right file and line." },
  { role: 'user', text: 'Does JSON5 handle this?' },
  { role: 'assistant', text: 'It does, but it also allows comments, so it is not a fit.' },
  { role: 'user', text: 'Keep strict mode as the default and fix it in the lexer.' },
  { role: 'assistant', text: 'Done. Trailing commas are accepted outside strict mode and both fixtures pass.' },
  { role: 'user', text: 'Thanks.' },
];

/** The design's History: two open conversations today, two on Tuesday, two last week. */
export const designConversations = (): MockConversation[] => [
  { id: 'trailing', title: 'Trailing commas across the config stack', at: at(9, 9, 10, 40), open: true, state: 'working', tasks: 5, tasksRunning: 4, messages: [{ role: 'user', text: 'Make trailing commas work across the config stack.' }] },
  { id: 'release', title: 'Release v2.4', at: at(9, 9, 10, 15), open: true, state: 'needs-you', reason: 'Tag v2.4.1?', messages: [{ role: 'user', text: 'Prepare the v2.4 release.' }] },
  {
    id: 'lexer', title: 'Fix it in the lexer', at: at(9, 6, 14, 2), tasks: 2, messages: lexerMessages,
    recap: {
      line: 'Decided to keep strict mode as the default and fix it in the lexer',
      discussed: "CI was failing because a shared fixture had a trailing comma. We weighed fixing it in the parser against the lexer, and checked whether JSON5 would do. It wouldn't, because it also allows comments.",
      decided: [{ text: 'Keep strict mode as the default for the public API', by: 'you' }, { text: 'Fix in the lexer, not the parser', by: 'codeaf', how: 'accepted' }],
      outcome: 'Trailing commas are accepted outside strict mode, and both fixtures pass.',
      files: [{ path: 'internal/config/lexer.go', added: 12, removed: 3 }, { path: 'testdata/nested.json', added: 6, removed: 0 }],
      messages: 9,
    },
    taskHits: [{ taskId: 't1', title: 'Update fixtures', snippet: 'Removed the trailing comma from nested.json' }],
  },
  { id: 'json5', title: 'Does JSON5 handle this?', at: at(9, 6, 11, 40), messages: [{ role: 'user', text: 'Does JSON5 handle trailing commas?' }], recap: { line: 'Ruled out JSON5, because it also allows comments', discussed: 'JSON5 accepts trailing commas but also comments.', decided: [{ text: 'Rule out JSON5', by: 'you' }], outcome: '', files: [] } },
  { id: 'naming', title: 'Naming for the config loader API', at: at(9, 1, 16, 20), messages: [{ role: 'user', text: 'What should the loader be called?' }], recap: { line: 'Discussed Load vs Open. No decision yet', discussed: 'We compared Load and Open for the loader.', decided: [], outcome: '', files: [] } },
  { id: 'ci', title: 'Why does CI fail on configs?', at: at(8, 30, 9, 5), messages: [{ role: 'user', text: 'Why does CI fail on configs?' }], recap: { line: 'A trailing comma in the shared fixture, and the error pointed at the wrong file', discussed: 'The failing fixture has a trailing comma.', decided: [], outcome: 'The error named the wrong file.', files: [] } },
];

/** A long history for the virtual list: `count` conversations, one every few hours, newest first. */
export function manyConversations(count: number): MockConversation[] {
  return Array.from({ length: count }, (_, index) => {
    const when = new Date(NOW.getTime() - (index + 2) * 5 * 3_600_000).toISOString();
    return { id: `bulk-${index}`, title: `Conversation ${index + 1}`, at: when, messages: [{ role: 'user' as const, text: `Question ${index + 1}` }], recap: { line: `Discussed topic ${index + 1}. No decision yet`, discussed: `We talked about topic ${index + 1}.`, decided: [], outcome: '', files: index % 7 === 0 ? [{ path: `src/topic${index}.go`, added: 4, removed: 1 }] : [] } };
  });
}

export const withHistory = (conversations: MockConversation[] = designConversations()): Scenario => ({ ...plainReply(), history: { conversations } });
