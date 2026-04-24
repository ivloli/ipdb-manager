# ipdb-manager Jenkins Deployment Guide

This guide is for operations engineers configuring Jenkins for `ipdb-manager`.

## 1) What this pipeline does

`Jenkinsfile` supports:

1. Build/test/package only
2. Build/test/package + remote deployment

Main behavior:

- Inject deployment `config.yaml` from Jenkins
- Run tests (`make test`)
- Build release package
- Archive package + checksum
- Optionally deploy to remote host and restart systemd service

## 2) Jenkins prerequisites

- Jenkins plugin: `Config File Provider`
- Jenkins plugin: `SSH Agent`
- Agent/node tools: `make`, `go`, `tar`, `shasum`, `ssh`, `scp`

## 3) Managed config file (critical)

Create one managed file in Jenkins:

- Type: custom file
- Suggested file ID: `ipdb-manager-config`
- File content: production `config.yaml`

Injected path during build:

- `jenkins/config.yaml`

Used by Makefile:

- `make release-package CONFIG_SRC=jenkins/config.yaml`

## 4) Jenkins credentials

For remote deployment, create SSH credential:

- Suggested credentials ID: `ipdb-manager-ssh`

## 5) Pipeline parameters (key ones)

- `CONFIG_YAML_ID`
- `TARGET_OS`, `TARGET_ARCH`
- `RELEASE_TAG`
- `DEPLOY_ENABLED`
- `DEPLOY_HOST`, `DEPLOY_PORT`, `DEPLOY_USER`, `DEPLOY_DIR`
- `RUN_REMOTE_INSTALL`, `USE_SUDO`, `REMOTE_SERVICE` (default `ipdb-manager`)

## 6) Build and package flow

Pipeline runs:

```bash
make test
make release-package CONFIG_SRC=jenkins/config.yaml TARGET_OS=<...> TARGET_ARCH=<...> [RELEASE_TAG=<...>]
make release-checksum CONFIG_SRC=jenkins/config.yaml TARGET_OS=<...> TARGET_ARCH=<...> [RELEASE_TAG=<...>]
shasum -a 256 *.tar.gz > SHA256SUMS
```

## 7) Deploy flow

When `DEPLOY_ENABLED=true`, pipeline:

1. Uploads tarball to remote `${DEPLOY_DIR}`
2. Extracts tarball
3. Executes:

```bash
make install CONFIG_SRC=config.yaml
systemctl restart ipdb-manager
systemctl status ipdb-manager --no-pager
```

(`sudo` is prefixed if `USE_SUDO=true`)

## 8) Remote host requirements

- Linux host with systemd
- `tar`, `make`, `systemctl`
- Write permission to:
  - `/usr/local/bin`
  - `/etc/ipdb-manager`
  - `/etc/systemd/system`
  - `/var/lib/ipdb-manager/ip2region`

## 9) Manual fallback commands

Build/package manually:

```bash
make test
make release-package CONFIG_SRC=jenkins/config.yaml
make release-checksum CONFIG_SRC=jenkins/config.yaml
```

Remote install/restart:

```bash
make install CONFIG_SRC=config.yaml
systemctl restart ipdb-manager
systemctl status ipdb-manager --no-pager
```
