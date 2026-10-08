import { execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const root = fileURLToPath(new URL('../../', import.meta.url));
const desktop = fileURLToPath(new URL('../', import.meta.url));
const git = args => execFileSync('git', args, { cwd: root, encoding: 'utf8' }).trim();
const branch = git(['branch', '--show-current']);
if (!branch || ['dev', 'main', 'staging'].includes(branch)) {
 throw new Error('Switch to an integration branch before syncing public dev.');
}
if (git(['status', '--porcelain'])) {
 throw new Error('Commit your work before syncing. No files were changed.');
}
const upstream = git(['remote', 'get-url', 'upstream']);
if (!['git@github.com:Agent-Field/CodeAF.git', 'https://github.com/Agent-Field/CodeAF.git'].includes(upstream)) {
 throw new Error('upstream must point to the public Agent-Field/CodeAF repository.');
}
execFileSync('git', ['fetch', '--no-tags', 'upstream', 'dev'], { cwd: root, stdio: 'inherit' });
// Merge shared history without rewriting collaborators' commits or pushing an unchecked result.
execFileSync('git', ['merge', '--no-edit', 'upstream/dev'], { cwd: root, stdio: 'inherit' });
execFileSync('npm', ['run', 'check'], { cwd: desktop, stdio: 'inherit' });
execFileSync('npm', ['run', 'test:ui'], { cwd: desktop, stdio: 'inherit' });
execFileSync('make', ['pr-ready', 'BASE=upstream/dev'], { cwd: root, stdio: 'inherit' });
console.log(`Synced and checked ${branch}. Review the result, then git push.`);
