// The development proxy loses its internal connection under concurrent local load.
// Patch a private runtime copy so only that transport error can retry; the Worker
// under test and every application response remain unchanged.
import { cpSync, mkdirSync, readFileSync, realpathSync, symlinkSync, writeFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

/** patchProxy fails closed if the pinned Wrangler changes its proxy structure. */
export function patchProxy(source, retries) {
  if (!Number.isInteger(retries) || retries < 0) throw new Error('invalid local proxy retry limit');
  const replace = (before, after) => {
    if (source.split(before).length !== 2) throw new Error('unsupported local Wrangler proxy structure');
    source = source.replace(before, after);
  };
  replace('const attemptUserWorkerFetch = (attempt = 0) => void fetch(userWorkerUrl, new Request(request, { headers })).then(',
    'const replay = request.clone();\n      const attemptUserWorkerFetch = (attempt = 0) => void fetch(userWorkerUrl, new Request(replay.clone(), { headers })).then(');
  replace(') && (request.method === "GET" || request.method === "HEAD") && attempt < 2) {',
    `) && error.message === "Network connection lost." && attempt < ${retries}) {\n            console.warn("codeaf local proxy transport retry: Network connection lost.");`);
  return source;
}

/** prepare leaves the installed package untouched and gives each run its own asset. */
export function prepare(state, binary, retries) {
  const original = dirname(dirname(realpathSync(binary)));
  const copied = join(state, 'wrangler');
  mkdirSync(state, { recursive: true });
  cpSync(original, copied, { recursive: true });
  symlinkSync(dirname(original), join(state, 'node_modules'), 'dir');
  const asset = join(copied, 'wrangler-dist', 'ProxyWorker.js');
  writeFileSync(asset, patchProxy(readFileSync(asset, 'utf8'), retries));
  return join(copied, 'bin', 'wrangler.js');
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  console.log(prepare(process.argv[2], process.argv[3], Number(process.env.CODEAF_RELAYCONF_RETRIES ?? 0)));
}
