use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, PartialOrd, Ord, Serialize, Deserialize)]
#[serde(rename_all = "kebab-case")]
#[repr(u8)]
pub enum ContentClass {
    #[default]
    Source,
    VcsMeta,
    ConfigSecret,
    Dependency,
    BuildOutput,
    Database,
    Lockfile,
    Scratch,
}

impl ContentClass {
    pub const ALL: [Self; 8] = [
        Self::Source,
        Self::VcsMeta,
        Self::ConfigSecret,
        Self::Dependency,
        Self::BuildOutput,
        Self::Database,
        Self::Lockfile,
        Self::Scratch,
    ];

    pub fn bit(self) -> u16 {
        1 << self as u8
    }

    pub fn as_str(self) -> &'static str {
        match self {
            Self::Source => "source",
            Self::VcsMeta => "vcs-meta",
            Self::ConfigSecret => "config-secret",
            Self::Dependency => "dependency",
            Self::BuildOutput => "build-output",
            Self::Database => "database",
            Self::Lockfile => "lockfile",
            Self::Scratch => "scratch",
        }
    }
}

pub fn classify(relative: &[u8]) -> ContentClass {
    let components: Vec<&[u8]> = relative
        .split(|byte| *byte == b'/')
        .filter(|component| !component.is_empty())
        .collect();
    let Some(name) = components.last().copied() else {
        return ContentClass::Source;
    };

    if has_component(&components, &[b".git", b".jj"]) {
        return ContentClass::VcsMeta;
    }
    if is_secret(&components, name) {
        return ContentClass::ConfigSecret;
    }
    if is_lockfile(name) {
        return ContentClass::Lockfile;
    }
    if is_database(name) {
        return ContentClass::Database;
    }
    if has_component(
        &components,
        &[b"node_modules", b".venv", b"venv", b"vendor", b".pnpm"],
    ) || has_component_suffix(&components, b"site-packages")
    {
        return ContentClass::Dependency;
    }
    if has_component(
        &components,
        &[
            b"target",
            b"dist",
            b"build",
            b".next",
            b".nuxt",
            b".turbo",
            b".parcel-cache",
            b"coverage",
            b"out",
        ],
    ) || has_suffix(name, &[b".o", b".obj", b".class", b".pyc"])
    {
        return ContentClass::BuildOutput;
    }
    if has_component(
        &components,
        &[b"tmp", b"temp", b"logs", b"log", b".cache", b"__pycache__"],
    ) || name.starts_with(b".#")
        || name.ends_with(b"~")
        || has_suffix(name, &[b".log", b".tmp", b".swp", b".swo"])
    {
        return ContentClass::Scratch;
    }
    ContentClass::Source
}

fn has_component(components: &[&[u8]], candidates: &[&[u8]]) -> bool {
    components
        .iter()
        .any(|component| candidates.contains(component))
}

fn has_component_suffix(components: &[&[u8]], suffix: &[u8]) -> bool {
    components
        .iter()
        .any(|component| component.ends_with(suffix))
}

fn has_suffix(name: &[u8], suffixes: &[&[u8]]) -> bool {
    suffixes.iter().any(|suffix| name.ends_with(suffix))
}

fn is_secret(components: &[&[u8]], name: &[u8]) -> bool {
    has_component(components, &[b".ssh", b".gnupg"])
        || name == b".env"
        || name.starts_with(b".env.")
        || matches!(name, b"id_rsa" | b"id_ed25519" | b"id_ecdsa")
        || has_suffix(name, &[b".pem", b".key", b".p12", b".pfx"])
}

fn is_lockfile(name: &[u8]) -> bool {
    matches!(
        name,
        b"Cargo.lock"
            | b"package-lock.json"
            | b"pnpm-lock.yaml"
            | b"yarn.lock"
            | b"bun.lock"
            | b"bun.lockb"
            | b"poetry.lock"
            | b"Pipfile.lock"
            | b"go.sum"
            | b"composer.lock"
            | b"Gemfile.lock"
    )
}

fn is_database(name: &[u8]) -> bool {
    has_suffix(name, &[b".sqlite", b".sqlite3", b".db"])
        || has_suffix(
            name,
            &[
                b".sqlite-wal",
                b".sqlite-shm",
                b".sqlite3-wal",
                b".sqlite3-shm",
                b".db-wal",
                b".db-shm",
            ],
        )
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn classifies_retention_and_security_relevant_paths() {
        let cases = [
            (b"src/main.rs".as_slice(), ContentClass::Source),
            (b".git/index".as_slice(), ContentClass::VcsMeta),
            (b"config/.env.local".as_slice(), ContentClass::ConfigSecret),
            (b"keys/deploy.pem".as_slice(), ContentClass::ConfigSecret),
            (b"Cargo.lock".as_slice(), ContentClass::Lockfile),
            (b"data/dev.sqlite-wal".as_slice(), ContentClass::Database),
            (
                b"node_modules/pkg/index.js".as_slice(),
                ContentClass::Dependency,
            ),
            (b"target/debug/app.o".as_slice(), ContentClass::BuildOutput),
            (b"logs/server.log".as_slice(), ContentClass::Scratch),
        ];
        for (path, expected) in cases {
            assert_eq!(
                classify(path),
                expected,
                "{}",
                String::from_utf8_lossy(path)
            );
        }
    }

    #[test]
    fn legacy_tree_entries_default_to_source() {
        let entry: crate::model::TreeEntry = serde_json::from_value(serde_json::json!({
            "name": [102],
            "kind": "File",
            "target": null,
            "link_target": [],
            "mode": 33188,
            "size": 0,
            "mtime_secs": 0,
            "mtime_nanos": 0,
            "xattrs": null
        }))
        .unwrap();
        assert_eq!(entry.class, ContentClass::Source);
    }
}
