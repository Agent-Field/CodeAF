import { DatabaseSync } from 'node:sqlite';

/** memorySql is a Durable Object's sql.exec over an in-memory SQLite, so pure-SQL classes test without workerd. */
export function memorySql() {
  const db = new DatabaseSync(':memory:');
  return {
    exec(query, ...params) {
      const statement = db.prepare(query);
      const rows = /^\s*(SELECT|WITH)/i.test(query) ? statement.all(...params).map((r) => ({ ...r })) : (statement.run(...params), []);
      return {
        toArray: () => rows,
        one() {
          if (rows.length !== 1) throw new Error(`one() saw ${rows.length} rows`);
          return rows[0];
        },
      };
    },
  };
}
