//! A directory that lives outside the workspace but is sealed as part of it.
//!
//! The overlay stands for the tree entry [`NAME`]: sealing composes it into
//! the workspace's tree, and restoring puts it back wherever the caller says.
//! Everything that walks the tree asks the overlay to translate between a
//! tree-relative path and the place that path really lives.

use std::ffi::OsString;
use std::os::unix::ffi::{OsStrExt, OsStringExt};
use std::path::{Path, PathBuf};

/// Name of the composed entry at the top of the sealed tree.
pub const NAME: &[u8] = b".cell";

#[derive(Debug, Clone)]
pub struct Overlay {
    source: PathBuf,
}

impl Overlay {
    pub fn new(source: PathBuf) -> Self {
        Self { source }
    }

    pub fn source(&self) -> &Path {
        &self.source
    }

    /// The tree-relative path of `path` when it lies inside the overlay.
    pub fn logical(&self, path: &Path) -> Option<Vec<u8>> {
        let rest = path.strip_prefix(&self.source).ok()?;
        let mut logical = NAME.to_vec();
        if !rest.as_os_str().is_empty() {
            logical.push(b'/');
            logical.extend_from_slice(rest.as_os_str().as_bytes());
        }
        Some(logical)
    }

    /// Where a tree-relative path inside the overlay really lives.
    pub fn physical(&self, relative: &[u8]) -> Option<PathBuf> {
        let rest = relative.strip_prefix(NAME)?;
        match rest.split_first() {
            None => Some(self.source.clone()),
            Some((b'/', tail)) => Some(self.source.join(OsString::from_vec(tail.to_vec()))),
            Some(_) => None,
        }
    }
}

/// Merges the overlay's name into a bytewise-sorted stream of directory names,
/// replacing any real entry of the same name.
pub struct WithOverlay<I: Iterator> {
    names: std::iter::Peekable<I>,
    pending: bool,
}

impl<I, E> WithOverlay<I>
where
    I: Iterator<Item = Result<OsString, E>>,
{
    pub fn new(names: I) -> Self {
        Self {
            names: names.peekable(),
            pending: true,
        }
    }
}

impl<I, E> Iterator for WithOverlay<I>
where
    I: Iterator<Item = Result<OsString, E>>,
{
    type Item = Result<OsString, E>;

    fn next(&mut self) -> Option<Self::Item> {
        loop {
            let ahead = match self.names.peek() {
                Some(Ok(name)) => Some(name.as_bytes().cmp(NAME)),
                Some(Err(_)) => return self.names.next(),
                None => None,
            };
            match ahead {
                Some(std::cmp::Ordering::Equal) => {
                    self.names.next();
                }
                Some(std::cmp::Ordering::Less) => return self.names.next(),
                _ if self.pending => {
                    self.pending = false;
                    return Some(Ok(OsString::from_vec(NAME.to_vec())));
                }
                _ => return self.names.next(),
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn names(input: &[&str]) -> Vec<String> {
        let stream = input.iter().map(|name| Ok::<_, ()>(OsString::from(name)));
        WithOverlay::new(stream)
            .map(|name| name.unwrap().to_string_lossy().into_owned())
            .collect()
    }

    #[test]
    fn the_overlay_name_lands_in_sorted_position() {
        assert_eq!(names(&[]), [".cell"]);
        assert_eq!(names(&["a", "b"]), [".cell", "a", "b"]);
        assert_eq!(
            names(&[".a", ".cellx", "z"]),
            [".a", ".cell", ".cellx", "z"]
        );
        assert_eq!(names(&["!", "a"]), ["!", ".cell", "a"]);
    }

    #[test]
    fn a_real_entry_of_the_same_name_is_replaced() {
        assert_eq!(names(&[".cell", "a"]), [".cell", "a"]);
    }

    #[test]
    fn paths_translate_both_ways() {
        let overlay = Overlay::new(PathBuf::from("/private/cell"));
        assert_eq!(
            overlay.logical(Path::new("/private/cell/log/a")).unwrap(),
            b".cell/log/a"
        );
        assert_eq!(
            overlay.logical(Path::new("/private/cell")).unwrap(),
            b".cell"
        );
        assert!(overlay.logical(Path::new("/work/a")).is_none());
        assert_eq!(
            overlay.physical(b".cell/log/a").unwrap(),
            PathBuf::from("/private/cell/log/a")
        );
        assert_eq!(
            overlay.physical(b".cell").unwrap(),
            PathBuf::from("/private/cell")
        );
        assert!(overlay.physical(b".cellar/a").is_none());
    }
}
