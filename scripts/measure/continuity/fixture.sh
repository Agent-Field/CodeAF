#!/usr/bin/env bash
# fixture.sh <dir> : a small real repository for the continuity session. It has a failing test the session fixes,
# secrets of three modes, an untracked file, a lockfile and a rebuildable node_modules, so that a move has
# something of every kind to carry. Distinct facts live only in files (the release codename), so a question
# about them can be answered from the transcript alone once the files were read.
set -euo pipefail
D=$1
rm -rf "$D"; mkdir -p "$D/src" "$D/test" "$D/docs"; cd "$D"
git init -q
cat > package.json <<'J'
{"name":"invoice-kit","version":"1.0.0","scripts":{"test":"node --test"},"dependencies":{}}
J
cat > README.md <<'M'
# invoice-kit
Tiny invoice maths. Release codename: heron-42. Maintainer on call: Ines Okafor.
Run `npm test` for the suite. The dev server (`server.js`) listens on the port in PORT.
M
cat > src/money.js <<'J'
// Rounds a decimal amount to cents.
function toCents(amount) {
  return Math.floor(amount * 100); // BUG: floors instead of rounding half up
}
module.exports = { toCents };
J
cat > src/tax.js <<'J'
const { toCents } = require('./money');
const RATES = { standard: 0.2 };
function withTax(amount, kind = 'standard') {
  const rate = RATES[kind];
  if (rate === undefined) throw new Error('unknown tax kind: ' + kind);
  return toCents(amount * (1 + rate));
}
module.exports = { withTax, RATES };
J
cat > server.js <<'J'
const http = require('http');
const { withTax } = require('./src/tax');
http.createServer((req, res) => res.end(String(withTax(10)))).listen(process.env.PORT || 8790, '127.0.0.1');
J
cat > test/money.test.js <<'J'
const test = require('node:test'); const assert = require('node:assert');
const { toCents } = require('../src/money');
test('rounds half up to cents', () => { assert.strictEqual(toCents(0.015), 2); });
test('whole amounts', () => { assert.strictEqual(toCents(2), 200); });
J
cat > test/tax.test.js <<'J'
const test = require('node:test'); const assert = require('node:assert');
const { withTax } = require('../src/tax');
test('standard rate', () => { assert.strictEqual(withTax(10), 1200); });
test('reduced rate is 7 percent', () => { assert.strictEqual(withTax(100, 'reduced'), 10700); });
J
printf '# keep order\nZED=last\n\n# second block\nALPHA="quoted # not a comment"\nMID=1' > .env; chmod 600 .env
mkdir -p svc/api; printf 'API_TOKEN=nested-1\nAAA=2\n' > svc/api/.env; chmod 640 svc/api/.env
printf '#!/bin/sh\necho run\n' > run.sh; chmod 755 run.sh
# a vendored package the session installs offline (the session's jail has no network), so node_modules is a real,
# rebuildable folder made by the session and withheld from the move
mkdir -p vendor/pad-lite; printf '{"name":"pad-lite","version":"1.0.0","main":"index.js"}\n' > vendor/pad-lite/package.json
printf 'module.exports = (s, n) => String(s).padStart(n, " ");\n' > vendor/pad-lite/index.js
(cd vendor/pad-lite && npm pack --silent >/dev/null 2>&1 && mv pad-lite-1.0.0.tgz ../ )
printf '.env\nnode_modules/\nsvc/api/.env\n.npm-cache/\n' > .gitignore
git add . && git -c user.name=v -c user.email=v@x commit -qm "invoice-kit baseline"
printf 'scratch notes, untracked on purpose\n' > NOTES.untracked.txt
npm install --no-audit --no-fund >/dev/null 2>&1
echo fixture ready "$D"
