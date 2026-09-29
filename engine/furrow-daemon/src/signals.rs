//! Termination signals as a flag the serving loop polls.

use std::sync::atomic::{AtomicBool, Ordering};

static RECEIVED: AtomicBool = AtomicBool::new(false);

extern "C" fn note(_: libc::c_int) {
    RECEIVED.store(true, Ordering::SeqCst);
}

/// From now on SIGTERM and SIGINT set [`received`] instead of killing the
/// process, so the daemon can finish what it is doing.
pub fn install() {
    let handler = note as extern "C" fn(libc::c_int) as libc::sighandler_t;
    for signal in [libc::SIGTERM, libc::SIGINT] {
        // SAFETY: the handler only stores to an atomic.
        unsafe { libc::signal(signal, handler) };
    }
}

pub fn received() -> bool {
    RECEIVED.load(Ordering::SeqCst)
}
