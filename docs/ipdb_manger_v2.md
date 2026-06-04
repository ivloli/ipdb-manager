# IPDB Manager 二期方案

## 1. 架构原则

- `ipdb-manager` 设计为**零状态控制面**。
- 所有业务状态统一存储在 **Nacos + 制品库**。
- 程序重启、节点迁移、机器替换后，可从外部状态源完整恢复。

## 2. 实现功能

1. XDB 历史版本展示、最新版本展示、当前目标版本展示。
2. 获取全部 Agent 的状态：健康检测、当前使用 XDB 版本、更新状态（更新中、更新完成、更新失败）
3. 指定Agent 更新/回滚 XDB。
4. 指定Agent 配置是否自动更新到新版本。
5. 子网收敛 SubMap 发布与回滚。
6. 子网收敛 SubMap 自动更新开关（全局）。
7. Agent 与 coredns 节点状态查询（在线状态、当前版本、错误信息）。
   - Agent 是获取 XDB 版本，coredns 是获取上面运行的 submap 对应的 XDB 版本。
8. ip 库服务下载 git 上 XDB 的 release 文件的超时时间可配置。

## 3. 实现机制

### 3.1 制品库（文件事实源）

- 存放所有历史 XDB 文件，不删除旧版本。
- XDB 版本历史通过制品库目录/标签获取。

### 3.2 Nacos（控制与状态中心）

- 存放发布指令、版本元信息、策略配置、节点状态。

- `ipdb-manager` 作为写入入口；执行端按约定读取并上报。

- Key：

  - `ip2region_meta`（现有）：XDB 下载信息（`version/url/sha`）。

  - `ipdb_agent_goal.<agent_id>`（新增）：Agent 定向版本，ip 库管理服务写入，agent 监听。

  - `ipdb_agent_policy`（新增）：Agent 自动更新策略（保存哪些 agent 要自动更新）ip 管理服务使用。
  - `ipdb_agent_id_list`（新增）：保存要关注的 agent 列表，ip 管理服务读取，agent 写入。

  - `ipdb_agent_status.<agent_id>`（新增）：Agent 心跳ts、执行状态、当前版本，ip 管理服务读取，agent 写入。

  - `subnet_map` / `subnet_map_v6`（现有）：SubMap 本体（不改格式）。

  - `subnet_map_meta`（现有）：SubMap 元信息，保存版本和更新时间（当前已有 `version/updated_at`）。

  - `subnet_map_policy`（新增）：SubMap 自动更新策略（保存是否自动更新 submap）。

  - `dns_node_status.<node_id>`（新增）：coredns 节点心跳 ts与当前版本submap 版本。


## 4. 自动更新策略

### 4.1 Agent（多实例）

- 维度：`agent_id`。
- 支持每个 Agent 单独配置 `auto_update=true/false`。
- `true` 跟随新目标版本；`false` 仅响应人工 rollout/rollback。

示例（`ipdb_agent_policy`）：

```json
{
  "default_auto_update": true,
  "agents": {
    "agent-hz-01": { "auto_update": true },
    "agent-hz-02": { "auto_update": false }
  },
  "updated_at": "2026-05-29T17:10:00Z",
  "updated_by": "ipdb-manager"
}
```

### 4.2 SubMap（单实例）

- 维度：全局单开关。
- `auto_update=true/false` 控制是否自动推进 SubMap。

示例（`subnet_map_policy`）：

```json
{
  "auto_update": false,
  "updated_at": "2026-05-29T17:10:00Z",
  "updated_by": "ipdb-manager"
}
```

### 4.3 更新过程

#### xdb更新

1. ipdb-manager 下载最新的 xdb 文件，上传制品库（现有逻辑）
2. ipdb-manager 读取更新策略，将最新的版本号写入需要自动更新的ipdb_agent_goal.<agent_id>
3. agent 监听ipdb_agent_goal.<agent_id>，有变化自动走更新路线
4. 回滚时候一样，ipdb-manager 更改 ipdb_agent_goal.<agent_id>，agent 监听到变化，进行更新

#### submap更新

1. ipdb-manager 下载最新的 xdb 文件后，判断 submap 更新策略，如果自动更新，则解析并更新 submap 的 key 内容
2. coredns 节点监听到之后，自动同步（现有逻辑）
3. 回滚时候，ipdb-manager 去制品库获取历史 xdb 并解析更新到 submap 的 key 里面，coredns 监听同上

### 4.4 节点状态收集

agent 端更新ipdb_agent_status.<agent_id>，coredns 更新dns_node_status.<node_id>，ipdb_manager 读取

## 5. 接口设计

所有接口（除 healthz/readyz 外）需要 Bearer Token 认证（如配置了 token）。

### 5.1 接口总览

| # | Method | Path | 说明 |
|---|--------|------|------|
| 1 | GET | `/api/v1/xdb/versions` | 查看已处理的 XDB 版本列表 |
| 2 | POST | `/api/v1/xdb/target` | 设置 Agent 目标版本（触发更新） |
| 3 | POST | `/api/v1/xdb/rollback` | 回滚 Agent 目标版本 |
| 4 | POST | `/api/v1/xdb/reconcile/tag` | 触发制品对齐（下载 + 上传制品库 + 发布 meta） |
| 5 | GET | `/api/v1/xdb/reconcile/tag` | 查询 reconcile 任务状态 |
| 6 | GET | `/api/v1/xdb/status` | 查看服务状态和最近 reconcile |
| 7 | POST | `/api/v1/heartbeat` | Agent 心跳上报 |
| 8 | GET | `/api/v1/agents/status` | 查看 Agent 列表和状态 |
| 9 | GET | `/api/v1/agents/policy` | 查看 Agent 自动更新策略 |
| 10 | PUT | `/api/v1/agents/policy` | 更新 Agent 自动更新策略 |
| 11 | GET | `/api/v1/submap/current` | 查看当前 SubMap 版本 |
| 12 | POST | `/api/v1/submap/publish` | 切换 SubMap 到指定版本 |
| 13 | POST | `/api/v1/submap/rollback` | 回滚 SubMap 到历史版本 |
| 14 | GET | `/api/v1/submap/policy` | 查看 SubMap 自动更新策略 |
| 15 | PUT | `/api/v1/submap/policy` | 更新 SubMap 自动更新策略 |
| 16 | POST | `/api/v1/reconcile` | 手动触发 latest release poll |
| 17 | GET | `/healthz` `/readyz` | 健康检查（无需认证） |

---

### 5.2 XDB 版本管理

#### 5.2.1 GET `/api/v1/xdb/versions`

查看已处理的 XDB 版本列表。

**请求参数：** 无

**响应：**
```json
{
  "current_version": "v3.16.0",
  "current_updated_at": "2026-06-01T14:30:00Z",
  "versions": ["v3.16.0", "v3.15.0", "v3.14.0"]
}
```

| 响应字段 | 类型 | 说明 |
|----------|------|------|
| current_version | string | 当前 subnet_map_meta 中的版本 |
| current_updated_at | string | 当前版本更新时间（ISO 8601） |
| versions | []string | PG reconcile_task 中 status=done 的版本列表 |

---

#### 5.2.2 POST `/api/v1/xdb/target`

设置指定系统的 Agent 目标版本。写入 Nacos goal key，Agent 监听后自动拉取对应版本。

**请求：**
```json
{"system": "default", "version": "v3.16.0"}
```

| 请求字段 | 类型 | 必填 | 说明 |
|----------|------|------|------|
| system | string | 是 | 系统标识，对应 Nacos goal group 后缀 |
| version | string | 是 | 目标版本号（如 v3.16.0） |

**响应：**
```json
{"status": "ok", "system": "default", "version": "v3.16.0"}
```

---

#### 5.2.3 POST `/api/v1/xdb/rollback`

回滚指定系统的 Agent 目标版本。语义为将目标回退到旧版本，内部逻辑同 target。

**请求：**
```json
{"system": "default", "version": "v3.15.0"}
```

| 请求字段 | 类型 | 必填 | 说明 |
|----------|------|------|------|
| system | string | 是 | 系统标识 |
| version | string | 是 | 回退到的目标版本号 |

**响应：**
```json
{"status": "ok", "system": "default", "version": "v3.15.0"}
```

---

#### 5.2.4 POST `/api/v1/xdb/reconcile/tag`

触发指定版本的制品对齐（异步）。流程：下载 GitHub release → 上传制品库 → 发布 Nacos versioned meta。

**请求：**
```json
{"version": "v3.16.0"}
```

| 请求字段 | 类型 | 必填 | 说明 |
|----------|------|------|------|
| version | string | 是 | 要对齐的版本号 |

**响应场景：**

| 场景 | HTTP Code | 响应示例 |
|------|-----------|----------|
| 任务已完成且 <1h | 200 | `{"status":"done","version":"v3.16.0","finished_at":"...","result":{...}}` |
| 正在运行 | 202 | `{"status":"running","version":"v3.16.0","pod_id":"...","started_at":"..."}` |
| 接受执行 | 202 | `{"status":"accepted","version":"v3.16.0","pod_id":"..."}` |

**result 字段结构：**
```json
{
  "version": "v3.16.0",
  "artifact_uploaded": true,
  "nacos_meta_published": true,
  "skipped": false,
  "error": ""
}
```

| result 字段 | 类型 | 说明 |
|-------------|------|------|
| version | string | 处理的版本号 |
| artifact_uploaded | bool | 是否上传了制品库 |
| nacos_meta_published | bool | 是否发布了 Nacos versioned meta |
| skipped | bool | 是否跳过（制品+meta 已就绪） |
| error | string | 错误信息（成功时为空） |

---

#### 5.2.5 GET `/api/v1/xdb/reconcile/tag?version=v3.16.0`

查询指定版本 reconcile 任务状态。

| Query 参数 | 类型 | 必填 | 说明 |
|------------|------|------|------|
| version | string | 是 | 要查询的版本号 |

**响应：**
```json
{
  "version": "v3.16.0",
  "status": "done",
  "pod_id": "ipdb-manager-xxx",
  "started_at": "2026-06-01T14:00:00Z",
  "finished_at": "2026-06-01T14:01:30Z",
  "updated_at": "2026-06-01T14:01:30Z",
  "result": { ... }
}
```

| 响应字段 | 类型 | 说明 |
|----------|------|------|
| status | string | pending / running / done / failed |
| pod_id | string | 执行任务的 Pod 标识 |
| started_at | string | 任务开始时间 |
| finished_at | string | 任务完成时间（运行中为空） |
| result | object | 任务结果（同 5.2.4） |

---

#### 5.2.6 GET `/api/v1/xdb/status`

查看当前 Pod 身份和最近一次 reconcile 状态。

**请求参数：** 无

**响应：**
```json
{
  "pod_id": "ipdb-manager-xxx",
  "latest_reconcile": {
    "version": "v3.16.0",
    "status": "done",
    "started_at": "2026-06-01T14:00:00Z",
    "finished_at": "2026-06-01T14:01:30Z",
    "updated_at": "2026-06-01T14:01:30Z"
  }
}
```

---

### 5.3 Agent 管理

#### 5.3.1 POST `/api/v1/heartbeat`

Agent 心跳上报接口（由 coredns-xdb-updater 周期调用，默认 30s）。

**请求：**
```json
{
  "agent_id": "httpdns-test-zj-01",
  "system": "default",
  "goal_version": "v3.16.0",
  "current_version": "v3.16.0",
  "status": "ok",
  "last_error": "",
  "downstream": {
    "coredns-18181": {"status": "up", "latency_ms": 3},
    "coredns-18182": {"status": "up", "latency_ms": 5}
  }
}
```

| 请求字段 | 类型 | 必填 | 说明 |
|----------|------|------|------|
| agent_id | string | 是 | Agent 唯一标识（取 hostname） |
| system | string | 是 | 所属系统标识 |
| goal_version | string | 否 | 当前 goal 目标版本 |
| current_version | string | 否 | 本地实际运行的 XDB 版本 |
| status | string | 否 | 状态：ok / updating / error |
| last_error | string | 否 | 最后一次错误信息 |
| downstream | map | 否 | 下游探测结果，key=名称，value={status, latency_ms} |

**响应：**
```json
{"status": "ok"}
```

---

#### 5.3.2 GET `/api/v1/agents/status?system=default`

查看指定系统下所有 Agent 在线状态和版本信息。

| Query 参数 | 类型 | 必填 | 说明 |
|------------|------|------|------|
| system | string | 否 | 系统标识，不传则返回所有 |

**响应：**
```json
[
  {
    "agent_id": "httpdns-test-zj-01",
    "system": "default",
    "last_seen_at": "2026-06-01T14:30:00Z",
    "goal_version": "v3.16.0",
    "current_version": "v3.16.0",
    "status": "ok",
    "last_error": "",
    "online": true,
    "downstream": {
      "coredns-18181": {"status": "up", "latency_ms": 3}
    }
  }
]
```

| 响应字段 | 类型 | 说明 |
|----------|------|------|
| online | bool | 最后心跳距今 < 90s 为 true |
| last_seen_at | string | 最后心跳时间（ISO 8601） |
| status | string | Agent 自报状态 |
| downstream | map | 下游探测结果快照 |

---

#### 5.3.3 GET `/api/v1/agents/policy?system=default`

查看指定系统的 Agent 自动更新策略。

| Query 参数 | 类型 | 必填 | 说明 |
|------------|------|------|------|
| system | string | 是 | 系统标识 |

**响应：**
```json
{"system": "default", "auto_update": true, "updated_by": "admin"}
```

| 响应字段 | 类型 | 说明 |
|----------|------|------|
| auto_update | bool | 是否自动跟随新版本推送 goal |
| updated_by | string | 最后修改人 |

---

#### 5.3.4 PUT `/api/v1/agents/policy`

更新 Agent 自动更新策略。auto_update=true 时新版本发布后自动推送 goal 给该系统的所有 Agent。

**请求：**
```json
{"system": "default", "auto_update": true, "updated_by": "admin"}
```

| 请求字段 | 类型 | 必填 | 说明 |
|----------|------|------|------|
| system | string | 是 | 系统标识 |
| auto_update | bool | 是 | 是否自动跟随新版本 |
| updated_by | string | 否 | 操作人 |

**响应：**
```json
{"status": "ok"}
```

---

### 5.4 SubMap 管理

#### 5.4.1 GET `/api/v1/submap/current`

查看当前 subnet_map 版本信息（v4/v6 分别展示）。

**请求参数：** 无

**响应：**
```json
{
  "v4": {"data_id": "subnet_map", "version": "v3.16.0", "updated_at": "2026-06-01T14:30:00Z"},
  "v6": {"data_id": "subnet_map_v6", "version": "v3.16.0", "updated_at": "2026-06-01T14:30:00Z"}
}
```

| 响应字段 | 类型 | 说明 |
|----------|------|------|
| data_id | string | Nacos 中 SubMap 的 dataId |
| version | string | 当前 SubMap 对应的 XDB 版本 |
| updated_at | string | 最后更新时间 |

---

#### 5.4.2 POST `/api/v1/submap/publish`

将 subnet_map 切换到指定版本。异步执行：下载 XDB → 解析子网 → 重建 subnet_map → 发布到 Nacos。

**请求：**
```json
{"version": "v3.16.0"}
```

| 请求字段 | 类型 | 必填 | 说明 |
|----------|------|------|------|
| version | string | 是 | 目标版本号（需已 reconcile 完成） |

**响应：**
```json
{"status": "accepted", "version": "v3.16.0"}
```

---

#### 5.4.3 POST `/api/v1/submap/rollback`

回滚 subnet_map 到指定历史版本。内部逻辑同 publish，语义为回退。

**请求：**
```json
{"version": "v3.15.0"}
```

| 请求字段 | 类型 | 必填 | 说明 |
|----------|------|------|------|
| version | string | 是 | 回滚到的版本号 |

**响应：**
```json
{"status": "accepted", "version": "v3.15.0", "action": "rollback"}
```

---

#### 5.4.4 GET `/api/v1/submap/policy`

查看 SubMap 自动更新策略。

**请求参数：** 无

**响应：**
```json
{"auto_update": false, "updated_by": "admin"}
```

| 响应字段 | 类型 | 说明 |
|----------|------|------|
| auto_update | bool | 新版本 reconcile 完成后是否自动重建 SubMap |
| updated_by | string | 最后修改人 |

---

#### 5.4.5 PUT `/api/v1/submap/policy`

更新 SubMap 自动更新策略。

**请求：**
```json
{"auto_update": true, "updated_by": "admin"}
```

| 请求字段 | 类型 | 必填 | 说明 |
|----------|------|------|------|
| auto_update | bool | 是 | 是否自动更新 SubMap |
| updated_by | string | 否 | 操作人 |

**响应：**
```json
{"status": "ok"}
```

---

### 5.5 系统接口

#### 5.5.1 POST `/api/v1/reconcile`

手动触发一次 latest release poll（与 cron 定时任务逻辑相同）。

**请求：** 无 body

**响应：**
```json
{"status": "ok"}
```

---

#### 5.5.2 GET `/healthz` / GET `/readyz`

健康检查接口，无需认证。用于 K8S liveness/readiness 探针。

**请求参数：** 无

**响应：**
```json
{"status": "ok"}
```

## 6. 兼容性与持久化

- 不修改 `subnet_map` / `subnet_map_v6` 结构，避免影响存量 coredns。
- 版本信息通过现有 `subnet_map_meta` 承载；约定 `subnet_map_meta.version` 即 ip2region 的 release 版本号（如 `v3.16.0`）。
- 策略配置与运行状态全部持久化到 Nacos。
- 本地配置文件仅保留连接参数与基础运行参数，不承载业务状态。

---

## 7. coredns-xdb-updater 二期新增功能

### 7.1 Goal 模式

Agent 从 Nacos 监听 goal key，获取目标版本号后从对应的 versioned meta 获取下载信息。

- **goal key 格式**: group=`ipdb_agent_goal_<system>`, dataId=`default`
- **goal 内容**: `{"version":"v3.16.0","updated_at":"...","updated_by":"ipdb-manager"}`
- **触发方式**: `POST /api/v1/xdb/target {"system":"default","version":"v3.16.0"}` 由 ipdb-manager 写入

Agent 监听到 goal 变更后自动拉取对应 versioned meta（`ip2region_meta_v3.16.0`），然后对比本地 SHA256 决定是否下载。

### 7.2 心跳上报

Agent 周期性（默认 30s）向 ipdb-manager 发送心跳：

```json
POST /api/v1/heartbeat
{
  "agent_id": "httpdns-test-zj-01",
  "system": "default",
  "goal_version": "v3.16.0",
  "current_version": "v3.16.0",
  "status": "ok",
  "last_error": "",
  "downstream": {
    "coredns-18181": {"status": "up", "latency_ms": 3},
    "coredns-18182": {"status": "up", "latency_ms": 5}
  }
}
```

配置项 `report.endpoint` 只写到根路径，代码自动拼 `/api/v1/heartbeat`。

### 7.3 下游探测 (Downstream Probes)

对配置的 URL 做 HTTP GET，返回 2xx 视为 `up`，否则 `down`。结果附在心跳里上报。

### 7.4 下载抖动 (Download Jitter)

防止大规模 agent 同时下载制品库产生流量尖峰。每个 agent 在确认需要下载后，随机等待 `[0, download_jitter)` 时间后再开始下载。

- 随机数使用 Go 1.22+ `math/rand/v2`，自动 crypto-seeded，保证分散性。
- 若 jitter 等待期间收到新的 goal 版本变更，会立即取消当前等待并进入新的更新流程。
- 取消信号通过 `atomic.Pointer[context.CancelFunc]` 传递，无锁、无死锁风险。
- timer 正常到期后调 `cancel()` 释放 context，无内存泄漏。

配置项 `update.download_jitter`，默认 120s。设为 0 禁用抖动。

---

## 8. 测试环境配置

### 网络拓扑

```
Agent (16.162.8.139, hk-test-01)
  ↓ heartbeat / goal listen
Nginx (120.25.74.229:39191, stream proxy)
  ↓
ipdb-manager (10.0.25.221:39191)
  ↓
PostgreSQL (10.0.25.221:5432, db=ipdb_manager)
Nacos (120.25.74.229:18848, ns=scloud-prod)
JFrog (artifactory.gainetics.io, repo=observe-dnps)
```

### ipdb-manager 测试配置 (`config.test_v2.yaml`)

- **API 端口**: 39191（通过 nginx stream 转发）
- **PG DSN**: `postgres://ipdb_manager:B87nSmvIyHhgbUILPWVbTaGW@10.0.25.221:5432/ipdb_manager?sslmode=disable`
- **Nacos**: 120.25.74.229:18848, namespace=scloud-prod
- **定时任务**: cron `0 1 * * *`（每天凌晨 1 点）

### coredns-xdb-updater 测试配置 (`config.test_v2.yaml`)

- **Nacos**: 120.25.74.229:18848, namespace=scloud-prod
- **Goal**: group=`ipdb_agent_goal_default`, dataId=`default`
- **Report endpoint**: `http://120.25.74.229:39191`（经 nginx 转发到 ipdb-manager）
- **Download jitter**: 120s
- **Downstream**: coredns prometheus metrics (29159/29160)

### 操作命令

```bash
# 设置 goal 版本
curl -X POST http://120.25.74.229:39191/api/v1/xdb/target \
  -H "Content-Type: application/json" \
  -d '{"system":"default","version":"v3.16.0"}'

# 查看 agent 状态
curl http://120.25.74.229:39191/api/v1/agents/status

# 查看当前 submap 版本
curl http://120.25.74.229:39191/api/v1/submap/current

# 准备指定版本（制品+meta）
curl -X POST http://120.25.74.229:39191/api/v1/xdb/reconcile/tag \
  -H "Content-Type: application/json" \
  -d '{"version":"v3.16.0"}'

# 切换 submap 到指定版本
curl -X POST http://120.25.74.229:39191/api/v1/submap/publish \
  -H "Content-Type: application/json" \
  -d '{"version":"v3.16.0"}'
```
