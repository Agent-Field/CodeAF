terraform {
  required_version = ">= 1.5"
  required_providers {
    railway = {
      source  = "terraform-community-providers/railway"
      version = "~> 0.6"
    }
  }
  backend "local" {}
}

provider "railway" {}

resource "railway_project" "box" {
  name         = "codeaf-${var.name}"
  private      = true
  workspace_id = var.workspace_id == "" ? null : var.workspace_id
}

resource "railway_service" "box" {
  name                = "box"
  project_id          = railway_project.box.id
  source_image        = var.image
  restart_policy_type = "ALWAYS"
  serverless          = false
  # The folder writer adds an explicit region only when requested.
  # Provider 0.6.3 cannot validate an unknown conditional regions list.
  regions = null
  volume = {
    name       = "home"
    mount_path = "/data"
  }
  start_command = "bash -c 'printf %s \"$CODEAF_DEVBOX_BOOT\" | base64 -d > /tmp/codeaf-boot.sh && exec bash /tmp/codeaf-boot.sh'"
}

resource "railway_variable_collection" "box" {
  environment_id = railway_project.box.default_environment.id
  service_id     = railway_service.box.id
  variables = [for variable in [
    { name = "CODEAF_DEVBOX_BOOT", value = filebase64("${path.module}/boot.sh") },
    { name = "CODEAF_DEVBOX_INSTALL_URL", value = var.install_url },
    { name = "CODEAF_DEVBOX_BINARY", value = var.binary },
    { name = "CODEAF_DEVBOX_VERSION", value = var.install_version },
    { name = "CODEAF_DEVBOX_NAME", value = var.name },
    { name = "CODEAF_DEVBOX_VIA", value = var.via },
    { name = "CODEAF_DEVBOX_CODE", value = var.pair_code },
    { name = "CODEAF_DEVBOX_REMOTE_KEY", value = var.remote_key },
    { name = "CODEAF_DEVBOX_REMOTE_DEVICES", value = var.remote_devices },
  ] : variable if variable.value != ""]
}
