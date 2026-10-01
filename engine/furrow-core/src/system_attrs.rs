//! Extended attributes the operating system keeps for itself.
//!
//! macOS stamps a provenance attribute on every file a process creates, and
//! adds quarantine, access-list and Spotlight attributes by itself, none of
//! which a person edits and none of which can be removed. A tree sealed on
//! another machine lacks them, so counting them as content made every file of
//! a Mac tree differ from the head it was restored from, and every take
//! rewrote the whole tree. They are left out of what is captured and what is
//! restored, so the same content gives the same tree on every platform.

use std::ffi::OsStr;
use std::os::unix::ffi::OsStrExt;

/// Whole names the system manages.
const MANAGED_NAMES: [&[u8]; 4] = [
    b"com.apple.provenance",
    b"com.apple.quarantine",
    b"com.apple.macl",
    b"com.apple.lastuseddate#PS",
];

/// Name prefixes of attribute families the system manages.
const MANAGED_PREFIXES: [&[u8]; 2] = [b"com.apple.metadata:", b"com.apple.diskimages."];

/// True for an attribute that belongs to the system and so is not content.
pub fn is_system_managed(name: &OsStr) -> bool {
    let name = name.as_bytes();
    MANAGED_NAMES.contains(&name)
        || MANAGED_PREFIXES
            .iter()
            .any(|prefix| name.starts_with(prefix))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn the_attributes_macos_adds_by_itself_are_not_content() {
        for name in [
            "com.apple.provenance",
            "com.apple.quarantine",
            "com.apple.macl",
            "com.apple.lastuseddate#PS",
            "com.apple.metadata:kMDItemWhereFroms",
        ] {
            assert!(is_system_managed(OsStr::new(name)), "{name}");
        }
    }

    #[test]
    fn the_attributes_a_person_or_tool_sets_are_content() {
        for name in ["user.comment", "com.apple.FinderInfo", "security.selinux"] {
            assert!(!is_system_managed(OsStr::new(name)), "{name}");
        }
    }
}
