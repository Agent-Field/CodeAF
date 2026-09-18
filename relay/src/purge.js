// purge.js lists and removes the stored sheet keys that carry a fixture vendor.
//
// The public pool took rows from a test fixture whose model or judge vendor is
// `crew` or `other`. Those rows are real triples under
// `sheet/<install>/<day>/<cellKey>` in the relay's KV, and the judge list the
// published index carries is derived from them (sheet.js judgesOf), so deleting
// the keys is what removes the judge on the next publish — nothing else stores
// it.
//
// Only the selection and the deletion live here. They read a store through the
// two calls the Worker's POOL binding and the tests' fake KV both answer — list
// a page of `{name}` keys, delete one key — so the tool that runs against
// wrangler (relay/tools/purge.js) is a shell around tested code.

// The prefix every stored triple lives under.
export const SHEET_PREFIX = 'sheet/';

// vendorOf reads the vendor half of a "<vendor>/<id>" model id, lowercased.
function vendorOf(id) {
  const slash = id.indexOf('/');
  return (slash < 0 ? id : id.slice(0, slash)).toLowerCase();
}

// cellVendors reads the model and judge vendors one sheet key names, or null
// for a key that is not a well-formed sheet key. A sheet key is
// sheet/<install>/<day>/<cellKey>: the install and the day carry no "/" of
// their own, so everything after them is the cellKey, whose dimensions hold no
// "|". Five segments are a role_quality key (role, model, judge, door, size);
// six lead with the metric. This is the same read loadEntries makes of a stored
// key name.
export function cellVendors(key, prefix = SHEET_PREFIX) {
  const parts = key.slice(prefix.length).split('/');
  if (parts.length < 3) {
    return null;
  }
  const dims = parts.slice(2).join('/').split('|');
  if (dims.length !== 5 && dims.length !== 6) {
    return null;
  }
  const at = dims.length === 6 ? 2 : 1;
  return { model: vendorOf(dims[at]), judge: vendorOf(dims[at + 1]) };
}

// isFixtureKey answers whether a sheet key names a model or judge vendor in the
// set. A key that is not a sheet key, or does not parse, is never one.
export function isFixtureKey(key, vendors, prefix = SHEET_PREFIX) {
  const v = cellVendors(key, prefix);
  if (v === null) {
    return false;
  }
  return vendors.has(v.model) || vendors.has(v.judge);
}

// listKeys answers every key name under prefix, following the store's pages.
export async function listKeys(store, prefix = SHEET_PREFIX) {
  const names = [];
  let cursor;
  do {
    const page = await store.list({ prefix, cursor });
    for (const key of page.keys) {
      names.push(key.name);
    }
    cursor = page.list_complete ? null : page.cursor;
  } while (cursor);
  return names;
}

// planPurge answers the sorted sheet keys that carry a vendor in the set: what
// a dry run prints and what a delete run removes.
export async function planPurge(store, vendors, prefix = SHEET_PREFIX) {
  const names = await listKeys(store, prefix);
  return names.filter((name) => isFixtureKey(name, vendors, prefix)).sort();
}

// runPurge purges the store. With dryRun it answers the keys it would delete
// and touches nothing; otherwise it deletes each one and answers the keys it
// deleted. The count the tool prints is the length of what it answers.
export async function runPurge(store, vendors, { dryRun = false, prefix = SHEET_PREFIX } = {}) {
  const keys = await planPurge(store, vendors, prefix);
  if (dryRun) {
    return keys;
  }
  for (const key of keys) {
    await store.delete(key);
  }
  return keys;
}
