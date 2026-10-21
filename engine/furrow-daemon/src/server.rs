//! The socket, the idle clock and shutdown.

use crate::router::Router;
use crate::verbs;
use crate::wire::{self, Request, Response};
use anyhow::Context;
use fs2::FileExt;
use std::fs::{self, File, OpenOptions};
use std::io::BufReader;
use std::os::fd::AsRawFd;
use std::os::unix::fs::PermissionsExt;
use std::os::unix::net::{UnixListener, UnixStream};
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicBool, AtomicUsize, Ordering};
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

const TICK: Duration = Duration::from_millis(50);

#[derive(Debug, Clone)]
pub struct Config {
    pub socket: PathBuf,
    /// The daemon exits after this long with no connection and no request.
    pub idle: Duration,
}

/// Asks a running server to finish and return.
#[derive(Clone, Default)]
pub struct Stop(Arc<AtomicBool>);

impl Stop {
    pub fn stop(&self) {
        self.0.store(true, Ordering::SeqCst);
    }

    fn requested(&self) -> bool {
        self.0.load(Ordering::SeqCst) || crate::signals::received()
    }
}

/// Connections open now and when the last request finished.
struct Activity {
    open: AtomicUsize,
    last: Mutex<Instant>,
}

impl Activity {
    fn touch(&self) {
        *self.last.lock().expect("activity lock") = Instant::now();
    }

    fn idle_for(&self) -> Option<Duration> {
        let quiet = self.open.load(Ordering::SeqCst) == 0;
        quiet.then(|| self.last.lock().expect("activity lock").elapsed())
    }
}

pub struct Server {
    listener: UnixListener,
    config: Config,
    stop: Stop,
    router: Arc<Router>,
    activity: Arc<Activity>,
    // Held for the daemon's life: one daemon per socket.
    _lock: File,
}

impl Server {
    /// Takes the socket's lock and binds it, owner-only. `Ok(None)` means
    /// another daemon already serves this socket.
    pub fn bind(config: Config) -> anyhow::Result<Option<Self>> {
        let dir = config.socket.parent().context("socket has no directory")?;
        fs::create_dir_all(dir)?;
        fs::set_permissions(dir, fs::Permissions::from_mode(0o700))?;
        let Some(lock) = take_lock(&config.socket)? else {
            return Ok(None);
        };
        let _ = fs::remove_file(&config.socket);
        let listener = UnixListener::bind(&config.socket).context("bind socket")?;
        fs::set_permissions(&config.socket, fs::Permissions::from_mode(0o600))?;
        listener.set_nonblocking(true)?;
        Ok(Some(Self {
            listener,
            config,
            stop: Stop::default(),
            router: Arc::new(Router::new()),
            activity: Arc::new(Activity {
                open: AtomicUsize::new(0),
                last: Mutex::new(Instant::now()),
            }),
            _lock: lock,
        }))
    }

    pub fn stop_handle(&self) -> Stop {
        self.stop.clone()
    }

    /// Serves until stopped, signalled or idle, then finishes in-flight
    /// requests, closes every repository and removes the socket.
    pub fn run(self) -> anyhow::Result<()> {
        while !self.should_exit() {
            self.wait_for_client();
            self.accept_pending();
        }
        self.router.close();
        let _ = fs::remove_file(&self.config.socket);
        Ok(())
    }

    fn should_exit(&self) -> bool {
        let idle = self.activity.idle_for();
        self.stop.requested() || idle.is_some_and(|quiet| quiet >= self.config.idle)
    }

    /// Sleeps until a client connects or a tick passes, whichever is first, so
    /// a connection is accepted at once and the exit conditions are still
    /// looked at every tick.
    fn wait_for_client(&self) {
        let mut waiting = libc::pollfd {
            fd: self.listener.as_raw_fd(),
            events: libc::POLLIN,
            revents: 0,
        };
        // SAFETY: one valid pollfd, and the descriptor outlives the call.
        unsafe { libc::poll(&mut waiting, 1, TICK.as_millis() as libc::c_int) };
    }

    fn accept_pending(&self) {
        while let Ok((stream, _)) = self.listener.accept() {
            self.activity.open.fetch_add(1, Ordering::SeqCst);
            let session = Session {
                router: Arc::clone(&self.router),
                activity: Arc::clone(&self.activity),
                stop: self.stop.clone(),
            };
            std::thread::spawn(move || session.serve(stream));
        }
    }
}

fn take_lock(socket: &Path) -> anyhow::Result<Option<File>> {
    let mut name = socket.as_os_str().to_owned();
    name.push(".lock");
    let file = OpenOptions::new()
        .create(true)
        .truncate(false)
        .write(true)
        .open(PathBuf::from(name))?;
    Ok(file.try_lock_exclusive().is_ok().then_some(file))
}

/// One client connection: requests in, answers out, in order.
struct Session {
    router: Arc<Router>,
    activity: Arc<Activity>,
    stop: Stop,
}

impl Session {
    fn serve(self, stream: UnixStream) {
        let _ = stream.set_nonblocking(false);
        let _ = self.exchange(&stream);
        self.activity.touch();
        self.activity.open.fetch_sub(1, Ordering::SeqCst);
    }

    /// Ends when the client hangs up or a write fails; a client that dies
    /// mid-request leaves the request to finish and its answer unread.
    fn exchange(&self, stream: &UnixStream) -> std::io::Result<()> {
        let mut reader = BufReader::new(stream);
        let mut writer = stream;
        while let Some(line) = wire::read_line(&mut reader)? {
            let response = self.answer(&line);
            self.activity.touch();
            wire::write_response(&mut writer, &response)?;
        }
        Ok(())
    }

    fn answer(&self, line: &str) -> Response {
        match wire::parse_request(line) {
            Ok(request) => self.respond(request),
            Err(refusal) => refusal,
        }
    }

    fn respond(&self, request: Request) -> Response {
        let id = request.id;
        match self.dispatch(request) {
            Ok(value) => Response::ok(id, value),
            Err(error) => Response::err(id, format!("{error:#}")),
        }
    }

    /// What a client needs to know before trusting this daemon: who it is. The
    /// engine name is the running binary's file name, which the client's
    /// installer already makes unique per build, so an old daemon left over
    /// from an upgrade is recognised without comparing version numbers.
    fn health(&self) -> serde_json::Value {
        let mut health = self.router.health();
        health["version"] = env!("CARGO_PKG_VERSION").into();
        health["engine"] = engine_name().into();
        health
    }

    /// The verbs about the daemon itself, which need no store.
    fn control(&self, verb: &str) -> Option<serde_json::Value> {
        match verb {
            "health" => Some(self.health()),
            "shutdown" => {
                self.stop.stop();
                Some(serde_json::json!({"stopping": true}))
            }
            _ => None,
        }
    }

    fn dispatch(&self, request: Request) -> anyhow::Result<serde_json::Value> {
        if let Some(answer) = self.control(&request.verb) {
            return Ok(answer);
        }
        let verb = verbs::lookup(&request.verb)
            .with_context(|| format!("unknown verb {:?}", request.verb))?;
        let target = request.target.context("verb needs a target")?;
        self.router.submit(target, verb, request.args)
    }
}

/// The file name of the running engine, empty when the OS will not say.
fn engine_name() -> String {
    std::env::current_exe()
        .ok()
        .and_then(|path| {
            path.file_name()
                .map(|name| name.to_string_lossy().into_owned())
        })
        .unwrap_or_default()
}
