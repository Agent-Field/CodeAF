//! Process isolation drivers for parallel workspace universes.

use anyhow::{bail, Context};
use serde::{Deserialize, Serialize};
use std::ffi::OsString;
use std::path::{Path, PathBuf};
#[cfg(target_os = "linux")]
use std::process::Stdio;
use std::process::{Command, ExitStatus};

#[derive(Clone, Copy, Debug, Deserialize, Eq, PartialEq, Serialize)]
#[serde(rename_all = "kebab-case")]
pub enum ExecutionDriver {
    LinuxMountNamespace,
    SiblingDirectory,
}

impl std::fmt::Display for ExecutionDriver {
    fn fmt(&self, formatter: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::LinuxMountNamespace => formatter.write_str("linux-mount-namespace"),
            Self::SiblingDirectory => formatter.write_str("sibling-directory"),
        }
    }
}

#[derive(Clone, Debug, Deserialize, Eq, PartialEq, Serialize)]
pub struct DriverSelection {
    pub driver: ExecutionDriver,
    pub same_canonical_path: bool,
    pub reason: String,
}

/// Select the strongest locally available isolation driver.
pub fn select_driver() -> DriverSelection {
    #[cfg(target_os = "linux")]
    {
        if std::env::var_os("FURROW_DISABLE_NAMESPACES").is_some() {
            return DriverSelection {
                driver: ExecutionDriver::SiblingDirectory,
                same_canonical_path: false,
                reason: "mount namespaces disabled by FURROW_DISABLE_NAMESPACES".to_owned(),
            };
        }
        match probe_linux_namespace() {
            Ok(()) => DriverSelection {
                driver: ExecutionDriver::LinuxMountNamespace,
                same_canonical_path: true,
                reason: "unprivileged user and mount namespaces are available".to_owned(),
            },
            Err(error) => DriverSelection {
                driver: ExecutionDriver::SiblingDirectory,
                same_canonical_path: false,
                reason: format!("mount namespace unavailable: {error}"),
            },
        }
    }
    #[cfg(not(target_os = "linux"))]
    {
        DriverSelection {
            driver: ExecutionDriver::SiblingDirectory,
            same_canonical_path: false,
            reason: "this platform has no Linux mount namespaces".to_owned(),
        }
    }
}

#[cfg(target_os = "linux")]
fn probe_linux_namespace() -> anyhow::Result<()> {
    let executable = std::env::current_exe().context("resolve furrow executable")?;
    let output = Command::new(executable)
        .arg("__namespace-probe")
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::piped())
        .output()
        .context("start namespace capability probe")?;
    if output.status.success() {
        return Ok(());
    }
    let detail = String::from_utf8_lossy(&output.stderr);
    let detail = detail.lines().last().unwrap_or("probe failed").trim();
    bail!("{detail}")
}

/// Enter a private user/mount namespace, overlay `source` at `target`, and exec.
#[cfg(target_os = "linux")]
pub fn exec_linux_namespace(
    source: &Path,
    target: &Path,
    command: &[OsString],
) -> anyhow::Result<()> {
    use std::os::unix::ffi::OsStrExt;
    use std::os::unix::process::CommandExt;

    anyhow::ensure!(!command.is_empty(), "namespace helper requires a command");
    let source = source
        .canonicalize()
        .with_context(|| format!("resolve universe {}", source.display()))?;
    let target = target
        .canonicalize()
        .with_context(|| format!("resolve canonical workspace {}", target.display()))?;

    let result = unsafe { libc::unshare(libc::CLONE_NEWUSER | libc::CLONE_NEWNS) };
    if result != 0 {
        return Err(std::io::Error::last_os_error()).context("create user/mount namespace");
    }
    configure_user_namespace()?;

    let root = std::ffi::CString::new("/")?;
    let result = unsafe {
        libc::mount(
            std::ptr::null(),
            root.as_ptr(),
            std::ptr::null(),
            libc::MS_REC | libc::MS_PRIVATE,
            std::ptr::null(),
        )
    };
    if result != 0 {
        return Err(std::io::Error::last_os_error()).context("make mounts private");
    }

    let source_c = std::ffi::CString::new(source.as_os_str().as_bytes())?;
    let target_c = std::ffi::CString::new(target.as_os_str().as_bytes())?;
    let result = unsafe {
        libc::mount(
            source_c.as_ptr(),
            target_c.as_ptr(),
            std::ptr::null(),
            libc::MS_BIND | libc::MS_REC,
            std::ptr::null(),
        )
    };
    if result != 0 {
        return Err(std::io::Error::last_os_error()).with_context(|| {
            format!(
                "bind universe {} over {}",
                source.display(),
                target.display()
            )
        });
    }

    let (program, arguments) = command.split_first().expect("checked above");
    let error = Command::new(program)
        .args(arguments)
        .current_dir(&target)
        .exec();
    Err(error).with_context(|| format!("exec {:?}", program))
}

#[cfg(target_os = "linux")]
fn configure_user_namespace() -> anyhow::Result<()> {
    let uid = unsafe { libc::getuid() };
    let gid = unsafe { libc::getgid() };
    if let Err(error) = std::fs::write("/proc/self/setgroups", b"deny\n") {
        if error.kind() != std::io::ErrorKind::NotFound {
            return Err(error).context("disable setgroups in user namespace");
        }
    }
    std::fs::write("/proc/self/uid_map", format!("0 {uid} 1\n")).context("map namespace uid")?;
    std::fs::write("/proc/self/gid_map", format!("0 {gid} 1\n")).context("map namespace gid")?;
    Ok(())
}

#[cfg(target_os = "linux")]
pub fn probe_namespace_helper() -> anyhow::Result<()> {
    use std::os::unix::ffi::OsStrExt;

    let mountpoint = tempfile::tempdir().context("create namespace probe mountpoint")?;
    let result = unsafe { libc::unshare(libc::CLONE_NEWUSER | libc::CLONE_NEWNS) };
    if result != 0 {
        return Err(std::io::Error::last_os_error()).context("create user/mount namespace");
    }
    configure_user_namespace()?;
    let root = std::ffi::CString::new("/")?;
    let result = unsafe {
        libc::mount(
            std::ptr::null(),
            root.as_ptr(),
            std::ptr::null(),
            libc::MS_REC | libc::MS_PRIVATE,
            std::ptr::null(),
        )
    };
    if result != 0 {
        return Err(std::io::Error::last_os_error()).context("make probe mounts private");
    }
    let path = std::ffi::CString::new(mountpoint.path().as_os_str().as_bytes())?;
    let result = unsafe {
        libc::mount(
            path.as_ptr(),
            path.as_ptr(),
            std::ptr::null(),
            libc::MS_BIND,
            std::ptr::null(),
        )
    };
    if result != 0 {
        return Err(std::io::Error::last_os_error()).context("test private bind mount");
    }
    let result = unsafe { libc::umount2(path.as_ptr(), libc::MNT_DETACH) };
    if result != 0 {
        return Err(std::io::Error::last_os_error()).context("unmount namespace probe");
    }
    Ok(())
}

#[cfg(not(target_os = "linux"))]
pub fn probe_namespace_helper() -> anyhow::Result<()> {
    bail!("Linux namespaces are not supported on this platform")
}

#[cfg(not(target_os = "linux"))]
pub fn exec_linux_namespace(
    _source: &Path,
    _target: &Path,
    _command: &[OsString],
) -> anyhow::Result<()> {
    bail!("Linux namespaces are not supported on this platform")
}

pub struct UniverseCommand<'a> {
    pub driver: ExecutionDriver,
    pub executable: &'a Path,
    pub source: &'a Path,
    pub canonical_target: &'a Path,
    pub command: &'a [OsString],
}

impl UniverseCommand<'_> {
    pub fn command(&self) -> anyhow::Result<Command> {
        let (program, arguments) = self
            .command
            .split_first()
            .context("universe requires a command")?;
        let child = match self.driver {
            ExecutionDriver::LinuxMountNamespace => {
                let mut child = Command::new(self.executable);
                child
                    .arg("__exec-namespace")
                    .arg("--source")
                    .arg(self.source)
                    .arg("--target")
                    .arg(self.canonical_target)
                    .arg("--")
                    .args(self.command);
                child
            }
            ExecutionDriver::SiblingDirectory => {
                let mut child = Command::new(program);
                child.args(arguments).current_dir(self.source);
                child
            }
        };
        Ok(child)
    }
}

#[derive(Clone, Debug, Serialize)]
pub struct UniversePlan {
    pub index: usize,
    pub fork_id: String,
    pub name: String,
    pub destination: PathBuf,
    pub process_workdir: PathBuf,
    pub port: u16,
    pub base_snapshot: String,
    pub logical_bytes: u64,
    pub projected_fork_ms: u64,
}

#[derive(Clone, Debug, Serialize)]
pub struct ExecPlan {
    pub driver: DriverSelection,
    pub canonical_workdir: PathBuf,
    pub universes: Vec<UniversePlan>,
}

pub fn exit_code(status: ExitStatus) -> i32 {
    status.code().unwrap_or(1)
}
