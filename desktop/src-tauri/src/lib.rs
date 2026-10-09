mod dialogs;
#[cfg(target_os = "macos")]
mod menu;
mod native;
mod notifications;
// Public only so the native smoke test (tests/web_smoke.rs) can drive it.
#[doc(hidden)]
pub mod web;
mod windows;

use std::sync::Mutex;
use tauri::Manager;
use tauri_plugin_shell::{
    process::{CommandChild, CommandEvent},
    ShellExt,
};

#[derive(Clone, serde::Serialize, serde::Deserialize)]
struct EngineConnection {
    url: String,
    token: String,
    model: String,
}

#[derive(Default)]
struct EngineRuntime {
    connection: tauri::async_runtime::Mutex<Option<EngineConnection>>,
    child: Mutex<Option<CommandChild>>,
}

// A development shell may attach to an engine that already runs on another
// machine and is forwarded to this machine's loopback, so a provider key never
// has to be copied here. The variable names a connection file written by the
// engine; only a loopback URL is accepted, and the renderer never sees the path.
fn forwarded_connection() -> Result<Option<EngineConnection>, String> {
    let Some(path) = std::env::var_os("CODEAF_DESKTOP_CONNECTION") else {
        return Ok(None);
    };
    let text = std::fs::read_to_string(path)
        .map_err(|_| "The forwarded engine connection is unreadable")?;
    let connection: EngineConnection =
        serde_json::from_str(&text).map_err(|_| "The forwarded engine connection is invalid")?;
    let loopback = ["http://127.0.0.1:", "http://localhost:"]
        .iter()
        .any(|prefix| connection.url.starts_with(prefix));
    if !loopback || connection.token.is_empty() {
        return Err("The forwarded engine connection must be an authenticated loopback URL".into());
    }
    Ok(Some(connection))
}

// Only the canonical binary's fixed local transport can be started. The
// renderer never receives process arguments or a provider credential.
#[tauri::command]
async fn engine_connection(app: tauri::AppHandle) -> Result<EngineConnection, String> {
    let runtime = app.state::<EngineRuntime>();
    let mut cached = runtime.connection.lock().await;
    if let Some(connection) = cached.clone() {
        return Ok(connection);
    }
    if let Some(connection) = forwarded_connection()? {
        *cached = Some(connection.clone());
        return Ok(connection);
    }
    let (mut events, child) = app
        .shell()
        .sidecar("codeaf-engine")
        .map_err(|e| e.to_string())?
        .args(["desktop-bridge", "--listen", "127.0.0.1:0"])
        .spawn()
        .map_err(|e| e.to_string())?;
    let mut output = Vec::new();
    while let Some(event) = events.recv().await {
        match event {
            CommandEvent::Stdout(bytes) => {
                output.extend(bytes);
                if let Ok(connection) = serde_json::from_slice::<EngineConnection>(&output) {
                    *cached = Some(connection.clone());
                    *app.state::<EngineRuntime>()
                        .child
                        .lock()
                        .map_err(|_| "Engine state unavailable")? = Some(child);
                    // Drain stderr without rendering keys or machine configuration.
                    let owner = app.clone();
                    tauri::async_runtime::spawn(async move {
                        while let Some(event) = events.recv().await {
                            if matches!(event, CommandEvent::Terminated(_) | CommandEvent::Error(_))
                            {
                                break;
                            }
                        }
                        *owner.state::<EngineRuntime>().connection.lock().await = None;
                    });
                    return Ok(connection);
                }
            }
            CommandEvent::Terminated(_) | CommandEvent::Error(_) => {
                return Err("The local engine could not start".into())
            }
            _ => {}
        }
    }
    Err("The local engine did not announce its connection".into())
}

#[derive(serde::Serialize)]
struct EngineHealth {
    status: String,
    version: String,
    platform: String,
}

#[tauri::command]
async fn engine_health(app: tauri::AppHandle) -> Result<EngineHealth, String> {
    engine_connection(app).await?;
    Ok(EngineHealth {
        status: "ready".into(),
        version: env!("CARGO_PKG_VERSION").into(),
        platform: std::env::consts::OS.into(),
    })
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    let builder = tauri::Builder::default();
    #[cfg(target_os = "macos")]
    let builder = builder.menu(menu::build).on_menu_event(menu::handle);
    builder
        .manage(EngineRuntime::default())
        .manage(windows::Windows::default())
        .manage(notifications::Attention::default())
        .plugin(tauri_plugin_shell::init())
        .plugin(tauri_plugin_http::init())
        .plugin(tauri_plugin_dialog::init())
        .plugin(tauri_plugin_notification::init())
        .on_window_event(|window, event| {
            if let tauri::WindowEvent::Destroyed = event {
                windows::on_destroyed(window.app_handle(), window.label());
            }
        })
        .invoke_handler(tauri::generate_handler![
            engine_health,
            engine_connection,
            native::open_path,
            native::reveal_path,
            native::host_name,
            native::open_url,
            windows::window_open,
            windows::window_move_tab,
            windows::window_claim_handoff,
            windows::window_list,
            windows::window_focus,
            windows::window_set_title,
            windows::window_context,
            dialogs::dialog_pick,
            notifications::notify_permission,
            notifications::notify_request_permission,
            notifications::notify_attention,
            notifications::badge_set,
            web::web_open,
            web::web_navigate,
            web::web_bounds,
            web::web_visible,
            web::web_history,
            web::web_close,
            web::web_snapshot,
            web::web_list
        ])
        .build(tauri::generate_context!())
        .expect("error while building codeaf")
        .run(|app, event| {
            if matches!(event, tauri::RunEvent::Exit) {
                // Only our bundled transport belongs to this app. A forwarded
                // development engine has no child here and remains untouched.
                if let Ok(mut owned) = app.state::<EngineRuntime>().child.lock() {
                    if let Some(child) = owned.take() {
                        let _ = child.kill();
                    }
                }
            }
        });
}
