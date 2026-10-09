// @ts-nocheck -- the app tsconfig has no node types; this file runs under node --test.
import test from 'node:test';
import assert from 'node:assert/strict';
import { headingLabel, parseBrief } from './brief.ts';

// The layout composeBriefScoped writes: heading, one rule line, blank, body; sections split by a blank line.
const ASK_RULE = 'This is the message this work came out of. Where anything below reads differently from it, their words are what was asked for.';
const brief = [
  'WHAT THE PERSON ASKED FOR, IN THEIR OWN WORDS',
  ASK_RULE,
  '',
  'Split this into two tasks and run them: (a) create src/a3.txt containing A, (b) create src/b3.txt containing B.',
  '',
  'THE WORK',
  '',
  'Create src/b3.txt containing the single line B.',
  '',
  'WHAT TO PRODUCE',
  '',
  'src/b3.txt in the working folder.',
  '',
  'DONE WHEN',
  '',
  'src/b3.txt exists and holds exactly B.',
  '',
  "THE PERSON'S ORIGINAL MESSAGE",
  'The restatement above is bounded. Their original words are at this path and line.',
  '',
  '/tmp/journal.jsonl:12',
].join('\n');

test('the task work leads; the shouted header is not the text', () => {
  const parsed = parseBrief(brief);
  assert.equal(parsed.summary, 'Create src/b3.txt containing the single line B.');
  assert.deepEqual(parsed.sections.map((section) => section.heading), [
    'WHAT THE PERSON ASKED FOR, IN THEIR OWN WORDS',
    'THE WORK',
    'WHAT TO PRODUCE',
    'DONE WHEN',
    "THE PERSON'S ORIGINAL MESSAGE",
  ]);
});

test('the rule line under a heading is split from the body', () => {
  const [ask] = parseBrief(brief).sections;
  assert.equal(ask.rule, ASK_RULE);
  assert.equal(ask.body, 'Split this into two tasks and run them: (a) create src/a3.txt containing A, (b) create src/b3.txt containing B.');
});

test("with no work section the person's own words lead", () => {
  const onlyAsk = ['WHAT THE PERSON ASKED FOR, IN THEIR OWN WORDS', ASK_RULE, '', 'Rename the app to Foo.'].join('\n');
  assert.equal(parseBrief(onlyAsk).summary, 'Rename the app to Foo.');
});

test('a plan-born brief that opens on its own task line still parses', () => {
  const planBorn = ['t-3 is your task in the plan.', 'ancestor tasks are background, not extra assignments', '', 'THE WORK', '', 'Do the thing.'].join('\n');
  assert.equal(parseBrief(planBorn).summary, 'Do the thing.');
});

test('text that is not a brief comes back whole, and shouted prose inside a body is not a heading', () => {
  assert.deepEqual(parseBrief('Just do it.'), { summary: 'Just do it.', sections: [] });
  const inside = ['THE WORK', '', 'Read the file.', 'THE WORK IS NOT DONE UNTIL TESTS PASS', 'Then stop.'].join('\n');
  assert.equal(parseBrief(inside).summary, 'Read the file.\nTHE WORK IS NOT DONE UNTIL TESTS PASS\nThen stop.');
});

test('headings read as sentences', () => {
  assert.equal(headingLabel('WHAT TO PRODUCE'), 'What to produce');
});
