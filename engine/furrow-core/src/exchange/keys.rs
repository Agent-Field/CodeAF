//! Where the identity's secrets come from: the process environment only.

use crate::sealer::Keys;
use anyhow::Context;
use zeroize::Zeroizing;

pub const CELL_KEY_VAR: &str = "FURROW_CELL_KEY";
pub const DEDUP_VAR: &str = "FURROW_DEDUP_SECRET";

/// Reads both secrets through `lookup`, so a caller decides where the
/// environment is. Errors name the variable and never repeat its value.
pub fn keys_from(lookup: impl Fn(&str) -> Option<String>) -> anyhow::Result<Keys> {
    Ok(Keys {
        cell_key: secret(&lookup, CELL_KEY_VAR)?,
        dedup: secret(&lookup, DEDUP_VAR)?,
    })
}

pub fn keys_from_env() -> anyhow::Result<Keys> {
    keys_from(|name| std::env::var(name).ok())
}

fn secret(lookup: &impl Fn(&str) -> Option<String>, name: &str) -> anyhow::Result<[u8; 32]> {
    let text = Zeroizing::new(lookup(name).with_context(|| format!("{name} is not set"))?);
    let bytes = Zeroizing::new(
        hex::decode(text.as_bytes())
            .ok()
            .with_context(|| format!("{name} must be 64 hexadecimal characters"))?,
    );
    <[u8; 32]>::try_from(bytes.as_slice())
        .ok()
        .with_context(|| format!("{name} must be 64 hexadecimal characters"))
}
