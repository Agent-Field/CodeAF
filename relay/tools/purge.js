#!/usr/bin/env node
// purge.js deletes the stored sheet keys that carry a fixture vendor.
//
// The relay's KV is reachable only through wrangler, so this tool is a thin
// shell: it lists the keys under `sheet/` through `wrangler kv key list` and
// deletes the ones relay/src/purge.js selects through `wrangler kv key delete`,
// one key at a time. All the selection is in that module, which the tests drive
// against a fake KV, so the deletion logic is the tested code.
//
//   node tools/purge.js --vendor crew --vendor other --dry-run
//   node tools/purge.js --vendor crew --vendor other
//
// A dry run prints every key it would delete; a run deletes them and prints how
// many. Run the dry run first, then the delete, then publish — see
// docs/design/model-pool/RUNBOOK.md.

import { execFileSync } from 'node:child_process';

import { runPurge } from '../src/purge.js';

// parseArgs reads the flags: --vendor (repeatable), --dry-run, and the
// --binding/--prefix/--local that pick the namespace wrangler talks to.
function parseArgs(argv) {
  const opts = { vendors: [], dryRun: false, binding: 'POOL', prefix: 'sheet/', remote: true };
  for (let i = 0; i < argv.length; i++) {
    switch (argv[i]) {
      case '--vendor': opts.vendors.push(argv[++i]); break;
      case '--dry-run': opts.dryRun = true; break;
      case '--binding': opts.binding = argv[++i]; break;
      case '--prefix': opts.prefix = argv[++i]; break;
      case '--local': opts.remote = false; break;
      case '--remote': opts.remote = true; break;
      default: throw new Error(`unknown argument: ${argv[i]}`);
    }
  }
  return opts;
}

// wrangler runs one `wrangler kv key ...` command and answers its stdout. It
// talks to the deployed namespace unless --local names the local one.
function wrangler(opts, args) {
  const argv = ['kv', 'key', ...args, '--binding', opts.binding];
  if (opts.remote) {
    argv.push('--remote');
  }
  return execFileSync('wrangler', argv, { encoding: 'utf8' });
}

// store is the shape relay/src/purge.js reads: one list page per prefix and one
// delete per key, both through wrangler. `kv key list` prints a JSON array of
// {name} objects and reads the whole prefix, so one page is the whole listing.
function store(opts) {
  return {
    async list({ prefix }) {
      const keys = JSON.parse(wrangler(opts, ['list', '--prefix', prefix]))
        .map((entry) => ({ name: entry.name }));
      return { keys, list_complete: true, cursor: undefined };
    },
    async delete(key) {
      wrangler(opts, ['delete', key]);
    },
  };
}

async function main() {
  const opts = parseArgs(process.argv.slice(2));
  if (opts.vendors.length === 0) {
    throw new Error('name at least one vendor: --vendor <name>');
  }
  const vendors = new Set(opts.vendors.map((vendor) => vendor.toLowerCase()));
  const keys = await runPurge(store(opts), vendors, { dryRun: opts.dryRun, prefix: opts.prefix });
  if (opts.dryRun) {
    for (const key of keys) {
      console.log(key);
    }
  } else {
    console.log(`deleted ${keys.length} key${keys.length === 1 ? '' : 's'}`);
  }
}

main().catch((err) => {
  console.error(err.message);
  process.exitCode = 1;
});
