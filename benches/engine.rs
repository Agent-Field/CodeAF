use agit::chunker::ChunkStream;
use agit::content_class::ContentClass;
use agit::model::{
    Blob, ChunkRef, EntryKind, ObjectId, ObjectKind, SealQuality, Snapshot, SnapshotTrigger, Tree,
    TreeEntry,
};
use agit::store::ObjectStore;
use agit::AgitRepository;
use agit::{refs::RefLog, tree};
use anyhow::Context;
use serde::{Deserialize, Serialize};
use std::fs::{self, File};
use std::io::{Read, Write};
use std::path::{Path, PathBuf};
use std::process::Command;
use std::time::Instant;
use std::time::{SystemTime, UNIX_EPOCH};

#[derive(Clone, Copy)]
struct Profile {
    files: usize,
    changed_files: usize,
    chunk_bytes: u64,
    warm_bytes: u64,
    iterations: usize,
    history_snapshots: usize,
    lookup_iterations: usize,
    universes: usize,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
struct Sample {
    scenario: String,
    wall_ms: f64,
    cpu_ms: f64,
    max_rss_bytes: u64,
    units: u64,
    bytes: u64,
    tier: Option<String>,
    inner_ms: Option<f64>,
}

#[derive(Serialize)]
struct Summary {
    scenario: String,
    samples: usize,
    wall_median_ms: f64,
    wall_p95_ms: f64,
    cpu_p95_ms: f64,
    max_rss_bytes: u64,
    min_throughput_mib_s: Option<f64>,
    min_units_per_second: f64,
    units: u64,
    bytes: u64,
    tier: Option<String>,
    inner_p95_ms: Option<f64>,
}

#[derive(Clone, Copy)]
struct Usage {
    cpu_micros: u64,
    max_rss_bytes: u64,
}

fn main() -> anyhow::Result<()> {
    let arguments: Vec<String> = std::env::args().collect();
    if let Some(index) = arguments
        .iter()
        .position(|argument| argument == "--scenario")
    {
        let name = arguments.get(index + 1).context("missing scenario name")?;
        let sample = run_scenario(name, profile()?)?;
        println!("{}", serde_json::to_string(&sample)?);
        return Ok(());
    }

    let profile = profile()?;
    let executable = std::env::current_exe()?;
    let mut summaries = Vec::new();
    for scenario in [
        "chunk",
        "tree-diff",
        "refs",
        "cold-seal",
        "delta-seal",
        "fork",
        "universes",
        "gc",
    ] {
        let mut samples = Vec::new();
        for _ in 0..profile.iterations {
            let output = Command::new(&executable)
                .args(["--scenario", scenario])
                .output()
                .with_context(|| format!("run benchmark scenario {scenario}"))?;
            anyhow::ensure!(
                output.status.success(),
                "scenario {scenario} failed: {}",
                String::from_utf8_lossy(&output.stderr)
            );
            samples.push(serde_json::from_slice::<Sample>(&output.stdout)?);
        }
        summaries.push(summarize(samples));
    }

    println!(
        "{:<12} {:>10} {:>10} {:>10} {:>10} {:>11} {:>12} {:>12}",
        "scenario",
        "median ms",
        "p95 ms",
        "inner ms",
        "CPU p95",
        "peak RSS",
        "min units/s",
        "min MiB/s"
    );
    for summary in &summaries {
        println!(
            "{:<12} {:>10.2} {:>10.2} {:>10} {:>10.2} {:>8.1} MiB {:>12.0} {:>12}",
            summary.scenario,
            summary.wall_median_ms,
            summary.wall_p95_ms,
            summary
                .inner_p95_ms
                .map(|value| format!("{value:.2}"))
                .unwrap_or_else(|| "-".to_owned()),
            summary.cpu_p95_ms,
            summary.max_rss_bytes as f64 / 1024.0 / 1024.0,
            summary.min_units_per_second,
            summary
                .min_throughput_mib_s
                .map(|value| format!("{value:.1}"))
                .unwrap_or_else(|| "-".to_owned())
        );
    }
    println!("BENCHMARK_RESULT {}", serde_json::to_string(&summaries)?);
    if std::env::var_os("AGIT_BENCH_ENFORCE").is_some() {
        enforce(&summaries, profile)?;
    }
    Ok(())
}

fn profile() -> anyhow::Result<Profile> {
    let reference = std::env::var_os("AGIT_BENCH_PROFILE").as_deref()
        == Some(std::ffi::OsStr::new("reference"));
    let defaults = if reference {
        Profile {
            files: 1_000_000,
            changed_files: 100,
            chunk_bytes: 1024 * 1024 * 1024,
            warm_bytes: 64 * 1024 * 1024,
            iterations: 5,
            history_snapshots: 17_281,
            lookup_iterations: 10_000,
            universes: 10,
        }
    } else {
        Profile {
            files: 5_000,
            changed_files: 100,
            chunk_bytes: 128 * 1024 * 1024,
            warm_bytes: 32 * 1024 * 1024,
            iterations: 3,
            history_snapshots: 721,
            lookup_iterations: 1_000,
            universes: 5,
        }
    };
    Ok(Profile {
        files: environment_usize("AGIT_BENCH_FILES", defaults.files)?,
        changed_files: environment_usize("AGIT_BENCH_CHANGED_FILES", defaults.changed_files)?,
        chunk_bytes: environment_u64("AGIT_BENCH_CHUNK_BYTES", defaults.chunk_bytes)?,
        warm_bytes: environment_u64("AGIT_BENCH_WARM_BYTES", defaults.warm_bytes)?,
        iterations: environment_usize("AGIT_BENCH_ITERATIONS", defaults.iterations)?.max(1),
        history_snapshots: environment_usize(
            "AGIT_BENCH_HISTORY_SNAPSHOTS",
            defaults.history_snapshots,
        )?,
        lookup_iterations: environment_usize(
            "AGIT_BENCH_LOOKUP_ITERATIONS",
            defaults.lookup_iterations,
        )?,
        universes: environment_usize("AGIT_BENCH_UNIVERSES", defaults.universes)?.max(1),
    })
}

fn run_scenario(name: &str, profile: Profile) -> anyhow::Result<Sample> {
    match name {
        "chunk" => benchmark_chunk(profile),
        "tree-diff" => benchmark_tree_diff(profile),
        "refs" => benchmark_refs(profile),
        "cold-seal" => benchmark_cold_seal(profile),
        "delta-seal" => benchmark_delta_seal(profile),
        "fork" => benchmark_fork(profile),
        "universes" => benchmark_universes(profile),
        "gc" => benchmark_gc(profile),
        _ => anyhow::bail!("unknown benchmark scenario `{name}`"),
    }
}

fn benchmark_chunk(profile: Profile) -> anyhow::Result<Sample> {
    let before = usage()?;
    let started = Instant::now();
    let mut stream = ChunkStream::new(PatternReader::new(profile.chunk_bytes));
    let mut chunks = 0_u64;
    let mut bytes = 0_u64;
    while let Some(chunk) = stream.next_chunk()? {
        chunks += 1;
        bytes += chunk.len() as u64;
        std::hint::black_box(blake3::hash(&chunk));
    }
    sample("chunk", started, before, chunks, bytes, None)
}

fn benchmark_tree_diff(profile: Profile) -> anyhow::Result<Sample> {
    let root = tempfile::tempdir()?;
    let store = ObjectStore::open(root.path().join("store"))?;
    let entries = |changed: bool| {
        (0..profile.files)
            .map(|index| TreeEntry {
                name: format!("file-{index:09}.dat").into_bytes(),
                kind: EntryKind::File,
                target: Some(if changed && index == profile.files / 2 {
                    [0xff; 32]
                } else {
                    *blake3::hash(&index.to_le_bytes()).as_bytes()
                }),
                link_target: Vec::new(),
                mode: 0o100644,
                size: Workspace::FILE_BYTES as u64,
                mtime_secs: 0,
                mtime_nanos: 0,
                xattrs: None,
                class: ContentClass::Source,
            })
            .collect::<Vec<_>>()
    };
    let before_tree = tree::write(&store, entries(false))?;
    let after_tree = tree::write(&store, entries(true))?;
    let before = usage()?;
    let started = Instant::now();
    let mut total_changes = 0_u64;
    for _ in 0..profile.lookup_iterations {
        let mut changes = 0_u64;
        tree::diff_entries(&store, &before_tree, &after_tree, &mut |left, right| {
            if left != right {
                changes += 1;
            }
            Ok(())
        })?;
        total_changes += changes;
    }
    std::hint::black_box(total_changes);
    sample(
        "tree-diff",
        started,
        before,
        profile.lookup_iterations as u64,
        0,
        None,
    )
}

fn benchmark_refs(profile: Profile) -> anyhow::Result<Sample> {
    let root = tempfile::tempdir()?;
    let refs = RefLog::open(root.path(), "history")?;
    for index in 0..profile.history_snapshots {
        refs.append(
            *blake3::hash(&index.to_le_bytes()).as_bytes(),
            index as i64,
            None,
            SnapshotTrigger::Watcher,
        )?;
    }
    let before = usage()?;
    let started = Instant::now();
    let mut observed = 0_usize;
    for _ in 0..profile.lookup_iterations {
        observed += refs.recent(20)?.len();
    }
    std::hint::black_box(observed);
    sample(
        "refs",
        started,
        before,
        profile.lookup_iterations as u64,
        0,
        None,
    )
}

fn benchmark_cold_seal(profile: Profile) -> anyhow::Result<Sample> {
    let workspace = Workspace::new(profile, false)?;
    let before = usage()?;
    let started = Instant::now();
    let _ = AgitRepository::attach_and_snapshot(
        &workspace.repo,
        Some("benchmark cold seal".to_owned()),
        SnapshotTrigger::Manual,
    )?;
    sample(
        "cold-seal",
        started,
        before,
        profile.files as u64,
        profile.files as u64 * Workspace::FILE_BYTES as u64,
        None,
    )
}

fn benchmark_delta_seal(profile: Profile) -> anyhow::Result<Sample> {
    let workspace = Workspace::new(profile, false)?;
    let (mut repository, _) = AgitRepository::attach_and_snapshot(
        &workspace.repo,
        Some("benchmark baseline".to_owned()),
        SnapshotTrigger::Manual,
    )?;
    let changed = workspace.change_files(profile.changed_files.min(profile.files))?;
    let before = usage()?;
    let started = Instant::now();
    repository.snapshot_changed_paths(
        Some("benchmark delta".to_owned()),
        SnapshotTrigger::Watcher,
        &changed,
    )?;
    sample(
        "delta-seal",
        started,
        before,
        changed.len() as u64,
        changed.len() as u64 * Workspace::FILE_BYTES as u64,
        None,
    )
}

fn benchmark_fork(profile: Profile) -> anyhow::Result<Sample> {
    let workspace = Workspace::new(profile, true)?;
    let (mut repository, _) = AgitRepository::attach_and_snapshot(
        &workspace.repo,
        Some("benchmark fork baseline".to_owned()),
        SnapshotTrigger::Manual,
    )?;
    let destination = workspace.root.path().join("fork");
    let before = usage()?;
    let started = Instant::now();
    let result = repository.fork("benchmark", &destination)?;
    let mut sample = sample(
        "fork",
        started,
        before,
        result.files,
        result.logical_bytes,
        Some(result.tier.to_string()),
    )?;
    sample.inner_ms = Some(result.elapsed_ms as f64);
    Ok(sample)
}

fn benchmark_universes(profile: Profile) -> anyhow::Result<Sample> {
    let workspace = Workspace::new(profile, true)?;
    let (mut repository, _) = AgitRepository::attach_and_snapshot(
        &workspace.repo,
        Some("benchmark universe baseline".to_owned()),
        SnapshotTrigger::Manual,
    )?;
    let first_destination = workspace.root.path().join("universe-1");
    let first = repository.prepare_fork("universe-1", &first_destination)?;
    let before = usage()?;
    let started = Instant::now();
    let mut destinations = Vec::with_capacity(profile.universes);
    for offset in 0..profile.universes {
        let index = offset + 1;
        let mut plan = first.clone();
        plan.name = format!("universe-{index}");
        plan.destination = workspace.root.path().join(&plan.name);
        let summary = repository.materialize_fork(plan)?;
        destinations.push(summary.destination);
    }
    let mut children = Vec::with_capacity(profile.universes);
    for destination in &destinations {
        children.push(
            Command::new("true")
                .current_dir(destination)
                .spawn()
                .context("start benchmark universe")?,
        );
    }
    for mut child in children {
        anyhow::ensure!(child.wait()?.success(), "benchmark universe failed");
    }
    sample(
        "universes",
        started,
        before,
        profile.universes as u64,
        first.logical_bytes.saturating_mul(profile.universes as u64),
        Some(if cfg!(target_os = "linux") {
            "sibling-benchmark; namespace excluded".to_owned()
        } else {
            "sibling-directory".to_owned()
        }),
    )
}

fn benchmark_gc(profile: Profile) -> anyhow::Result<Sample> {
    let root = tempfile::tempdir()?;
    let mut store = ObjectStore::open(root.path().join("store"))?;
    store.ensure_workspace("history", b"/history")?;
    let now = SystemTime::now().duration_since(UNIX_EPOCH)?.as_secs() as i64;
    let history_window = 180 * 24 * 60 * 60_i64;
    let interval = (history_window / profile.history_snapshots.max(1) as i64).max(1);
    let mut parent: Option<ObjectId> = None;
    for index in 0..profile.history_snapshots {
        let byte = (index % 251) as u8;
        let payload = vec![byte; 4096];
        let chunk = store.put_bytes(ObjectKind::Chunk, &payload)?;
        let blob = store.put_struct(
            ObjectKind::Blob,
            &Blob {
                chunks: vec![ChunkRef {
                    id: chunk,
                    len: payload.len() as u32,
                }],
                total_len: payload.len() as u64,
            },
        )?;
        let sealed_at = now - history_window + index as i64 * interval;
        let tree = store.put_struct(
            ObjectKind::Tree,
            &Tree {
                entries: vec![TreeEntry {
                    name: format!("cache-{index:09}.log").into_bytes(),
                    kind: EntryKind::File,
                    target: Some(blob),
                    link_target: Vec::new(),
                    mode: 0o100644,
                    size: payload.len() as u64,
                    mtime_secs: sealed_at,
                    mtime_nanos: 0,
                    xattrs: None,
                    class: ContentClass::Scratch,
                }],
                pages: Vec::new(),
            },
        )?;
        let snapshot = Snapshot {
            root_tree: tree,
            parent,
            merge_parents: Vec::new(),
            sealed_at_secs: sealed_at,
            sealed_at_nanos: 0,
            quality: SealQuality::Quiescent,
            trigger: SnapshotTrigger::Watcher,
            label: None,
            sqlite_backups: Vec::new(),
            claims: Vec::new(),
            excluded_paths: Vec::new(),
        };
        let id = store.put_struct(ObjectKind::Snapshot, &snapshot)?;
        store.publish_snapshot("history", id, sealed_at, None, SnapshotTrigger::Watcher)?;
        parent = Some(id);
    }
    let before = usage()?;
    let started = Instant::now();
    let report = agit::gc::collect(&mut store, false)?;
    sample(
        "gc",
        started,
        before,
        report.published_snapshots,
        report.reclaimed_bytes,
        None,
    )
}

fn sample(
    scenario: &str,
    started: Instant,
    before: Usage,
    units: u64,
    bytes: u64,
    tier: Option<String>,
) -> anyhow::Result<Sample> {
    let wall_ms = started.elapsed().as_secs_f64() * 1000.0;
    let after = usage()?;
    Ok(Sample {
        scenario: scenario.to_owned(),
        wall_ms,
        cpu_ms: after.cpu_micros.saturating_sub(before.cpu_micros) as f64 / 1000.0,
        max_rss_bytes: after.max_rss_bytes,
        units,
        bytes,
        tier,
        inner_ms: None,
    })
}

fn summarize(mut samples: Vec<Sample>) -> Summary {
    samples.sort_by(|left, right| left.wall_ms.total_cmp(&right.wall_ms));
    let median = samples[samples.len() / 2].wall_ms;
    let p95_index = ((samples.len() as f64 * 0.95).ceil() as usize)
        .saturating_sub(1)
        .min(samples.len() - 1);
    let p95 = samples[p95_index].wall_ms;
    let mut cpu: Vec<_> = samples.iter().map(|sample| sample.cpu_ms).collect();
    cpu.sort_by(f64::total_cmp);
    let cpu_p95 = cpu[p95_index];
    let max_rss_bytes = samples
        .iter()
        .map(|sample| sample.max_rss_bytes)
        .max()
        .unwrap_or(0);
    let inner_p95_ms = if samples.iter().all(|sample| sample.inner_ms.is_some()) {
        let mut values: Vec<_> = samples
            .iter()
            .filter_map(|sample| sample.inner_ms)
            .collect();
        values.sort_by(f64::total_cmp);
        Some(values[p95_index])
    } else {
        None
    };
    let min_throughput_mib_s =
        if matches!(samples[0].scenario.as_str(), "chunk" | "fork" | "universes") {
            samples
                .iter()
                .filter(|sample| sample.bytes > 0 && sample.wall_ms > 0.0)
                .map(|sample| sample.bytes as f64 / 1024.0 / 1024.0 / (sample.wall_ms / 1000.0))
                .min_by(f64::total_cmp)
        } else {
            None
        };
    let min_units_per_second = samples
        .iter()
        .map(|sample| sample.units as f64 / (sample.wall_ms / 1000.0))
        .min_by(f64::total_cmp)
        .unwrap_or(0.0);
    Summary {
        scenario: samples[0].scenario.clone(),
        samples: samples.len(),
        wall_median_ms: median,
        wall_p95_ms: p95,
        cpu_p95_ms: cpu_p95,
        max_rss_bytes,
        min_throughput_mib_s,
        min_units_per_second,
        units: samples[0].units,
        bytes: samples[0].bytes,
        tier: samples.iter().find_map(|sample| sample.tier.clone()),
        inner_p95_ms,
    }
}

fn enforce(summaries: &[Summary], profile: Profile) -> anyhow::Result<()> {
    let find = |name: &str| {
        summaries
            .iter()
            .find(|summary| summary.scenario == name)
            .with_context(|| format!("missing benchmark {name}"))
    };
    let chunk = find("chunk")?;
    anyhow::ensure!(
        chunk.min_throughput_mib_s.unwrap_or(0.0) >= 50.0,
        "chunk throughput fell below 50 MiB/s"
    );
    let delta = find("delta-seal")?;
    anyhow::ensure!(delta.wall_p95_ms <= 2_000.0, "delta seal exceeded 2 s");
    let tree_diff = find("tree-diff")?;
    anyhow::ensure!(
        tree_diff.wall_p95_ms / tree_diff.units.max(1) as f64 <= 100.0,
        "tree diff exceeded 100 ms per lookup"
    );
    let refs = find("refs")?;
    anyhow::ensure!(
        refs.wall_p95_ms / refs.units.max(1) as f64 <= 5.0,
        "reference lookup exceeded 5 ms per read"
    );
    let fork = find("fork")?;
    let fork_limit = if fork.tier.as_deref() == Some("native-cow") {
        2_000.0 * (profile.files.max(100_000) as f64 / 100_000.0)
    } else {
        15_000.0
    };
    anyhow::ensure!(fork.wall_p95_ms <= fork_limit, "fork latency regressed");
    let universes = find("universes")?;
    anyhow::ensure!(
        universes.wall_p95_ms <= 30_000.0,
        "multi-universe startup exceeded 30 s"
    );
    anyhow::ensure!(find("gc")?.wall_p95_ms <= 10_000.0, "GC exceeded 10 s");
    for summary in summaries {
        anyhow::ensure!(
            summary.max_rss_bytes <= 512 * 1024 * 1024,
            "{} exceeded the 512 MiB quick-gate RSS ceiling",
            summary.scenario
        );
    }
    Ok(())
}

struct Workspace {
    root: tempfile::TempDir,
    repo: PathBuf,
}

impl Workspace {
    const FILE_BYTES: usize = 128;

    fn new(profile: Profile, warm: bool) -> anyhow::Result<Self> {
        let root = tempfile::tempdir()?;
        let repo = root.path().join("repo");
        fs::create_dir(&repo)?;
        git(&repo, &["init", "-q", "-b", "main"])?;
        git(&repo, &["config", "user.email", "bench@example.com"])?;
        git(&repo, &["config", "user.name", "Benchmark"])?;
        fs::write(repo.join(".gitignore"), b"node_modules/\n")?;
        git(&repo, &["add", ".gitignore"])?;
        git(&repo, &["commit", "-q", "-m", "benchmark baseline"])?;
        write_files(&repo, profile.files)?;
        if warm {
            let cache = repo.join("node_modules/runtime/cache.bin");
            fs::create_dir_all(cache.parent().unwrap())?;
            let mut file = File::create(cache)?;
            let block = vec![0x5a; 1024 * 1024];
            let mut remaining = profile.warm_bytes;
            while remaining > 0 {
                let take = remaining.min(block.len() as u64) as usize;
                file.write_all(&block[..take])?;
                remaining -= take as u64;
            }
        }
        std::env::set_var("AGIT_DATA_DIR", root.path().join("data"));
        std::env::set_var("AGIT_NO_DAEMON", "1");
        Ok(Self { root, repo })
    }

    fn change_files(&self, count: usize) -> anyhow::Result<Vec<PathBuf>> {
        let mut changed = Vec::with_capacity(count);
        for index in 0..count {
            let path = file_path(&self.repo, index);
            fs::write(&path, vec![0xa5; Self::FILE_BYTES])?;
            changed.push(path);
        }
        Ok(changed)
    }
}

fn write_files(root: &Path, count: usize) -> anyhow::Result<()> {
    let bytes = vec![0x3c; Workspace::FILE_BYTES];
    for index in 0..count {
        let path = file_path(root, index);
        if index % 1_000 == 0 {
            fs::create_dir_all(path.parent().unwrap())?;
        }
        fs::write(path, &bytes)?;
    }
    Ok(())
}

fn file_path(root: &Path, index: usize) -> PathBuf {
    root.join(format!("files/{:06}/file-{index:09}.dat", index / 1_000))
}

fn git(repo: &Path, arguments: &[&str]) -> anyhow::Result<()> {
    let status = Command::new("git")
        .arg("-C")
        .arg(repo)
        .args(arguments)
        .status()?;
    anyhow::ensure!(status.success(), "git command failed");
    Ok(())
}

struct PatternReader {
    remaining: u64,
    offset: u64,
}

impl PatternReader {
    fn new(remaining: u64) -> Self {
        Self {
            remaining,
            offset: 0,
        }
    }
}

impl Read for PatternReader {
    fn read(&mut self, buffer: &mut [u8]) -> std::io::Result<usize> {
        let take = self.remaining.min(buffer.len() as u64) as usize;
        for (index, byte) in buffer[..take].iter_mut().enumerate() {
            *byte = ((self.offset + index as u64).wrapping_mul(31) % 251) as u8;
        }
        self.offset += take as u64;
        self.remaining -= take as u64;
        Ok(take)
    }
}

fn usage() -> anyhow::Result<Usage> {
    let mut usage = std::mem::MaybeUninit::<libc::rusage>::zeroed();
    let result = unsafe { libc::getrusage(libc::RUSAGE_SELF, usage.as_mut_ptr()) };
    anyhow::ensure!(result == 0, "getrusage failed");
    let usage = unsafe { usage.assume_init() };
    let micros = |time: libc::timeval| {
        (time.tv_sec as u64)
            .saturating_mul(1_000_000)
            .saturating_add(time.tv_usec as u64)
    };
    #[cfg(target_os = "macos")]
    let max_rss_bytes = usage.ru_maxrss as u64;
    #[cfg(not(target_os = "macos"))]
    let max_rss_bytes = (usage.ru_maxrss as u64).saturating_mul(1024);
    Ok(Usage {
        cpu_micros: micros(usage.ru_utime).saturating_add(micros(usage.ru_stime)),
        max_rss_bytes,
    })
}

fn environment_usize(name: &str, default: usize) -> anyhow::Result<usize> {
    std::env::var(name).map_or(Ok(default), |value| {
        value
            .parse()
            .with_context(|| format!("parse environment variable {name}"))
    })
}

fn environment_u64(name: &str, default: u64) -> anyhow::Result<u64> {
    std::env::var(name).map_or(Ok(default), |value| {
        value
            .parse()
            .with_context(|| format!("parse environment variable {name}"))
    })
}
