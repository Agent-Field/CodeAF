// Meta is a small key-value table of JSON values in an identity's SQLite: the few facts about the
// identity that are not the directory or the frames (its running counts).
export class Meta {
  constructor(sql) {
    this.sql = sql;
    sql.exec('CREATE TABLE IF NOT EXISTS meta (k TEXT PRIMARY KEY, v TEXT NOT NULL) WITHOUT ROWID');
  }

  get(key) {
    const row = this.sql.exec('SELECT v FROM meta WHERE k = ?', key).toArray()[0];
    return row ? JSON.parse(row.v) : undefined;
  }

  set(key, value) {
    this.sql.exec('INSERT OR REPLACE INTO meta VALUES (?,?)', key, JSON.stringify(value));
  }
}
