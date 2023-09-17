#!/bin/bash

apt update
apt install -y openssh-server
systemctl enable --now ssh
echo "PermitRootLogin yes" >> /etc/ssh/sshd_config
systemctl restart ssh
echo root:$1 | chpasswd