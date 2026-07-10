//! Small, auditable capture-policy parser.

use anyhow::Context;
use std::fs;
use std::os::unix::ffi::OsStrExt;
use std::path::Path;

pub const POLICY_FILE: &str = ".furrowpolicy";
pub const POLICY_FILE_BYTES: &[u8] = b".furrowpolicy";
const MAX_POLICY_BYTES: u64 = 64 * 1024;
const MAX_RULES: usize = 1_024;

#[derive(Debug, Clone, Default)]
pub struct CapturePolicy {
    excluded: Vec<Vec<u8>>,
}

impl CapturePolicy {
    pub fn load(root: &Path) -> anyhow::Result<Self> {
        let path = root.join(POLICY_FILE);
        let metadata = match fs::symlink_metadata(&path) {
            Ok(metadata) => metadata,
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => {
                return Ok(Self::default())
            }
            Err(error) => return Err(error.into()),
        };
        anyhow::ensure!(
            metadata.is_file() && !metadata.file_type().is_symlink(),
            ".furrowpolicy must be a regular file"
        );
        anyhow::ensure!(
            metadata.len() <= MAX_POLICY_BYTES,
            ".furrowpolicy exceeds 64 KiB"
        );
        let contents = fs::read_to_string(&path).context(".furrowpolicy must be valid UTF-8")?;
        let mut excluded = Vec::new();
        for (index, raw) in contents.lines().enumerate() {
            let line = raw.trim();
            if line.is_empty() || line.starts_with('#') {
                continue;
            }
            let value = line.strip_prefix("exclude ").with_context(|| {
                format!(
                    ".furrowpolicy line {} must use `exclude <relative-subtree>`",
                    index + 1
                )
            })?;
            let value = value.trim().trim_end_matches('/');
            let bytes = value.as_bytes();
            validate_rule(bytes)
                .with_context(|| format!("invalid .furrowpolicy line {}", index + 1))?;
            excluded.push(bytes.to_vec());
            anyhow::ensure!(
                excluded.len() <= MAX_RULES,
                ".furrowpolicy exceeds 1024 rules"
            );
        }
        excluded.sort();
        excluded.dedup();
        Ok(Self { excluded })
    }

    pub fn from_rules(rules: &[String]) -> anyhow::Result<Self> {
        anyhow::ensure!(
            rules.len() <= MAX_RULES,
            "snapshot policy exceeds 1024 rules"
        );
        let mut excluded = Vec::with_capacity(rules.len());
        for rule in rules {
            validate_rule(rule.as_bytes())?;
            excluded.push(rule.as_bytes().to_vec());
        }
        excluded.sort();
        excluded.dedup();
        Ok(Self { excluded })
    }

    pub fn union(&self, other: &Self) -> Self {
        let mut excluded = self.excluded.clone();
        excluded.extend(other.excluded.iter().cloned());
        excluded.sort();
        excluded.dedup();
        Self { excluded }
    }

    pub fn excludes_bytes(&self, relative: &[u8]) -> bool {
        self.excluded.iter().any(|rule| {
            relative == rule.as_slice()
                || (relative.starts_with(rule) && relative.get(rule.len()) == Some(&b'/'))
        })
    }

    pub fn excludes_path(&self, root: &Path, path: &Path) -> bool {
        path.strip_prefix(root)
            .ok()
            .is_some_and(|relative| self.excludes_bytes(relative.as_os_str().as_bytes()))
    }

    pub fn rules(&self) -> impl Iterator<Item = &[u8]> {
        self.excluded.iter().map(Vec::as_slice)
    }

    pub fn rule_strings(&self) -> Vec<String> {
        self.excluded
            .iter()
            .map(|rule| String::from_utf8(rule.clone()).expect("policy rules are UTF-8"))
            .collect()
    }
}

fn validate_rule(rule: &[u8]) -> anyhow::Result<()> {
    anyhow::ensure!(!rule.is_empty(), "exclude path cannot be empty");
    anyhow::ensure!(!rule.starts_with(b"/"), "exclude path must be relative");
    anyhow::ensure!(!rule.contains(&0), "exclude path contains NUL");
    anyhow::ensure!(
        rule.split(|byte| *byte == b'/')
            .all(|part| !part.is_empty() && part != b"." && part != b".."),
        "exclude path contains an unsafe component"
    );
    let first = rule.split(|byte| *byte == b'/').next().unwrap_or_default();
    anyhow::ensure!(
        first != b".git" && first != b".furrow" && rule != POLICY_FILE_BYTES,
        "Git and furrow control state cannot be excluded"
    );
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_literal_subtrees_and_rejects_unsafe_rules() {
        let temporary = tempfile::tempdir().unwrap();
        fs::write(
            temporary.path().join(POLICY_FILE),
            b"# local policy\nexclude node_modules/\nexclude packages/app/cache\n",
        )
        .unwrap();
        let policy = CapturePolicy::load(temporary.path()).unwrap();
        assert!(policy.excludes_bytes(b"node_modules/pkg/index.js"));
        assert!(policy.excludes_bytes(b"packages/app/cache"));
        assert!(!policy.excludes_bytes(b"packages/app/cacheable"));

        fs::write(temporary.path().join(POLICY_FILE), b"exclude ../outside\n").unwrap();
        assert!(CapturePolicy::load(temporary.path()).is_err());
        fs::write(temporary.path().join(POLICY_FILE), b"exclude .git\n").unwrap();
        assert!(CapturePolicy::load(temporary.path()).is_err());
    }
}
