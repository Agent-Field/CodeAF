//! Lightweight Model Context Protocol server over newline-delimited stdio.

use crate::model::{id_hex, SnapshotTrigger};
use crate::repository::AgitRepository;
use anyhow::Context;
use serde::Serialize;
use serde_json::{json, Map, Value};
use std::io::{self, BufRead, BufReader, BufWriter, Write};
use std::path::{Path, PathBuf};

const LATEST_PROTOCOL: &str = "2025-11-25";
const SUPPORTED_PROTOCOLS: &[&str] = &["2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"];
const MAX_REQUEST_BYTES: usize = 1024 * 1024;

#[derive(Clone, Copy, PartialEq, Eq)]
enum Lifecycle {
    AwaitInitialize,
    AwaitInitialized,
    Ready,
}

pub fn run(repository: AgitRepository) -> anyhow::Result<()> {
    run_with_io(
        repository,
        BufReader::new(io::stdin().lock()),
        BufWriter::new(io::stdout().lock()),
    )
}

fn run_with_io(
    mut repository: AgitRepository,
    mut input: impl BufRead,
    mut output: impl Write,
) -> anyhow::Result<()> {
    let mut lifecycle = Lifecycle::AwaitInitialize;
    loop {
        let message = match read_message(&mut input) {
            Ok(Some(message)) => message,
            Ok(None) => break,
            Err(error) => {
                write_message(
                    &mut output,
                    &rpc_error(Value::Null, -32700, &error.to_string()),
                )?;
                continue;
            }
        };
        if message.is_empty() {
            continue;
        }
        let request: Value = match serde_json::from_slice(&message) {
            Ok(request) => request,
            Err(error) => {
                write_message(
                    &mut output,
                    &rpc_error(Value::Null, -32700, &format!("parse error: {error}")),
                )?;
                continue;
            }
        };
        let Some(object) = request.as_object() else {
            write_message(
                &mut output,
                &rpc_error(Value::Null, -32600, "request must be a JSON object"),
            )?;
            continue;
        };
        let id = object.get("id").cloned();
        let Some(method) = object.get("method").and_then(Value::as_str) else {
            if let Some(id) = id {
                write_message(
                    &mut output,
                    &rpc_error(id, -32600, "request method must be a string"),
                )?;
            }
            continue;
        };
        if object.get("jsonrpc").and_then(Value::as_str) != Some("2.0") {
            if let Some(id) = id {
                write_message(
                    &mut output,
                    &rpc_error(id, -32600, "jsonrpc must equal 2.0"),
                )?;
            }
            continue;
        }

        if id.is_none() {
            if method == "notifications/initialized" && lifecycle == Lifecycle::AwaitInitialized {
                lifecycle = Lifecycle::Ready;
            }
            continue;
        }
        let id = id.expect("checked above");
        if id.is_null() || (!id.is_string() && !id.is_number()) {
            write_message(
                &mut output,
                &rpc_error(Value::Null, -32600, "request id must be a string or number"),
            )?;
            continue;
        }
        let response = match method {
            "initialize" => {
                if lifecycle != Lifecycle::AwaitInitialize {
                    rpc_error(id, -32600, "server is already initialized")
                } else {
                    match parse_initialize(object) {
                        Ok(requested) => {
                            let protocol = if SUPPORTED_PROTOCOLS.contains(&requested) {
                                requested
                            } else {
                                LATEST_PROTOCOL
                            };
                            lifecycle = Lifecycle::AwaitInitialized;
                            rpc_result(
                                id,
                                json!({
                                    "protocolVersion": protocol,
                                    "capabilities": {"tools": {"listChanged": false}},
                                    "serverInfo": {
                                        "name": "agit",
                                        "title": "agit Working-State Engine",
                                        "version": env!("CARGO_PKG_VERSION"),
                                        "description": "Reversible snapshots, isolated forks, and verified convergence"
                                    },
                                    "instructions": "Inspect before mutation. Rewind requires a full snapshot ID and the same value in confirm_snapshot."
                                }),
                            )
                        }
                        Err(error) => rpc_error(id, -32602, &error.to_string()),
                    }
                }
            }
            "ping" => rpc_result(id, json!({})),
            _ if lifecycle != Lifecycle::Ready => rpc_error(id, -32002, "server is not ready"),
            "tools/list" => {
                let params = object.get("params");
                if params.is_some_and(|params| !params.is_object()) {
                    rpc_error(id, -32602, "tools/list params must be an object")
                } else if params
                    .and_then(Value::as_object)
                    .is_some_and(|params| params.contains_key("cursor"))
                {
                    rpc_error(id, -32602, "tools/list cursor is not supported")
                } else {
                    rpc_result(id, json!({"tools": tool_definitions()}))
                }
            }
            "tools/call" => match parse_tool_call(object) {
                Ok((name, _arguments)) if !tool_names().contains(&name) => {
                    rpc_error(id, -32602, &format!("unknown tool: {name}"))
                }
                Ok((name, arguments)) => {
                    let empty = Map::new();
                    match call_tool(&mut repository, name, arguments.unwrap_or(&empty)) {
                        Ok(value) => rpc_result(id, tool_result(value, false)),
                        Err(error) => rpc_result(id, tool_error(&error)),
                    }
                }
                Err(error) => rpc_error(id, -32602, &error.to_string()),
            },
            _ => rpc_error(id, -32601, &format!("method not found: {method}")),
        };
        write_message(&mut output, &response)?;
    }
    Ok(())
}

fn call_tool(
    repository: &mut AgitRepository,
    name: &str,
    arguments: &Map<String, Value>,
) -> anyhow::Result<Value> {
    validate_argument_keys(name, arguments)?;
    match name {
        "agit.status" => {
            let status = repository.status()?;
            if optional_bool(arguments, "fidelity")?.unwrap_or(false) {
                Ok(json!({"status": status, "fidelity": repository.fidelity()?}))
            } else {
                serialize(status)
            }
        }
        "agit.timeline" => {
            let limit = optional_u64(arguments, "limit")?.unwrap_or(20);
            anyhow::ensure!(
                (1..=1000).contains(&limit),
                "limit must be between 1 and 1000"
            );
            let limit = limit as usize;
            serialize(repository.timeline(limit)?)
        }
        "agit.snapshot" => {
            let message = optional_string(arguments, "message")?.map(str::to_owned);
            anyhow::ensure!(
                message.as_ref().map_or(0, String::len) <= 4096,
                "message exceeds 4096 bytes"
            );
            let id = repository.snapshot(message, SnapshotTrigger::Manual)?;
            Ok(json!({"snapshot": id_hex(&id)}))
        }
        "agit.diff" => serialize(repository.diff(required_string(arguments, "target")?)?),
        "agit.forks" => serialize(repository.forks()?),
        "agit.events" => serialize(repository.events(
            optional_string(arguments, "after")?,
            optional_u64(arguments, "limit")?.unwrap_or(100).min(1000) as usize,
        )?),
        "agit.fork" => {
            let name = required_string(arguments, "name")?;
            let destination = default_fork_destination(repository.root(), name);
            let plan = repository.prepare_fork(name, &destination)?;
            let result = repository.materialize_fork(plan.clone())?;
            Ok(json!({"plan": plan, "result": result}))
        }
        "agit.merge_plan" => {
            serialize(repository.merge(required_string(arguments, "fork")?, None, true)?)
        }
        "agit.claims" => serialize(repository.claims()?),
        "agit.claim" => {
            let owner = optional_string(arguments, "owner")?
                .map(str::to_owned)
                .unwrap_or_else(|| repository.default_claim_owner());
            serialize(repository.claim(
                required_string(arguments, "pattern")?,
                &owner,
                optional_u64(arguments, "ttl_seconds")?.unwrap_or(3600),
            )?)
        }
        "agit.release" => {
            let owner = optional_string(arguments, "owner")?
                .map(str::to_owned)
                .unwrap_or_else(|| repository.default_claim_owner());
            serialize(repository.release_claim(required_string(arguments, "claim")?, &owner)?)
        }
        "agit.coord_list" => serialize(repository.coord_list()?),
        "agit.coord_read" => {
            let path = required_string(arguments, "path")?;
            let bytes = repository.coord_read(Path::new(path))?;
            let value = String::from_utf8(bytes).context("coord value is not UTF-8")?;
            Ok(json!({"path": path, "value": value}))
        }
        "agit.coord_write" => {
            let owner = optional_string(arguments, "owner")?
                .map(str::to_owned)
                .unwrap_or_else(|| repository.default_claim_owner());
            serialize(repository.coord_write(
                Path::new(required_string(arguments, "path")?),
                required_string(arguments, "value")?.as_bytes(),
                &owner,
            )?)
        }
        "agit.coord_remove" => {
            let owner = optional_string(arguments, "owner")?
                .map(str::to_owned)
                .unwrap_or_else(|| repository.default_claim_owner());
            serialize(
                repository.coord_remove(Path::new(required_string(arguments, "path")?), &owner)?,
            )
        }
        "agit.fork_updates" => serialize(repository.fork_updates(
            required_string(arguments, "fork")?,
            optional_string(arguments, "after")?,
            optional_u64(arguments, "limit")?.unwrap_or(100).min(1000) as usize,
        )?),
        "agit.rewind_plan" => {
            let requested = required_string(arguments, "snapshot")?;
            anyhow::ensure!(
                requested.len() == 64,
                "rewind planning requires the full 64-character snapshot ID"
            );
            let target = repository.resolve_snapshot(requested)?;
            let paths = optional_paths(arguments, "paths")?;
            let plan = repository.plan_rewind(&target, &paths)?;
            Ok(json!({"applied": false, "plan": plan}))
        }
        "agit.rewind_apply" => {
            let requested = required_string(arguments, "snapshot")?;
            anyhow::ensure!(
                requested.len() == 64,
                "applying rewind requires the full 64-character snapshot ID"
            );
            anyhow::ensure!(
                optional_string(arguments, "confirm_snapshot")? == Some(requested),
                "confirm_snapshot must exactly match snapshot before applying rewind"
            );
            let target = repository.resolve_snapshot(requested)?;
            let paths = optional_paths(arguments, "paths")?;
            let (pre, applied) = repository.rewind(
                &target,
                &paths,
                optional_bool(arguments, "sqlite_consistent")?.unwrap_or(false),
            )?;
            Ok(json!({
                "applied": true,
                "pre_rewind_snapshot": id_hex(&pre),
                "plan": applied
            }))
        }
        _ => unreachable!("tool name was validated before dispatch"),
    }
}

fn tool_names() -> &'static [&'static str] {
    &[
        "agit.status",
        "agit.timeline",
        "agit.snapshot",
        "agit.diff",
        "agit.forks",
        "agit.events",
        "agit.fork",
        "agit.merge_plan",
        "agit.claims",
        "agit.claim",
        "agit.release",
        "agit.coord_list",
        "agit.coord_read",
        "agit.coord_write",
        "agit.coord_remove",
        "agit.fork_updates",
        "agit.rewind_plan",
        "agit.rewind_apply",
    ]
}

fn validate_argument_keys(name: &str, arguments: &Map<String, Value>) -> anyhow::Result<()> {
    let allowed: &[&str] = match name {
        "agit.status" => &["fidelity"],
        "agit.timeline" => &["limit"],
        "agit.snapshot" => &["message"],
        "agit.diff" => &["target"],
        "agit.forks" => &[],
        "agit.events" => &["after", "limit"],
        "agit.fork" => &["name"],
        "agit.merge_plan" => &["fork"],
        "agit.claims" => &[],
        "agit.claim" => &["pattern", "owner", "ttl_seconds"],
        "agit.release" => &["claim", "owner"],
        "agit.coord_list" => &[],
        "agit.coord_read" => &["path"],
        "agit.coord_write" => &["path", "value", "owner"],
        "agit.coord_remove" => &["path", "owner"],
        "agit.fork_updates" => &["fork", "after", "limit"],
        "agit.rewind_plan" => &["snapshot", "paths"],
        "agit.rewind_apply" => &["snapshot", "paths", "confirm_snapshot", "sqlite_consistent"],
        _ => unreachable!("tool name was validated before dispatch"),
    };
    for key in arguments.keys() {
        anyhow::ensure!(allowed.contains(&key.as_str()), "unknown argument: {key}");
    }
    Ok(())
}

fn tool_definitions() -> Vec<Value> {
    vec![
        tool(
            "agit.status",
            "Inspect workspace protection, store health, and optional capture fidelity.",
            json!({
                "type": "object",
                "properties": {"fidelity": {"type": "boolean", "default": false}},
                "additionalProperties": false
            }),
            true,
            false,
        ),
        tool(
            "agit.timeline",
            "List recent complete working-state snapshots.",
            json!({
                "type": "object",
                "properties": {"limit": {"type": "integer", "minimum": 1, "maximum": 1000}},
                "additionalProperties": false
            }),
            true,
            false,
        ),
        tool(
            "agit.snapshot",
            "Seal the complete current workspace, including ignored and untracked files.",
            json!({
                "type": "object",
                "properties": {"message": {"type": "string", "maxLength": 4096}},
                "additionalProperties": false
            }),
            false,
            false,
        ),
        tool(
            "agit.diff",
            "Inspect path-level additions, modifications, and deletions in a fork or since a snapshot.",
            json!({
                "type": "object",
                "properties": {"target": {"type": "string"}},
                "required": ["target"],
                "additionalProperties": false
            }),
            false,
            false,
        ),
        tool(
            "agit.forks",
            "List isolated universes with stable IDs and live conflict counts.",
            json!({"type": "object", "additionalProperties": false}),
            true,
            false,
        ),
        tool(
            "agit.events",
            "Read durable conflict transitions after an exclusive event cursor.",
            json!({
                "type": "object",
                "properties": {
                    "after": {"type": "string"},
                    "limit": {"type": "integer", "minimum": 1, "maximum": 1000, "default": 100}
                },
                "additionalProperties": false
            }),
            true,
            false,
        ),
        tool(
            "agit.fork",
            "Create an isolated full-state workspace and return its pre-materialization cost plan.",
            json!({
                "type": "object",
                "properties": {
                    "name": {"type": "string"}
                },
                "required": ["name"],
                "additionalProperties": false
            }),
            false,
            false,
        ),
        tool(
            "agit.merge_plan",
            "Plan a three-way full-state fork merge without executing checks or changing files.",
            json!({
                "type": "object",
                "properties": {"fork": {"type": "string"}},
                "required": ["fork"],
                "additionalProperties": false
            }),
            false,
            false,
        ),
        tool(
            "agit.claims",
            "List active advisory path claims shared by sibling forks.",
            json!({"type": "object", "additionalProperties": false}),
            true,
            false,
        ),
        tool(
            "agit.claim",
            "Claim a path glob with a TTL; overlapping claims from another agent are refused.",
            json!({
                "type": "object",
                "properties": {
                    "pattern": {"type": "string", "maxLength": 1024},
                    "owner": {"type": "string", "maxLength": 256},
                    "ttl_seconds": {"type": "integer", "minimum": 1, "maximum": 604800, "default": 3600}
                },
                "required": ["pattern"],
                "additionalProperties": false
            }),
            false,
            false,
        ),
        tool(
            "agit.release",
            "Release this agent's claim by ID or exact pattern.",
            json!({
                "type": "object",
                "properties": {
                    "claim": {"type": "string"},
                    "owner": {"type": "string", "maxLength": 256}
                },
                "required": ["claim"],
                "additionalProperties": false
            }),
            false,
            false,
        ),
        tool(
            "agit.coord_list",
            "List eagerly replicated coordination files without reading their content.",
            json!({"type": "object", "additionalProperties": false}),
            true,
            false,
        ),
        tool(
            "agit.coord_read",
            "Read one UTF-8 value from the versioned coordination directory.",
            json!({
                "type": "object",
                "properties": {"path": {"type": "string", "maxLength": 1024}},
                "required": ["path"],
                "additionalProperties": false
            }),
            true,
            false,
        ),
        tool(
            "agit.coord_write",
            "Write a UTF-8 coordination value and eagerly propagate it to live sibling forks.",
            json!({
                "type": "object",
                "properties": {
                    "path": {"type": "string", "maxLength": 1024},
                    "value": {"type": "string", "maxLength": 1048576},
                    "owner": {"type": "string", "maxLength": 256}
                },
                "required": ["path", "value"],
                "additionalProperties": false
            }),
            false,
            false,
        ),
        tool(
            "agit.coord_remove",
            "Remove a coordination value and propagate a durable tombstone to sibling forks.",
            json!({
                "type": "object",
                "properties": {
                    "path": {"type": "string", "maxLength": 1024},
                    "owner": {"type": "string", "maxLength": 256}
                },
                "required": ["path"],
                "additionalProperties": false
            }),
            false,
            false,
        ),
        tool(
            "agit.fork_updates",
            "Return fork seals newer than an optional exact snapshot cursor.",
            json!({
                "type": "object",
                "properties": {
                    "fork": {"type": "string"},
                    "after": {"type": "string", "pattern": "^[0-9a-f]{64}$"},
                    "limit": {"type": "integer", "minimum": 1, "maximum": 1000, "default": 100}
                },
                "required": ["fork"],
                "additionalProperties": false
            }),
            true,
            false,
        ),
        tool(
            "agit.rewind_plan",
            "Preview the exact impact of a rewind without changing workspace files.",
            json!({
                "type": "object",
                "properties": {
                    "snapshot": {"type": "string", "pattern": "^[0-9a-f]{64}$"},
                    "paths": {"type": "array", "items": {"type": "string"}, "maxItems": 10000}
                },
                "required": ["snapshot"],
                "additionalProperties": false
            }),
            true,
            false,
        ),
        tool(
            "agit.rewind_apply",
            "Apply a reversible rewind after an exact-ID impact preview and explicit repeated confirmation.",
            json!({
                "type": "object",
                "properties": {
                    "snapshot": {"type": "string", "pattern": "^[0-9a-f]{64}$"},
                    "paths": {"type": "array", "items": {"type": "string"}, "maxItems": 10000},
                    "confirm_snapshot": {"type": "string", "pattern": "^[0-9a-f]{64}$"},
                    "sqlite_consistent": {"type": "boolean", "default": false}
                },
                "required": ["snapshot", "confirm_snapshot"],
                "additionalProperties": false
            }),
            false,
            true,
        ),
    ]
}

fn tool(
    name: &str,
    description: &str,
    input_schema: Value,
    read_only: bool,
    destructive: bool,
) -> Value {
    json!({
        "name": name,
        "description": description,
        "inputSchema": input_schema,
        "annotations": {
            "readOnlyHint": read_only,
            "destructiveHint": destructive,
            "idempotentHint": read_only,
            "openWorldHint": false
        },
        "execution": {"taskSupport": "forbidden"}
    })
}

fn parse_tool_call(
    request: &Map<String, Value>,
) -> anyhow::Result<(&str, Option<&Map<String, Value>>)> {
    let params = request
        .get("params")
        .and_then(Value::as_object)
        .context("tools/call requires an object params value")?;
    anyhow::ensure!(
        !params.contains_key("task"),
        "this server does not support task-augmented tool calls"
    );
    let name = params
        .get("name")
        .and_then(Value::as_str)
        .context("tools/call requires a string name")?;
    let arguments = params
        .get("arguments")
        .map(|arguments| arguments.as_object().context("arguments must be an object"))
        .transpose()?;
    Ok((name, arguments))
}

fn parse_initialize(request: &Map<String, Value>) -> anyhow::Result<&str> {
    let params = request
        .get("params")
        .and_then(Value::as_object)
        .context("initialize requires object params")?;
    anyhow::ensure!(
        params.get("capabilities").is_some_and(Value::is_object),
        "initialize requires object capabilities"
    );
    let client = params
        .get("clientInfo")
        .and_then(Value::as_object)
        .context("initialize requires object clientInfo")?;
    anyhow::ensure!(
        client.get("name").is_some_and(Value::is_string)
            && client.get("version").is_some_and(Value::is_string),
        "clientInfo requires string name and version"
    );
    params
        .get("protocolVersion")
        .and_then(Value::as_str)
        .context("initialize requires string protocolVersion")
}

fn required_string<'a>(arguments: &'a Map<String, Value>, name: &str) -> anyhow::Result<&'a str> {
    arguments
        .get(name)
        .and_then(Value::as_str)
        .with_context(|| format!("{name} must be a string"))
}

fn optional_string<'a>(
    arguments: &'a Map<String, Value>,
    name: &str,
) -> anyhow::Result<Option<&'a str>> {
    arguments
        .get(name)
        .map(|value| {
            value
                .as_str()
                .with_context(|| format!("{name} must be a string"))
        })
        .transpose()
}

fn optional_bool(arguments: &Map<String, Value>, name: &str) -> anyhow::Result<Option<bool>> {
    arguments
        .get(name)
        .map(|value| {
            value
                .as_bool()
                .with_context(|| format!("{name} must be a boolean"))
        })
        .transpose()
}

fn optional_u64(arguments: &Map<String, Value>, name: &str) -> anyhow::Result<Option<u64>> {
    arguments
        .get(name)
        .map(|value| {
            value
                .as_u64()
                .with_context(|| format!("{name} must be a non-negative integer"))
        })
        .transpose()
}

fn optional_paths(arguments: &Map<String, Value>, name: &str) -> anyhow::Result<Vec<PathBuf>> {
    let Some(value) = arguments.get(name) else {
        return Ok(Vec::new());
    };
    let paths = value
        .as_array()
        .with_context(|| format!("{name} must be an array"))?;
    anyhow::ensure!(paths.len() <= 10_000, "{name} contains too many paths");
    let paths: Vec<PathBuf> = paths
        .iter()
        .map(|path| {
            path.as_str()
                .map(PathBuf::from)
                .with_context(|| format!("{name} entries must be strings"))
        })
        .collect::<anyhow::Result<_>>()?;
    for path in &paths {
        anyhow::ensure!(!path.is_absolute(), "{name} entries must be relative paths");
        anyhow::ensure!(
            path.components()
                .all(|component| matches!(component, std::path::Component::Normal(_))),
            "{name} entries must not contain dot or parent components"
        );
    }
    Ok(paths)
}

fn serialize(value: impl Serialize) -> anyhow::Result<Value> {
    Ok(serde_json::to_value(value)?)
}

fn tool_result(value: Value, is_error: bool) -> Value {
    let text = serde_json::to_string_pretty(&value).unwrap_or_else(|_| value.to_string());
    json!({
        "content": [{"type": "text", "text": text}],
        "structuredContent": value,
        "isError": is_error
    })
}

fn tool_error(error: &anyhow::Error) -> Value {
    json!({
        "content": [{"type": "text", "text": error.to_string()}],
        "isError": true
    })
}

fn rpc_result(id: Value, result: Value) -> Value {
    json!({"jsonrpc": "2.0", "id": id, "result": result})
}

fn rpc_error(id: Value, code: i64, message: &str) -> Value {
    json!({
        "jsonrpc": "2.0",
        "id": id,
        "error": {"code": code, "message": message}
    })
}

fn write_message(output: &mut impl Write, message: &Value) -> anyhow::Result<()> {
    serde_json::to_writer(&mut *output, message)?;
    output.write_all(b"\n")?;
    output.flush()?;
    Ok(())
}

fn read_message(input: &mut impl BufRead) -> anyhow::Result<Option<Vec<u8>>> {
    let mut message = Vec::new();
    let mut oversized = false;
    loop {
        let available = input.fill_buf()?;
        if available.is_empty() {
            if message.is_empty() && !oversized {
                return Ok(None);
            }
            break;
        }
        let newline = available.iter().position(|byte| *byte == b'\n');
        let take = newline.map_or(available.len(), |position| position + 1);
        if !oversized && message.len().saturating_add(take) <= MAX_REQUEST_BYTES + 1 {
            message.extend_from_slice(&available[..take]);
        } else {
            oversized = true;
        }
        input.consume(take);
        if newline.is_some() {
            break;
        }
    }
    anyhow::ensure!(!oversized, "MCP request exceeds the 1 MiB limit");
    if message.last() == Some(&b'\n') {
        message.pop();
    }
    if message.last() == Some(&b'\r') {
        message.pop();
    }
    anyhow::ensure!(
        message.len() <= MAX_REQUEST_BYTES,
        "MCP request exceeds the 1 MiB limit"
    );
    Ok(Some(message))
}

fn default_fork_destination(root: &Path, name: &str) -> PathBuf {
    let parent = root.parent().unwrap_or_else(|| Path::new("."));
    let repository_name = root
        .file_name()
        .unwrap_or_else(|| std::ffi::OsStr::new("workspace"));
    let mut forks_name = repository_name.to_os_string();
    forks_name.push(".agit-forks");
    parent.join(forks_name).join(name)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn bounded_reader_rejects_oversized_messages_and_resumes_at_next_line() {
        let mut bytes = vec![b'x'; MAX_REQUEST_BYTES + 1];
        bytes.extend_from_slice(b"\n{}\n");
        let mut input = BufReader::new(bytes.as_slice());
        assert!(read_message(&mut input).is_err());
        assert_eq!(read_message(&mut input).unwrap().unwrap(), b"{}");
    }
}
