# IPDB Manager 启动与测试指南

## 前置条件

- Go 1.25+
- PostgreSQL（用于 node_status + reconcile_task）
- Nacos 实例
- 制品库（Nexus 或 JFrog）
- GitHub Token（可选，避免匿名限速 60 req/h）

---

## 1. 本地开发启动

### 1.1 编译

```bash
cd ipdb_manager
make build
# 产出: bin/ipdb-manager
```

### 1.2 准备配置文件

从零开始：
```bash
cp config.yaml.example config.local.yaml
```

或直接使用已有测试配置（已配好 Nacos + JFrog + PG）：
```bash
cp config.test.new.yaml config.local.yaml
# 然后设置环境变量: GITHUB_TOKEN, PG_DSN, IPDB_MANAGER_API_TOKEN
```

关键字段：
```yaml
nacos:
  addr: "your-nacos:8848"
  namespace: ""
  username: "nacos"
  password: "nacos"

ip2region:
  dir: "/tmp/ipdb-data"
  download_timeout: "300s"
  github_token: "${GITHUB_TOKEN}"   # 环境变量引用

api:
  listen: ":9090"
  token: "your-dev-token"           # 留空则不鉴权

node_status:
  persist: true
  persist_driver: "pg"
  persist_dsn: "postgres://user:pass@localhost:5432/ipdb?sslmode=disable"

artifact_repos:
  - id: "nexus-main"
    type: "nexus"
    base_url: "https://your-nexus.example.com"
    repo: "ipdb-releases"
    enabled: true
    auth:
      username_ref: "NEXUS_USER"
      password_ref: "NEXUS_PASS"

nacos_targets:
  - id: "prod-sg"
    server_addr: "nacos-sg:8848"
    namespace: ""
    auth:
      username_ref: "NACOS_SG_USER"
      password_ref: "NACOS_SG_PASS"
    artifact_repo_id: "nexus-main"
    artifact_path_templates:
      v4: "ip2region/{{version}}/ip2region_v4.xdb"
      v6: "ip2region/{{version}}/ip2region_v6.xdb"
    publish:
      v4:
        group: "ip2region"
        data_id: "ip2region_meta_v4"
      v6:
        group: "ip2region"
        data_id: "ip2region_meta_v6"
    enabled: true
```

### 1.3 启动

**Poll 模式**（每小时检查一次 GitHub 最新版）：
```bash
export GITHUB_TOKEN="ghp_xxx"
export NEXUS_USER="admin"
export NEXUS_PASS="xxx"
export NACOS_SG_USER="nacos"
export NACOS_SG_PASS="nacos"

./bin/ipdb-manager -config config.local.yaml
```

**Cron 模式**（按 cron 表达式触发，Asia/Shanghai 时区）：
```yaml
scheduler:
  cron: "0 2 * * *"   # 每天凌晨 2 点
```

---

## 2. Docker 启动

```bash
docker build -t ipdb-manager:dev .

docker run -d \
  --name ipdb-manager \
  -p 9090:9090 \
  -e GITHUB_TOKEN=ghp_xxx \
  -e NEXUS_USER=admin \
  -e NEXUS_PASS=xxx \
  -e NACOS_SG_USER=nacos \
  -e NACOS_SG_PASS=nacos \
  -v $(pwd)/config.local.yaml:/etc/ipdb-manager/config.yaml \
  ipdb-manager:dev
```

---

## 3. K8S 部署（Helm）

```bash
# 本地渲染检查
helm template ipdb-manager deploy/chart/ \
  -f deploy/values-prod.yaml \
  --set image.tag=dev

# 部署
helm upgrade --install ipdb-manager deploy/chart/ \
  --namespace scloud-dnps-dev \
  -f deploy/values-prod.yaml \
  --set image.tag=<your-tag>
```

Secret 需提前创建：
```bash
kubectl -n scloud-dnps-dev create secret generic ipdb-manager-secrets \
  --from-literal=pg-dsn="postgres://..." \
  --from-literal=nacos-username="nacos" \
  --from-literal=nacos-password="nacos"
```

---

## 4. API 测试

### 接口总览

| Method | Path | 说明 |
|--------|------|------|
| GET | `/healthz` `/readyz` | 健康检查 |
| POST | `/api/v1/reconcile` | 触发一次 latest poll（异步） |
| POST | `/api/v1/heartbeat` | Agent 心跳上报 |
| GET | `/api/v1/agents/status` | 查看 Agent 在线状态 |
| GET/PUT | `/api/v1/agents/policy` | Agent 自动更新策略 |
| GET/PUT | `/api/v1/submap/policy` | SubMap 自动更新策略 |
| GET | `/api/v1/xdb/versions` | 当前版本 + 历史版本列表 |
| POST | `/api/v1/xdb/target` | 设置 Agent 目标版本（Goal） |
| POST | `/api/v1/xdb/rollback` | 回滚 Agent 目标版本 |
| POST | `/api/v1/xdb/reconcile/tag` | 按指定 Tag 对齐（异步） |
| GET | `/api/v1/xdb/reconcile/tag?version=` | 查询对齐任务状态 |
| GET | `/api/v1/xdb/status` | 系统状态 + 最近 reconcile |
| GET | `/api/v1/submap/current` | SubMap 当前版本状态 |
| POST | `/api/v1/submap/publish` | 指定版本重建 SubMap（异步） |
| POST | `/api/v1/submap/rollback` | 回滚 SubMap 到指定版本（异步） |

### 4.1 健康检查

```bash
curl http://localhost:9090/healthz
# {"status":"ok"}
```

### 4.2 手动触发 latest reconcile

```bash
curl -X POST http://localhost:9090/api/v1/reconcile \
  -H "Authorization: Bearer your-dev-token"
# {"status":"ok","trigger":"manual"}
```

### 4.3 按 Tag 对齐（异步）

触发：
```bash
curl -X POST http://localhost:9090/api/v1/xdb/reconcile/tag \
  -H "Authorization: Bearer your-dev-token" \
  -H "Content-Type: application/json" \
  -d '{"version": "v2.8.0"}'
# {"status":"accepted","version":"v2.8.0","pod_id":"ipdb-manager-xxx"}
```

查询进度：
```bash
curl "http://localhost:9090/api/v1/xdb/reconcile/tag?version=v2.8.0" \
  -H "Authorization: Bearer your-dev-token"
# {"version":"v2.8.0","status":"done","result":{...},"finished_at":"..."}
```

### 4.4 查看系统状态

```bash
curl http://localhost:9090/api/v1/xdb/status \
  -H "Authorization: Bearer your-dev-token"
# {"pod_id":"...","latest_reconcile":{"version":"v2.9.0","status":"done",...}}
```

### 4.5 设置 Goal（指定 agent 目标版本）

```bash
curl -X POST http://localhost:9090/api/v1/xdb/target \
  -H "Authorization: Bearer your-dev-token" \
  -H "Content-Type: application/json" \
  -d '{"system": "default", "version": "v2.8.0"}'
```

### 4.6 回滚

```bash
curl -X POST http://localhost:9090/api/v1/xdb/rollback \
  -H "Authorization: Bearer your-dev-token" \
  -H "Content-Type: application/json" \
  -d '{"system": "default", "version": "v2.7.0"}'
```

### 4.7 查看 XDB 版本列表

```bash
curl http://localhost:9090/api/v1/xdb/versions \
  -H "Authorization: Bearer your-dev-token"
# {"current_version":"v2.9.0","current_updated_at":"...","versions":["v2.9.0","v2.8.0"]}
```

### 4.8 查看 SubMap 当前状态

```bash
curl http://localhost:9090/api/v1/submap/current \
  -H "Authorization: Bearer your-dev-token"
# {"v4":{"data_id":"subnet_map","version":"v2.9.0","updated_at":"..."},"v6":{...}}
```

### 4.9 手动发布 SubMap（指定版本）

```bash
curl -X POST http://localhost:9090/api/v1/submap/publish \
  -H "Authorization: Bearer your-dev-token" \
  -H "Content-Type: application/json" \
  -d '{"version": "v2.9.0"}'
# {"status":"accepted","version":"v2.9.0"}
```

### 4.10 回滚 SubMap（指定版本）

```bash
curl -X POST http://localhost:9090/api/v1/submap/rollback \
  -H "Authorization: Bearer your-dev-token" \
  -H "Content-Type: application/json" \
  -d '{"version": "v2.7.0"}'
# {"status":"accepted","version":"v2.7.0","action":"rollback"}
```

---

## 5. 测试场景

### 场景 A：冷启动（全新 Pod，远端已全部就绪）

预期行为：
1. 启动 → 触发 reconcile
2. `checkArtifactsExist` → true
3. `checkNacosFullySynced` → true
4. 日志输出 "fully synced, nothing to do"
5. **不下载任何文件**

### 场景 B：冷启动（远端部分缺失）

预期行为：
1. 启动 → 触发 reconcile
2. 某项检查 → false
3. 下载 GitHub release → 补齐制品库 → 发布 Nacos
4. 日志输出 "update complete"

### 场景 C：人工删除 Nacos key

预期行为：
- 下次 poll/cron 触发时，`checkNacosFullySynced` → false
- 重新下载+发布，自动修复

### 场景 D：按 Tag 对齐历史版本

```bash
# 指定一个历史 tag
curl -X POST .../api/v1/xdb/reconcile/tag -d '{"version":"v2.7.0"}'
# 等待完成
curl .../api/v1/xdb/reconcile/tag?version=v2.7.0
# result 应显示 artifact_uploaded=true, nacos_meta_published=true
# 注意：reconcile/tag 不更新 subnet_map，需要手动调 submap/publish 切换
```

### 场景 E：并发请求幂等性

```bash
# 两个终端同时发
curl -X POST .../api/v1/xdb/reconcile/tag -d '{"version":"v2.8.0"}'
curl -X POST .../api/v1/xdb/reconcile/tag -d '{"version":"v2.8.0"}'
# 一个返回 "accepted"，另一个返回 "running"
# 只有一个 Pod 实际执行
```

### 场景 F：Pod 崩溃恢复

- Pod A 拿到锁后崩溃（不再心跳）
- 10 分钟后，任意 Pod 的新请求可以重新抢到锁执行

---

## 6. 日志关键字

| 日志前缀 | 含义 |
|----------|------|
| `[watcher] reconcile trigger=` | reconcile 循环开始 |
| `[watcher] latest upstream release:` | 从 GitHub 获取到的最新 tag |
| `[watcher] version ... fully synced` | 三项全满足，跳过 |
| `[watcher] version ... not fully in artifact repo` | 需要下载 |
| `[watcher] reconcile-tag=... completed` | 按 tag 对齐完成 |
| `[api] reconcile-tag=... failed` | 按 tag 对齐失败 |
| `[syncer] fast path hit` | subnet_map 已是目标版本，跳过重建 |

---

## 7. 环境变量参考

| 变量 | 说明 | 必须 |
|------|------|------|
| `GITHUB_TOKEN` | GitHub API token，避免限速 | 推荐 |
| `HOSTNAME` | Pod ID（K8S 自动设置） | 自动 |
| `PG_DSN` | PostgreSQL 连接串 | 是 |
| 制品库凭证 | 对应 config 中 `auth.*_ref` 的值 | 是 |
| Nacos 凭证 | 对应 config 中 `auth.*_ref` 的值 | 是 |
