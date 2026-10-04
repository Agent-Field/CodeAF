//! Crash points for the crash-safety tests. A build with the `fault-injection`
//! feature aborts the process, without unwinding, at the point named by
//! `FURROW_FAULT`; every other build compiles the points away.

#[cfg(feature = "fault-injection")]
pub fn point(name: &str) {
    if std::env::var("FURROW_FAULT").is_ok_and(|armed| armed == name) {
        std::process::abort();
    }
}

#[cfg(not(feature = "fault-injection"))]
#[inline(always)]
pub fn point(_name: &str) {}
