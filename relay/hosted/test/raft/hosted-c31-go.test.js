import test from 'node:test';
import assert from 'node:assert/strict';
import {spawn} from 'node:child_process';
import {join} from 'node:path';
import {hostedFixture} from './hosted-fixture.js';

const root = new URL('../../../../', import.meta.url);
const footprint = new URL('../../../../scripts/raft-proof-footprint.sh', import.meta.url).pathname;
async function run(args, env = {}) {
 const child = spawn('sh', [footprint, ...args], {cwd: root, env: {...process.env, ...env}, stdio: ['ignore', 'pipe', 'pipe']});
 let output = '';
 for (const stream of [child.stdout, child.stderr]) stream.on('data', bytes => { output += bytes; process.stdout.write(bytes); });
 const code = await new Promise((resolve, reject) => { child.once('error', reject); child.once('exit', resolve); });
 return {code, output};
}

// Linking the client tests can leave a disk backlog, so it finishes before
// the Worker starts and cannot spend the proof's own time limit.
const binary = join(process.env.CODEAF_RAFT_PROOF_DIR, 'hosted-c31.test');
const compiled = await run(['go', 'test', '-c', '-p=1', '-o', binary, './internal/syncsetup']);
assert.equal(compiled.code, 0, compiled.output);

test('C31 Home offers a chat after its holder is killed without goodbye', {timeout: 180000}, async () => {
 const relay = await hostedFixture({fixture: true});
 try {
  await relay.start();
  const url = await relay.mf.ready;
  const {code, output} = await run([binary, '-test.run=^TestC31HostedTakeBackOfferAfterHolderStopsWithoutGoodbye$', '-test.count=1', '-test.timeout=15m', '-test.v'], {CODEAF_HOSTED_PROOF_URL: url.origin});
  assert.equal(code, 0, output);
  assert.doesNotMatch(output, /--- SKIP:/);
  assert.match(output, /C31 resume offer on B: true after/);
 } finally { await relay.close(); }
});
