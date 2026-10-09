// Development transport for the browser preview; all AI stays in the root binary.
import { spawn, execFileSync } from 'node:child_process';
import { chmodSync, mkdirSync, writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
const root = fileURLToPath(new URL('../../', import.meta.url));
const connectionFile = fileURLToPath(new URL('../node_modules/.cache/codeaf-engine-connection.json', import.meta.url));
if (!process.argv.includes('--skip-build')) execFileSync('make', ['build'], { cwd: root, stdio: 'inherit' });
mkdirSync(fileURLToPath(new URL('../node_modules/.cache', import.meta.url)), { recursive: true });
const engine = spawn(`${root}/bin/codeaf`, ['desktop-bridge', '--workspace', process.env.CODEAF_DESKTOP_WORKSPACE || root], { cwd: root, stdio: ['ignore', 'pipe', 'inherit'] });
let output = '';
engine.stdout.on('data', chunk => {
  output += chunk.toString();
  const end = output.indexOf('\n');
  if (end < 0) return;
  const connection = JSON.parse(output.slice(0, end));
  writeFileSync(connectionFile, JSON.stringify(connection), { mode: 0o600 });
  chmodSync(connectionFile, 0o600);
  console.log(`Local engine ready at ${connection.url} using ${connection.model}.`);
  console.log('Start the browser preview in another terminal with npm run dev.');
  engine.stdout.removeAllListeners('data');
});
engine.on('exit', code => { process.exitCode = code ?? 1; });
for (const signal of ['SIGINT', 'SIGTERM']) process.on(signal, () => engine.kill(signal));
