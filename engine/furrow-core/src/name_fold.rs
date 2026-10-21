//! Names that one file system keeps apart and another folds into one.
//!
//! Linux keeps `README.md` and `Readme.md` as two files, and `café` spelled
//! with one code point apart from `café` spelled with two. A case-insensitive
//! file system (APFS on a Mac by default) or a normalizing one keeps one of each
//! pair. Writing both there makes the second write land on the first file, so
//! a restore that wrote the whole tree would either overwrite one copy or fail
//! its own check afterwards. The restore therefore asks the file system it is
//! about to write into how it folds names, and keeps one copy of each such pair.
//!
//! The copy kept is the one whose name sorts first byte for byte, which is the
//! order every tree lists its entries in: the choice depends on the names alone,
//! so every machine that restores the same snapshot keeps the same copy.

use std::fs;
use std::path::Path;
use unicode_normalization::UnicodeNormalization;

/// Test-only switch that stands in for the probe: `case`, `unicode`, or both
/// separated by a comma. It lets a Linux test restore as a Mac would.
pub const TEST_ENV: &str = "FURROW_TEST_NAME_FOLDING";

/// How the file system under a folder folds names.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub struct NameFolding {
    /// Names that differ only in letter case are one name.
    pub case: bool,
    /// Names that differ only in Unicode normalization form are one name.
    pub unicode: bool,
}

impl NameFolding {
    /// What the file system holding `dir` does, found by making two probe files
    /// there. A file system that cannot be probed counts as keeping every name.
    pub fn probe(dir: &Path) -> Self {
        if let Some(forced) = std::env::var_os(TEST_ENV) {
            return Self::from_spec(&forced.to_string_lossy());
        }
        Self {
            case: folds(dir, ".furrow-fold-Aa", ".furrow-fold-aA"),
            unicode: folds(dir, ".furrow-fold-\u{e9}", ".furrow-fold-e\u{301}"),
        }
    }

    fn from_spec(spec: &str) -> Self {
        Self {
            case: spec.split(',').any(|part| part.trim() == "case"),
            unicode: spec.split(',').any(|part| part.trim() == "unicode"),
        }
    }

    /// Whether any pair of names can collide here.
    pub fn is_active(&self) -> bool {
        self.case || self.unicode
    }

    /// The spelling every name of one folded group shares. A name that is not
    /// text is its own key: no file system folds raw bytes.
    pub fn key(&self, name: &[u8]) -> Vec<u8> {
        let Ok(text) = std::str::from_utf8(name) else {
            return name.to_vec();
        };
        let text: String = if self.unicode {
            text.nfd().collect()
        } else {
            text.to_owned()
        };
        if self.case {
            text.to_lowercase().into_bytes()
        } else {
            text.into_bytes()
        }
    }
}

/// Whether creating `first` in `dir` makes `second` exist too.
fn folds(dir: &Path, first: &str, second: &str) -> bool {
    let created = dir.join(first);
    if fs::write(&created, b"").is_err() {
        return false;
    }
    let same = fs::symlink_metadata(dir.join(second)).is_ok();
    let _ = fs::remove_file(&created);
    same
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn case_folding_joins_names_that_differ_only_in_case() {
        let folding = NameFolding {
            case: true,
            unicode: false,
        };
        assert_eq!(folding.key(b"README.md"), folding.key(b"Readme.md"));
        assert_ne!(folding.key(b"a"), folding.key(b"b"));
    }

    #[test]
    fn unicode_folding_joins_composed_and_decomposed_spellings() {
        let folding = NameFolding {
            case: false,
            unicode: true,
        };
        assert_eq!(
            folding.key("caf\u{e9}".as_bytes()),
            folding.key("cafe\u{301}".as_bytes())
        );
        assert_ne!(folding.key(b"Readme"), folding.key(b"readme"));
    }

    #[test]
    fn a_case_sensitive_plain_file_system_joins_nothing() {
        let folding = NameFolding::default();
        assert!(!folding.is_active());
        assert_ne!(folding.key(b"A"), folding.key(b"a"));
    }

    #[test]
    fn spec_names_the_behaviours() {
        assert_eq!(
            NameFolding::from_spec("case, unicode"),
            NameFolding {
                case: true,
                unicode: true
            }
        );
        assert_eq!(NameFolding::from_spec(""), NameFolding::default());
    }

    #[test]
    fn probe_of_a_linux_temp_folder_finds_no_folding() {
        let dir = tempfile::tempdir().unwrap();
        if std::env::var_os(TEST_ENV).is_none() {
            let found = NameFolding::probe(dir.path());
            // tmpfs and ext4 keep both names; a developer on a Mac sees folding.
            assert_eq!(found.is_active(), cfg!(target_os = "macos"));
        }
    }
}
