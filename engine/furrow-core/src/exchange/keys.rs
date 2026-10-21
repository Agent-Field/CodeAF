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
        cell_key: parse_secret(CELL_KEY_VAR, lookup(CELL_KEY_VAR))?,
        dedup: parse_secret(DEDUP_VAR, lookup(DEDUP_VAR))?,
    })
}

pub fn keys_from_env() -> anyhow::Result<Keys> {
    keys_from(|name| std::env::var(name).ok())
}

/// One secret in its 64-hex spelling. `name` is how the caller knows the value
/// (a variable or a JSON field); it is the only thing an error repeats.
pub fn parse_secret(name: &str, value: Option<String>) -> anyhow::Result<[u8; 32]> {
    let text = Zeroizing::new(value.with_context(|| format!("{name} is not set"))?);
    let bytes = Zeroizing::new(
        hex::decode(text.as_bytes())
            .ok()
            .with_context(|| format!("{name} must be 64 hexadecimal characters"))?,
    );
    <[u8; 32]>::try_from(bytes.as_slice())
        .ok()
        .with_context(|| format!("{name} must be 64 hexadecimal characters"))
}
