//! The furrow daemon.
//!
//! One process holds one open repository per store and answers requests on a
//! unix socket, so a seal pays neither the store open nor the catalog replay
//! that a spawn per seal pays. The wire is one JSON object per line
//! ([`wire`]); the operations are the [`verbs`]; a [`router::Router`] gives
//! every store one writer thread while different stores run in parallel; the
//! [`server::Server`] owns the socket, the idle clock and shutdown.

pub mod handle;
pub mod router;
pub mod server;
pub mod signals;
pub mod verbs;
pub mod wire;

pub use server::{Config, Server, Stop};
pub use verbs::hook_label;
pub use wire::{Request, Response, Target};
