import { ActorFixture as MigrationFixture } from './legacy-worker.js';
import { Directory } from '../../src/directory.js';
import { LegacySource } from '../../src/raft/legacy.js';
import { sharedRuntime } from '../../src/raft/actor.js';
import { policyOf } from '../../src/rules.js';
export { default } from './legacy-worker.js';

export class ActorFixture extends MigrationFixture {
  async control(request) {
    const value = await request.clone().json();
    if (value.op === 'sql') {
      try {
        return Response.json({ok: this.ctx.storage.sql.exec(value.query, ...(value.args ?? [])).toArray()});
      } catch (error) {
        return Response.json({error: error.message});
      }
    }
    if (value.op === 'rollback-freeze') {
      const storage = this.ctx.storage;
      const directory = new Directory(storage.sql, value.config.identity, () => value.config.now, policyOf());
      const source = new LegacySource({
        sql: storage.sql,
        get: key => storage.get(key),
        sync: () => storage.sync(),
        transactionSync: fn => storage.transactionSync(() => {
          fn();
          throw new Error('interrupted before commit');
        }),
      }, directory, sharedRuntime());
      try {
        await source.freeze(value.config);
        return Response.json({error: 'freeze committed unexpectedly'});
      } catch (error) {
        return Response.json({ok: error.message});
      }
    }
    return super.control(request);
  }
}
