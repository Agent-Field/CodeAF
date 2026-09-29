use crate::model::SnapshotTrigger;
use crate::repository::FurrowRepository;
use crate::self_write::{self, FilterResult};
use anyhow::Context;
use notify::{Event, RecommendedWatcher, RecursiveMode, Watcher};
use std::collections::BTreeSet;
use std::path::PathBuf;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{mpsc, Arc};
use std::time::Duration;
use std::time::Instant;

const EVENT_QUEUE: usize = 4096;

pub fn run(mut repository: FurrowRepository, debounce: Duration) -> anyhow::Result<()> {
    let (sender, receiver) = mpsc::sync_channel::<notify::Result<Event>>(EVENT_QUEUE);
    let overflow = Arc::new(AtomicBool::new(false));
    let callback_overflow = Arc::clone(&overflow);
    let mut watcher: RecommendedWatcher = notify::recommended_watcher(move |event| {
        if sender.try_send(event).is_err() {
            callback_overflow.store(true, Ordering::Release);
        }
    })?;
    watcher.watch(repository.root(), RecursiveMode::Recursive)?;
    eprintln!(
        "furrow: continuously protecting {} (Ctrl-C to stop)",
        repository.root().display()
    );

    loop {
        let first = receiver.recv().context("filesystem watcher stopped")?;
        let mut changed_paths = BTreeSet::new();
        record_event(first, &mut changed_paths, &overflow);

        loop {
            match receiver.recv_timeout(debounce) {
                Ok(event) => record_event(event, &mut changed_paths, &overflow),
                Err(mpsc::RecvTimeoutError::Timeout) => break,
                Err(mpsc::RecvTimeoutError::Disconnected) => {
                    anyhow::bail!("filesystem watcher disconnected")
                }
            }
        }

        let overflowed = overflow.swap(false, Ordering::AcqRel);
        while self_write::filter_events(
            &repository.workspace_data_dir(),
            repository.root(),
            &mut changed_paths,
        )? == FilterResult::ApplyActive
        {
            std::thread::sleep(Duration::from_millis(20));
        }
        let changed_paths = match repository.filter_capture_paths(changed_paths) {
            Ok(paths) => paths,
            Err(error) => {
                eprintln!("furrow: automatic snapshot deferred: {error:#}");
                continue;
            }
        };
        if !overflowed && changed_paths.is_empty() {
            continue;
        }
        let label = if overflowed {
            "automatic snapshot after watcher overflow/rescan"
        } else {
            "automatic snapshot after write quiescence"
        };
        let seal_started = Instant::now();
        let result = if overflowed || changed_paths.is_empty() {
            repository.snapshot(Some(label.to_owned()), SnapshotTrigger::Watcher)
        } else {
            repository.snapshot_changed_paths(
                Some(label.to_owned()),
                SnapshotTrigger::Watcher,
                &changed_paths,
            )
        };
        match result {
            Ok(id) => eprintln!(
                "furrow: sealed {} in {:.3}s",
                &hex::encode(id)[..12],
                seal_started.elapsed().as_secs_f64()
            ),
            Err(error) => eprintln!("furrow: automatic snapshot deferred: {error:#}"),
        }
    }
}

fn record_event(
    event: notify::Result<Event>,
    changed_paths: &mut BTreeSet<PathBuf>,
    overflow: &AtomicBool,
) {
    match event {
        Ok(event) => changed_paths.extend(event.paths),
        Err(error) => {
            eprintln!("furrow: watcher warning: {error}");
            overflow.store(true, Ordering::Release);
        }
    }
}
