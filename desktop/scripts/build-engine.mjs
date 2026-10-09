import { execFileSync } from 'node:child_process';
import { copyFileSync, mkdirSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
const root = fileURLToPath(new URL('../../', import.meta.url));
const host = execFileSync('rustc', ['-vV'], { encoding: 'utf8' }).match(/^host: (.+)$/m)?.[1];
const target = process.env.CARGO_BUILD_TARGET || host;
const platforms = {
 'aarch64-apple-darwin': ['darwin', 'arm64'],
 'x86_64-apple-darwin': ['darwin', 'amd64'],
 'aarch64-unknown-linux-gnu': ['linux', 'arm64'],
 'x86_64-unknown-linux-gnu': ['linux', 'amd64'],
 'x86_64-pc-windows-msvc': ['windows', 'amd64'],
};
if (!platforms[target]) throw new Error(`Unsupported sidecar target: ${target}`);
const [GOOS, GOARCH] = platforms[target];
mkdirSync('src-tauri/binaries', { recursive: true });
// Package the canonical root binary: there is one engine and one prompt/storage implementation.
execFileSync('make', ['build'], {
 cwd: root, env: { ...process.env, GOOS, GOARCH, CGO_ENABLED: '0' }, stdio: 'inherit',
});
copyFileSync(`${root}/bin/codeaf`, `src-tauri/binaries/codeaf-engine-${target}${GOOS === 'windows' ? '.exe' : ''}`);
console.log(`Built canonical codeaf engine for ${target}`);
