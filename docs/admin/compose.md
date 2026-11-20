---
title: Compose VMs
parent: Administrators
nav_order: 6
---

# Compose VMs

A Compose target deploys exactly one Compose file to one VM over SSH.
Projects that need more files or steps use an [Ansible target](ansible.md).

## What a deployment does

The executor connects as the deploy user with the key and `known_hosts` from Vault (`StrictHostKeyChecking=yes`, batch mode) and runs, in the target's `dir`:

1. Upload the Compose file from `.cd.yaml` as `docker-compose.yml`. It writes a temporary file and renames it, so a dropped connection never leaves half a file.
2. Upload `.cd.env` with one line per image, for example `SERVER_IMAGE=ghcr.io/example/app-server@sha256:...`.
3. Run `docker compose --env-file .env --env-file .cd.env pull` (without `--env-file .env` when the VM has no `.env`).
4. Run `docker compose --env-file .env --env-file .cd.env up -d --remove-orphans`.

Because the images are pinned by digest, exactly the built variant runs.
The executor image has no Docker CLI; `docker compose` runs on the VM.

## Prepare the VM

1. Install Docker Engine with the Compose plugin (`docker compose`, not `docker-compose`).
2. Create the deploy user. The default name is `deploy`; set `user` in the Vault secret for another name.
   ```sh
   sudo useradd --create-home --shell /bin/sh deploy
   sudo usermod -aG docker deploy
   ```
   Membership in the `docker` group is equivalent to root on the VM. Use a dedicated VM per target where you can.
3. Add the public key of the simple-cd key pair to `/home/deploy/.ssh/authorized_keys`.
4. Create the deploy directory, owned by the deploy user, for example `/opt/app`. The executor runs `mkdir -p`, so the user needs write access to it or its parent.
5. Put configuration and secrets the application needs into that directory, for example `.env` and mounted config files. They stay on the VM; simple-cd never reads or overwrites them, except `docker-compose.yml` and `.cd.env`.
6. Record the host key and store it with the private key in Vault:
   ```sh
   ssh-keyscan -t ed25519 vm1.example.org > known_hosts
   ssh-keygen -lf known_hosts   # compare with the fingerprint on the VM
   vault kv put secret/cd/ssh/app-prod private_key=@id_ed25519 known_hosts=@known_hosts user=deploy
   ```

The deploy user's login shell runs the remote commands, so it must be a POSIX shell.

## Constraints

- `host` must be a plain host name or IP address. The executor connects to port 22; other ports are not supported.
- `dir` must be a clean absolute path of letters, digits, `.`, `_`, `-` and `/`, and not `/`.
- Exactly one Compose file. It must use `${<NAME>_IMAGE}` for every image in `deploy.json` and must not contain `build:`.
- Variables from `.env` and `.cd.env` are available for interpolation in the Compose file. Pass them to containers explicitly, for example with `env_file: .env`.
- Changes to the Compose file need a confirmation in the deploy dialog. Changes to `.env` on the VM are invisible to simple-cd.

## Target configuration

```yaml
targets:
  - id: app-prod
    project: example/app
    type: compose
    role: deploy:app-prod
    lockable: true
    host: vm1.example.org
    ssh: vault:secret/cd/ssh/app-prod
    dir: /opt/app
    jobTimeout: 10m
```

All fields are described in the [target configuration reference](configuration.md#compose-targets).
