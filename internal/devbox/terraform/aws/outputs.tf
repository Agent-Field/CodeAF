output "instance_id" {
  value = aws_instance.box.id
}
output "name" {
  value = var.name
}
output "region" {
  value = var.region
}
output "volume_id" {
  value = aws_ebs_volume.home.id
}
