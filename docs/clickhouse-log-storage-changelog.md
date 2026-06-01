# ClickHouse 日志存储 — Changelog & 实现细节

> 本文记录从 commit 99d183a70 起对调用明细日志（`logs` 表）所做的接入 ClickHouse 异步日志库的全部改动。设计依据见 `docs/clickhouse-log-storage-plan.md`。

---

## 概览

### 新增

- **可选 ClickHouse 日志后端**：`LOG_SQL_DSN=clickhouse://...` 即可让 `logs` 表的写入、查询、统计、清理全部走 ClickHouse；主业务库（user/token/quota/task/midjourney/quota_data 等）仍由 SQLite/MySQL/PostgreSQL 持有，不受影响。
- **`LogStore` 抽象层**：所有原直接 `LOG_DB.*` 的调用收敛到 `model.LogStore` 接口，`SQLLogStore` 与 `ClickHouseLogStore` 各自负责一类持久化策略。SQL 模式行为零回归。
- **应用侧 Snowflake-like ID 生成器**：`common.LogIDGenerator`（41 bit 毫秒 + 10 bit nodeID + 12 bit seq），不依赖 ClickHouse 自增能力。
- **异步批量写入**：`clickhouseWriter` 内存队列 + N 个 worker goroutine，按 batch size / flush 间隔聚合 INSERT。fail-open，请求热路径永不阻塞。
- **后台 ensureTable 自愈**：ClickHouse 启动时不可达不再阻塞主进程；后台每 60s 重试建表，成功后自动恢复 flush。
- **专用 ClickHouse SQL 查询**：列表/统计/清理走手写 SQL，含时间范围保护、offset 上限、TTL 模式下用 `ALTER TABLE DELETE` 异步 mutation。
- **Snowflake ID 无损输出**：响应中新增 `id_str` 字段，避免 JS 客户端在 ID 超 `2^53` 后静默丢精度。
- **docker-compose.clickhouse.yml**：开箱即用的 PostgreSQL + Redis + ClickHouse 24.8 + new-api 栈。
- **`.env.example`**：12 个新 env 变量（中英双语注释）。

### 修改

- `gorm.io/gorm` 1.25 → 1.30（`go get gorm.io/driver/clickhouse` 触发的连带升级）。
- `model/log.go`：所有 `LOG_DB.Create/.Where/.Count/.Find/.Scan` 改走 `GetLogStore()`。函数签名（`RecordLog`、`RecordConsumeLog`、`GetAllLogs`、`SumUsedQuota`、`DeleteOldLog` 等）保持完全兼容。
- `model/main.go`：`chooseDB` 增加 `clickhouse://` / `tcp://` 识别；主库使用 ClickHouse 时 `FatalLog` 退出；`InitLogDB` 跳过 ClickHouse 的 `AutoMigrate(&Log{})`；`CloseDB` 在关连接前 best-effort flush 队列。
- `model/log.go::Log`：新增 `IdStr string \`json:"id_str,omitempty" gorm:"-"\``。
- 测试 `TestMain` / 测试 helper（`model/task_cas_test.go`、`controller/token_test.go`、`controller/model_list_test.go`、`service/task_billing_test.go`、`service/waffo_pancake_test.go`）注入 `SetLogStoreForTest(NewSQLLogStoreForTest())`。

### 不变 / 不在范围

- 主业务数据库（SQLite/MySQL/PostgreSQL）逻辑零改动。
- `tasks`、`midjourneys`、`quota_data`、`perf_metrics` 表不动。
- 历史 SQL 日志不迁移、不双读、不双写。
- 前端 `web/default/src/features/usage-logs/` 不需改动。
- 压测脚本和管理后台 ClickHouse 健康面板不在本期。

---

## 影响面与运行时行为

### 默认部署（`LOG_SQL_DSN` 未设置）

行为完全不变：`LOG_DB == DB`，`InitLogStore` 安装 `sqlLogStore`，每条日志同步 `LOG_DB.Create(&Log{})`。SQLite 默认数据库照常 `AutoMigrate(&Log{})`。

### 独立 SQL 日志库（`LOG_SQL_DSN=mysql/postgres/sqlite`）

行为完全不变：`InitLogStore` 安装 `sqlLogStore` 但指向独立 `LOG_DB`，写入仍同步。

### ClickHouse 日志库（`LOG_SQL_DSN=clickhouse://...`）

启动时序：
1. `chooseDB("LOG_SQL_DSN", true)` 走 `clickhouse://` 分支，用 `gorm.io/driver/clickhouse` 打开连接（懒连接，DSN 错误此时不抛）；置 `common.LogSqlType=DatabaseTypeClickHouse`、`common.UsingClickHouse=true`。
2. `InitLogDB` 跳过 `AutoMigrate(&Log{})`（ClickHouse 不是 OLTP，GORM 推断不出 MergeTree/分区/TTL）。
3. `InitLogStore` 调用 `newClickHouseLogStore`：
   - `ensureTable` 执行显式 `CREATE TABLE IF NOT EXISTS logs (...) ENGINE = MergeTree ...`；如果设置了 `LOG_RETENTION_DAYS` 还会跑一次 `ALTER TABLE logs MODIFY TTL` 同步当前 env 值。
   - 创建 `clickhouseWriter` 启动 N 个 worker。
   - 启动 stats logger goroutine（30s tick）。
4. 失败但 `LOG_CLICKHOUSE_FAIL_OPEN=true`：仍安装 store，但 `tableReady=false`；启动后台 retry goroutine 每 60s 重试 `ensureTable`。
5. 失败且 `LOG_CLICKHOUSE_FAIL_OPEN=false`：返回错误，主进程 `FatalLog` 退出。

写入路径（`RecordConsumeLog` 等）：
1. 业务层填好 `*Log`，调 `GetLogStore().Record(log)`。
2. `clickhouseLogStore.Record`：`log.Id == 0` 时调 `idGen.Generate()` 填 `int(uint64)`；非 sync_write 模式 `writer.Submit(log)`。
3. `Submit` 持读锁判 `closed`/queue 满，全在内存操作，**永不阻塞主请求**。
4. Worker 累积到 `LOG_BATCH_SIZE` 或 `LOG_BATCH_FLUSH_MS` 后 `flush(batch)`：
   - 若 `tableReady=false`：增 `droppedNotReady` 立即返回；
   - 否则拼一条多行 `INSERT INTO logs (...) VALUES (...),(...),...`，重试 1 次失败仍失败则丢批 + 增 `droppedInsertFailed`。

查询路径（`GetAllLogs`、`GetUserLogs` 等）：
1. 走 `GetLogStore().QueryAll(LogQueryParams)`。
2. ClickHouse 模式：`applyClickHouseQueryGuards` 强制时间窗口 ≤ `LOG_CLICKHOUSE_MAX_QUERY_RANGE_DAYS`（`request_id` 精确查询除外）；offset > `LOG_CLICKHOUSE_MAX_OFFSET` 直接报错；查询走 `Raw + Scan` 到 `clickhouseLogRow{Id uint64}`，`toLog()` 同时填 `Id int` 与 `IdStr string`；channel 名走主库 `DB.Table("channels")` 或 `CacheGetChannel` 补全。
3. 用户接口 `QueryUser` / `GetByTokenId` 在返回前调 `formatUserLogs` 脱敏（删 `admin_info` / `stream_status`，重写 `Id` 为序号，清 `IdStr`）。

清理路径（`DELETE /api/log`）：
1. ClickHouse 模式默认（`LOG_CLICKHOUSE_ALLOW_MANUAL_DELETE=false`）：`SELECT count()` 拿到符合条件的行数后**不执行删除**，返回估算值并打 SysLog 提示「TTL 异步处理」。
2. 手动模式（`LOG_CLICKHOUSE_ALLOW_MANUAL_DELETE=true`）：先估算行数，再 `ALTER TABLE logs DELETE WHERE created_at < ?`（异步 mutation，立即返回估算值）。

关闭路径：
1. `main.go` defer `model.CloseDB()` → `CloseLogStore(ctx)` → `clickhouseLogStore.Close(ctx)`。
2. 关闭 retry goroutine；调 `writer.Close(ctx)`：写锁守卫下 `close(queue)` + 关闭 stats goroutine；workers 收到 channel close 后 flush 剩余 batch 然后退出。
3. `ctx` 是 `model.contextWithFlushTimeout()` 计算出的 1-10s 超时，超时则丢弃未 flush 的日志（保护进程关停时延）。

---

## 关键文件

### 新增

| 路径 | 说明 |
|---|---|
| `model/log_store.go` | `LogStore` 接口 + `LogQueryParams`/`LogStatParams` + `InitLogStore`/`CloseLogStore` + `SetLogStoreForTest`/`NewSQLLogStoreForTest` |
| `model/log_store_sql.go` | `sqlLogStore`：包装现有 GORM 调用，行为零回归 |
| `model/log_store_clickhouse.go` | `clickhouseLogStore` 主体，含 `tableReady` 状态机 + 后台 `retryEnsureTable` |
| `model/log_clickhouse_ddl.go` | 显式建表 DDL + TTL ALTER 渲染 |
| `model/log_clickhouse_writer.go` | 异步队列 + worker + 批量 INSERT + drop 计数 + shutdown flush |
| `model/log_clickhouse_query.go` | 列表/统计/查询保护/SQL 构造 + `clickhouseLogRow.toLog()` 含 `IdStr` 填充 |
| `common/log_id.go` | `LogIDGenerator`：Snowflake-like UInt64 |
| `model/log_store_test.go` | SQLLogStore 兼容回归 + ID 单调/并发 + queue drop + close-race + tableReady drop + IdStr 验证 + 查询保护 |
| `docker-compose.clickhouse.yml` | 一键启动 PostgreSQL + Redis + ClickHouse 24.8 + new-api |

### 修改

| 路径 | 改动 |
|---|---|
| `go.mod` / `go.sum` | 新增 `gorm.io/driver/clickhouse v0.7.0`；`gorm.io/gorm` 1.25.2 → 1.30.0 |
| `common/database.go` | `DatabaseTypeClickHouse = "clickhouse"` |
| `common/constants.go` | 11 个 `LogXxx` 全局变量 |
| `common/init.go` `InitEnv()` | 11 个 env 读取 |
| `model/main.go` | `chooseDB` 加 `clickhouse://` 识别 + 主库守卫；`InitLogDB` 调 `InitLogStore`；`migrateLOGDB` 跳过 CH AutoMigrate；`initCol` CH 兼容；`CloseDB` 加 flush 钩子 |
| `model/log.go` | 全部 `LOG_DB.*` 改走 store；`Log` 结构体加 `IdStr`；`formatUserLogs` 清空 `IdStr` |
| `.env.example` | 12 个新 env 中英文双语注释 |
| `docker-compose.yml` | 顶部注释加指引 |
| `model/task_cas_test.go` 等 5 个测试文件 | `TestMain` 注入 `SetLogStoreForTest` |

---

## 配置（全部新增 env）

| 变量 | 默认 | 说明 |
|---|---|---|
| `LOG_SQL_DSN` | `""` | 现有；新增 `clickhouse://` / `tcp://` 协议支持 |
| `LOG_RETENTION_DAYS` | `90` | TTL 天数；`0` 禁用 TTL |
| `LOG_BATCH_SIZE` | `10000` | 单批 INSERT 行数上限 |
| `LOG_BATCH_FLUSH_MS` | `1000` | 批 flush 间隔 |
| `LOG_QUEUE_SIZE` | `100000` | 内存队列容量 |
| `LOG_BATCH_WORKERS` | `1` | flush worker goroutine 数 |
| `LOG_NODE_ID` | `0` | Snowflake nodeID（多实例**必须唯一**，0-1023） |
| `LOG_CLICKHOUSE_FAIL_OPEN` | `true` | 不可用时丢日志保护主请求 |
| `LOG_CLICKHOUSE_SYNC_WRITE` | `false` | 同步写（debug only） |
| `LOG_CLICKHOUSE_MAX_QUERY_RANGE_DAYS` | `31` | 列表/统计的最大时间窗口 |
| `LOG_CLICKHOUSE_MAX_OFFSET` | `100000` | 最大分页 offset |
| `LOG_CLICKHOUSE_ALLOW_MANUAL_DELETE` | `false` | 是否允许 `DELETE /api/log` 触发 mutation |

---

## 实现细节

### 1. ClickHouse 表设计

```sql
CREATE TABLE IF NOT EXISTS logs
(
    id UInt64,
    user_id Int32,
    created_at Int64,
    type Int16,
    content String CODEC(ZSTD(3)),
    username LowCardinality(String),
    token_name String,
    model_name LowCardinality(String),
    quota Int64,
    prompt_tokens Int64,
    completion_tokens Int64,
    use_time Int32,
    is_stream UInt8,
    channel_id Int32,
    token_id Int32,
    `group` LowCardinality(String),
    ip String,
    request_id String,
    other String CODEC(ZSTD(3)),
    created_date Date MATERIALIZED toDate(toDateTime(created_at)),
    INDEX idx_request_id request_id TYPE bloom_filter(0.01) GRANULARITY 4,
    INDEX idx_model_name model_name TYPE set(1000) GRANULARITY 4,
    INDEX idx_username username TYPE set(10000) GRANULARITY 4,
    INDEX idx_token_name token_name TYPE bloom_filter(0.01) GRANULARITY 4
)
ENGINE = MergeTree
PARTITION BY toYYYYMMDD(created_date)
ORDER BY (created_at, type, user_id, token_id, channel_id, id)
TTL toDateTime(created_at) + INTERVAL 90 DAY DELETE
SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1
```

设计要点：
- **`id UInt64`**：应用侧生成；ORDER BY 末位以保证同毫秒同节点同序的稳定排序。
- **`created_date` MATERIALIZED**：仅作 PARTITION/TTL 用途，节省写入开销。
- **`PARTITION BY toYYYYMMDD`**：每日分区，TTL 走 `ttl_only_drop_parts` 直接删 part，避免昂贵的 mutation。
- **`ORDER BY (created_at, type, user_id, ...)`**：优先服务时间范围扫描；type/user/token/channel 作为常见过滤字段进入主键尾部，跳数索引可生效。
- **`bloom_filter` for request_id**：精确查询命中 part 上的零行 part 直接被跳过。
- **`set(1000) for model_name`**：模型基数小，set 索引比 bloom 紧凑。
- **`LowCardinality`**：username/model_name/channel_name/group 重复率高，编码后磁盘占用 < 10%。
- **`CODEC(ZSTD(3))` for content/other**：长字符串字段；level 3 在压缩比/CPU 间平衡。

DDL 由 `model/log_clickhouse_ddl.go` 渲染，`LOG_RETENTION_DAYS=0` 时省略 TTL 子句和 `ttl_only_drop_parts`。每次启动还会跑一次 `ALTER TABLE logs MODIFY TTL ...`，让运维改 env 后立即生效。

### 2. Snowflake-like ID

```
| reserved 1 | timestamp 41 | node 10 | seq 12 |
```

- **epoch**：2025-01-01 UTC（`LogIDEpochMs = 1735689600000`）；41 bit 可表达 ~69 年。
- **node**：从 `LOG_NODE_ID` 读，0-1023；超出 clamp 并 SysLog 警告。
- **seq**：每毫秒 0-4095；溢出时 `runtime.Gosched()` + 自旋等下一毫秒（修过 P0 CPU 忙循环）。
- **monotonic**：时钟回拨时 pin 到 `lastMs` + 序列递增，保证单节点严格递增。
- **并发**：`sync.Mutex`；测试覆盖 8 goroutine × 2000 并发无重复。

### 3. LogStore 接口

```go
type LogStore interface {
    Record(log *Log) error
    GetByTokenId(tokenId int, limit int) ([]*Log, error)
    QueryAll(p LogQueryParams) ([]*Log, int64, error)
    QueryUser(p LogQueryParams) ([]*Log, int64, error)
    SumUsedQuota(p LogStatParams) (Stat, error)
    SumUsedToken(p LogStatParams) (int, error)
    DeleteOldLogs(ctx context.Context, targetTs int64, batchSize int) (int64, error)
    Close(ctx context.Context) error
}
```

- **入参收敛**：原 `model/log.go` 各函数 7-12 个位置参数 → `LogQueryParams`/`LogStatParams` struct；调用点（`controller/log.go`、`model/log.go` thin wrapper）零功能改动。
- **`QueryUser` 内部调 `formatUserLogs`**：用户接口的 ID 重写、`admin_info`/`stream_status` 脱敏在 store 层完成，业务层（controller）不需要也不能感知后端类型。
- **测试钩子**：`SetLogStoreForTest(s LogStore)` + `NewSQLLogStoreForTest() LogStore` 让 `model` 包外的 test fixture 注入。

### 4. 异步批量写入

```
请求 → RecordConsumeLog → store.Record → writer.Submit (chan)
                                            ↓
                           [N workers]: 累积到 batch_size 或 flush_ms → INSERT
```

关键点：
- **Submit 永不阻塞**：`select { case queue <- log: default: drop+counter }`。
- **Submit / Close race**：`closeMu sync.RWMutex`：Submit 持读锁、Close 持写锁后再 `close(queue)`。修复了首版的「send on closed channel」panic 风险（已加 `TestClickHouseWriter_SubmitDuringClose`）。
- **flush 双层防御**：先看 `tableReady`，未就绪丢批；再看实际 `INSERT` 失败计数；任何阶段 fail 都不影响主请求。
- **重试**：单批失败 200ms 后重试 1 次；不持久化重试，不阻塞队列。
- **shutdown flush**：`Close(ctx)` 关 channel 后 worker 走完最后一个 batch；`ctx` 1-10s 超时强制返回。
- **stats logger**：30s tick；只有出现非零 drop/fail 时才打印一行 `queue_len=... batch_ok=... batch_fail=... drop_queue_full=... drop_insert_fail=... drop_not_ready=...`。

### 5. fail-open 状态机（P1 修复）

```
                                          retry every 60s
            ┌─────────────────────────────────────────┐
            ↓                                         │
   ┌───────────────────┐    ensureTable OK    ┌───────────────────┐
   │ tableReady=false  │─────────────────────▶│ tableReady=true   │
   │ flush drops batch │                      │ flush executes    │
   │ queries error out │                      │ queries serve     │
   └───────────────────┘                      └───────────────────┘
            ↑
            │  ensureTable fails on startup OR ClickHouse outage
```

- 启动时 `ensureTable` 失败 + `LOG_CLICKHOUSE_FAIL_OPEN=true`：store 仍安装、writer 仍创建、retry goroutine 起来，**主进程不退出**。
- writer.flush 起手即查 `tableReady`，未就绪直接增 `droppedNotReady` 返回，**绝不调用 `db.Exec`**——这就消除了 P1 报告的「同步写把请求拖到 dial_timeout」风险。
- 查询接口同步检查 `tableReady`，未就绪返回 `clickhouse log store is not ready, please retry shortly`，前端可优雅降级。
- 后台 goroutine 每 60s 跑一次 `ensureTable`；成功后 `tableReady.Store(true)`，下一次 worker tick 自动恢复 flush，并打 SysLog `clickhouse log store recovered: table is now ready, resuming flushes`。

### 6. JS 安全 ID 输出（P2 修复）

JavaScript 的 `Number` 是 IEEE 754 双精度，最大安全整数 `2^53 - 1`。Snowflake 41 bit 时间戳从 2025-01-01 起累计 ~25 天后，整体 ID 超 `2^53`，前端 `JSON.parse` 会**静默丢精度**。

修复方案：
- `Log` 结构体新增 `IdStr string \`json:"id_str,omitempty" gorm:"-"\``。
- 仅 ClickHouse 查询路径填充：`clickhouseLogRow.toLog()` 用 `strconv.FormatUint(r.Id, 10)`。
- SQL 模式 `IdStr` 为空（自增 ID 永远 < 2^31，JS 安全）；`omitempty` 让现有 API 响应零变化。
- `formatUserLogs`（用户视图脱敏）会把 `Id` 改写为 `1, 2, 3...` 序号；同步清空 `IdStr` 防止前端看到「id=1, id_str=84573928347...」的不一致。
- 前端可零破坏迁移：`row.id_str || row.id`。

### 7. 查询保护

`applyClickHouseQueryGuards`：
- `EndTimestamp == 0` → `EndTimestamp = now`。
- `EndTimestamp - StartTimestamp > LOG_CLICKHOUSE_MAX_QUERY_RANGE_DAYS * 86400` → `StartTimestamp = EndTimestamp - maxRange`。
- `RequestId != ""` → 跳过时间范围 clamp（精确查询）。
- `StartIdx > LOG_CLICKHOUSE_MAX_OFFSET` → 拒绝。

仅 ClickHouse 模式启用；SQL 模式不强加保护，避免回归。

### 8. 清理策略

- **TTL 模式（默认）**：每天分区 + `ttl_only_drop_parts=1`，过期分区直接 drop。`DELETE /api/log` 接口仅 `SELECT count()` 估算并返回行数；实际清理由 ClickHouse 后台合并触发。
- **手动 mutation 模式（`LOG_CLICKHOUSE_ALLOW_MANUAL_DELETE=true`）**：`ALTER TABLE logs DELETE WHERE created_at < ?` 异步 mutation。提交即返回估算值；实际删除发生在后续 part merge。**频繁手动 mutation 在 ClickHouse 上是反模式**，env 默认关闭。

### 9. CloseDB 钩子

```go
func CloseDB() error {
    flushCtx, cancel := contextWithFlushTimeout()  // 1-10s
    defer cancel()
    if err := CloseLogStore(flushCtx); err != nil {
        common.SysError("failed to close log store: " + err.Error())
    }
    if LOG_DB != DB {
        if err := closeDB(LOG_DB); err != nil { return err }
    }
    return closeDB(DB)
}
```

shutdown 顺序：先 flush 队列（写完 ClickHouse），再关连接。SQL 模式 `CloseLogStore` 是 no-op；ClickHouse 模式触发上述 worker 收尾流程。

### 10. 测试

`go test -race ./model/... ./common/...` 覆盖：

| 测试 | 验证目标 |
|---|---|
| `TestSQLLogStore_RecordAndQuery` | SQL 写入 + 多条件 QueryAll |
| `TestSQLLogStore_QueryUserHidesAdminInfo` | `formatUserLogs` 删 admin_info / stream_status 保留 public |
| `TestSQLLogStore_SumUsedQuota` | 用户/类型过滤后聚合 quota |
| `TestSQLLogStore_DeleteOldLogs` | 分批删除 |
| `TestLogIDGenerator_Monotonic` | 单 goroutine 单调递增 50000 次 |
| `TestLogIDGenerator_DistinctNodes` | 跨 nodeID 不冲突 |
| `TestLogIDGenerator_Concurrent` | 8 goroutine × 2000 并发无重复 |
| `TestClickHouseWriter_QueueDrop` | 队列满时 drop 计数 |
| `TestClickHouseWriter_SubmitDuringClose` | Submit/Close race 不 panic |
| `TestClickHouseWriter_NotReadyDropsBatch` | tableReady=false 时 flush 不碰 db |
| `TestApplyClickHouseQueryGuards` | 时间窗口 clamp、offset 上限、request_id 例外 |
| `TestClickHouseLogRow_PopulatesIdStr` | uint64 转 string 无损；formatUserLogs 清空 IdStr |

### 11. 已知限制

- **ClickHouse 真实集成测试**：需要外部 ClickHouse 实例，CI 不跑；本地用 `docker-compose.clickhouse.yml` 自测。
- **TTL 不是即时删**：ClickHouse 后台合并触发，可能延迟数分钟到数小时。
- **`LIKE '%kw%'` 在超大表上仍昂贵**：管理端模糊搜索保留但不是推荐主路径；高频搜索应用 request_id / 用户 / token / 模型 / 时间结构化条件。
- **多实例 LOG_NODE_ID 必须配置唯一**：错配会产生 ID 冲突；启动日志会打印 `using log node id N`。
- **`relay/helper` 与 `relay/channel/claude` 测试 flaky**：与本次改动无关（在 stash 后未变代码上同样失败）。

---

## 迁移指南

### 全新部署

```env
SQL_DSN=postgresql://...
LOG_SQL_DSN=clickhouse://default:password@host:9000/new_api_logs?dial_timeout=10s
LOG_RETENTION_DAYS=90
LOG_NODE_ID=0
```

或直接 `docker compose -f docker-compose.clickhouse.yml up -d`。

### 已有部署切换

1. 备份现有 `logs` 表（如果需要保留历史）：`mysqldump --tables logs > old-logs.sql`。
2. 准备 ClickHouse 实例（建议独立 VM 或容器，与主业务 DB 物理隔离）。
3. 配置 `LOG_SQL_DSN=clickhouse://...` 重启服务。
4. 第一次启动会显式建表；旧 SQL 日志**不可见**（设计预期，参见 `docs/clickhouse-log-storage-plan.md` §"生产切换行为"）。
5. 切换前后的 `/api/log/stat` 数据点不连续（一段空窗），quota 余额扣减不受影响。

### 多实例

每个实例必须配置不同的 `LOG_NODE_ID`（0-1023）。建议用容器环境变量或 K8s `Deployment.spec.template.spec.containers[0].env` 注入 pod 序号。

### 回退

修改 `LOG_SQL_DSN` 回旧值并重启即可；ClickHouse 中的日志数据保留（除非显式 `DROP TABLE`），但前端不再可见。

---

## 设计文档与可观测性

- 设计原稿：`docs/clickhouse-log-storage-plan.md`（840 行）
- 实施 plan：`/Users/mtyq_ztw/.claude/plans/lucky-gathering-shamir.md`
- 运行时观测：检索 `clickhouse log writer stats` 关键字（30s tick，仅在出现 drop/fail 时打印）；启动日志 `clickhouse log store ready: ...` / `clickhouse log store is in degraded mode and will retry every minute` / `clickhouse log store recovered`
