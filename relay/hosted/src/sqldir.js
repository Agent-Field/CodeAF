// The directory in a Durable Object's SQLite. The object is single-threaded and each method is one
// synchronous turn (no await between read and write), so nothing can interleave inside a swap.
import { DirectoryOps } from './dirops.js';

export class SqlDirectory extends DirectoryOps {
  constructor(sql, identity, clock, policy) {
    super(identity, clock, policy);
    this.sql = sql;
    sql.exec('CREATE TABLE IF NOT EXISTS dir (kind TEXT NOT NULL, id TEXT NOT NULL, doc TEXT NOT NULL, PRIMARY KEY (kind, id))');
  }

  read(kind, id) {
    const row = this.sql.exec('SELECT doc FROM dir WHERE kind = ? AND id = ?', kind, id).toArray()[0];
    return row ? JSON.parse(row.doc) : null;
  }

  write(kind, id, doc) {
    this.sql.exec('INSERT OR REPLACE INTO dir VALUES (?,?,?)', kind, id, JSON.stringify(doc));
    return doc;
  }

  swap(id, fn) {
    const now = this.clock();
    return Promise.resolve().then(() => ({ now, cell: this.write('cells', id, fn(this.read('cells', id), now)) }));
  }

  record(kind, id, fn) {
    const key = id ?? '';
    return Promise.resolve().then(() => this.write(kind, key, fn(this.read(kind, key))));
  }

  readCell(id) {
    return this.read('cells', id);
  }

  async list() {
    const rows = this.sql.exec('SELECT kind, id, doc FROM dir').toArray();
    return this.assemble(rows.map((r) => [r.kind, r.id, JSON.parse(r.doc)]));
  }
}
