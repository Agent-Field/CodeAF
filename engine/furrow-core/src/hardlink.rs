//! Hard links of a workspace: noted while it is captured, recorded in the
//! snapshot as groups of paths, and put back when a snapshot is restored.
//!
//! A link count above one is not evidence of a group. A file may share its
//! inode with something outside the tree (a seeded copy of the previous
//! folder, a cache), and that is not a link between two paths of the
//! snapshot. A group therefore exists only when two or more captured paths
//! name the same device and inode, and a restore only ever links paths of the
//! snapshot to one another, never to anything else.

use crate::model::LinkGroup;
use anyhow::Context;
use serde_bytes::ByteBuf;
use std::fs;
use std::os::unix::ffi::OsStrExt;
use std::os::unix::fs::MetadataExt;
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicU64, Ordering};

/// The device and inode of a regular file that has another directory entry,
/// which is the only kind of file that can belong to a group.
pub fn identity(metadata: &fs::Metadata) -> Option<(u64, u64)> {
    (metadata.is_file() && metadata.nlink() > 1).then(|| (metadata.dev(), metadata.ino()))
}

/// The groups the path index noted, in the form a snapshot records.
pub fn groups(noted: Vec<Vec<Vec<u8>>>) -> Vec<LinkGroup> {
    noted
        .into_iter()
        .map(|paths| LinkGroup {
            paths: paths.into_iter().map(ByteBuf::from).collect(),
        })
        .collect()
}

/// What a restore says it could not keep: paths left as separate copies of
/// the same bytes because the file system refused the link.
pub type Unlinked = Vec<Vec<u8>>;

/// Makes every group's intact members one file again. `intact` answers the
/// place on disk of a member that is selected, present and still equal to its
/// entry in the snapshot, and None for any other path, which is left alone.
/// The first intact member is the one the others are linked to, so the bytes
/// of a member are never replaced by anything but its own equal twin.
///
/// A link is made beside the member and renamed over it, so a member is
/// either its old file or the link and never missing. Where the link is
/// refused (a file system without links, a limit on them) the member stays a
/// copy and its path is returned so the caller can say so.
pub fn restore(
    groups: &[LinkGroup],
    intact: impl Fn(&[u8]) -> anyhow::Result<Option<PathBuf>>,
) -> anyhow::Result<Unlinked> {
    let mut unlinked = Vec::new();
    for group in groups {
        let mut members = Vec::new();
        for path in &group.paths {
            if let Some(place) = intact(path)? {
                members.push((path, place));
            }
        }
        let Some(((_, anchor), rest)) = members.split_first() else {
            continue;
        };
        for (path, place) in rest {
            if !same_file(anchor, place)? && relink(anchor, place).is_err() {
                unlinked.push(path.to_vec());
            }
        }
    }
    Ok(unlinked)
}

fn same_file(left: &Path, right: &Path) -> anyhow::Result<bool> {
    let (left, right) = (fs::symlink_metadata(left)?, fs::symlink_metadata(right)?);
    Ok((left.dev(), left.ino()) == (right.dev(), right.ino()))
}

fn relink(anchor: &Path, member: &Path) -> anyhow::Result<()> {
    static COUNTER: AtomicU64 = AtomicU64::new(0);
    let beside = member.with_file_name(format!(
        ".furrow-link-{}-{}",
        std::process::id(),
        COUNTER.fetch_add(1, Ordering::Relaxed)
    ));
    fs::hard_link(anchor, &beside).with_context(|| {
        format!(
            "link {}",
            String::from_utf8_lossy(member.as_os_str().as_bytes())
        )
    })?;
    fs::rename(&beside, member).inspect_err(|_| {
        let _ = fs::remove_file(&beside);
    })?;
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::ffi::OsStr;

    #[test]
    fn a_member_the_file_system_refuses_to_link_stays_a_copy_and_is_named() {
        let temp = tempfile::tempdir().unwrap();
        // A directory cannot be the target of a hard link, as a file system
        // that keeps no links cannot take one: the refusal is the same error.
        let anchor = temp.path().join("anchor");
        fs::create_dir(&anchor).unwrap();
        let member = temp.path().join("member");
        fs::write(&member, "bytes").unwrap();
        let group = LinkGroup {
            paths: vec![
                ByteBuf::from(b"anchor".to_vec()),
                ByteBuf::from(b"member".to_vec()),
            ],
        };

        let unlinked = restore(&[group], |path| {
            Ok(Some(temp.path().join(OsStr::from_bytes(path))))
        })
        .unwrap();

        assert_eq!(unlinked, vec![b"member".to_vec()]);
        assert_eq!(fs::read(&member).unwrap(), b"bytes");
        let leftovers = fs::read_dir(temp.path()).unwrap().count();
        assert_eq!(leftovers, 2, "no link was left beside the member");
    }
}
