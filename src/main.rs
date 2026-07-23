use anyhow::Context;
use clap::{Parser, Subcommand};
use furrow::model::{id_hex, SnapshotTrigger};
use furrow::{FurrowRepository, SyncDisposition, SyncFollowOutcome};
use std::ffi::OsString;
use std::fs;
use std::io::{self, IsTerminal, Read, Write};
use std::os::unix::fs::PermissionsExt;
use std::path::PathBuf;

#[derive(Parser)]
#[command(
    name = "furrow",
    version,
    about = "Undo everything between Git commits"
)]
struct Cli {
    #[arg(long, global = true, default_value = ".")]
    repo: PathBuf,
    #[arg(long, global = true)]
    json: bool,
    #[command(subcommand)]
    command: Command,
}

#[derive(Subcommand)]
enum Command {
    /// Project first-capture cost after policy and existing-store deduplication.
    Estimate,
    /// Attach a repository and create its first complete snapshot.
    Watch {
        /// Keep running and seal after filesystem write quiescence.
        #[arg(long)]
        foreground: bool,
        /// Attach and snapshot without leaving a background watcher running.
        #[arg(long, conflicts_with = "foreground")]
        no_daemon: bool,
        #[arg(long, default_value_t = 500)]
        debounce_ms: u64,
    },
    #[command(name = "__daemon", hide = true)]
    Daemon {
        #[arg(long, default_value_t = 500)]
        debounce_ms: u64,
    },
    #[command(name = "__remote", hide = true)]
    RemoteHelper { namespace: String },
    #[command(name = "__namespace-probe", hide = true)]
    NamespaceProbe,
    #[command(name = "__exec-namespace", hide = true)]
    ExecNamespace {
        #[arg(long)]
        source: PathBuf,
        #[arg(long)]
        target: PathBuf,
        #[arg(last = true, required = true)]
        command: Vec<OsString>,
    },
    /// Create a complete labeled snapshot now.
    Snap {
        #[arg(short, long)]
        message: Option<String>,
    },
    /// List recent workspace snapshots.
    Timeline {
        #[arg(short, long, default_value_t = 20)]
        limit: usize,
    },
    /// Keep a snapshot exact across timeline thinning and garbage collection.
    Pin { snapshot: String },
    /// Return a pinned snapshot to the normal retention policy.
    Unpin { snapshot: String },
    /// Inspect path-level changes in a fork or since a snapshot.
    Diff {
        /// Fork name, full snapshot ID, or snapshot prefix.
        target: String,
    },
    /// Preview or restore a previous workspace snapshot.
    Rewind {
        snapshot: String,
        #[arg(long = "paths")]
        paths: Vec<PathBuf>,
        #[arg(long)]
        dry_run: bool,
        #[arg(long)]
        yes: bool,
        /// Restore SQLite databases from their logically consistent backup image.
        #[arg(long)]
        sqlite_consistent: bool,
    },
    /// Show workspace protection, store health, and optional fidelity guarantees.
    Status {
        /// Enumerate what rewind preserves exactly, approximately, or not at all.
        #[arg(long)]
        fidelity: bool,
    },
    /// Open the local Mission Control workspace dashboard.
    Ui {
        /// Loopback port; 0 selects an available ephemeral port.
        #[arg(long, default_value_t = 0)]
        port: u16,
        /// Print the URL without launching a browser.
        #[arg(long)]
        no_open: bool,
        /// Fixed verification command allowed for UI merge actions.
        #[arg(long)]
        merge_check: Option<String>,
    },
    /// Create an isolated full-state workspace, optionally running a command inside it.
    Fork {
        /// Stable name used by `furrow forks` and as the default directory name.
        name: Option<String>,
        /// Destination directory. Defaults to <repo>.furrow-forks/<name> beside the repository.
        #[arg(long)]
        destination: Option<PathBuf>,
        /// Materialize from this stored snapshot instead of the live workspace;
        /// it also becomes the fork's merge base.
        #[arg(long)]
        at: Option<String>,
        /// Command to run inside the completed fork, supplied after `--`.
        #[arg(last = true)]
        command: Vec<OsString>,
    },
    /// List full-state workspace forks created from this repository.
    Forks,
    /// Stream durable machine-readable workspace events as NDJSON.
    #[command(hide = true)]
    Events {
        /// Exclusive event cursor returned by an earlier line.
        #[arg(long)]
        after: Option<String>,
        #[arg(long, default_value_t = 100)]
        limit: usize,
        #[arg(long)]
        follow: bool,
        #[arg(long, default_value_t = 250)]
        interval_ms: u64,
    },
    /// Stream newly sealed snapshots from a sibling fork.
    WatchFork {
        name: String,
        #[arg(long)]
        after: Option<String>,
        #[arg(long)]
        once: bool,
        #[arg(long, default_value_t = 250)]
        interval_ms: u64,
    },
    /// Remove a completed fork and detach its timeline.
    #[command(name = "fork-rm")]
    ForkRemove {
        name: String,
        /// Forget the fork record but leave its directory and timeline intact.
        #[arg(long)]
        keep_files: bool,
    },
    /// Claim a path glob advisory lease for an agent or workspace.
    Claim {
        pattern: String,
        #[arg(long)]
        owner: Option<String>,
        #[arg(long, default_value_t = 3600)]
        ttl_seconds: u64,
    },
    /// List active advisory path claims in this workspace family.
    Claims,
    /// Release a claim by ID or exact pattern.
    Release {
        claim: String,
        #[arg(long)]
        owner: Option<String>,
    },
    /// Read and write the eagerly replicated coordination directory.
    Coord {
        #[command(subcommand)]
        command: CoordCommand,
    },
    /// Install or execute vendor-neutral agent turn-boundary hooks.
    Hook {
        #[command(subcommand)]
        command: HookCommand,
    },
    /// Run any agent or command inside a new isolated full-state fork.
    Run {
        name: String,
        #[arg(long)]
        destination: Option<PathBuf>,
        /// Materialize from this stored snapshot instead of the live workspace;
        /// it also becomes the fork's merge base.
        #[arg(long)]
        at: Option<String>,
        #[arg(last = true, required = true)]
        command: Vec<OsString>,
    },
    /// Run one or more commands in isolated, whole-workspace universes.
    Exec {
        /// Stable fork name. May only be used for a single universe.
        #[arg(long)]
        fork: Option<String>,
        /// Number of universes to create and run concurrently.
        #[arg(short = 'n', default_value_t = 1)]
        count: usize,
        /// Materialize every universe from this stored snapshot instead of
        /// the live workspace; it also becomes each fork's merge base.
        #[arg(long)]
        at: Option<String>,
        /// Print the platform driver, paths, ports, and fork costs without executing.
        #[arg(long)]
        plan: bool,
        #[arg(last = true)]
        command: Vec<OsString>,
    },
    /// Run a command with exact before/after restore points in the current workspace.
    #[command(name = "try")]
    Attempt {
        /// Human label recorded on both restore points.
        #[arg(short, long)]
        message: Option<String>,
        #[arg(last = true, required = true)]
        command: Vec<OsString>,
    },
    /// Find and reversibly remove dependency and build caches.
    Shrink {
        /// Remove the reported candidates after sealing a complete restore point.
        #[arg(long)]
        yes: bool,
        /// Include an additional repository-relative path.
        #[arg(long = "path")]
        paths: Vec<PathBuf>,
    },
    /// Find the first retained snapshot where a check starts failing.
    Bisect {
        /// Known passing snapshot; must be paired with --bad.
        #[arg(long)]
        good: Option<String>,
        /// Known failing snapshot; must be paired with --good.
        #[arg(long)]
        bad: Option<String>,
        /// Maximum number of recent snapshots in the search window.
        #[arg(long, default_value_t = 100)]
        limit: usize,
        #[arg(last = true, required = true)]
        command: Vec<OsString>,
    },
    /// Three-way merge a fork, or a stored snapshot, after verifying the
    /// result in a scratch workspace.
    Merge {
        /// Fork name to merge from. Mutually exclusive with --snapshot.
        #[arg(conflicts_with = "snapshot", required_unless_present = "snapshot")]
        fork: Option<String>,
        /// Merge from a stored snapshot ID (for example one fetched with
        /// `furrow sync --pull`) instead of a fork.
        #[arg(long, conflicts_with = "fork")]
        snapshot: Option<String>,
        /// Explicit merge base snapshot ID. Only valid with --snapshot;
        /// required when its ancestry does not unambiguously resolve one
        /// against this workspace's own lineage.
        #[arg(long, requires = "snapshot")]
        base: Option<String>,
        /// Project verification command executed through /bin/sh in the scratch workspace.
        #[arg(long)]
        check: Option<String>,
        /// Plan and report changes/conflicts without materializing or checking them.
        #[arg(long)]
        dry_run: bool,
    },
    /// Stop watching this repository.
    Forget {
        #[arg(long)]
        purge: bool,
    },
    /// Reclaim objects unreachable from every retained workspace timeline.
    Gc {
        /// Report what would be reclaimed without changing the store.
        #[arg(long)]
        dry_run: bool,
    },
    /// Inspect or configure the global content-store disk budget.
    Budget {
        /// Maximum physical pack size, for example 20GiB or 500MiB.
        #[arg(long)]
        max: Option<String>,
        /// Minimum filesystem free space to preserve.
        #[arg(long = "reserve-free")]
        reserve_free: Option<String>,
    },
    /// Pair with an encrypted directory or direct ssh://user@host remote.
    Pair {
        remote: PathBuf,
        /// Shared remote workspace name. Use the same value on every machine.
        #[arg(long)]
        name: Option<String>,
        /// Existing 64-character pairing key from another machine.
        #[arg(long)]
        key: Option<String>,
    },
    /// Configure the encrypted transport for this workspace.
    Remote {
        #[command(subcommand)]
        command: RemoteCommand,
    },
    /// Materialize a complete workspace from an encrypted network remote.
    Clone {
        /// Workspace URL printed by `furrow remote add`.
        remote: String,
        /// New destination directory. Defaults to the remote workspace name.
        destination: Option<PathBuf>,
        /// Existing 64-character recovery key.
        #[arg(long, env = "FURROW_RECOVERY_KEY")]
        key: String,
        /// Attach without starting the background watcher.
        #[arg(long)]
        no_watch: bool,
    },
    /// Transfer complete encrypted working-state snapshots through the paired remote.
    Sync {
        #[arg(long, conflicts_with_all = ["pull", "follow"])]
        push: bool,
        #[arg(long, conflicts_with_all = ["push", "follow"])]
        pull: bool,
        /// Continuously reconcile sealed local state and the encrypted remote.
        #[arg(long, conflicts_with_all = ["push", "pull", "takeover", "bootstrap"])]
        follow: bool,
        /// Bucket polling interval and live-session subscription fallback.
        #[arg(long, default_value_t = 5, requires = "follow")]
        poll_seconds: u64,
        /// Explicitly take the single-writer lease from another machine.
        #[arg(long, requires = "push")]
        takeover: bool,
        /// Replace this machine's initial state with the remote state, reversibly.
        #[arg(long, requires = "pull")]
        bootstrap: bool,
        /// Publish or follow an independent named ref instead of the
        /// default HEAD, so concurrent publishers to the same remote never
        /// contend. Omit to use today's single, well-known HEAD.
        #[arg(long = "ref")]
        ref_name: Option<String>,
        /// Print transport phase timings to stderr.
        #[arg(long)]
        timings: bool,
    },
    /// Serve furrow tools to coding agents over MCP stdio.
    Mcp,
}

#[derive(Subcommand)]
enum RemoteCommand {
    /// Add an encrypted directory, SSH, or S3-compatible remote.
    Add {
        remote: PathBuf,
        /// Shared workspace name. Defaults to the source folder name.
        #[arg(long)]
        name: Option<String>,
        /// Existing 64-character recovery key.
        #[arg(long)]
        key: Option<String>,
    },
}

#[derive(Subcommand)]
enum CoordCommand {
    /// Write a coordination value and propagate it to live sibling forks.
    Write {
        path: PathBuf,
        #[arg(long, conflicts_with = "file")]
        value: Option<String>,
        #[arg(long, conflicts_with = "value")]
        file: Option<PathBuf>,
        #[arg(long)]
        owner: Option<String>,
    },
    /// Read a coordination value from this workspace.
    Read { path: PathBuf },
    /// List coordination values without reading their contents.
    List,
    /// Remove a coordination value and propagate a durable tombstone.
    Remove {
        path: PathBuf,
        #[arg(long)]
        owner: Option<String>,
    },
}

#[derive(Subcommand)]
enum HookCommand {
    /// Install executable pre-turn, post-tool, and turn-end adapters.
    Install,
    /// Seal immediately before an agent turn begins.
    PreTurn {
        #[arg(long)]
        agent: Option<String>,
        #[arg(long)]
        turn: Option<String>,
    },
    /// Seal after an agent tool invocation completes.
    PostTool {
        #[arg(long)]
        agent: Option<String>,
        #[arg(long)]
        turn: Option<String>,
        #[arg(long)]
        tool: Option<String>,
    },
    /// Seal when an agent turn ends.
    TurnEnd {
        #[arg(long)]
        agent: Option<String>,
        #[arg(long)]
        turn: Option<String>,
    },
}

fn main() -> anyhow::Result<()> {
    let cli = Cli::parse();
    match cli.command {
        Command::Estimate => {
            let estimate = FurrowRepository::estimate(&cli.repo)?;
            if cli.json {
                println!("{}", serde_json::to_string_pretty(&estimate)?);
            } else {
                println!(
                    "{} files, {} directories, {} logical, {} physical",
                    estimate.files,
                    estimate.directories,
                    human_bytes(estimate.logical_bytes),
                    human_bytes(estimate.physical_bytes)
                );
                println!(
                    "{} unique chunks; {} already in the store; {} projected new chunk payload",
                    estimate.unique_chunks,
                    human_bytes(estimate.deduplicated_chunk_bytes),
                    human_bytes(estimate.projected_new_chunk_bytes)
                );
                println!(
                    "{} policy rule(s), {} excluded subtree root(s)",
                    estimate.policy_rules, estimate.excluded_subtrees
                );
            }
        }
        Command::Watch {
            foreground,
            no_daemon,
            debounce_ms,
        } => {
            let (repository, id) = FurrowRepository::watch(&cli.repo)?;
            if cli.json {
                println!(
                    "{}",
                    serde_json::json!({
                        "workspace": repository.root(),
                        "snapshot": id_hex(&id),
                        "store": repository.store_root(),
                    })
                );
            } else {
                println!("Protected {}", repository.root().display());
                println!("Snapshot {}", id_hex(&id));
                println!("Store {}", repository.store_root().display());
            }
            if foreground {
                furrow::watcher::run(
                    repository,
                    std::time::Duration::from_millis(debounce_ms.max(10)),
                )?;
            } else if !no_daemon && std::env::var_os("FURROW_NO_DAEMON").is_none() {
                spawn_background_watcher(&repository, debounce_ms.max(10))?;
            }
        }
        Command::Daemon { debounce_ms } => {
            let repository = FurrowRepository::open(&cli.repo)?;
            furrow::watcher::run(
                repository,
                std::time::Duration::from_millis(debounce_ms.max(10)),
            )?;
        }
        Command::RemoteHelper { namespace } => {
            furrow::remote::serve(&namespace)?;
        }
        Command::NamespaceProbe => {
            furrow::universe::probe_namespace_helper()?;
        }
        Command::ExecNamespace {
            source,
            target,
            command,
        } => {
            furrow::universe::exec_linux_namespace(&source, &target, &command)?;
        }
        Command::Snap { message } => {
            let mut repository = FurrowRepository::open(&cli.repo)?;
            let id = repository.snapshot(message, SnapshotTrigger::Manual)?;
            if cli.json {
                println!("{}", serde_json::json!({"snapshot": id_hex(&id)}));
            } else {
                println!("Snapshot {}", id_hex(&id));
            }
        }
        Command::Timeline { limit } => {
            let repository = FurrowRepository::open(&cli.repo)?;
            let timeline = repository.timeline(limit)?;
            if cli.json {
                println!("{}", serde_json::to_string_pretty(&timeline)?);
            } else {
                for snapshot in timeline {
                    println!(
                        "{}  {}  {:<10} {:<7} {}",
                        &snapshot.id[..12],
                        snapshot.sealed_at,
                        snapshot.trigger,
                        snapshot.materialization.grade,
                        snapshot.label.unwrap_or_default()
                    );
                }
            }
        }
        Command::Pin { snapshot } => {
            let mut repository = FurrowRepository::open(&cli.repo)?;
            let id = repository.resolve_snapshot(&snapshot)?;
            let changed = repository.pin(&id)?;
            if cli.json {
                println!(
                    "{}",
                    serde_json::json!({"snapshot": id_hex(&id), "pinned": true, "changed": changed})
                );
            } else if changed {
                println!("Pinned {}", id_hex(&id));
            } else {
                println!("Already pinned {}", id_hex(&id));
            }
        }
        Command::Unpin { snapshot } => {
            let mut repository = FurrowRepository::open(&cli.repo)?;
            let id = repository.resolve_snapshot(&snapshot)?;
            let changed = repository.unpin(&id)?;
            if cli.json {
                println!(
                    "{}",
                    serde_json::json!({"snapshot": id_hex(&id), "pinned": false, "changed": changed})
                );
            } else if changed {
                println!("Unpinned {}", id_hex(&id));
            } else {
                println!("Snapshot was not pinned {}", id_hex(&id));
            }
        }
        Command::Diff { target } => {
            let mut repository = FurrowRepository::open(&cli.repo)?;
            let diff = repository.diff(&target)?;
            if cli.json {
                println!("{}", serde_json::to_string_pretty(&diff)?);
            } else {
                println!(
                    "Diff {} ({} -> {})",
                    diff.target,
                    &diff.base_snapshot[..12],
                    &diff.target_snapshot[..12]
                );
                if diff.changes.is_empty() {
                    println!("No changes");
                } else {
                    for change in diff.changes {
                        println!("  {:<7} {}", change.action, change.path);
                    }
                }
            }
        }
        Command::Rewind {
            snapshot,
            paths,
            dry_run,
            yes,
            sqlite_consistent,
        } => {
            let mut repository = FurrowRepository::open(&cli.repo)?;
            let target = repository.resolve_snapshot(&snapshot)?;
            let plan = repository.plan_rewind(&target, &paths)?;
            if cli.json || dry_run {
                println!("{}", serde_json::to_string_pretty(&plan)?);
            } else {
                print_plan(&plan);
            }
            if dry_run || plan.changes.is_empty() {
                return Ok(());
            }
            if !yes {
                anyhow::ensure!(
                    io::stdin().is_terminal(),
                    "non-interactive rewind requires --yes"
                );
                print!("Apply this rewind? [y/N] ");
                io::stdout().flush()?;
                let mut answer = String::new();
                io::stdin().read_line(&mut answer)?;
                anyhow::ensure!(answer.trim().eq_ignore_ascii_case("y"), "rewind cancelled");
            }
            let (pre, applied) = repository
                .rewind(&target, &paths, sqlite_consistent)
                .context("rewind failed")?;
            if cli.json {
                println!(
                    "{}",
                    serde_json::json!({
                        "restored": applied.target,
                        "pre_rewind_snapshot": id_hex(&pre),
                        "changes": applied.changes.len(),
                    })
                );
            } else {
                println!("Restored {} paths", applied.changes.len());
                println!("Undo snapshot: {}", id_hex(&pre));
            }
        }
        Command::Status { fidelity } => {
            let repository = FurrowRepository::open(&cli.repo)?;
            let status = repository.status()?;
            if cli.json {
                if fidelity {
                    println!(
                        "{}",
                        serde_json::json!({"status": status, "fidelity": repository.fidelity()?})
                    );
                } else {
                    println!("{}", serde_json::to_string_pretty(&status)?);
                }
            } else {
                println!("Workspace: {}", status.workspace.display());
                println!("Store:     {}", status.store.display());
                if let Some(head) = status.head {
                    println!("Head:      {head}");
                }
                println!("Snapshots: {}", status.snapshots);
                println!("Objects:   {}", status.objects);
                println!("Pack data: {} bytes", status.physical_bytes);
                println!(
                    "Budget:    {} / {} ({} free; {})",
                    human_bytes(status.budget.physical_bytes),
                    human_bytes(status.budget.max_store_bytes),
                    human_bytes(status.budget.available_bytes),
                    if status.budget.satisfied {
                        "satisfied"
                    } else {
                        "under pressure"
                    }
                );
                println!(
                    "Watcher:   {}",
                    if status.watcher_running {
                        "running"
                    } else {
                        "stopped"
                    }
                );
                if fidelity {
                    let report = repository.fidelity()?;
                    println!("Fidelity:  {} ({})", report.grade, report.platform);
                    for aspect in report.aspects {
                        println!(
                            "  {:<28} {:<24} {}",
                            aspect.aspect, aspect.fidelity, aspect.detail
                        );
                    }
                }
            }
        }
        Command::Ui {
            port,
            no_open,
            merge_check,
        } => {
            let repository = FurrowRepository::open(&cli.repo)?;
            furrow::ui::run(repository.root(), port, no_open, merge_check, cli.json)?;
        }
        Command::Fork {
            name,
            destination,
            at,
            command,
        } => {
            anyhow::ensure!(
                !cli.json || command.is_empty(),
                "--json cannot be combined with a fork command"
            );
            let mut repository = FurrowRepository::open(&cli.repo)?;
            let name = name.unwrap_or_else(default_fork_name);
            let destination =
                destination.unwrap_or_else(|| default_fork_destination(&repository, &name));
            let plan = match at {
                Some(at) => {
                    let at = repository.resolve_snapshot(&at)?;
                    repository.prepare_fork_at(&name, &destination, &at)?
                }
                None => repository.prepare_fork(&name, &destination)?,
            };
            if !cli.json {
                print_fork_plan(&plan);
                io::stdout().flush()?;
            }
            let summary = repository.materialize_fork(plan.clone())?;
            if cli.json {
                println!("{}", serde_json::json!({"plan": plan, "result": summary}));
            } else {
                println!("Fork {}", summary.name);
                println!("Path {}", summary.destination.display());
                println!(
                    "Tier {} | {} files | {} logical bytes | {} cloned | {} copied | {} ms",
                    summary.tier,
                    summary.files,
                    summary.logical_bytes,
                    summary.cloned_bytes,
                    summary.copied_bytes,
                    summary.elapsed_ms
                );
                println!("Base {}", &summary.base_snapshot[..12]);
            }
            if let Some((program, arguments)) = command.split_first() {
                let status = std::process::Command::new(program)
                    .args(arguments)
                    .current_dir(&summary.destination)
                    .env("FURROW_FORK_NAME", &summary.name)
                    .env("FURROW_FORK_BASE", &summary.base_snapshot)
                    .status()
                    .with_context(|| format!("run {:?} in fork", program))?;
                anyhow::ensure!(status.success(), "fork command exited with {status}");
            }
        }
        Command::Forks => {
            let repository = FurrowRepository::open(&cli.repo)?;
            let forks = repository.forks()?;
            if cli.json {
                println!("{}", serde_json::to_string_pretty(&forks)?);
            } else if forks.is_empty() {
                println!("No forks");
            } else {
                for fork in forks {
                    println!(
                        "{:<20} {:<14} {:>8} ms  {:>9}  {}",
                        fork.name,
                        fork.tier,
                        fork.elapsed_ms,
                        if fork.conflicts == 0 && !fork.radar_stale {
                            "clear".to_owned()
                        } else if fork.conflicts == 0 {
                            "clear/stale".to_owned()
                        } else if fork.radar_stale {
                            format!("{} conflict/stale", fork.conflicts)
                        } else {
                            format!("{} conflict", fork.conflicts)
                        },
                        fork.destination.display()
                    );
                }
            }
        }
        Command::Events {
            after,
            limit,
            follow,
            interval_ms,
        } => {
            let repository = FurrowRepository::open(&cli.repo)?;
            let mut cursor = after;
            loop {
                let page = repository.events(cursor.as_deref(), limit)?;
                anyhow::ensure!(
                    page.cursor_found,
                    "event cursor expired; resume from {}",
                    page.earliest_cursor
                        .as_deref()
                        .unwrap_or("the current stream")
                );
                for event in &page.events {
                    write_ndjson(event)?;
                }
                if let Some(event) = page.events.last() {
                    cursor = Some(event.cursor.clone());
                }
                if !follow {
                    break;
                }
                std::thread::sleep(std::time::Duration::from_millis(interval_ms.max(50)));
            }
        }
        Command::WatchFork {
            name,
            after,
            once,
            interval_ms,
        } => {
            let repository = FurrowRepository::open(&cli.repo)?;
            let mut cursor = after;
            loop {
                let updates = repository.fork_updates(&name, cursor.as_deref(), 1000)?;
                if !updates.cursor_found {
                    eprintln!(
                        "warning: cursor was not in the latest 1000 seals; replaying the retained window"
                    );
                }
                if !updates.snapshots.is_empty() {
                    if cli.json {
                        println!("{}", serde_json::to_string(&updates)?);
                    } else {
                        for snapshot in &updates.snapshots {
                            println!(
                                "{}  {}  {:<12} {}",
                                &snapshot.id[..12],
                                snapshot.sealed_at,
                                snapshot.trigger,
                                snapshot.label.clone().unwrap_or_default()
                            );
                        }
                    }
                }
                cursor = updates.head;
                if once {
                    return Ok(());
                }
                std::thread::sleep(std::time::Duration::from_millis(interval_ms.max(50)));
            }
        }
        Command::ForkRemove { name, keep_files } => {
            let mut repository = FurrowRepository::open(&cli.repo)?;
            let removal = repository.remove_fork(&name, keep_files)?;
            if cli.json {
                println!("{}", serde_json::to_string_pretty(&removal)?);
            } else if removal.files_removed {
                println!(
                    "Removed fork {} and {}",
                    removal.name,
                    removal.destination.display()
                );
            } else {
                println!(
                    "Forgot fork {}; files remain at {}",
                    removal.name,
                    removal.destination.display()
                );
            }
        }
        Command::Claim {
            pattern,
            owner,
            ttl_seconds,
        } => {
            let mut repository = FurrowRepository::open(&cli.repo)?;
            let owner = owner.unwrap_or_else(|| repository.default_claim_owner());
            let outcome = repository.claim(&pattern, &owner, ttl_seconds)?;
            if cli.json {
                println!("{}", serde_json::to_string_pretty(&outcome)?);
            } else {
                println!(
                    "Claimed {} as {} until {}",
                    outcome.claim.pattern, outcome.claim.owner, outcome.claim.expires_at
                );
                println!("Claim {}", outcome.claim.id);
                println!("Snapshot {}", &outcome.snapshot[..12]);
            }
        }
        Command::Claims => {
            let repository = FurrowRepository::open(&cli.repo)?;
            let claims = repository.claims()?;
            if cli.json {
                println!("{}", serde_json::to_string_pretty(&claims)?);
            } else if claims.is_empty() {
                println!("No active claims");
            } else {
                for claim in claims {
                    println!(
                        "{}  {:<20} {:<32} expires {}",
                        &claim.id[..12],
                        claim.owner,
                        claim.pattern,
                        claim.expires_at
                    );
                }
            }
        }
        Command::Release { claim, owner } => {
            let mut repository = FurrowRepository::open(&cli.repo)?;
            let owner = owner.unwrap_or_else(|| repository.default_claim_owner());
            let outcome = repository.release_claim(&claim, &owner)?;
            if cli.json {
                println!("{}", serde_json::to_string_pretty(&outcome)?);
            } else {
                println!("Released {} claim(s)", outcome.released.len());
                println!("Snapshot {}", &outcome.snapshot[..12]);
            }
        }
        Command::Coord { command } => match command {
            CoordCommand::Write {
                path,
                value,
                file,
                owner,
            } => {
                let bytes = match (value, file) {
                    (Some(value), None) => value.into_bytes(),
                    (None, Some(file)) => fs::read(file)?,
                    (None, None) => {
                        let mut bytes = Vec::new();
                        io::stdin().take(1024 * 1024 + 1).read_to_end(&mut bytes)?;
                        bytes
                    }
                    (Some(_), Some(_)) => unreachable!("clap enforces conflicts"),
                };
                anyhow::ensure!(bytes.len() <= 1024 * 1024, "coord value exceeds 1 MiB");
                let mut repository = FurrowRepository::open(&cli.repo)?;
                let owner = owner.unwrap_or_else(|| repository.default_claim_owner());
                let outcome = repository.coord_write(&path, &bytes, &owner)?;
                if cli.json {
                    println!("{}", serde_json::to_string_pretty(&outcome)?);
                } else {
                    println!(
                        "Wrote coord/{} ({} bytes) to {} workspace(s)",
                        outcome.propagation.path,
                        outcome.propagation.bytes,
                        outcome.propagation.propagated_workspaces
                    );
                    for failure in outcome.propagation.failures {
                        eprintln!(
                            "warning: {}: {}",
                            failure.workspace.display(),
                            failure.error
                        );
                    }
                    println!("Snapshot {}", &outcome.snapshot[..12]);
                }
            }
            CoordCommand::Read { path } => {
                let repository = FurrowRepository::open(&cli.repo)?;
                let bytes = repository.coord_read(&path)?;
                if cli.json {
                    let value = String::from_utf8(bytes)
                        .context("--json coord read requires UTF-8 content")?;
                    println!("{}", serde_json::json!({"path": path, "value": value}));
                } else {
                    io::stdout().write_all(&bytes)?;
                }
            }
            CoordCommand::List => {
                let repository = FurrowRepository::open(&cli.repo)?;
                let entries = repository.coord_list()?;
                if cli.json {
                    println!("{}", serde_json::to_string_pretty(&entries)?);
                } else if entries.is_empty() {
                    println!("No coordination values");
                } else {
                    for entry in entries {
                        println!("{:>8}  {}", entry.bytes, entry.path);
                    }
                }
            }
            CoordCommand::Remove { path, owner } => {
                let mut repository = FurrowRepository::open(&cli.repo)?;
                let owner = owner.unwrap_or_else(|| repository.default_claim_owner());
                let outcome = repository.coord_remove(&path, &owner)?;
                if cli.json {
                    println!("{}", serde_json::to_string_pretty(&outcome)?);
                } else {
                    println!(
                        "Removed coord/{} from {} workspace(s)",
                        outcome.propagation.path, outcome.propagation.propagated_workspaces
                    );
                    println!("Snapshot {}", &outcome.snapshot[..12]);
                }
            }
        },
        Command::Hook { command } => match command {
            HookCommand::Install => {
                let hooks = install_hook_adapters(&cli.repo)?;
                let (_, snapshot) = FurrowRepository::attach_and_snapshot(
                    &cli.repo,
                    Some("installed agent hooks".to_owned()),
                    SnapshotTrigger::AgentRun,
                )?;
                if cli.json {
                    println!(
                        "{}",
                        serde_json::json!({"hooks": hooks, "snapshot": id_hex(&snapshot)})
                    );
                } else {
                    for hook in hooks {
                        println!("Installed {}", hook.display());
                    }
                    println!("Snapshot {}", id_hex(&snapshot));
                }
            }
            HookCommand::PreTurn { agent, turn } => {
                run_hook(&cli.repo, cli.json, "pre-turn", agent, turn, None)?;
            }
            HookCommand::PostTool { agent, turn, tool } => {
                run_hook(&cli.repo, cli.json, "post-tool", agent, turn, tool)?;
            }
            HookCommand::TurnEnd { agent, turn } => {
                run_hook(&cli.repo, cli.json, "turn-end", agent, turn, None)?;
            }
        },
        Command::Run {
            name,
            destination,
            at,
            command,
        } => {
            anyhow::ensure!(!cli.json, "--json cannot be combined with `furrow run`");
            let mut repository = FurrowRepository::open(&cli.repo)?;
            let destination =
                destination.unwrap_or_else(|| default_fork_destination(&repository, &name));
            let plan = match at {
                Some(at) => {
                    let at = repository.resolve_snapshot(&at)?;
                    repository.prepare_fork_at(&name, &destination, &at)?
                }
                None => repository.prepare_fork(&name, &destination)?,
            };
            print_fork_plan(&plan);
            io::stdout().flush()?;
            let summary = repository.materialize_fork(plan)?;
            println!(
                "Running in {} ({}; {} cloned, {} copied)",
                summary.destination.display(),
                summary.tier,
                summary.cloned_bytes,
                summary.copied_bytes
            );
            let (program, arguments) = command
                .split_first()
                .context("`furrow run` requires a command after --")?;
            let status = std::process::Command::new(program)
                .args(arguments)
                .current_dir(&summary.destination)
                .env("FURROW_FORK_NAME", &summary.name)
                .env("FURROW_FORK_BASE", &summary.base_snapshot)
                .status()
                .with_context(|| format!("run {:?} in fork", program))?;
            let mut fork_repository = FurrowRepository::open(&summary.destination)?;
            let head = fork_repository.snapshot(
                Some(format!("command completed in {}", summary.name)),
                SnapshotTrigger::AgentRun,
            )?;
            println!("Fork head {}", &id_hex(&head)[..12]);
            println!("Source workspace was not modified");
            println!(
                "Merge with: furrow merge {} --check '<command>'",
                summary.name
            );
            anyhow::ensure!(status.success(), "fork command exited with {status}");
        }
        Command::Exec {
            fork,
            count,
            at,
            plan,
            command,
        } => {
            execute_universes(&cli.repo, cli.json, fork, count, at, plan, &command)?;
        }
        Command::Attempt { message, command } => {
            anyhow::ensure!(!cli.json, "--json cannot be combined with `furrow try`");
            let (program, arguments) = command
                .split_first()
                .context("`furrow try` requires a command after --")?;
            let command_name = std::path::Path::new(program)
                .file_name()
                .unwrap_or(program.as_os_str())
                .to_string_lossy();
            let label = message.unwrap_or_else(|| command_name.into_owned());
            anyhow::ensure!(!label.trim().is_empty(), "try label cannot be empty");
            anyhow::ensure!(label.len() <= 256, "try label is limited to 256 bytes");
            let (mut repository, before) = FurrowRepository::attach_and_snapshot(
                &cli.repo,
                Some(format!("before try: {label}")),
                SnapshotTrigger::AgentRun,
            )?;
            eprintln!("Protected {}", id_hex(&before));

            let status = std::process::Command::new(program)
                .args(arguments)
                .current_dir(repository.root())
                .env("FURROW_TRY_SNAPSHOT", id_hex(&before))
                .status()
                .with_context(|| format!("run {:?}", program))?;
            let outcome = status
                .code()
                .map_or_else(|| status.to_string(), |code| format!("exit {code}"));
            let after = repository.snapshot(
                Some(format!("after try ({outcome}): {label}")),
                SnapshotTrigger::AgentRun,
            )?;
            eprintln!("Result {}", id_hex(&after));
            eprintln!("Undo with: furrow rewind {}", id_hex(&before));
            if !status.success() {
                exit_with_status(status);
            }
        }
        Command::Shrink { yes, paths } => {
            let plan = furrow::shrink::discover(&cli.repo, &paths)?;
            if !yes {
                if cli.json {
                    println!("{}", serde_json::to_string_pretty(&plan)?);
                } else {
                    print_shrink_plan(&plan);
                    if !plan.candidates.is_empty() {
                        println!("Preview only. Re-run this command with `--yes` to reclaim them.");
                    }
                }
                return Ok(());
            }
            if plan.candidates.is_empty() {
                if cli.json {
                    println!("{}", serde_json::json!({"plan": plan, "changed": false}));
                } else {
                    println!("No recognized dependency or build caches found");
                }
                return Ok(());
            }

            let store_before = FurrowRepository::global_store_physical_bytes()?;
            let (mut repository, before) = FurrowRepository::attach_and_snapshot(
                &cli.repo,
                Some("before shrink".to_owned()),
                SnapshotTrigger::Manual,
            )?;
            eprintln!("Protected {}", id_hex(&before));
            let removal = furrow::shrink::apply(repository.root(), &plan);
            let after =
                repository.snapshot(Some("after shrink".to_owned()), SnapshotTrigger::Manual)?;
            let store_after = repository.store_physical_bytes()?;
            let store_added = store_after.saturating_sub(store_before);
            let net_reclaimed = plan.total_physical_bytes.saturating_sub(store_added);
            let net_added = store_added.saturating_sub(plan.total_physical_bytes);
            if cli.json {
                println!(
                    "{}",
                    serde_json::json!({
                        "plan": plan,
                        "changed": true,
                        "before_snapshot": id_hex(&before),
                        "after_snapshot": id_hex(&after),
                        "estimated_workspace_bytes_removed": plan.total_physical_bytes,
                        "protected_store_bytes_added": store_added,
                        "estimated_net_bytes_reclaimed": net_reclaimed,
                        "estimated_net_bytes_added": net_added,
                    })
                );
            } else {
                print_shrink_plan(&plan);
                println!(
                    "Removed about {} from the workspace; protected store added {}",
                    human_bytes(plan.total_physical_bytes),
                    human_bytes(store_added)
                );
                if net_added == 0 {
                    println!(
                        "Estimated net disk reclaimed: {}",
                        human_bytes(net_reclaimed)
                    );
                } else {
                    println!(
                        "Estimated net disk increase: {} to keep this cleanup reversible",
                        human_bytes(net_added)
                    );
                }
                println!("Undo with: furrow rewind {}", id_hex(&before));
            }
            removal?;
        }
        Command::Bisect {
            good,
            bad,
            limit,
            command,
        } => {
            let mut repository = FurrowRepository::open(&cli.repo)?;
            let outcome = repository.bisect(&command, good.as_deref(), bad.as_deref(), limit)?;
            if cli.json {
                println!("{}", serde_json::to_string_pretty(&outcome)?);
            } else {
                for check in &outcome.checks {
                    println!(
                        "{}  {:<4}  check {:>6} ms  fork {:>4} ms  {}",
                        &check.snapshot[..12],
                        if check.passed { "pass" } else { "fail" },
                        check.elapsed_ms,
                        check.probe_fork_ms,
                        check.label.clone().unwrap_or_default()
                    );
                }
                println!("Last good: {}", outcome.good_snapshot);
                println!("First bad: {}", outcome.first_bad_snapshot);
            }
        }
        Command::Merge {
            fork,
            snapshot,
            base,
            check,
            dry_run,
        } => {
            let mut repository = FurrowRepository::open(&cli.repo)?;
            let outcome = if let Some(snapshot) = snapshot {
                repository.merge_snapshot(&snapshot, base.as_deref(), check.as_deref(), dry_run)?
            } else {
                let fork =
                    fork.context("merge requires a fork name or --snapshot <snapshot-id>")?;
                repository.merge(&fork, check.as_deref(), dry_run)?
            };
            if cli.json {
                println!("{}", serde_json::to_string_pretty(&outcome)?);
            } else {
                println!(
                    "Merge {}: {} changes, {} conflicts",
                    outcome.fork,
                    outcome.changes,
                    outcome.conflicts.len()
                );
                for conflict in &outcome.conflicts {
                    println!(
                        "  conflict {:<16} {}",
                        conflict.kind,
                        String::from_utf8_lossy(&conflict.path)
                    );
                }
                if let Some(snapshot) = &outcome.result_snapshot {
                    println!("Merged snapshot {}", &snapshot[..12]);
                    if let Some(output) = &outcome.check_output {
                        if !output.is_empty() {
                            println!("Check output:\n{output}");
                        }
                    }
                } else if dry_run && outcome.conflicts.is_empty() {
                    println!("Dry run: source workspace was not changed");
                }
            }
            anyhow::ensure!(
                outcome.conflicts.is_empty(),
                "merge stopped with {} conflict(s)",
                outcome.conflicts.len()
            );
        }
        Command::Forget { purge } => {
            FurrowRepository::open(&cli.repo)?.forget(purge)?;
            println!("Repository detached from furrow");
        }
        Command::Gc { dry_run } => {
            let report = FurrowRepository::gc_global(dry_run)?;
            if cli.json {
                println!("{}", serde_json::to_string_pretty(&report)?);
            } else {
                println!(
                    "{} objects reachable; {} unreachable",
                    report.reachable_objects, report.unreachable_objects
                );
                println!(
                    "{} retained snapshots; {} historical snapshots thinned",
                    report.retained_snapshots, report.thinned_snapshots
                );
                println!(
                    "{} bytes {} ({} -> {})",
                    report.reclaimed_bytes,
                    if dry_run { "reclaimable" } else { "reclaimed" },
                    report.physical_bytes_before,
                    report.physical_bytes_after
                );
            }
        }
        Command::Budget { max, reserve_free } => {
            let max = max.as_deref().map(parse_byte_size).transpose()?;
            let reserve_free = reserve_free.as_deref().map(parse_byte_size).transpose()?;
            let status = FurrowRepository::budget_global(max, reserve_free)?;
            if cli.json {
                println!("{}", serde_json::to_string_pretty(&status)?);
            } else {
                println!(
                    "Store {} / {}",
                    human_bytes(status.physical_bytes),
                    human_bytes(status.max_store_bytes)
                );
                println!(
                    "Free {} / {} reserved",
                    human_bytes(status.available_bytes),
                    human_bytes(status.reserved_free_bytes)
                );
                println!(
                    "Budget {}",
                    if status.satisfied {
                        "satisfied"
                    } else {
                        "cannot be met without deleting protected bytes"
                    }
                );
            }
        }
        Command::Pair { remote, name, key } => {
            let repository = FurrowRepository::open(&cli.repo)?;
            let namespace = name.unwrap_or_else(|| default_sync_name(&repository));
            let summary = repository.pair(&remote, &namespace, key.as_deref())?;
            print_pair_summary(&summary, cli.json)?;
        }
        Command::Remote {
            command: RemoteCommand::Add { remote, name, key },
        } => {
            let repository = FurrowRepository::open(&cli.repo)?;
            let namespace = name.unwrap_or_else(|| default_sync_name(&repository));
            let summary = repository.pair(&remote, &namespace, key.as_deref())?;
            print_pair_summary(&summary, cli.json)?;
            if !cli.json {
                println!(
                    "Clone URL {}/{}",
                    summary.remote.trim_end_matches('/'),
                    summary.namespace
                );
            }
        }
        Command::Clone {
            remote,
            destination,
            key,
            no_watch,
        } => {
            let (transport, namespace) = split_clone_url(&remote)?;
            let destination = destination.unwrap_or_else(|| PathBuf::from(&namespace));
            anyhow::ensure!(
                !destination.exists(),
                "clone destination already exists: {}",
                destination.display()
            );
            fs::create_dir_all(&destination)?;
            let result = (|| -> anyhow::Result<(FurrowRepository, furrow::SyncPullOutcome)> {
                let status = std::process::Command::new("git")
                    .arg("init")
                    .arg("--quiet")
                    .arg("--")
                    .arg(&destination)
                    .status()
                    .context("start git for clone destination")?;
                anyhow::ensure!(status.success(), "git init failed for clone destination");
                let (mut repository, _) = FurrowRepository::watch(&destination)?;
                repository.pair(PathBuf::from(&transport).as_path(), &namespace, Some(&key))?;
                let outcome = repository
                    .sync_pull(true, None)
                    .context("workspace was not found or the recovery key does not match")?;
                Ok((repository, outcome))
            })();
            let (repository, outcome) = match result {
                Ok(value) => value,
                Err(error) => {
                    let _ = fs::remove_dir_all(&destination);
                    return Err(error.context("clone failed; incomplete destination was removed"));
                }
            };
            if !no_watch && std::env::var_os("FURROW_NO_DAEMON").is_none() {
                spawn_background_watcher(&repository, 500)?;
            }
            if cli.json {
                println!(
                    "{}",
                    serde_json::json!({
                        "workspace": repository.root(),
                        "remote": remote,
                        "sync": outcome,
                    })
                );
            } else {
                println!("Cloned {}", repository.root().display());
                println!("Remote snapshot {}", &outcome.remote_snapshot[..12]);
                println!(
                    "{} objects fetched ({} bytes); {} reused; {} files materialized",
                    outcome.fetched_objects,
                    outcome.fetched_bytes,
                    outcome.reused_objects,
                    outcome.materialized_files
                );
            }
        }
        Command::Sync {
            push,
            pull,
            follow,
            poll_seconds,
            takeover,
            bootstrap,
            ref_name,
            timings,
        } => {
            anyhow::ensure!(
                push || pull || follow,
                "choose exactly one of --push, --pull, or --follow"
            );
            let ref_name = ref_name.as_deref();
            let mut repository = FurrowRepository::open(&cli.repo)?;
            if follow {
                anyhow::ensure!(poll_seconds > 0, "--poll-seconds must be greater than zero");
                let mut session = repository.sync_follow_session(ref_name)?;
                loop {
                    let result = repository.sync_follow_once(&mut session);
                    let session_failed = result.is_err();
                    match result {
                        Ok(SyncFollowOutcome::Idle) => {}
                        Ok(SyncFollowOutcome::Published { report }) => {
                            println!(
                                "Published {} ({} objects, {} bytes)",
                                &report.snapshot[..12],
                                report.uploaded_objects,
                                report.uploaded_bytes
                            );
                            if timings {
                                print_transport_timings(&report.timings);
                            }
                        }
                        Ok(SyncFollowOutcome::Pulled { outcome }) => {
                            println!(
                                "Remote {} {:?}",
                                &outcome.remote_snapshot[..12],
                                outcome.disposition
                            );
                            if outcome.disposition == SyncDisposition::Diverged {
                                eprintln!(
                                    "Divergence preserved; follow is paused until it is resolved"
                                );
                            }
                            if timings {
                                print_transport_timings(&outcome.timings);
                                print_apply_timings(&outcome.apply_timings);
                            }
                        }
                        Err(error) => eprintln!("sync follow: {error:#}"),
                    }
                    if session_failed {
                        std::thread::sleep(std::time::Duration::from_secs(poll_seconds));
                        session = repository.sync_follow_session(ref_name)?;
                        continue;
                    }
                    if let Err(error) =
                        session.wait_for_remote_change(std::time::Duration::from_secs(poll_seconds))
                    {
                        eprintln!("sync follow notification: {error:#}");
                        std::thread::sleep(std::time::Duration::from_secs(poll_seconds));
                        session = repository.sync_follow_session(ref_name)?;
                    }
                }
            } else if push {
                let report = repository.sync_push(takeover, ref_name)?;
                if timings {
                    print_transport_timings(&report.timings);
                }
                if cli.json {
                    println!("{}", serde_json::to_string_pretty(&report)?);
                } else {
                    println!("Published snapshot {}", &report.snapshot[..12]);
                    println!(
                        "{} objects uploaded ({} bytes); {} reused",
                        report.uploaded_objects, report.uploaded_bytes, report.reused_objects
                    );
                }
            } else {
                let outcome = repository.sync_pull(bootstrap, ref_name)?;
                if timings {
                    print_transport_timings(&outcome.timings);
                    print_apply_timings(&outcome.apply_timings);
                }
                if cli.json {
                    println!("{}", serde_json::to_string_pretty(&outcome)?);
                } else {
                    println!("Remote snapshot {}", &outcome.remote_snapshot[..12]);
                    println!(
                        "{} objects fetched ({} bytes); {} reused; {} files materialized",
                        outcome.fetched_objects,
                        outcome.fetched_bytes,
                        outcome.reused_objects,
                        outcome.materialized_files
                    );
                    match outcome.disposition {
                        SyncDisposition::FastForwarded => println!("Workspace fast-forwarded"),
                        SyncDisposition::Bootstrapped => {
                            println!("Workspace initialized from the remote")
                        }
                        SyncDisposition::UpToDate => println!("Workspace already up to date"),
                        SyncDisposition::Diverged => println!(
                            "Divergence preserved; inspect or materialize the full remote snapshot {}",
                            outcome.remote_snapshot
                        ),
                    }
                }
                anyhow::ensure!(
                    outcome.disposition != SyncDisposition::Diverged,
                    "local and remote working states diverged; neither side was overwritten"
                );
            }
        }
        Command::Mcp => {
            let repository = FurrowRepository::open(&cli.repo)?;
            furrow::mcp::run(repository)?;
        }
    }
    Ok(())
}

fn print_transport_timings(timings: &furrow::sync::TransportTimings) {
    eprintln!(
        "sync timings: connect/auth={}ms negotiate={}ms stream={}ms fsync-wait={}ms notify={} total={}ms reused_connection={}",
        timings.connect_auth_ms,
        timings.negotiate_ms,
        timings.stream_ms,
        timings.durability_ms,
        timings
            .notify_ms
            .map_or_else(|| "n/a".to_owned(), |value| format!("{value}ms")),
        timings.total_ms,
        timings.connection_reused
    );
}

fn print_apply_timings(timings: &furrow::repository::ApplyTimings) {
    eprintln!(
        "apply timings: diff-compute={}ms divergence-check={}ms write={}ms fsync={}ms baseline-install={}ms watcher-requiesce={}ms",
        timings.diff_compute_ms,
        timings.divergence_check_ms,
        timings.write_ms,
        timings.fsync_ms,
        timings.baseline_install_ms,
        timings.watcher_requiesce_ms,
    );
}

fn default_fork_name() -> String {
    let seconds = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap_or_default()
        .as_secs();
    format!("fork-{seconds}")
}

fn print_pair_summary(summary: &furrow::sync::PairSummary, json: bool) -> anyhow::Result<()> {
    if json {
        println!("{}", serde_json::to_string_pretty(summary)?);
    } else {
        println!("Paired {}", summary.namespace);
        println!("Remote {}", summary.remote);
        println!("Machine {}", summary.machine_id);
        println!("Recovery key {}", summary.key_hex);
        println!("Keep the recovery key private; enter it once on each additional machine.");
    }
    Ok(())
}

fn split_clone_url(value: &str) -> anyhow::Result<(String, String)> {
    let trimmed = value.trim_end_matches('/');
    let (transport, namespace) = trimmed
        .rsplit_once('/')
        .context("clone URL must end with a workspace name")?;
    furrow::remote::validate_namespace(namespace)?;
    if transport.starts_with("s3://") {
        anyhow::ensure!(
            transport.len() > "s3://".len(),
            "clone URL must include a bucket and workspace name"
        );
        furrow::s3_remote::S3Spec::from_uri(transport)?;
    } else if let Some(host) = transport.strip_prefix("ssh://") {
        anyhow::ensure!(
            !host.is_empty() && !host.contains('/'),
            "SSH clone URL must be ssh://HOST/WORKSPACE"
        );
    } else {
        anyhow::bail!("clone currently requires an ssh:// or s3:// URL");
    }
    Ok((transport.to_owned(), namespace.to_owned()))
}

fn default_exec_name() -> String {
    let seconds = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap_or_default()
        .as_secs();
    format!("exec-{seconds}-{}", std::process::id())
}

fn execute_universes(
    repo: &std::path::Path,
    json: bool,
    explicit_name: Option<String>,
    count: usize,
    at: Option<String>,
    plan_only: bool,
    command: &[OsString],
) -> anyhow::Result<()> {
    use furrow::universe::{ExecPlan, ExecutionDriver, UniverseCommand, UniversePlan};

    anyhow::ensure!((1..=32).contains(&count), "-n must be between 1 and 32");
    anyhow::ensure!(
        explicit_name.is_none() || count == 1,
        "--fork names one universe and cannot be combined with -n greater than 1"
    );
    anyhow::ensure!(
        plan_only || !command.is_empty(),
        "`furrow exec` requires a command after -- (or use --plan)"
    );

    let mut repository = FurrowRepository::open(repo)?;
    let canonical_workdir = repository.root().to_path_buf();
    let driver = furrow::universe::select_driver();
    let base_name = explicit_name.unwrap_or_else(default_exec_name);
    let first_name = if count == 1 {
        base_name.clone()
    } else {
        format!("{base_name}-1")
    };
    let first_destination = default_fork_destination(&repository, &first_name);
    let first_fork_plan = match at {
        Some(at) => {
            let at = repository.resolve_snapshot(&at)?;
            repository.prepare_fork_at(&first_name, &first_destination, &at)?
        }
        None => repository.prepare_fork(&first_name, &first_destination)?,
    };
    let base_port = std::env::var("PORT")
        .ok()
        .and_then(|value| value.parse::<u16>().ok())
        .unwrap_or(3000);
    anyhow::ensure!(
        usize::from(base_port).saturating_add(count - 1) <= usize::from(u16::MAX),
        "PORT range exceeds 65535"
    );

    let mut fork_plans = Vec::with_capacity(count);
    let mut universes = Vec::with_capacity(count);
    for offset in 0..count {
        let index = offset + 1;
        let name = if count == 1 {
            base_name.clone()
        } else {
            format!("{base_name}-{index}")
        };
        let destination = if offset == 0 {
            first_fork_plan.destination.clone()
        } else {
            default_fork_destination(&repository, &name)
        };
        let mut fork_plan = first_fork_plan.clone();
        if offset > 0 {
            fork_plan.fork_id = furrow::new_fork_id()?;
        }
        fork_plan.name = name.clone();
        fork_plan.destination = destination.clone();
        let process_workdir = match driver.driver {
            ExecutionDriver::LinuxMountNamespace => canonical_workdir.clone(),
            ExecutionDriver::SiblingDirectory => destination.clone(),
        };
        universes.push(UniversePlan {
            index,
            fork_id: fork_plan.fork_id.clone(),
            name,
            destination,
            process_workdir,
            port: base_port + u16::try_from(offset)?,
            base_snapshot: fork_plan.base_snapshot.clone(),
            logical_bytes: fork_plan.logical_bytes,
            projected_fork_ms: fork_plan.projected_native_cow_ms,
        });
        fork_plans.push(fork_plan);
    }
    let exec_plan = ExecPlan {
        driver,
        canonical_workdir,
        universes,
    };

    if plan_only {
        if json {
            println!("{}", serde_json::to_string_pretty(&exec_plan)?);
        } else {
            print_exec_plan(&exec_plan);
        }
        return Ok(());
    }

    if !json {
        print_exec_plan(&exec_plan);
        io::stdout().flush()?;
    }
    let started = std::time::Instant::now();
    let mut summaries = Vec::with_capacity(count);
    for fork_plan in fork_plans {
        summaries.push(repository.materialize_fork(fork_plan)?);
    }
    let materialized_ms = u64::try_from(started.elapsed().as_millis()).unwrap_or(u64::MAX);
    if !json {
        println!("Materialized {count} universe(s) in {materialized_ms} ms");
        io::stdout().flush()?;
    }

    let executable = std::env::current_exe().context("resolve furrow executable")?;
    let mut children = Vec::with_capacity(count);
    for (universe, summary) in exec_plan.universes.iter().zip(&summaries) {
        let mut child = UniverseCommand {
            driver: exec_plan.driver.driver,
            executable: &executable,
            source: &summary.destination,
            canonical_target: &exec_plan.canonical_workdir,
            command,
        }
        .command()?;
        child
            .env("FURROW_WORKDIR", &universe.process_workdir)
            .env("FURROW_CANONICAL_WORKDIR", &exec_plan.canonical_workdir)
            .env("FURROW_FORK_NAME", &summary.name)
            .env("FURROW_FORK_BASE", &summary.base_snapshot)
            .env("FURROW_UNIVERSE_INDEX", universe.index.to_string())
            .env("FURROW_UNIVERSE_COUNT", count.to_string())
            .env("PORT", universe.port.to_string());
        if json {
            redirect_stdout_to_stderr(&mut child)?;
        }
        match child.spawn() {
            Ok(child) => children.push(child),
            Err(error) => {
                for child in &mut children {
                    let _ = child.kill();
                }
                for mut child in children {
                    let _ = child.wait();
                }
                return Err(error).with_context(|| format!("start universe {}", universe.name));
            }
        }
    }

    let mut statuses = Vec::with_capacity(count);
    for mut child in children {
        statuses.push(child.wait().context("wait for universe command")?);
    }

    let mut results = Vec::with_capacity(count);
    for ((universe, summary), status) in exec_plan.universes.iter().zip(&summaries).zip(&statuses) {
        let mut fork_repository = FurrowRepository::open(&summary.destination)?;
        let outcome = status
            .code()
            .map_or_else(|| status.to_string(), |code| format!("exit {code}"));
        let head = fork_repository.snapshot(
            Some(format!("exec {} completed ({outcome})", universe.name)),
            SnapshotTrigger::AgentRun,
        )?;
        results.push(serde_json::json!({
            "index": universe.index,
            "fork": universe.name,
            "fork_id": universe.fork_id,
            "base_snapshot": summary.base_snapshot,
            "head_snapshot": id_hex(&head),
            "path": summary.destination,
            "process_workdir": universe.process_workdir,
            "port": universe.port,
            "exit_code": furrow::universe::exit_code(*status),
        }));
    }

    if json {
        println!(
            "{}",
            serde_json::to_string_pretty(&serde_json::json!({
                "driver": exec_plan.driver,
                "materialized_ms": materialized_ms,
                "universes": results,
            }))?
        );
    } else {
        for result in &results {
            println!(
                "{}  exit {}  port {}  {}",
                result["fork"].as_str().unwrap_or("unknown"),
                result["exit_code"],
                result["port"],
                result["path"].as_str().unwrap_or("unknown")
            );
        }
        println!("Source workspace was not modified");
    }
    if let Some(status) = statuses.iter().find(|status| !status.success()) {
        exit_with_status(*status);
    }
    Ok(())
}

fn print_exec_plan(plan: &furrow::universe::ExecPlan) {
    println!(
        "Driver {} | same canonical path: {}",
        plan.driver.driver,
        if plan.driver.same_canonical_path {
            "yes"
        } else {
            "no"
        }
    );
    println!("Reason: {}", plan.driver.reason);
    for universe in &plan.universes {
        println!(
            "  {:>2}  {:<28} port {:>5}  {}",
            universe.index,
            universe.name,
            universe.port,
            universe.process_workdir.display()
        );
    }
}

fn write_ndjson(value: &impl serde::Serialize) -> anyhow::Result<()> {
    let stdout = io::stdout();
    let mut output = stdout.lock();
    if let Err(error) = serde_json::to_writer(&mut output, value)
        .and_then(|()| writeln!(output).map_err(serde_json::Error::io))
    {
        if error.io_error_kind() == Some(io::ErrorKind::BrokenPipe) {
            return Ok(());
        }
        return Err(error.into());
    }
    output.flush()?;
    Ok(())
}

fn redirect_stdout_to_stderr(command: &mut std::process::Command) -> anyhow::Result<()> {
    use std::os::fd::FromRawFd;

    let descriptor = unsafe { libc::dup(libc::STDERR_FILENO) };
    if descriptor < 0 {
        return Err(std::io::Error::last_os_error()).context("duplicate stderr");
    }
    let stderr = unsafe { std::fs::File::from_raw_fd(descriptor) };
    command.stdout(std::process::Stdio::from(stderr));
    Ok(())
}

fn default_fork_destination(repository: &FurrowRepository, name: &str) -> PathBuf {
    let root = repository.root();
    let parent = root.parent().unwrap_or_else(|| std::path::Path::new("."));
    let repository_name = root
        .file_name()
        .unwrap_or_else(|| std::ffi::OsStr::new("workspace"));
    let mut forks_name = repository_name.to_os_string();
    forks_name.push(".furrow-forks");
    parent.join(forks_name).join(name)
}

fn default_sync_name(repository: &FurrowRepository) -> String {
    repository
        .root()
        .file_name()
        .and_then(|name| name.to_str())
        .filter(|name| !name.is_empty())
        .unwrap_or("workspace")
        .to_owned()
}

fn print_plan(plan: &furrow::RewindPlan) {
    println!("Rewind to {}", &plan.target[..12]);
    for change in &plan.changes {
        println!("  {:<8} {}", change.action, change.path);
    }
}

fn print_fork_plan(plan: &furrow::ForkPlan) {
    println!(
        "Fork plan: {} files, {} directories, {} logical",
        plan.files,
        plan.directories,
        human_bytes(plan.logical_bytes)
    );
    println!(
        "Native CoW target: about {} ms; streaming fallback: about {} ms and up to {} copied",
        plan.projected_native_cow_ms,
        plan.projected_streaming_copy_ms,
        human_bytes(plan.worst_case_copied_bytes)
    );
    if plan.at_snapshot {
        println!(
            "Materializing from stored snapshot {}",
            &plan.base_snapshot[..12]
        );
    }
}

fn print_shrink_plan(plan: &furrow::shrink::ShrinkPlan) {
    if plan.candidates.is_empty() {
        println!("No recognized dependency or build caches found");
        return;
    }
    for candidate in &plan.candidates {
        println!(
            "  {:>9}  {:<24} {}",
            human_bytes(candidate.physical_bytes),
            candidate.class,
            candidate.path
        );
    }
    println!(
        "{} paths, {} entries, {} physical ({} logical)",
        plan.candidates.len(),
        plan.total_entries,
        human_bytes(plan.total_physical_bytes),
        human_bytes(plan.total_logical_bytes)
    );
}

fn human_bytes(bytes: u64) -> String {
    const UNITS: [&str; 5] = ["B", "KiB", "MiB", "GiB", "TiB"];
    let mut value = bytes as f64;
    let mut unit = 0;
    while value >= 1024.0 && unit + 1 < UNITS.len() {
        value /= 1024.0;
        unit += 1;
    }
    if unit == 0 {
        format!("{bytes} {}", UNITS[unit])
    } else {
        format!("{value:.1} {}", UNITS[unit])
    }
}

fn parse_byte_size(value: &str) -> anyhow::Result<u64> {
    let value = value.trim();
    let split = value
        .find(|character: char| !character.is_ascii_digit())
        .unwrap_or(value.len());
    anyhow::ensure!(split > 0, "byte size must start with an integer");
    let number: u64 = value[..split].parse()?;
    let suffix = value[split..].trim().to_ascii_lowercase();
    let multiplier = match suffix.as_str() {
        "" | "b" => 1,
        "k" | "kb" | "kib" => 1024_u64,
        "m" | "mb" | "mib" => 1024_u64.pow(2),
        "g" | "gb" | "gib" => 1024_u64.pow(3),
        "t" | "tb" | "tib" => 1024_u64.pow(4),
        _ => anyhow::bail!("unsupported byte-size suffix `{suffix}`"),
    };
    number
        .checked_mul(multiplier)
        .context("byte size exceeds the supported range")
}

fn install_hook_adapters(root: &std::path::Path) -> anyhow::Result<Vec<PathBuf>> {
    let root = root.canonicalize()?;
    anyhow::ensure!(
        root.join(".git").exists(),
        "furrow currently requires a Git repository"
    );
    let directory = root.join(".furrow/hooks");
    fs::create_dir_all(&directory)?;
    let mut installed = Vec::new();
    for event in ["pre-turn", "post-tool", "turn-end"] {
        let destination = directory.join(event);
        let script = format!(
            "#!/bin/sh\nset -eu\nrepo=$(CDPATH= cd \"$(dirname \"$0\")/../..\" && pwd)\nexec \"${{FURROW_BIN:-furrow}}\" --repo \"$repo\" hook {event} \"$@\"\n"
        );
        let mut temporary = tempfile::NamedTempFile::new_in(&directory)?;
        temporary.write_all(script.as_bytes())?;
        temporary.as_file().sync_all()?;
        fs::set_permissions(temporary.path(), fs::Permissions::from_mode(0o755))?;
        temporary
            .persist(&destination)
            .map_err(|error| error.error)?;
        installed.push(destination);
    }
    std::fs::File::open(&directory)?.sync_all()?;
    Ok(installed)
}

fn run_hook(
    root: &std::path::Path,
    json: bool,
    event: &str,
    agent: Option<String>,
    turn: Option<String>,
    tool: Option<String>,
) -> anyhow::Result<()> {
    let agent = hook_value(agent, "FURROW_AGENT_ID")?.unwrap_or_else(|| "agent".to_owned());
    let turn = hook_value(turn, "FURROW_TURN_ID")?;
    let tool = hook_value(tool, "FURROW_TOOL_NAME")?;
    let mut label = format!("hook {event} agent={agent}");
    if let Some(turn) = turn {
        label.push_str(" turn=");
        label.push_str(&turn);
    }
    if let Some(tool) = tool {
        label.push_str(" tool=");
        label.push_str(&tool);
    }
    let (_, snapshot) = FurrowRepository::attach_and_snapshot(
        root,
        Some(label.clone()),
        SnapshotTrigger::AgentRun,
    )?;
    if json {
        println!(
            "{}",
            serde_json::json!({"event": event, "label": label, "snapshot": id_hex(&snapshot)})
        );
    } else {
        println!("Sealed {} {}", &id_hex(&snapshot)[..12], label);
    }
    Ok(())
}

fn hook_value(explicit: Option<String>, environment: &str) -> anyhow::Result<Option<String>> {
    let value = explicit.or_else(|| {
        std::env::var_os(environment).map(|value| value.to_string_lossy().into_owned())
    });
    if let Some(value) = &value {
        anyhow::ensure!(!value.is_empty(), "hook metadata cannot be empty");
        anyhow::ensure!(value.len() <= 128, "hook metadata is limited to 128 bytes");
        anyhow::ensure!(
            !value.chars().any(char::is_control),
            "hook metadata cannot contain control characters"
        );
    }
    Ok(value)
}

fn exit_with_status(status: std::process::ExitStatus) -> ! {
    use std::os::unix::process::ExitStatusExt;

    let code = status
        .code()
        .unwrap_or_else(|| 128 + status.signal().unwrap_or(1));
    std::process::exit(code)
}

fn spawn_background_watcher(repository: &FurrowRepository, debounce_ms: u64) -> anyhow::Result<()> {
    let daemon_dir = repository.workspace_data_dir();
    std::fs::create_dir_all(&daemon_dir)?;
    let pid_path = daemon_dir.join("daemon.pid");
    if let Ok(pid) = std::fs::read_to_string(&pid_path) {
        if let Ok(pid) = pid.trim().parse::<i32>() {
            if unsafe { libc::kill(pid, 0) } == 0 {
                println!("Watcher already running with PID {pid}");
                return Ok(());
            }
        }
    }

    let log_path = daemon_dir.join("daemon.log");
    let stdout = std::fs::OpenOptions::new()
        .create(true)
        .append(true)
        .open(&log_path)?;
    let stderr = stdout.try_clone()?;
    let child = std::process::Command::new(std::env::current_exe()?)
        .arg("--repo")
        .arg(repository.root())
        .arg("__daemon")
        .arg("--debounce-ms")
        .arg(debounce_ms.to_string())
        .stdin(std::process::Stdio::null())
        .stdout(stdout)
        .stderr(stderr)
        .spawn()?;
    std::fs::write(&pid_path, format!("{}\n", child.id()))?;
    println!("Watcher started with PID {}", child.id());
    println!("Log {}", log_path.display());
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::{parse_byte_size, split_clone_url};

    #[test]
    fn parses_human_budget_sizes_without_ambiguous_fractions() {
        assert_eq!(parse_byte_size("512MiB").unwrap(), 512 * 1024 * 1024);
        assert_eq!(parse_byte_size("20 GiB").unwrap(), 20 * 1024 * 1024 * 1024);
        assert_eq!(parse_byte_size("1000").unwrap(), 1000);
        assert!(parse_byte_size("1.5GiB").is_err());
        assert!(parse_byte_size("lots").is_err());
    }

    #[test]
    fn clone_urls_separate_transport_from_workspace_name() {
        let (transport, namespace) = split_clone_url("s3://my-bucket/furrow/myproject").unwrap();
        assert_eq!(transport, "s3://my-bucket/furrow");
        assert_eq!(namespace, "myproject");
        assert!(split_clone_url("s3://my-bucket").is_err());
        assert!(split_clone_url("s3://my-bucket/furrow/../project").is_err());
        assert!(split_clone_url("https://example.com/project").is_err());
        assert_eq!(
            split_clone_url("ssh://developer@laptop-a/myproject").unwrap(),
            (
                "ssh://developer@laptop-a".to_owned(),
                "myproject".to_owned()
            )
        );
    }
}
