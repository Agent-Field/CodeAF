# This module has never been applied. The devbox command does not offer AWS.
terraform {
  required_version = ">= 1.5"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

provider "aws" {
  region = var.region
}

data "aws_ami" "debian" {
  most_recent = true
  owners      = ["136693071363"]
  filter {
    name   = "name"
    values = ["debian-12-amd64-*"]
  }
  filter {
    name   = "virtualization-type"
    values = ["hvm"]
  }
  filter {
    name   = "architecture"
    values = ["x86_64"]
  }
}

data "aws_subnet" "box" {
  id = var.subnet_id
}

resource "aws_security_group" "box" {
  name_prefix = "codeaf-${var.name}-"
  vpc_id      = data.aws_subnet.box.vpc_id
  ingress     = []
  egress {
    from_port        = 0
    to_port          = 0
    protocol         = "-1"
    cidr_blocks      = ["0.0.0.0/0"]
    ipv6_cidr_blocks = ["::/0"]
  }
}

resource "aws_ebs_volume" "home" {
  availability_zone = data.aws_subnet.box.availability_zone
  size              = var.volume_size
  type              = "gp3"
  encrypted         = true
  tags              = { Name = "codeaf-${var.name}-home" }
}

resource "aws_volume_attachment" "home" {
  device_name = "/dev/sdf"
  volume_id   = aws_ebs_volume.home.id
  instance_id = aws_instance.box.id
}

resource "aws_instance" "box" {
  ami                    = data.aws_ami.debian.id
  instance_type          = var.instance_type
  subnet_id              = var.subnet_id
  vpc_security_group_ids = [aws_security_group.box.id]
  tags                   = { Name = "codeaf-${var.name}" }
  user_data = <<-USERDATA
    #!/bin/bash
    set -euo pipefail
    apt-get update
    apt-get install -y bash curl ca-certificates tar git
    # Nitro exposes EBS disks by volume id; attachment can finish after boot.
    volume_id='${replace(aws_ebs_volume.home.id, "-", "")}'
    device=""
    for attempt in $(seq 1 180); do
      for candidate in /dev/disk/by-id/nvme-Amazon_Elastic_Block_Store_$volume_id /dev/sdf /dev/xvdf; do
        if [ -b "$candidate" ]; then device="$candidate"; break; fi
      done
      [ -n "$device" ] && break
      sleep 2
    done
    [ -n "$device" ]
    if ! blkid "$device"; then mkfs.ext4 "$device"; fi
    mkdir -p /data
    uuid=$(blkid -s UUID -o value "$device")
    printf 'UUID=%s /data ext4 defaults,nofail 0 2\n' "$uuid" >> /etc/fstab
    mount /data
    printf %s '${base64encode(file("${path.module}/../boot.sh"))}' | base64 -d > /usr/local/lib/codeaf-boot.sh
    printf %s '${base64encode(join("\n", [
  "CODEAF_DEVBOX_NAME=${var.name}",
  "CODEAF_DEVBOX_INSTALL_URL=${var.install_url}",
  "CODEAF_DEVBOX_BINARY=${var.binary}",
  "CODEAF_DEVBOX_VIA=${var.via}",
  "CODEAF_DEVBOX_CODE=${var.pair_code}",
  "CODEAF_DEVBOX_REMOTE_KEY=${var.remote_key}",
  "CODEAF_DEVBOX_REMOTE_DEVICES=${var.remote_devices}",
]))}' | base64 -d > /etc/codeaf-devbox.env
    chmod 600 /etc/codeaf-devbox.env
    cat > /etc/systemd/system/codeaf-devbox.service <<'UNIT'
    [Unit]
    Description=codeaf devbox
    Wants=network-online.target
    After=network-online.target
    RequiresMountsFor=/data
    [Service]
    Type=simple
    EnvironmentFile=/etc/codeaf-devbox.env
    ExecStart=/bin/bash /usr/local/lib/codeaf-boot.sh
    Restart=always
    RestartSec=10
    [Install]
    WantedBy=multi-user.target
    UNIT
    systemctl daemon-reload
    systemctl enable --now codeaf-devbox.service
  USERDATA
}
