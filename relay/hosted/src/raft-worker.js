import core from './raft/core.wasm';
import { installRaftModule } from './raft/member.js';
installRaftModule(core);
export * from './worker.js';
export { default } from './worker.js';
