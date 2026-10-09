// Starts the REAL engine transport (bin/codeaf desktop-bridge) for the workspace-sync browser suite, on its own
// port, against a throwaway HOME and CODEAF_HOME, with every provider key removed from its environment. The
// workspace routes never open a conversation, so nothing here can reach a model. Run `make build` first.
import { spawn } from 'node:child_process';
import { existsSync, mkdirSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';

export const BRIDGE_PORT = 17712;
export const BRIDGE_TOKEN = 'workspace-sync-browser-suite-token-0123456789abcdef';
const root = fileURLToPath(new URL('../../../', import.meta.url));
const binary = join(root, 'bin', 'codeaf');
if (!existsSync(binary)) { console.error(`workspace-sync: ${binary} is missing; run make build`); process.exit(1); }
// One fixed throwaway home per port, emptied at every start: a run killed without warning leaves one folder
// behind, never a growing pile.
const home = join(tmpdir(), `codeaf-workspace-sync-home-${BRIDGE_PORT}`);
rmSync(home, { recursive: true, force: true });
mkdirSync(home, { recursive: true, mode: 0o700 });
const env = { PATH: process.env.PATH, HOME: home, CODEAF_HOME: join(home, '.codeaf'), CODEAF_DESKTOP_TOKEN: BRIDGE_TOKEN };
const bridge = spawn(binary, ['desktop-bridge', '--listen', `127.0.0.1:${BRIDGE_PORT}`, '--workspace', home, '--places', join(home, '.codeaf', 'desktop', 'places.json')], { env, stdio: ['ignore', 'inherit', 'inherit'] });
const end = () => { bridge.kill('SIGTERM'); rmSync(home, { recursive: true, force: true }); };
bridge.on('exit', code => { rmSync(home, { recursive: true, force: true }); process.exit(code ?? 0); });
for (const signal of ['SIGINT', 'SIGTERM']) process.on(signal, end);
