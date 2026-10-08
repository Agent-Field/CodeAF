import { spawnSync } from 'node:child_process';
import { readFileSync, writeFileSync } from 'node:fs';
const mode = process.argv[2];
let args;
if (mode === 'remote') {
 const url = process.env.CODEAF_DEV_URL || 'http://localhost:1420';
 const parsed = new URL(url);
 if (!['http:', 'https:'].includes(parsed.protocol)) throw new Error('Use an HTTP(S) development URL');
 const build = spawnSync('npm', ['run', 'engine:build'], { stdio: 'inherit' });
 if (build.status !== 0) process.exit(build.status || 1);
 args = ['tauri', 'dev', '--config', JSON.stringify({ build: { beforeDevCommand: '', devUrl: url } })];
} else if (mode === 'dev' || mode === 'build') {
 args = ['tauri', mode, ...process.argv.slice(3)];
} else throw new Error('Use dev, remote, or build');
const manifestPath = new URL('../src-tauri/Cargo.toml', import.meta.url);
const before = readFileSync(manifestPath, 'utf8');
const result = spawnSync('npx', args, { stdio: 'inherit' });
// Tauri enables the Mac config's feature in Cargo.toml during a build. Restore only
// that exact generated change; preserve any other edit made while the process ran.
const generatedMacManifest = before.replace(
 'tauri = { version = "2", features = [] }',
 'tauri = { version = "2", features = ["macos-private-api"] }',
);
if (process.platform === 'darwin' && readFileSync(manifestPath, 'utf8') === generatedMacManifest) {
 writeFileSync(manifestPath, before);
}
process.exit(result.status || (result.error ? 1 : 0));
