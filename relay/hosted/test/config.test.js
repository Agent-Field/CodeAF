import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

// wrangler.toml is the base every deployment starts from. Fresh installs and
// upgrades must both reach the Raft reactor entry and the three anchor stores,
// and the anchors may only be appended so that existing namespace IDs, and the
// saved chats behind them, stay where they are.
test('R30 the base config uses the Raft entry and an append-only v3-raft-members migration', () => {
  const text = readFileSync(new URL('../wrangler.toml', import.meta.url), 'utf8');
  assert.match(text, /^main = "src\/raft-worker\.js"$/m);
  assert.match(text, /name = "RAFT_MEMBERS"\s+class_name = "RaftMemberDO"/);
  assert.match(text, /\[build\]\s+command = "sh \.\.\/\.\.\/scripts\/raft-hosted-build\.sh"/);
  const tags = [...text.matchAll(/^tag = "([^"]+)"/gm)].map((m) => m[1]);
  assert.deepEqual(tags, ['v1', 'v2', 'v3-raft-members', 'v4-machines']);
  assert.match(text, /tag = "v3-raft-members"\s+new_sqlite_classes = \["RaftMemberDO"\]/);
  assert.doesNotMatch(text, /deleted_classes|renamed_classes/);
  assert.match(text, /name = "IDENTITY"\s+class_name = "IdentityDO"/);
  assert.match(text, /name = "MACHINE"\s+class_name = "MachineDO"/);
  assert.match(text, /tag = "v4-machines"\s+new_sqlite_classes = \["MachineDO"\]/);
});
