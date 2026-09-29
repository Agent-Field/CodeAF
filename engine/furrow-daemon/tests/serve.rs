use furrow_daemon::{Config, Server, Stop};
use serde_json::{json, Value};
use std::fs;
use std::io::{BufRead, BufReader, Write};
use std::os::unix::fs::PermissionsExt;
use std::os::unix::net::UnixStream;
use std::path::{Path, PathBuf};
use std::thread::JoinHandle;
use std::time::Duration;
use tempfile::TempDir;

struct Daemon {
    dir: TempDir,
    stop: Stop,
    thread: Option<JoinHandle<()>>,
}

impl Daemon {
    fn start(idle: Duration) -> Self {
        let dir = TempDir::new().unwrap();
        let config = Config {
            socket: dir.path().join("run").join("furrow.sock"),
            idle,
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

    fn socket(&self) -> PathBuf {
        self.dir.path().join("run").join("furrow.sock")
    }

    fn connect(&self) -> Client {
        let stream = UnixStream::connect(self.socket()).unwrap();
        Client {
            reader: BufReader::new(stream.try_clone().unwrap()),
            stream,
            next: 1,
        }
    }

    /// A store and a tree that belong together, under this daemon's directory.
    fn store(&self, name: &str) -> Store {
        let tree = self.dir.path().join(name).join("tree");
        fs::create_dir_all(&tree).unwrap();
        Store {
            data_dir: self.dir.path().join(name).join("data"),
            tree,
        }
    }

    fn finish(mut self) {
        self.stop.stop();
        self.thread.take().unwrap().join().unwrap();
    }
}

struct Store {
    data_dir: PathBuf,
    tree: PathBuf,
}

impl Store {
    fn target(&self) -> Value {
        json!({"data_dir": self.data_dir, "tree": self.tree})
    }

    fn write(&self, name: &str, body: &str) {
        fs::write(self.tree.join(name), body).unwrap();
    }

    fn read(&self, name: &str) -> String {
        fs::read_to_string(self.tree.join(name)).unwrap()
    }
}

struct Client {
    stream: UnixStream,
    reader: BufReader<UnixStream>,
    next: u64,
}

impl Client {
    fn send(&mut self, verb: &str, target: Option<Value>, args: Value) -> u64 {
        let id = self.next;
        self.next += 1;
        let request = json!({"V": 1, "id": id, "verb": verb, "target": target, "args": args});
        writeln!(self.stream, "{request}").unwrap();
        id
    }

    fn recv(&mut self) -> Value {
        let mut line = String::new();
        self.reader.read_line(&mut line).unwrap();
        serde_json::from_str(&line).unwrap()
    }

    fn call(&mut self, verb: &str, store: &Store, args: Value) -> Value {
        let id = self.send(verb, Some(store.target()), args);
        let response = self.recv();
        assert_eq!(response["id"], id);
        assert_eq!(response["V"], 1);
        response
    }

    fn ok(&mut self, verb: &str, store: &Store, args: Value) -> Value {
        let response = self.call(verb, store, args);
        assert!(response.get("err").is_none(), "{response}");
        response["ok"].clone()
    }
}

fn snapshot(sealed: &Value) -> String {
    sealed["snapshot"].as_str().unwrap().to_owned()
}

#[test]
fn seal_and_restore_round_trip() {
    let daemon = Daemon::start(Duration::from_secs(60));
    let store = daemon.store("a");
    let mut client = daemon.connect();

    store.write("f.txt", "one");
    let first = client.ok("seal", &store, json!({"turn": "t1"}));
    assert_eq!(first["label"], "hook turn-end agent=agent turn=t1");
    store.write("f.txt", "two");
    store.write("g.txt", "new");
    let second = client.ok(
        "seal",
        &store,
        json!({"turn": "t2", "changed": ["f.txt", "g.txt"]}),
    );
    assert_ne!(snapshot(&first), snapshot(&second));

    let restored = client.ok("restore", &store, json!({"snapshot": snapshot(&first)}));
    assert!(restored["changes"].as_u64().unwrap() >= 1);
    assert_eq!(store.read("f.txt"), "one");
    assert!(!store.tree.join("g.txt").exists());

    let log = client.ok("log", &store, json!({"limit": 10}));
    assert!(log.as_array().unwrap().len() >= 3);
    let capture = client.ok("snap", &store, json!({"message": "capture"}));
    assert_eq!(snapshot(&capture).len(), 64);
    daemon.finish();
}

#[test]
fn health_and_refusals_keep_the_connection() {
    let daemon = Daemon::start(Duration::from_secs(60));
    let store = daemon.store("a");
    let mut client = daemon.connect();

    let id = client.send("health", None, json!({}));
    let health = client.recv();
    assert_eq!(health["id"], id);
    assert_eq!(health["ok"]["stores"], 0);

    assert!(client.call("no-such-verb", &store, json!({}))["err"].is_string());
    let restore = client.call("restore", &store, json!({"snapshot": "0".repeat(64)}));
    assert!(restore["err"].is_string());
    writeln!(client.stream, "not json").unwrap();
    assert!(client.recv()["err"]
        .as_str()
        .unwrap()
        .contains("bad request"));
    writeln!(client.stream, r#"{{"V":9,"id":5,"verb":"health"}}"#).unwrap();
    let refused = client.recv();
    assert_eq!(refused["id"], 5);
    assert!(refused["err"].as_str().unwrap().contains("not supported"));

    store.write("f", "x");
    client.ok("seal", &store, json!({}));
    daemon.finish();
}

#[test]
fn stores_run_in_parallel_and_each_has_one_writer() {
    let daemon = Daemon::start(Duration::from_secs(60));
    let stores: Vec<Store> = (0..3).map(|i| daemon.store(&format!("s{i}"))).collect();
    let threads: Vec<_> = stores
        .iter()
        .map(|store| {
            let (mut client, target) = (daemon.connect(), store.target());
            let tree = store.tree.clone();
            std::thread::spawn(move || {
                let mut ids = Vec::new();
                for n in 0..6 {
                    fs::write(tree.join("f"), n.to_string()).unwrap();
                    let id =
                        client.send("seal", Some(target.clone()), json!({"turn": n.to_string()}));
                    let response = client.recv();
                    assert_eq!(response["id"], id);
                    ids.push(
                        response["ok"]["snapshot"]
                            .as_str()
                            .unwrap_or_else(|| panic!("{response}"))
                            .to_owned(),
                    );
                }
                ids
            })
        })
        .collect();
    for thread in threads {
        let ids = thread.join().unwrap();
        let mut unique = ids.clone();
        unique.sort();
        unique.dedup();
        assert_eq!(unique.len(), ids.len(), "each seal is its own snapshot");
    }
    let mut client = daemon.connect();
    client.send("health", None, json!({}));
    assert_eq!(client.recv()["ok"]["stores"], 3);
    daemon.finish();
}

#[test]
fn a_client_dying_mid_request_does_not_stop_the_store() {
    let daemon = Daemon::start(Duration::from_secs(60));
    let store = daemon.store("a");
    store.write("f", "one");

    let mut doomed = daemon.connect();
    doomed.send("seal", Some(store.target()), json!({"turn": "orphan"}));
    drop(doomed);
    let mut torn = UnixStream::connect(daemon.socket()).unwrap();
    torn.write_all(br#"{"V":1,"id":1,"verb":"se"#).unwrap();
    drop(torn);

    let mut client = daemon.connect();
    // The orphaned request runs to completion though nobody reads its answer.
    let deadline = std::time::Instant::now() + Duration::from_secs(10);
    loop {
        let log = client.call("log", &store, json!({}));
        let sealed = log["ok"]
            .as_array()
            .is_some_and(|entries| entries.len() == 1);
        if sealed {
            break;
        }
        assert!(std::time::Instant::now() < deadline, "{log}");
        std::thread::sleep(Duration::from_millis(20));
    }
    store.write("f", "two");
    client.ok("seal", &store, json!({}));
    daemon.finish();
}

#[test]
fn the_socket_is_owner_only_and_single() {
    let daemon = Daemon::start(Duration::from_secs(60));
    let mode = fs::metadata(daemon.socket()).unwrap().permissions().mode();
    assert_eq!(mode & 0o777, 0o600);
    let config = Config {
        socket: daemon.socket(),
        idle: Duration::from_secs(1),
    };
    assert!(
        Server::bind(config).unwrap().is_none(),
        "a second daemon yields"
    );
    daemon.finish();
}

#[test]
fn an_idle_daemon_exits_and_removes_its_socket() {
    let daemon = Daemon::start(Duration::from_millis(200));
    let socket = daemon.socket();
    let mut daemon = daemon;
    daemon.thread.take().unwrap().join().unwrap();
    assert!(!Path::new(&socket).exists());
}

#[test]
fn a_shutdown_request_stops_the_daemon() {
    let mut daemon = Daemon::start(Duration::from_secs(60));
    let mut client = daemon.connect();
    client.send("shutdown", None, json!({}));
    assert_eq!(client.recv()["ok"]["stopping"], true);
    daemon.thread.take().unwrap().join().unwrap();
    assert!(!daemon.socket().exists());
}

#[test]
fn shutdown_finishes_the_request_in_flight() {
    let daemon = Daemon::start(Duration::from_secs(60));
    let store = daemon.store("a");
    store.write("f", "one");
    let mut client = daemon.connect();
    client.send("health", None, json!({}));
    client.recv();
    client.send("seal", Some(store.target()), json!({}));
    let socket = daemon.socket();
    daemon.finish();
    assert!(!socket.exists());
    let response = client.recv();
    assert!(response["ok"]["snapshot"].is_string() || response["err"].is_string());
}
