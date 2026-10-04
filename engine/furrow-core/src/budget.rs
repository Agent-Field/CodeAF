use anyhow::Context;
use serde::{Deserialize, Serialize};
use std::fs::{self, File, OpenOptions};
use std::io::Write;
use std::path::Path;
use std::time::{SystemTime, UNIX_EPOCH};

const CONFIG_FILE: &str = "budget.json";
const PRESSURE_FILE: &str = "budget-pressure.json";
const DEFAULT_MAX_STORE_BYTES: u64 = 20 * 1024 * 1024 * 1024;
const DEFAULT_RESERVED_FREE_BYTES: u64 = 512 * 1024 * 1024;
const RETRY_GROWTH_BYTES: u64 = 64 * 1024 * 1024;
const RETRY_AFTER_SECONDS: i64 = 24 * 60 * 60;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub struct BudgetConfig {
    pub max_store_bytes: u64,
    pub reserved_free_bytes: u64,
}

#[derive(Debug, Clone, Copy, Serialize)]
pub struct BudgetStatus {
    pub max_store_bytes: u64,
    pub reserved_free_bytes: u64,
    pub physical_bytes: u64,
    pub available_bytes: u64,
    pub over_store_bytes: u64,
    pub below_reserved_bytes: u64,
    pub satisfied: bool,
}

#[derive(Serialize, Deserialize)]
struct ConfigEnvelope {
    config: BudgetConfig,
    checksum: String,
}

#[derive(Serialize, Deserialize)]
struct PressureAttempt {
    physical_bytes: u64,
    attempted_at: i64,
}

impl Default for BudgetConfig {
    fn default() -> Self {
        Self {
            max_store_bytes: DEFAULT_MAX_STORE_BYTES,
            reserved_free_bytes: DEFAULT_RESERVED_FREE_BYTES,
        }
    }
}

impl BudgetStatus {
    pub fn pressured(self) -> bool {
        !self.satisfied
    }
}

pub fn load(store_root: &Path) -> anyhow::Result<BudgetConfig> {
    let path = store_root.join(CONFIG_FILE);
    if !path.exists() {
        return Ok(BudgetConfig::default());
    }
    let envelope: ConfigEnvelope = serde_json::from_slice(&fs::read(&path)?)
        .with_context(|| format!("read budget configuration {}", path.display()))?;
    let payload = serde_json::to_vec(&envelope.config)?;
    anyhow::ensure!(
        envelope.checksum == blake3::hash(&payload).to_hex().as_str(),
        "budget configuration checksum mismatch"
    );
    validate(envelope.config)?;
    Ok(envelope.config)
}

pub fn save(store_root: &Path, config: BudgetConfig) -> anyhow::Result<()> {
    validate(config)?;
    let payload = serde_json::to_vec(&config)?;
    let envelope = ConfigEnvelope {
        config,
        checksum: blake3::hash(&payload).to_hex().to_string(),
    };
    atomic_write(
        store_root,
        CONFIG_FILE,
        &serde_json::to_vec_pretty(&envelope)?,
    )?;
    clear_pressure(store_root)
}

pub fn status(
    store_root: &Path,
    config: BudgetConfig,
    physical_bytes: u64,
) -> anyhow::Result<BudgetStatus> {
    let available_bytes = fs2::available_space(store_root)?;
    let over_store_bytes = physical_bytes.saturating_sub(config.max_store_bytes);
    let below_reserved_bytes = config.reserved_free_bytes.saturating_sub(available_bytes);
    Ok(BudgetStatus {
        max_store_bytes: config.max_store_bytes,
        reserved_free_bytes: config.reserved_free_bytes,
        physical_bytes,
        available_bytes,
        over_store_bytes,
        below_reserved_bytes,
        satisfied: over_store_bytes == 0 && below_reserved_bytes == 0,
    })
}

pub fn should_retry(store_root: &Path, physical_bytes: u64) -> anyhow::Result<bool> {
    let path = store_root.join(PRESSURE_FILE);
    if !path.exists() {
        return Ok(true);
    }
    let attempt: PressureAttempt = serde_json::from_slice(&fs::read(path)?)?;
    let now = now()?;
    Ok(
        physical_bytes >= attempt.physical_bytes.saturating_add(RETRY_GROWTH_BYTES)
            || now.saturating_sub(attempt.attempted_at) >= RETRY_AFTER_SECONDS,
    )
}

pub fn record_attempt(
    store_root: &Path,
    physical_bytes: u64,
    satisfied: bool,
) -> anyhow::Result<()> {
    if satisfied {
        return clear_pressure(store_root);
    }
    atomic_write(
        store_root,
        PRESSURE_FILE,
        &serde_json::to_vec(&PressureAttempt {
            physical_bytes,
            attempted_at: now()?,
        })?,
    )
}

fn clear_pressure(store_root: &Path) -> anyhow::Result<()> {
    let path = store_root.join(PRESSURE_FILE);
    if path.exists() {
        fs::remove_file(path)?;
        File::open(store_root)?.sync_all()?;
    }
    Ok(())
}

fn validate(config: BudgetConfig) -> anyhow::Result<()> {
    anyhow::ensure!(config.max_store_bytes > 0, "store budget must be positive");
    Ok(())
}

fn atomic_write(store_root: &Path, name: &str, bytes: &[u8]) -> anyhow::Result<()> {
    let path = store_root.join(name);
    let temporary = store_root.join(format!(".{name}.tmp"));
    let mut file = OpenOptions::new()
        .create(true)
        .truncate(true)
        .write(true)
        .open(&temporary)?;
    file.write_all(bytes)?;
    file.sync_all()?;
    fs::rename(temporary, path)?;
    File::open(store_root)?.sync_all()?;
    Ok(())
}

fn now() -> anyhow::Result<i64> {
    Ok(SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .context("system clock is before Unix epoch")?
        .as_secs() as i64)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn configuration_round_trips_and_corruption_fails_closed() {
        let temporary = tempfile::tempdir().unwrap();
        assert_eq!(load(temporary.path()).unwrap(), BudgetConfig::default());
        let configured = BudgetConfig {
            max_store_bytes: 123_456,
            reserved_free_bytes: 654_321,
        };
        save(temporary.path(), configured).unwrap();
        assert_eq!(load(temporary.path()).unwrap(), configured);
        fs::write(temporary.path().join(CONFIG_FILE), b"{}").unwrap();
        assert!(load(temporary.path()).is_err());
    }

    #[test]
    fn pressure_backoff_retries_after_meaningful_growth() {
        let temporary = tempfile::tempdir().unwrap();
        assert!(should_retry(temporary.path(), 100).unwrap());
        record_attempt(temporary.path(), 100, false).unwrap();
        assert!(!should_retry(temporary.path(), 101).unwrap());
        assert!(should_retry(temporary.path(), 100 + RETRY_GROWTH_BYTES).unwrap());
        record_attempt(temporary.path(), 100, true).unwrap();
        assert!(should_retry(temporary.path(), 100).unwrap());
    }
}
