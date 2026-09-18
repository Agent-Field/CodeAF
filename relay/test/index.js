// Node's test runner takes files and globs, not a directory, so the
// directory-form command `node --test test` resolves here; this entry loads
// every test file the same way the glob does.

import { readdirSync } from 'node:fs';

const here = new URL('.', import.meta.url);
for (const name of readdirSync(here).sort()) {
  if (name.endsWith('.test.js')) {
    await import(new URL(name, here));
  }
}
