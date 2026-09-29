use anyhow::Context;
use rusqlite::backup::Backup;
use rusqlite::{Connection, OpenFlags};
use std::path::Path;
use std::time::Duration;
use tempfile::NamedTempFile;

pub struct ConsistentBackup {
    pub file: NamedTempFile,
    pub integrity_ok: bool,
}

const HEADER: &[u8; 16] = b"SQLite format 3\0";

/// True when `bytes` begin the way every SQLite database file begins.
pub fn has_header(bytes: &[u8]) -> bool {
    bytes.starts_with(HEADER)
}

pub fn is_sqlite(path: &Path) -> bool {
    let mut header = [0_u8; 16];
    let Ok(mut file) = std::fs::File::open(path) else {
        return false;
    };
    use std::io::Read;
    file.read_exact(&mut header).is_ok() && has_header(&header)
}

pub fn consistent_backup(path: &Path, temp_dir: &Path) -> anyhow::Result<ConsistentBackup> {
    std::fs::create_dir_all(temp_dir)?;
    let source = Connection::open_with_flags(
        path,
        OpenFlags::SQLITE_OPEN_READ_ONLY | OpenFlags::SQLITE_OPEN_NO_MUTEX,
    )
    .with_context(|| format!("open SQLite database {}", path.display()))?;
    source.busy_timeout(Duration::from_secs(2))?;

    let file = NamedTempFile::new_in(temp_dir)?;
    let mut destination = Connection::open(file.path())?;
    {
        let backup = Backup::new(&source, &mut destination)?;
        backup.run_to_completion(128, Duration::from_millis(2), None)?;
    }
    let integrity: String =
        destination.query_row("PRAGMA integrity_check", [], |row| row.get(0))?;
    destination.close().map_err(|(_, error)| error)?;
    Ok(ConsistentBackup {
        file,
        integrity_ok: integrity == "ok",
    })
}
