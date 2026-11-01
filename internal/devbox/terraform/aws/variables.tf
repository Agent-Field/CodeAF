variable "name" {
  type = string
}
variable "region" {
  type    = string
  default = "us-east-1"
}
variable "subnet_id" {
  type        = string
  description = "An existing subnet with outbound internet access."
}
variable "instance_type" {
  type    = string
  default = "t3.small"
}
variable "volume_size" {
  type    = number
  default = 20
}
variable "install_url" {
  type    = string
  default = "https://agentfield.ai/get/codeaf"
}
variable "binary" {
  type    = string
  default = "codeaf"
}
variable "via" {
  type = string
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
