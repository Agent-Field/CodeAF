#[cfg(target_os = "macos")]
mod menu;

use tauri_plugin_shell::ShellExt;

#[derive(serde::Serialize, serde::Deserialize)]
struct EngineHealth {
    status: String,
    version: String,
    platform: String,
}

// The renderer gets a narrow typed command, never arbitrary process execution.
#[tauri::command]
async fn engine_health(app: tauri::AppHandle) -> Result<EngineHealth, String> {
    let output = app
        .shell()
        .sidecar("codeaf-engine")
        .map_err(|e| e.to_string())?
        .args(["health"])
        .output()
        .await
        .map_err(|e| e.to_string())?;
    if !output.status.success() {
        return Err("The codeaf engine health check failed".into());
    }
    serde_json::from_slice(&output.stdout).map_err(|e| e.to_string())
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    let builder = tauri::Builder::default();
    #[cfg(target_os = "macos")]
    let builder = builder.menu(menu::build).on_menu_event(menu::handle);

    builder
        .plugin(tauri_plugin_shell::init())
        .invoke_handler(tauri::generate_handler![engine_health])
        .run(tauri::generate_context!())
        .expect("error while running codeaf");
}
