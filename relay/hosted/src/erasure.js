// What retiring an identity deletes once its grace has run out. The frames are deleted a page at a
// time, so one call never outgrows a Durable Object turn, and the caller asks again until a page
// comes back empty. Both halves are idempotent: a second run over what is already gone changes nothing.

/** erasePage deletes one page of the R2 objects under `prefix` and answers whether it found any. */
export async function erasePage(bucket, prefix, pageSize) {
  const { objects } = await bucket.list({ prefix, limit: pageSize });
  if (objects.length === 0) return false;
  await bucket.delete(objects.map((o) => o.key));
  return true;
}

// The platform's own tables (`_cf_KV` holds the object's key-value storage) are not ours to drop.
const OURS = "type = 'table' AND name NOT LIKE 'sqlite\\_%' ESCAPE '\\' AND name NOT LIKE '\\_cf\\_%' ESCAPE '\\'";

/** dropTables deletes every table the relay made in the identity's SQLite: directory, index, quota, counts. */
export function dropTables(sql) {
  for (const { name } of sql.exec(`SELECT name FROM sqlite_master WHERE ${OURS}`).toArray()) sql.exec(`DROP TABLE "${name}"`);
}
