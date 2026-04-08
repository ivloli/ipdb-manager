# ipdb-manager (Standalone)

`ipdb-manager` is split from `subnet-manager` and runs as an independent service.

## Quick start

```bash
make build
sudo make install
sudo make start
```

## Service files

- Binary: `/usr/local/bin/ipdb-manager`
- Config: `/etc/ipdb-manager/config.yaml`
- Env: `/etc/ipdb-manager/env`
- Data dir: `/var/lib/ipdb-manager/ip2region`
- Systemd: `/etc/systemd/system/ipdb-manager.service`

## Artifact publish + Nacos meta

When `artifact_repos` and `nacos_targets` are configured, each reconcile run will:

1. Upload missing `ip2region_v4.xdb` / `ip2region_v6.xdb` artifacts to target repository.
2. Publish Nacos `ip2region_meta` (`version/xdb_url/xdb_sha256`) for v4 and v6.

Use env files for secrets (references are env var names):

- `artifact_repos[].auth.token_ref` or `username_ref/password_ref`
- `nacos_targets[].auth.username_ref/password_ref`

## Release package

```bash
make release-package
make release-checksum
```

You can also set your own tag:

```bash
make release-package RELEASE_TAG=v20260405-r1
```
