# IPDB Manager 二期重构 — 技术变更文档

## 概览

本次修改完成 IPDB Manager Phase 2 的剩余核心任务，主要涉及：

1. **下载超时可配置**（默认 300s）
2. **零状态重构**（移除本地版本文件依赖）
3. **按 Tag 对齐接口**（异步 + PG 乐观锁幂等保证）

---

## 1. 下载超时可配置

### 变更文件
- `config/config.go` — 新增 `DownloadTimeout` 字段
- `watcher/version_watcher.go` — 新增 `getDownloadHTTPClient()`

### 设计
- `IP2RegionConfig` 新增 `download_timeout` 字段，默认 300s
- GitHub API 调用（获取 release 元数据）仍使用 30s 超时的 `getGitHubHTTPClient()`
- Release tarball 下载使用 `getDownloadHTTPClient()`，读取 `DownloadTimeout` 配置
- 制品库通信（上传/检查）使用 `getArtifactHTTPClient()`，同样使用 `DownloadTimeout`

### 配置示例
```yaml
ip2region:
  download_timeout: "300s"  # 可选，默认 300s
```

---

## 2. 零状态重构

### 变更文件
- `config/config.go` — 移除 `LocalStateConfig` 结构体和 `LocalState` 字段
- `watcher/version_watcher.go` — 移除 `VersionFile`/`LegacyVersion` 字段和相关方法
- `main.go` — 移除版本文件引用

### 设计思路

**之前**：ipdb-manager 依赖本地文件 `.upstream_release_tag` 判断当前版本，Pod 重启或 ephemeral storage 清空会导致重复下载。

**现在**：通过远端状态判定是否需要处理，三项全满足则完全跳过：

| 检查项 | 方法 | 含义 |
|--------|------|------|
| 制品库 xdb 存在 | `checkArtifactsExist()` — HEAD 请求 | 该版本已上传制品库 |
| Nacos versioned meta 存在 | `checkNacosFullySynced()` — GetConfig | 各 NacosTarget 的 versioned dataId 已发布 |

### Reconcile 流程（每次 poll/cron 触发）

```
1. GET GitHub /releases/latest → latestTag
2. checkArtifactsExist(latestTag)
   → 所有 nacos_targets 的 v4/v6 artifact path 都存在？
3. checkNacosFullySynced(latestTag)
   → 各 NacosTarget versioned dataId 存在？
4. 全部满足 → return（无需操作）
5. 任一不满足 → 下载 release → 上传制品库 → 发布 versioned meta
```

> SubMap 同步由独立流程处理（`submap/publish` API 或 poll 自动更新策略）。

### 移除的代码
- `migrateLegacyVersionFile()` — 不再有本地版本文件迁移
- `readLocalVersion()` / `writeLocalVersion()` — 不再读写本地状态
- `targetFilesMissing()` — 不再基于本地文件判断缺失
- `LocalStateConfig` 结构体 — 配置中不再有 `local_state` 段

---

## 3. 按 Tag 对齐接口（异步 + 乐观锁）

### 变更文件
- `store/pg.go` — 新增 `reconcile_task` 表 DDL
- `store/reconcile_task.go` — 新文件，PG CRUD 方法
- `watcher/version_watcher.go` — 新增 `ReconcileByTag()` + `fetchReleaseByTag()`
- `api/server.go` — 新增 `PodID` 字段 + 路由注册
- `api/handlers_reconcile.go` — 新文件，异步 reconcile 接口实现
- `main.go` — 新增 `podID()` 获取 Pod 标识

### 数据库表

```sql
CREATE TABLE reconcile_task (
    version     TEXT PRIMARY KEY,
    status      TEXT NOT NULL DEFAULT 'pending',   -- pending/running/done/failed
    pod_id      TEXT NOT NULL DEFAULT '',
    started_at  TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    result      JSONB,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

### 乐观锁机制

```sql
INSERT INTO reconcile_task (version, status, pod_id, started_at, updated_at)
VALUES ($1, 'running', $2, NOW(), NOW())
ON CONFLICT (version) DO UPDATE
  SET status = 'running', pod_id = $2, started_at = NOW(), updated_at = NOW()
  WHERE reconcile_task.status != 'running'
     OR reconcile_task.updated_at < NOW() - INTERVAL '10 minutes';
```

- `affected = 1` → 拿到锁，本 Pod 执行
- `affected = 0` → 其他 Pod 正在执行，返回 202
- 超时兜底（10 分钟）：Pod 崩溃后任务不会永远卡住
- 执行中 30s 心跳：`UPDATE updated_at` 防止被误抢
- 完成时带条件：`WHERE pod_id = $2 AND status = 'running'`

### API 接口

> 完整接口文档参见 `ipdb_manger_v2.md` 第 5 章。此处仅列出本次新增/修改的接口。

| # | Method | Path | 说明 |
|---|--------|------|------|
| 1 | POST | `/api/v1/xdb/reconcile/tag` | 触发指定 tag 制品对齐（异步） |
| 2 | GET | `/api/v1/xdb/reconcile/tag?version=xxx` | 查询 reconcile 任务状态 |
| 3 | GET | `/api/v1/xdb/status` | 当前 Pod + 最近 reconcile 状态 |
| 4 | GET | `/api/v1/xdb/versions` | 查看版本列表（current + history） |
| 5 | POST | `/api/v1/xdb/target` | 设置 Agent 目标版本 |
| 6 | POST | `/api/v1/xdb/rollback` | 回滚 Agent 目标版本 |
| 7 | GET | `/api/v1/submap/current` | 查看当前 SubMap 版本 |
| 8 | POST | `/api/v1/submap/publish` | 切换 SubMap 到指定版本（异步） |
| 9 | POST | `/api/v1/submap/rollback` | 回滚 SubMap 到历史版本（异步） |
| 10 | POST | `/api/v1/heartbeat` | Agent 心跳上报 |
| 11 | GET | `/api/v1/agents/status` | 查看 Agent 列表和状态 |
| 12 | GET/PUT | `/api/v1/agents/policy` | Agent 自动更新策略 |
| 13 | GET/PUT | `/api/v1/submap/policy` | SubMap 自动更新策略 |

### ReconcileByTag 执行逻辑

```
1. checkArtifactsExist + checkNacosFullySynced
   → 全部就绪？标记 skipped，done
2. fetchReleaseByTag(tag) — GitHub /releases/tags/{tag}
3. downloadAndExtractReleaseData
4. publishIP2RegionMeta (上传制品库 + 发布 Nacos versioned meta)
5. 返回 ReconcileResult（哪些步骤执行了）
```

> **注意**：reconcile/tag 不再同步 subnet_map。SubMap 切换由独立接口 `/api/v1/submap/publish` 完成。

### ReconcileResult 结构

```go
type ReconcileResult struct {
    Version            string `json:"version"`
    ArtifactUploaded   bool   `json:"artifact_uploaded"`
    NacosMetaPublished bool   `json:"nacos_meta_published"`
    Skipped            bool   `json:"skipped"`
    Error              string `json:"error,omitempty"`
}
```

### checkNacosFullySynced 逻辑

仅检查各 NacosTarget 的 versioned dataId（如 `ip2region_meta_v3.16.0`）是否存在且内容非空。
不再检查 subnet_map_meta 的 version 字段（因为 reconcile 和 submap 已解耦）。

### SyncSubnetMapByTag 方法

```go
func (w *VersionWatcher) SyncSubnetMapByTag(tag string) error
```

只做 subnet_map 的下载+重建+发布，不触发 artifact 上传和 ip2region_meta 发布。

---

## 4. 架构决策

| 决策 | 原因 |
|------|------|
| 不自动补齐历史 tag | 制品库是持久化层，不应丢失；补齐通过手动 API 触发 |
| Legacy agent 回滚不额外处理 | 旧 agent 不支持版本切换，永远跟最新版即可 |
| 用 PG 而非 Redis 做任务锁 | 已有 PG 依赖，不引入新组件 |
| 任务结果有效期 1h | 超过 1h 的 done 结果允许被重新触发，确保能修复中间故障 |
| PodID 取 HOSTNAME | K8S Pod 天然唯一，无需额外生成 |
| reconcile/tag 与 submap 解耦 | reconcile 只负责制品+meta 就绪，submap 切换是独立的发布动作，避免 reconcile 意外改变线上流量 |
| node_status 软删除 | `deleted_at BIGINT DEFAULT 0` + 唯一索引 `(agent_id, deleted_at)`，保留历史记录 |

---

## 5. 文件变更清单

| 文件 | 操作 |
|------|------|
| `config/config.go` | 新增 DownloadTimeout，移除 LocalStateConfig |
| `config.yaml.example` | 更新示例 |
| `config.test.new.yaml` | 新文件，二期测试配置 |
| `watcher/version_watcher.go` | 零状态重构 + ReconcileByTag + SyncSubnetMapByTag + fetchReleaseByTag |
| `store/pg.go` | 新增 reconcile_task 表 DDL |
| `store/reconcile_task.go` | 新文件，PG CRUD + ListReconcileVersions |
| `api/server.go` | 新增 PodID + 路由注册 (reconcile/tag, xdb/status, submap/current) |
| `api/handlers_reconcile.go` | 新文件，异步 reconcile-tag 接口 |
| `api/handlers_xdb.go` | 重写 handleXDBVersions 实现 |
| `api/handlers_submap.go` | 重写，实现 current/publish/rollback 三个接口 |
| `main.go` | PodID + DownloadTimeout + 移除 LocalState 引用 |
