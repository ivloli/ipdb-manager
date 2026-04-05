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

## Release package

```bash
make release-package
make release-checksum
```

You can also set your own tag:

```bash
make release-package RELEASE_TAG=v20260405-r1
```
