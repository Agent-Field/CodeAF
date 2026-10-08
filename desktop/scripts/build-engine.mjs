import { execFileSync } from 'node:child_process';
import { mkdirSync } from 'node:fs';
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
execFileSync('go', ['build', '-trimpath', '-o', `../src-tauri/binaries/codeaf-engine-${target}${GOOS === 'windows' ? '.exe' : ''}`, './cmd/codeaf-engine'], {
 cwd: 'engine', env: { ...process.env, GOOS, GOARCH, CGO_ENABLED: '0' }, stdio: 'inherit',
});
console.log(`Built codeaf engine for ${target}`);
