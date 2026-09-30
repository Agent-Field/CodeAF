//! Routes a request to its store's writer. Each store has one thread that owns
//! its open repository and runs that store's requests one at a time; requests
//! for different stores run in parallel on their own threads.

use crate::handle::Handle;
use crate::verbs::Verb;
use crate::wire::Target;
use anyhow::anyhow;
use serde_json::{json, Value};
use std::collections::HashMap;
use std::sync::mpsc::{channel, Sender};
use std::sync::Mutex;
use std::thread::JoinHandle;

struct Job {
    verb: &'static dyn Verb,
    args: Value,
    reply: Sender<anyhow::Result<Value>>,
}

struct Writer {
    jobs: Sender<Job>,
    thread: JoinHandle<()>,
}

impl Writer {
    fn spawn(target: Target) -> Self {
        let (jobs, inbox) = channel::<Job>();
        let thread = std::thread::spawn(move || {
            let mut handle = Handle::new(target);
            for job in inbox {
                let _ = job.reply.send(job.verb.run(&mut handle, job.args));
            }
        });
        Self { jobs, thread }
    }
}

/// `None` once closed: no new writer starts and every request is refused.
pub struct Router {
    writers: Mutex<Option<HashMap<Target, Writer>>>,
}

impl Router {
    /// The fields start as "no writers yet", so the blank state is exactly
    /// `new()`; the attribute keeps that fact one lint away from the gate.
    #[allow(clippy::new_without_default)]
    pub fn new() -> Self {
        Self {
            writers: Mutex::new(Some(HashMap::new())),
        }
    }

    /// Runs the verb on the target's writer and waits for its answer.
    pub fn submit(
        &self,
        target: Target,
        verb: &'static dyn Verb,
        args: Value,
    ) -> anyhow::Result<Value> {
        let (reply, answer) = channel();
        self.writer_inbox(target)?
            .send(Job { verb, args, reply })
            .map_err(|_| anyhow!("store writer stopped"))?;
        answer.recv().map_err(|_| anyhow!("store writer stopped"))?
    }

    fn writer_inbox(&self, target: Target) -> anyhow::Result<Sender<Job>> {
        let mut guard = self.writers.lock().expect("router lock");
        let writers = guard
            .as_mut()
            .ok_or_else(|| anyhow!("daemon is stopping"))?;
        let writer = writers
            .entry(target.clone())
            .or_insert_with(|| Writer::spawn(target));
        Ok(writer.jobs.clone())
    }

    pub fn health(&self) -> Value {
        let stores = self
            .writers
            .lock()
            .expect("router lock")
            .as_ref()
            .map(HashMap::len);
        json!({"pid": std::process::id(), "stores": stores.unwrap_or(0)})
    }

    /// Refuses new work, lets every in-flight request finish, and drops every
    /// open repository.
    pub fn close(&self) {
        let writers = self.writers.lock().expect("router lock").take();
        for (_, writer) in writers.unwrap_or_default() {
            drop(writer.jobs);
            let _ = writer.thread.join();
        }
    }
}
