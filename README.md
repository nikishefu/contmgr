# contmgr
Script to automate container creation. 
Allows to create lxc containers, forward ports and run setup scripts in a single command

This script is not yet ready to use on any machine except mine.
```
Usage of ./contmgr:
  -c    Create container
  -core int
        Core number (default 1)
  -cpu int
        Limit of CPU (%) (default 10)
  -d    Delete specified container
  -image string
        Image (default "ubuntu/23.10")
  -l    List existing containers
  -name string
        Container name (default random)
  -path string
        Path to directory with .tf files (default "/etc/contmgr/terraform")
  -ports string
        Comma separated ports except 22 to forvard
  -ram int
        Limit of RAM (MB) (default 100)
```

### Prerequisites
- Terraform
- Directory with a base terraform file which decalres provider
- `terraform init`
- Ansible
- custom inventory file: `/etc/contmgr/ansible/hosts`

A lot of manual work, but automated installation is coming soon
