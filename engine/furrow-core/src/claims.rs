//! Advisory path claims with transactional conflict detection and TTL expiry.

use crate::model::ClaimRecord;
use anyhow::Context;
use rusqlite::{params, Connection};
use std::path::{Path, PathBuf};
use std::time::{Duration, SystemTime, UNIX_EPOCH};

const MAX_PATTERN_BYTES: usize = 1024;
const MAX_OWNER_BYTES: usize = 256;
const MAX_TTL_SECONDS: u64 = 7 * 24 * 60 * 60;

pub struct Registry {
    connection: Connection,
}

impl Registry {
    pub fn open(store_root: &Path, family_id: &str) -> anyhow::Result<Self> {
        validate_family_id(family_id)?;
        let directory = store_root.join("families").join(family_id);
        std::fs::create_dir_all(&directory)?;
        let connection = Connection::open(directory.join("claims.sqlite3"))?;
        connection.busy_timeout(Duration::from_secs(5))?;
        connection.execute_batch(
            "PRAGMA journal_mode=WAL;
             PRAGMA synchronous=FULL;
             CREATE TABLE IF NOT EXISTS claims(
                id TEXT PRIMARY KEY,
                pattern TEXT NOT NULL,
                owner TEXT NOT NULL,
                workspace_id TEXT NOT NULL,
                created_at INTEGER NOT NULL,
                expires_at INTEGER NOT NULL
             );
             CREATE INDEX IF NOT EXISTS claims_expiry ON claims(expires_at);",
        )?;
        Ok(Self { connection })
    }

    pub fn claim(
        &mut self,
        pattern: &str,
        owner: &str,
        workspace_id: &str,
        ttl_seconds: u64,
    ) -> anyhow::Result<ClaimRecord> {
        validate_pattern(pattern)?;
        validate_owner(owner)?;
        anyhow::ensure!(
            (1..=MAX_TTL_SECONDS).contains(&ttl_seconds),
            "claim TTL must be between 1 second and 7 days"
        );
        let now = now()?;
        let transaction = self
            .connection
            .transaction_with_behavior(rusqlite::TransactionBehavior::Immediate)?;
        transaction.execute("DELETE FROM claims WHERE expires_at <= ?1", params![now])?;
        let mut statement = transaction.prepare(
            "SELECT id, pattern, owner, workspace_id, created_at, expires_at
             FROM claims ORDER BY created_at, id",
        )?;
        let existing = statement
            .query_map([], row_to_claim)?
            .collect::<Result<Vec<_>, _>>()?;
        drop(statement);
        if let Some(existing) = existing.iter().find(|claim| {
            claim.workspace_id == workspace_id && claim.owner == owner && claim.pattern == pattern
        }) {
            transaction.commit()?;
            return Ok(existing.clone());
        }
        if let Some(conflict) = existing.iter().find(|claim| {
            !(claim.workspace_id == workspace_id && claim.owner == owner)
                && patterns_may_overlap(&claim.pattern, pattern)
        }) {
            anyhow::bail!(
                "claim `{pattern}` overlaps `{}` held by {} until {}",
                conflict.pattern,
                conflict.owner,
                conflict.expires_at
            );
        }
        let claim = ClaimRecord {
            id: new_id()?,
            pattern: pattern.to_owned(),
            owner: owner.to_owned(),
            workspace_id: workspace_id.to_owned(),
            created_at: now,
            expires_at: now.saturating_add(ttl_seconds as i64),
        };
        transaction.execute(
            "INSERT INTO claims(id, pattern, owner, workspace_id, created_at, expires_at)
             VALUES(?1, ?2, ?3, ?4, ?5, ?6)",
            params![
                claim.id,
                claim.pattern,
                claim.owner,
                claim.workspace_id,
                claim.created_at,
                claim.expires_at
            ],
        )?;
        transaction.commit()?;
        Ok(claim)
    }

    pub fn active(&mut self) -> anyhow::Result<Vec<ClaimRecord>> {
        let now = now()?;
        let transaction = self
            .connection
            .transaction_with_behavior(rusqlite::TransactionBehavior::Immediate)?;
        transaction.execute("DELETE FROM claims WHERE expires_at <= ?1", params![now])?;
        let claims = {
            let mut statement = transaction.prepare(
                "SELECT id, pattern, owner, workspace_id, created_at, expires_at
                 FROM claims ORDER BY pattern, owner, id",
            )?;
            let mapped = statement.query_map([], row_to_claim)?;
            mapped.collect::<Result<Vec<_>, _>>()?
        };
        transaction.commit()?;
        Ok(claims)
    }

    pub fn release(
        &mut self,
        selector: &str,
        owner: &str,
        workspace_id: &str,
    ) -> anyhow::Result<Vec<ClaimRecord>> {
        validate_owner(owner)?;
        let transaction = self
            .connection
            .transaction_with_behavior(rusqlite::TransactionBehavior::Immediate)?;
        let released = {
            let mut statement = transaction.prepare(
                "SELECT id, pattern, owner, workspace_id, created_at, expires_at
                 FROM claims
                 WHERE (id = ?1 OR pattern = ?1) AND owner = ?2 AND workspace_id = ?3
                 ORDER BY id",
            )?;
            let mapped =
                statement.query_map(params![selector, owner, workspace_id], row_to_claim)?;
            mapped.collect::<Result<Vec<_>, _>>()?
        };
        anyhow::ensure!(
            !released.is_empty(),
            "no claim `{selector}` is held by {owner} in this workspace"
        );
        for claim in &released {
            transaction.execute("DELETE FROM claims WHERE id = ?1", params![claim.id])?;
        }
        transaction.commit()?;
        Ok(released)
    }

    pub fn restore(&mut self, claims: &[ClaimRecord]) -> anyhow::Result<()> {
        let transaction = self
            .connection
            .transaction_with_behavior(rusqlite::TransactionBehavior::Immediate)?;
        for claim in claims {
            transaction.execute(
                "INSERT OR REPLACE INTO claims
                 (id, pattern, owner, workspace_id, created_at, expires_at)
                 VALUES(?1, ?2, ?3, ?4, ?5, ?6)",
                params![
                    claim.id,
                    claim.pattern,
                    claim.owner,
                    claim.workspace_id,
                    claim.created_at,
                    claim.expires_at
                ],
            )?;
        }
        transaction.commit()?;
        Ok(())
    }

    pub fn release_workspace(&mut self, workspace_id: &str) -> anyhow::Result<u64> {
        let transaction = self
            .connection
            .transaction_with_behavior(rusqlite::TransactionBehavior::Immediate)?;
        let removed = transaction.execute(
            "DELETE FROM claims WHERE workspace_id = ?1",
            params![workspace_id],
        )?;
        transaction.commit()?;
        Ok(removed as u64)
    }
}

fn row_to_claim(row: &rusqlite::Row<'_>) -> rusqlite::Result<ClaimRecord> {
    Ok(ClaimRecord {
        id: row.get(0)?,
        pattern: row.get(1)?,
        owner: row.get(2)?,
        workspace_id: row.get(3)?,
        created_at: row.get(4)?,
        expires_at: row.get(5)?,
    })
}

fn validate_family_id(value: &str) -> anyhow::Result<()> {
    anyhow::ensure!(
        value.len() == 32 && value.bytes().all(|byte| byte.is_ascii_hexdigit()),
        "invalid family ID"
    );
    Ok(())
}

fn validate_owner(owner: &str) -> anyhow::Result<()> {
    anyhow::ensure!(!owner.is_empty(), "claim owner cannot be empty");
    anyhow::ensure!(owner.len() <= MAX_OWNER_BYTES, "claim owner is too long");
    anyhow::ensure!(
        !owner.chars().any(char::is_control),
        "claim owner contains control characters"
    );
    Ok(())
}

pub fn validate_pattern(pattern: &str) -> anyhow::Result<()> {
    anyhow::ensure!(!pattern.is_empty(), "claim pattern cannot be empty");
    anyhow::ensure!(
        pattern.len() <= MAX_PATTERN_BYTES,
        "claim pattern is too long"
    );
    anyhow::ensure!(!pattern.starts_with('/'), "claim pattern must be relative");
    anyhow::ensure!(
        !pattern.chars().any(char::is_control),
        "claim pattern contains control characters"
    );
    for component in pattern.split('/') {
        anyhow::ensure!(
            !component.is_empty() && component != "." && component != "..",
            "claim pattern contains an unsafe path component"
        );
    }
    Ok(())
}

fn patterns_may_overlap(left: &str, right: &str) -> bool {
    if left == right {
        return true;
    }
    let left_wild = contains_wildcard(left);
    let right_wild = contains_wildcard(right);
    if !left_wild && !right_wild {
        return false;
    }
    let left_prefix = literal_prefix(left);
    let right_prefix = literal_prefix(right);
    component_prefix(&left_prefix, &right_prefix) || component_prefix(&right_prefix, &left_prefix)
}

/// Match a validated claim glob against a lossless repository-relative path.
/// `*` and `?` stay within one component; `**` may cross `/` boundaries.
pub fn matches_path(pattern: &str, path: &[u8]) -> bool {
    let pattern = pattern.as_bytes();
    let width = path.len() + 1;
    let mut memo = vec![None; (pattern.len() + 1).saturating_mul(width)];
    glob_match(pattern, path, 0, 0, width, &mut memo)
}

fn glob_match(
    pattern: &[u8],
    path: &[u8],
    pattern_index: usize,
    path_index: usize,
    width: usize,
    memo: &mut [Option<bool>],
) -> bool {
    let slot = pattern_index * width + path_index;
    if let Some(result) = memo[slot] {
        return result;
    }
    let result = if pattern_index == pattern.len() {
        path_index == path.len()
    } else if pattern[pattern_index] == b'*' {
        let recursive = pattern.get(pattern_index + 1) == Some(&b'*');
        let next = pattern_index + if recursive { 2 } else { 1 };
        glob_match(pattern, path, next, path_index, width, memo)
            || (path_index < path.len()
                && (recursive || path[path_index] != b'/')
                && glob_match(pattern, path, pattern_index, path_index + 1, width, memo))
    } else if pattern[pattern_index] == b'?' {
        path_index < path.len()
            && path[path_index] != b'/'
            && glob_match(
                pattern,
                path,
                pattern_index + 1,
                path_index + 1,
                width,
                memo,
            )
    } else if pattern[pattern_index] == b'[' {
        match character_class(pattern, pattern_index, path.get(path_index).copied()) {
            Some((matches, next)) => {
                matches && glob_match(pattern, path, next, path_index + 1, width, memo)
            }
            None => {
                path.get(path_index) == Some(&b'[')
                    && glob_match(
                        pattern,
                        path,
                        pattern_index + 1,
                        path_index + 1,
                        width,
                        memo,
                    )
            }
        }
    } else {
        path.get(path_index) == Some(&pattern[pattern_index])
            && glob_match(
                pattern,
                path,
                pattern_index + 1,
                path_index + 1,
                width,
                memo,
            )
    };
    memo[slot] = Some(result);
    result
}

fn character_class(pattern: &[u8], start: usize, value: Option<u8>) -> Option<(bool, usize)> {
    let value = value.filter(|value| *value != b'/')?;
    let mut index = start + 1;
    let negated = matches!(pattern.get(index), Some(b'!' | b'^'));
    index += usize::from(negated);
    let content_start = index;
    let mut matched = false;
    while index < pattern.len() && pattern[index] != b']' {
        if index + 2 < pattern.len() && pattern[index + 1] == b'-' && pattern[index + 2] != b']' {
            matched |= pattern[index] <= value && value <= pattern[index + 2];
            index += 3;
        } else {
            matched |= pattern[index] == value;
            index += 1;
        }
    }
    if index == pattern.len() || index == content_start {
        return None;
    }
    Some((matched != negated, index + 1))
}

fn literal_prefix(pattern: &str) -> Vec<&str> {
    pattern
        .split('/')
        .take_while(|component| !contains_wildcard(component))
        .collect()
}

fn contains_wildcard(value: &str) -> bool {
    value.bytes().any(|byte| matches!(byte, b'*' | b'?' | b'['))
}

fn component_prefix(left: &[&str], right: &[&str]) -> bool {
    left.len() <= right.len() && left.iter().zip(right).all(|(left, right)| left == right)
}

fn new_id() -> anyhow::Result<String> {
    let mut bytes = [0_u8; 12];
    getrandom::getrandom(&mut bytes)
        .map_err(|error| anyhow::anyhow!("generate claim ID: {error}"))?;
    Ok(hex::encode(bytes))
}

fn now() -> anyhow::Result<i64> {
    Ok(SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .context("clock is before Unix epoch")?
        .as_secs() as i64)
}

pub fn registry_path(store_root: &Path, family_id: &str) -> PathBuf {
    store_root
        .join("families")
        .join(family_id)
        .join("claims.sqlite3")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn overlapping_claims_conflict_but_disjoint_and_same_owner_claims_work() {
        let temporary = tempfile::tempdir().unwrap();
        let mut registry = Registry::open(temporary.path(), &"a".repeat(32)).unwrap();
        let first = registry
            .claim("src/auth/**", "alpha", "workspace-a", 3600)
            .unwrap();
        assert!(registry
            .claim("src/auth/login.rs", "beta", "workspace-b", 3600)
            .is_err());
        registry
            .claim("src/payments/**", "beta", "workspace-b", 3600)
            .unwrap();
        assert_eq!(
            registry
                .claim("src/auth/**", "alpha", "workspace-a", 3600)
                .unwrap()
                .id,
            first.id
        );
        assert_eq!(registry.active().unwrap().len(), 2);
        registry.release(&first.id, "alpha", "workspace-a").unwrap();
        registry
            .claim("src/auth/login.rs", "beta", "workspace-b", 3600)
            .unwrap();
    }

    #[test]
    fn claim_globs_match_components_and_recursive_subtrees() {
        assert!(matches_path("src/auth/**", b"src/auth/login/session.rs"));
        assert!(matches_path("src/*.rs", b"src/main.rs"));
        assert!(!matches_path("src/*.rs", b"src/nested/main.rs"));
        assert!(matches_path("src/file[0-9].rs", b"src/file7.rs"));
        assert!(!matches_path("src/file[!0-9].rs", b"src/file7.rs"));
        assert!(matches_path("config/?.env", b"config/a.env"));
    }
}
