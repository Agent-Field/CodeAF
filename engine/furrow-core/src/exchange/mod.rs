//! Stage 1 verbs that move a snapshot between device-local stores as sealed
//! frames: `export` and `published` on the sending side, `want`, `import` and
//! `materialize` on the receiving side. The engine never touches a network or
//! a key file; a caller carries frames and hands the keys in per call.

pub mod export;
pub mod fetch;
pub mod frame;
pub mod keys;
pub mod ledger;
pub mod materialize;
pub mod ops;
pub mod survey;

use crate::repository::store_path;
use crate::store::ObjectStore;
use std::path::Path;

/// Opens the object store of a data directory, creating it when absent.
pub fn open_store(data_dir: &Path) -> anyhow::Result<ObjectStore> {
    ObjectStore::open(store_path(data_dir))
}
