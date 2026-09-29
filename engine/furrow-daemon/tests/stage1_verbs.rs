//! Stage 1 contract 5.2: the five sync verbs over the daemon's socket.

use furrow::exchange::frame;
use furrow::model::{id_hex, parse_id};
use furrow_daemon::{Config, Server, Stop};
use serde_json::{json, Value};
use std::collections::BTreeMap;
use std::fs;
use std::io::{BufRead, BufReader, Write};
use std::os::fd::AsRawFd;
use std::os::unix::fs::{symlink, MetadataExt};
use std::os::unix::net::UnixStream;
use std::path::{Path, PathBuf};
use std::thread::JoinHandle;
use std::time::Duration;
use tempfile::TempDir;

const LEDGER: &str = "0123456789abcdef";
const SMALL_FRAME: usize = 300 * 1024;
// Distinctive on purpose: a repeated byte would match by accident in a scan.
const CELL_KEY: &str = "c3a91f0e5b7d2648a0f1e3d5c7b9a1f2e4d6c8b0a2f4e6d8c0b1a3f5e7d9c1b3";
const DEDUP: &str = "9f8e7d6c5b4a39281706f5e4d3c2b1a0918273645546372819a0b1c2d3e4f506";

fn cell_args() -> Value {
    json!({"cell_key": CELL_KEY, "dedup": DEDUP})
}

struct Daemon {
    dir: TempDir,
    stop: Stop,
    thread: Option<JoinHandle<()>>,
}

impl Daemon {
    fn start() -> Self {
        let dir = TempDir::new().unwrap();
        let config = Config {
            socket: dir.path().join("run").join("furrow.sock"),
            idle: Duration::from_secs(60),
        };
        let server = Server::bind(config).unwrap().expect("first daemon binds");
        let stop = server.stop_handle();
        let thread = std::thread::spawn(move || server.run().unwrap());
        Self {
            dir,
            stop,
            thread: Some(thread),
        }
    }

    fn connect(&self) -> Client {
        let stream = UnixStream::connect(self.dir.path().join("run").join("furrow.sock")).unwrap();
        Client {
            reader: BufReader::new(stream.try_clone().unwrap()),
            stream,
            next: 1,
        }
    }

    /// A store with a small tree, a larger random file and a symlink.
    fn store(&self, name: &str) -> Store {
        let root = self.dir.path().join(name);
        let tree = root.join("tree");
        fs::create_dir_all(tree.join("src")).unwrap();
        Store {
            data_dir: root.join("data"),
            tree,
            root,
        }
    }

    fn finish(mut self) {
        self.stop.stop();
        self.thread.take().unwrap().join().unwrap();
    }
}

struct Store {
    root: PathBuf,
    data_dir: PathBuf,
    tree: PathBuf,
}

impl Store {
    fn target(&self) -> Value {
        json!({"data_dir": self.data_dir, "tree": self.tree})
    }

    fn fill(&self) {
        fs::write(self.tree.join("src/main.rs"), b"fn main() {}\n").unwrap();
        fs::write(self.tree.join("big.bin"), pseudo_random(1 << 20)).unwrap();
        symlink("src/main.rs", self.tree.join("link")).unwrap();
    }
}

fn pseudo_random(len: usize) -> Vec<u8> {
    let mut state = 0x9e37_79b9_7f4a_7c15_u64;
    (0..len)
        .map(|_| {
            state = state
                .wrapping_mul(6364136223846793005)
                .wrapping_add(1442695040888963407);
            (state >> 33) as u8
        })
        .collect()
}

struct Client {
    stream: UnixStream,
    reader: BufReader<UnixStream>,
    next: u64,
}

impl Client {
    fn send(&mut self, verb: &str, store: &Store, args: Value) {
        let id = self.next;
        self.next += 1;
        let request =
            json!({"V": 1, "id": id, "verb": verb, "target": store.target(), "args": args});
        writeln!(self.stream, "{request}").unwrap();
    }

    fn call(&mut self, verb: &str, store: &Store, args: Value) -> Value {
        self.send(verb, store, args);
        let mut line = String::new();
        self.reader.read_line(&mut line).unwrap();
        serde_json::from_str(&line).unwrap()
    }

    fn ok(&mut self, verb: &str, store: &Store, args: Value) -> Value {
        let response = self.call(verb, store, args);
        assert!(response.get("err").is_none(), "{response}");
        response["ok"].clone()
    }

    fn seal(&mut self, store: &Store) -> String {
        let sealed = self.ok("seal", store, json!({}));
        sealed["snapshot"].as_str().unwrap().to_owned()
    }

    fn export(&mut self, store: &Store, head: &str, outbox: &Path) -> Value {
        let mut args = cell_args();
        args["head"] = json!(head);
        args["outbox"] = json!(outbox);
        args["ledger"] = json!(LEDGER);
        args["max_frame"] = json!(SMALL_FRAME);
        self.ok("export", store, args)
    }
}

fn frame_files(report: &Value) -> Vec<PathBuf> {
    let frames = report["frames"].as_array().unwrap();
    frames
        .iter()
        .map(|f| PathBuf::from(f["path"].as_str().unwrap()))
        .collect()
}

/// Each object of the frames as its own file named by its rid, as a fetcher
/// would leave them.
fn deliver(frames: &[PathBuf], inbox: &Path) {
    fs::create_dir_all(inbox).unwrap();
    for path in frames {
        let bytes = fs::read(path).unwrap();
        let (_, objects) = frame::decode(&bytes).unwrap();
        for object in objects {
            fs::write(inbox.join(id_hex(&object.rid)), object.bytes).unwrap();
        }
    }
}

/// Path, mode, link target or content digest and mtime of every entry.
fn dump(root: &Path) -> BTreeMap<String, String> {
    let mut nodes = BTreeMap::new();
    let mut stack = vec![root.to_owned()];
    while let Some(dir) = stack.pop() {
        for entry in fs::read_dir(&dir).unwrap() {
            let path = entry.unwrap().path();
            let meta = fs::symlink_metadata(&path).unwrap();
            let body = if meta.is_dir() {
                stack.push(path.clone());
                String::new()
            } else if meta.file_type().is_symlink() {
                fs::read_link(&path).unwrap().display().to_string()
            } else {
                blake3::hash(&fs::read(&path).unwrap()).to_hex().to_string()
            };
            let name = path.strip_prefix(root).unwrap().display().to_string();
            if name.starts_with(".furrow") {
                // Each store's own attachment marker is device-local, never sealed.
                continue;
            }
            nodes.insert(name, format!("{:o} {body} {}", meta.mode(), meta.mtime()));
        }
    }
    nodes
}

#[test]
fn export_published_want_import_materialize_over_socket() {
    let daemon = Daemon::start();
    let (a, b) = (daemon.store("a"), daemon.store("b"));
    a.fill();
    let mut client = daemon.connect();
    let head = client.seal(&a);

    let outbox = a.root.join("outbox");
    let report = client.export(&a, &head, &outbox);
    let frames = frame_files(&report);
    assert!(
        frames.len() > 3,
        "a 1 MiB file needs several 300 KiB frames"
    );
    assert_eq!(report["head_rid"].as_str().unwrap().len(), 64);

    let recorded = client.ok("published", &a, json!({"ledger": LEDGER, "frames": frames}));
    assert_eq!(recorded["recorded"], report["objects"]);
    assert!(frames.iter().all(|path| !path.exists()));
    let again = client.export(&a, &head, &outbox);
    assert_eq!(again["frames"], json!([]), "everything is now published");

    let mut want_args = cell_args();
    want_args["head"] = json!(head);
    let first = client.ok("want", &b, want_args.clone());
    assert_eq!(
        first["want"].as_array().unwrap().len(),
        1,
        "only the snapshot"
    );

    // The frames were deleted by `published`, so export afresh to a new ledger.
    let mut resend = cell_args();
    resend.as_object_mut().unwrap().extend([
        ("head".into(), json!(head)),
        ("outbox".into(), json!(a.root.join("resend"))),
        ("ledger".into(), json!("fedcba9876543210")),
    ]);
    let resent = client.ok("export", &a, resend);
    let inbox = b.root.join("inbox");
    deliver(&frame_files(&resent), &inbox);

    let mut import = want_args.clone();
    import["inbox"] = json!(inbox);
    let imported = client.ok("import", &b, import);
    assert_eq!(imported["imported"], resent["objects"]);
    assert_eq!(client.ok("want", &b, want_args)["want"], json!([]));

    let restored = client.ok("materialize", &b, json!({"head": head}));
    assert_eq!(restored["snapshot"], head);
    assert_eq!(dump(&a.tree), dump(&b.tree));
    parse_id(&head).unwrap();
    daemon.finish();
}

#[test]
fn export_matches_the_command_line_path() {
    let daemon = Daemon::start();
    let a = daemon.store("a");
    a.fill();
    let mut client = daemon.connect();
    let head = client.seal(&a);

    let over_socket = client.export(&a, &head, &a.root.join("socket"));
    let keys = furrow::exchange::keys::keys_from(|name| {
        Some(
            if name == "FURROW_CELL_KEY" {
                CELL_KEY
            } else {
                DEDUP
            }
            .to_owned(),
        )
    })
    .unwrap();
    let direct = furrow::exchange::ops::export(
        &a.data_dir,
        &keys,
        &head,
        LEDGER,
        &a.root.join("direct"),
        SMALL_FRAME,
    )
    .unwrap();

    let names = |paths: Vec<PathBuf>| -> Vec<(String, Vec<u8>)> {
        paths
            .iter()
            .map(|p| {
                (
                    p.file_name().unwrap().to_string_lossy().into_owned(),
                    fs::read(p).unwrap(),
                )
            })
            .collect()
    };
    assert_eq!(
        names(frame_files(&over_socket)),
        names(frame_files(&direct))
    );
    assert_eq!(over_socket["head_rid"], direct["head_rid"]);
    assert_eq!(over_socket["bytes"], direct["bytes"]);
    daemon.finish();
}

#[test]
fn two_stores_export_in_parallel() {
    let daemon = Daemon::start();
    let threads: Vec<_> = ["a", "b"]
        .into_iter()
        .map(|name| {
            let store = daemon.store(name);
            store.fill();
            let mut client = daemon.connect();
            std::thread::spawn(move || {
                let head = client.seal(&store);
                let report = client.export(&store, &head, &store.root.join("outbox"));
                (
                    report["objects"].as_u64().unwrap(),
                    frame_files(&report).len(),
                )
            })
        })
        .collect();
    for thread in threads {
        let (objects, frames) = thread.join().unwrap();
        assert!(objects > 0 && frames > 3);
    }
    daemon.finish();
}

#[test]
fn a_client_leaving_mid_export_keeps_the_ledger_consistent() {
    let daemon = Daemon::start();
    let a = daemon.store("a");
    a.fill();
    let mut client = daemon.connect();
    let head = client.seal(&a);
    let outbox = a.root.join("outbox");

    let mut args = cell_args();
    args.as_object_mut().unwrap().extend([
        ("head".into(), json!(head)),
        ("outbox".into(), json!(outbox)),
        ("ledger".into(), json!(LEDGER)),
        ("max_frame".into(), json!(SMALL_FRAME)),
    ]);
    let mut doomed = daemon.connect();
    doomed.send("export", &a, args);
    drop(doomed);

    // The export runs to the end on the store's writer; nothing was marked
    // published, so the next export sends every frame again, identically.
    let first = client.export(&a, &head, &outbox);
    let ledger = a.data_dir.join(format!("published.{LEDGER}"));
    assert!(!ledger.exists() || fs::read_to_string(&ledger).unwrap().is_empty());
    let second = client.export(&a, &head, &outbox);
    assert_eq!(first, second);
    assert!(frame_files(&first).iter().all(|p| p.exists()));
    daemon.finish();
}

/// Points stdout and stderr at a file for the life of the guard.
struct Capture {
    saved: [i32; 2],
    file: PathBuf,
}

impl Capture {
    fn begin(file: PathBuf) -> Self {
        let target = fs::File::create(&file).unwrap();
        let redirect = |fd: i32| unsafe {
            let saved = libc::dup(fd);
            libc::dup2(target.as_raw_fd(), fd);
            saved
        };
        Self {
            saved: [redirect(1), redirect(2)],
            file,
        }
    }

    fn end(self) -> Vec<u8> {
        for (fd, saved) in [1, 2].into_iter().zip(self.saved) {
            unsafe {
                libc::dup2(saved, fd);
                libc::close(saved);
            }
        }
        fs::read(&self.file).unwrap()
    }
}

fn contains(haystack: &[u8], needle: &str) -> bool {
    haystack
        .windows(needle.len())
        .any(|w| w == needle.as_bytes())
}

#[test]
fn keys_absent_from_logs() {
    let daemon = Daemon::start();
    let a = daemon.store("a");
    a.fill();
    let mut client = daemon.connect();
    let head = client.seal(&a);

    let capture = Capture::begin(daemon.dir.path().join("captured.log"));
    let report = client.export(&a, &head, &a.root.join("outbox"));
    let mut bad = cell_args();
    bad["head"] = json!(head);
    bad["cell_key"] = json!(&CELL_KEY[..40]);
    let refusal = client.call("want", &a, bad);
    let wrong_type = client.call(
        "want",
        &a,
        json!({"head": head, "cell_key": 12345, "dedup": DEDUP}),
    );
    let logged = capture.end();

    for secret in [CELL_KEY, DEDUP] {
        assert!(!contains(&logged, secret), "daemon output holds a key");
        assert!(!refusal.to_string().contains(secret));
        assert!(!wrong_type.to_string().contains(secret));
        assert!(!report.to_string().contains(secret));
    }
    assert!(!refusal.to_string().contains(&CELL_KEY[..40]));
    assert!(refusal["err"].as_str().unwrap().contains("cell_key"));
    // No key reaches the disk either: not in the store, the outbox or the ledger.
    for file in walk(daemon.dir.path()) {
        let bytes = fs::read(&file).unwrap();
        for secret in [CELL_KEY, DEDUP] {
            assert!(!contains(&bytes, secret), "{} holds a key", file.display());
            assert!(!contains(
                &bytes,
                &String::from_utf8_lossy(&hex::decode(secret).unwrap())
            ));
        }
    }
    daemon.finish();
}

fn walk(dir: &Path) -> Vec<PathBuf> {
    let mut files = Vec::new();
    let mut stack = vec![dir.to_owned()];
    while let Some(next) = stack.pop() {
        for entry in fs::read_dir(&next).unwrap() {
            let path = entry.unwrap().path();
            let meta = fs::symlink_metadata(&path).unwrap();
            if meta.is_dir() {
                stack.push(path);
            } else if meta.is_file() {
                files.push(path);
            }
        }
    }
    files
}
