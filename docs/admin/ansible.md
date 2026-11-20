---
title: Ansible targets
parent: Administrators
nav_order: 7
---

# Ansible targets

An Ansible target runs `ansible-playbook` from a Git repository against an inventory.
Use it when a Compose file is not enough, for example for several files or VMs.

Ansible targets run in the `simple-cd-executor-ansible` image, which contains `ansible-core`, `git` and the `ssh` client.
It contains no Ansible collections beyond `ansible-core`; playbooks that need collections have to bring them along in the repository.

## Playbook source

| `ansible.source` | Clones | Checks out | Playbook from |
|---|---|---|---|
| `repo` | the app repository over SSH: `git@<gitSSHHost>:<owner>/<repo>.git` | the artifact's commit | `ansible.playbook` in `.cd.yaml` |
| a Git URL (`https://...`, `ssh://...` or `git@host:org/repo.git`) | that repository | `ansible.ref` from the target config (branch, tag or commit) | `ansible.playbook` from the target config |

`ansible.inventory` always comes from the target configuration and is a path inside the cloned repository.
Paths must be relative and stay inside the checkout.

For `source: repo` and SSH URLs, the key in `ansible.credentials` must be able to read the repository, for example as a read-only deploy key, and `known_hosts` must contain the Git host.
HTTPS URLs work only for public repositories; the executor sends no credentials for HTTPS.

### Which source to choose

A playbook is code, and it runs inside the executor Job, next to the Vault login.
All executor Jobs share one Vault role, so a playbook can read every deployment secret below `secret/cd/`, including kubeconfigs and SSH keys of other projects.

- **External Git source (recommended):** keep the playbooks in a separate repository that only operators can push to, and pin `ansible.ref`. Developers of the application cannot change what runs.
- **`source: repo`:** the playbook comes from the application repository at the deployed commit. Use it only when you trust everyone who can push to that repository and get a commit deployed to the target with all deployment secrets.

A project can mix both: one target with an external source for shared or production systems, another with `source: repo` for a sandbox.

## What a deployment does

1. Read `private_key`, `known_hosts` and `user` from the Vault secret in `ansible.credentials`.
2. Clone the repository and check out the ref (also tried as `origin/<ref>`).
3. Write the extra variables to a file in `/secrets`.
4. Run, in the checkout:
   ```sh
   ansible-playbook -i <inventory> <playbook> --private-key <key> -e @<extra-vars.json> [-u <user>]
   ```
   with `ANSIBLE_HOST_KEY_CHECKING=True` and `known_hosts` as the known hosts file.

Every managed host must therefore be in `known_hosts` too.

## Extra variables

| Variable | Content |
|---|---|
| `cd_images` | map of image name to pinned reference, e.g. `{"server": "ghcr.io/example/app-server@sha256:..."}` |
| `cd_sha` | the artifact's commit SHA |
| `cd_target` | the target id |
| `cd_repo` | the project, `owner/repo` |

{% raw %}
```yaml
- hosts: tools
  tasks:
    - name: Write the image reference for the service
      ansible.builtin.copy:
        dest: /opt/tools/images.env
        content: "SERVER_IMAGE={{ cd_images.server }}\n"
        mode: "0644"
```
{% endraw %}

## Target configuration

```yaml
targets:
  - id: tools-vm
    project: example/tools
    type: ansible
    ansible:
      source: git@github.com:example/infra.git   # or "repo"
      ref: main
      playbook: playbooks/tools.yml
      inventory: inventories/prod
      credentials: vault:secret/cd/ssh/tools
```

Set `ansible.ref` for external sources: the configuration check accepts it empty, but the executor then fails.
All fields are described in the [target configuration reference](configuration.md#ansible-targets).
