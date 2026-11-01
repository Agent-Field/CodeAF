variable "name" {
  type        = string
  description = "The name your devices use for this computer."
}
variable "workspace_id" {
  type    = string
  default = ""
}
variable "region" {
  type    = string
  default = ""
}
variable "image" {
  type    = string
  default = "buildpack-deps:bookworm-scm"
}
variable "install_url" {
  type    = string
  default = "https://agentfield.ai/get/codeaf"
}
variable "binary" {
  type    = string
  default = "codeaf"
}
variable "install_version" {
  type        = string
  default     = ""
  description = "The creating computer's release tag, used only for the first install."
}
variable "via" {
  default     = ""
  type        = string
  description = "The relay used for both identity pairing and serving."
}
variable "pair_code" {
  type      = string
  sensitive = true
  default   = ""
}
variable "remote_key" {
  type      = string
  sensitive = true
  default   = ""
}
variable "remote_devices" {
  type      = string
  sensitive = true
  default   = ""
}
