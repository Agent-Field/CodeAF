output "project_id" {
  value = railway_project.box.id
}
output "project_name" {
  value = railway_project.box.name
}
output "service_id" {
  value = railway_service.box.id
}
output "environment_id" {
  value = railway_project.box.default_environment.id
}
output "region" {
  value = try(railway_service.box.regions[0].region, "")
}
output "name" {
  value = var.name
}
