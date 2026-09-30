//! One durability step for a batch of writes.
//!
//! Writing a tree file by file and syncing each one makes the time of a
//! restore the number of files times the latency of a device flush, which is
//! milliseconds each. A restore writes all its files first and then makes
//! them durable together, so a crash before this call leaves the restore
//! intent to roll back and a crash after it leaves every file on disk.

use std::fs::File;
use std::io;
use std::path::PathBuf;

/// What a restore wrote: the files it created and the directories that gained
/// or lost an entry, all under one root.
pub struct Written {
    pub root: PathBuf,
    pub files: Vec<PathBuf>,
    pub directories: Vec<PathBuf>,
}

impl Written {
    /// Makes every write of the batch durable with the cheapest step the
    /// platform offers.
    pub fn make_durable(&self) -> io::Result<()> {
        platform::flush(self)
    }
}

#[cfg(target_os = "linux")]
mod platform {
    use super::*;
    use std::os::fd::AsRawFd;

    /// `syncfs` flushes the data and the directory entries of the whole
    /// filesystem in one journal commit, so its cost does not grow with the
    /// number of files written.
    pub fn flush(written: &Written) -> io::Result<()> {
        let root = File::open(&written.root)?;
        // SAFETY: the descriptor is open for the whole call.
        match unsafe { libc::syncfs(root.as_raw_fd()) } {
            0 => Ok(()),
            _ => Err(io::Error::last_os_error()),
        }
    }
}

#[cfg(not(target_os = "linux"))]
mod platform {
    use super::*;
    use std::os::fd::AsRawFd;
    use std::path::Path;

    /// A plain `fsync` on these systems hands the data to the drive without
    /// waiting for the drive's cache, which is cheap, and one full flush of
    /// the root afterwards (`File::sync_all` is `F_FULLFSYNC` on macOS)
    /// makes the drive persist everything handed to it before.
    pub fn flush(written: &Written) -> io::Result<()> {
        for path in written.files.iter().chain(&written.directories) {
            hand_to_drive(path)?;
        }
        File::open(&written.root)?.sync_all()
    }

    fn hand_to_drive(path: &Path) -> io::Result<()> {
        let file = File::open(path)?;
        // SAFETY: the descriptor is open for the whole call.
        match unsafe { libc::fsync(file.as_raw_fd()) } {
            0 => Ok(()),
            _ => Err(io::Error::last_os_error()),
        }
    }
}
