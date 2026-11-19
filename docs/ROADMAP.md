# Memorigo 扩展路线图（2026-11 调研）

> 研究快照：2026-11-18。方法：优先官方文档、论文与仓库；厂商自报数字单独标注；关键结论尽量交叉验证。
>
> 可信度标记：`[论文]` 论文/同行评审；`[官方]` 官方文档或仓库；`[厂商自报]` 未见独立复现；`[推断]` 本文推理；`[未核实]` 仅见二手来源。
>
> **重要提醒**：2026 年大量“前瞻日期”的博客与榜单存在合成/营销内容。本文对未拿到论文或官方文档的数字均已标注；不同系统的 benchmark 分数协议不同，**不可横向比较**。

---

## 0. 结论先行

1. **行业分水岭已经不是“记得更多”**，而是同时控制三件事：**写入成本、时态正确性、检索延迟**。主流系统正从“LLM 抽事实 + 向量库”转向“混合检索 + 冲突/时态处理 + 可治理的写入管道”。
2. **独立评测正在修正叙事**。Memory Tax `[论文]` 与 RAW-RAG `[论文]` 都指出：复杂记忆结构的默认净收益可能为负；原始对话 + 强检索 + 重排有时更强。**任何新记忆结构都必须先证明 benefit > tax。**
3. **Memorigo 的最优下一步不是图数据库**，落地顺序应为：
   - ✅ 写路径、生命周期、时态覆盖（本版本已完成）；
   - ✅ 确定性评测地基 + token 预算上下文 + MCP 工具面；
   - 下一步 **ANN/BM25 混合检索 + RRF**，并用外部数据集验证；
   - 然后 **时态 v3 + 多租户**；
   - 最后才是 **图记忆**。
4. **Go 生态已经够用，但要认清 SQLite 的边界**：pgvector-go 走纯 Go pgx；`modernc.org/sqlite` 内置 FTS5/BM25，但**不能加载 vec0 扩展**；纯 Go 的 ANN 需要 `gosqlite` 这类封装，或接受暴力向量扫描，生产规模优先用 PostgreSQL HNSW。官方 MCP Go SDK 已支持 2026-07-28 无状态协议。**单静态二进制**仍是 Memorigo 相对 Python 系的真实分发优势。
5. **差异化定位**：Memori 原版默认把对话送到托管增强 API；Memorigo 是“**完全离线 + BYODB + Go 单二进制 + 可审计**”。这条必须用可复现评测支撑，而不是口号。

---

## 1. 2026 年主流记忆系统与可借鉴点

| 系统 | 核心架构 | 对 Memorigo 的启示 | 不要照搬 |
|---|---|---|---|
| **Mem0** | ADD-only 抽取；语义 + BM25 + 实体多路融合；时态元数据与 recency 加权；图改为内置实体链接 | 多信号融合、异步写入、按查询类型路由 | 营销口径的分数；每轮全量重抽 |
| **Zep / Graphiti** | episode/entity/edge/community；**双时态**（valid time + transaction time）；旧事实失效而非删除；cosine + BM25 + 图 BFS | 双时态、失效不删除、查询期不调 LLM | 每个 episode 都跑 LLM 的成本模型；社区摘要 |
| **Letta（MemGPT）** | core/recall/archival 三层 + 可编辑 memory blocks；sleep-time compute 后台整理；2026 转向 git-backed MemFS | 工作记忆可编辑、离线巩固 | git 文件路线与 SDK 场景不符 |
| **LangMem / LangGraph** | semantic/episodic/procedural 三类；后台抽取；procedural 可改 system prompt | 三类划分、反思去抖 | 纯向量、无混合检索 |
| **cognee** | ECL 管道；LLM 抽实体/关系；RDF/OWL 本体 grounding；图 + 向量 | schema/本体先行 | 重型 ECL 的构建成本 |
| **Memobase** | profile-first：预定义 topic/sub-topic 画像 + 事件时间线；批量缓冲抽取 | 显式画像 schema、可纠正字段 | 自报 LoCoMo 分数需打折 |
| **MemoryOS** | 短/中/长三层 + 热度淘汰 | 分层与热度 | 摘要即记忆（证据丢失） |
| **A-MEM** | Zettelkasten 笔记 + 动态链接，新记忆反向更新旧笔记 | 记忆间链接 | 构建成本极高（117h/98.4K 次 LLM 调用） |
| **HippoRAG 2** | OpenIE 三元组 + 个性化 PageRank | 1-hop 关联扩展 | 三元组碎片化，丢执行状态 |
| **LightRAG / GraphRAG** | 双层关键词检索 / 社区 + 分层摘要 | 增量更新 | 社区摘要的 token 税；GraphRAG 已进入维护模式 |
| **新进入者** | MemOS（MemCube）、Hindsight（World/Experience/Mental Model）、Supermemory、Honcho 等 | 治理元数据、可重写的心智模型 | 榜单数字与未验证的“全自动人格化” |

**三点判断**

- **图记忆正在退潮为“实体链接 + 1-hop”**。除 Zep/Graphiti 外，多数产品不再把图数据库作为默认路径；GraphRAG 已进入维护模式。Memorigo 已有的 KG 表可以保留，但不应是 P0。
- **“托管 API + 插件分发”与“离线 BYODB”分成两条路线**。Memorigo 只能走后者，并把“数据不出域”做成可验证属性（无外部调用、可审计、可导出/删除）。
- **成熟度与声量不匹配**。选型应看第三方评测、公开失败模式与可复现实验，而不是 star 数。

---

## 2. 被验证有效的技术，以及被高估的技术

### 2.1 值得投入

| 技术 | 2026 的做法 | 对 Memorigo 的含义 |
|---|---|---|
| **混合检索** | dense + BM25 + 可选实体/图，RRF 融合；查询按类型路由 | 当前已有 BM25 + 稠密 + 新近度 + 频率；下一步是 ANN 候选与 RRF |
| **重排 / cross-encoder** | 宽召回（k≈150）→ rerank 到 20；单步 ROI 最高 | 做成可选层：API reranker 或本地 bge-reranker，默认关闭保延迟 |
| **时态有效性** | valid_from/valid_to + transaction time；冲突失效旧事实 | 已有 `slot/status`；缺 `valid_from/valid_to` 与 `source_message_id` |
| **巩固 / 反思** | 会话结束或空闲触发后台压缩；幂等、可回滚 | 做成离线 job，不在每轮写入时调用 LLM |
| **遗忘 / 衰减** | Ebbinghaus、热度、TTL；可审计可恢复 | 软删除 + 降权，保留原始消息第二通道 |
| **分层记忆** | working / episodic / semantic / procedural | 目前只有 message + fact；缺 working 装配与 procedural |
| **上下文工程** | 最小充分上下文、JIT 检索、compaction、引用 | 提供 `BuildContext(query, budget)`，带引用与 token 预算 |
| **双通道** | 事实用于精确召回，原始消息用于兜底/引用 | 复用现有 message 表；评测时分别报告两条通道 |
| **多租户/隐私** | 查询期权限过滤、RLS、PII 脱敏、来源追踪、投毒防护 | 当前仅 `entity_id`；缺 org/app 维度与写权限 |

### 2.2 需要克制

1. **“更多记忆 = 更好”**：抽取/改写会损失证据；净价值应按 `benefit − tax` 统计，并分查询类型报告。
2. **图结构**：图对主题型/全局问题有帮助，对简单事实查找更贵且更噪；个人记忆大多是后者。
3. **sleep-time / 反思的通用性**：收益来自“多查询共享同一上下文”；单次短对话上后台 LLM 只会增加成本与漂移。

---

## 3. 评测：没有它，所有优化都不可信

### 3.1 基准选择

| 基准 | 规模/形态 | 测什么 | 注意点 |
|---|---|---|---|
| **LoCoMo** `[论文]` | 平均 300 turn、约 9K token、最多 35 session | 单跳/多跳/时序/开放域 QA、事件摘要 | 样本小，协议差异大 |
| **LongMemEval** `[论文]` | 500 题，历史可任意长 | 抽取、多 session 推理、时序、知识更新、弃答 | 五大能力清晰，适合 SDK 回归 |
| **MemoryAgentBench** `[论文]` | 增量多轮，EventQA + FactConsolidation | 精确检索、测试时学习、长程理解、选择性遗忘 | 冲突消解是全场最弱项 |
| **BEAM** `[论文]` | 100 段对话、2000 题、最长 10M token | 10 类记忆能力 | 1M→10M 普遍掉约 25% |
| **ConvoMem** `[论文]` | 75,336 QA、6 类、1K–3M token | 用户/助手事实、弃答、偏好、变化事实 | 1–150 轮内长上下文可能反超 RAG |
| **MemBench** `[论文]` | 约 100K token | 事实/反思记忆；参与/观察 | 指标设计适合借鉴 |
| **PersonaMem v1/v2** `[论文]` | 1000 persona、20K+ 偏好、128K 上下文 | 个性化偏好跟随 | 验证“画像 vs 事实” |

### 3.2 三条固定基线

任何新功能必须同时报告：

1. **Full-context**：截断到 token 预算的原始对话。
2. **Naive chunk RAG**：原始对话分块 + 稠密检索 + cross-encoder rerank。
3. **Memorigo**：事实通道（可选事实 + 原始消息双通道）。

### 3.3 指标

- 答案正确率（LLM-judge + 人工抽检）
- 检索 recall@k / evidence coverage
- 弃答正确率
- 知识更新正确率、冲突消解正确率
- P50/P95 延迟、token/query、$/query
- 构建成本（LLM 调用次数/小时）
- 记忆占用、删除合规、跨租户泄漏 = 0

### 3.4 小 SDK 的可信评测协议

1. **协议冻结**：同一 reader、同一 judge、同一 top-k、同一 token 预算；输出配置哈希与随机种子；报告中位数 + bootstrap 置信区间。
2. **数据集分层**：LoCoMo/LongMemEval 全量或固定子集；另建 50–100 条私有场景（改名、搬家、偏好反转、跨 session 指代），防止污染与过拟合。
3. **双通道指标**：分别报告“仅事实”与“事实 + 原始消息”的增益，量化 memory tax。
4. **确定性回归**：写入幂等、slot 覆盖、软删除、租户隔离、embedding 维度不混用，全部用 `go test` 固化；LLM 只测质量。
5. **可观测**：每次 recall 产生一条 OTel span，带候选数、命中数、token、耗时、模型与维度；评测与生产共用指标。

### 3.5 已落地的最小评测闭环

本轮先交付了零依赖、可进 CI 的回归地基：

```text
eval/
  eval.go       # Runner、断言、Report、JSON
  scenarios.go  # 15 个内置场景
  eval_test.go
cmd/memorigo-eval/
  main.go       # -filter / -json / -timeout
```

```bash
go run ./cmd/memorigo-eval
go run ./cmd/memorigo-eval -json -filter context
```

覆盖：事实召回、偏好覆盖、搬家覆盖、跨 session 连续、遗忘、噪声抵抗、中文召回、token 预算、原始证据回退、常驻画像、动态画像开关、覆盖率弃权。全部使用内存 SQLite + 离线 hash 嵌入，无 API key、无网络。

它解决的是“核心闭环退化”问题，不替代外部质量评测。下一阶段仍需：

```text
eval/
  datasets/     # 下载脚本 + 校验和，不提交大数据
  baselines/    # full-context、naive RAG
  judge/        # LLM-judge + 人工抽检
```

验收信号：外部数据集上的 accuracy / recall@k / latency / token / cost 表格，配置哈希与随机种子可复现。当前 15/15 只代表确定性回归通过，**不可与 LoCoMo/LongMemEval 分数横向比较**。

---

## 4. Memorigo 当前状态（2026-11）

### 已完成

| 能力 | 状态 |
|---|---|
| 写路径幂等（前缀/后缀重叠） | ✅ |
| 真实 SQL 事务 + 可重试错误 | ✅ |
| 异步队列、重试、Flush/Close、panic recover | ✅ |
| 中英文启发式抽取 + LLM 严格 JSON 抽取 | ✅ |
| slot / status / superseded / 软删除 | ✅ |
| 稠密 + BM25 + 新近度 + 频率混合排序 | ✅ |
| 自动注入（含流式）与原始消息持久化 | ✅ |
| 嵌入模型/维度治理 + Reindex | ✅ |
| SQLite / PostgreSQL / MongoDB Repos | ✅ |
| 归因隔离、错误不 panic、Stats | ✅ |
| 单元 + SQLite 集成 + httptest 协议测试 + race | ✅ |
| **确定性评测 harness + CLI（15 场景）** | ✅ |
| **`BuildContext`：token 预算 + 引用 + 原始证据双通道** | ✅ |
| **常驻 Profile + MMR 多样性 + 覆盖率弃权 + 位置重排** | ✅ |
| **MCP server：search/context/remember/forget/profile/stats + stdio/HTTP** | ✅ |

### 仍缺

1. **外部评测与三基线**：当前只有确定性回归，尚未接 LoCoMo / LongMemEval，也没有 full-context / naive-RAG / LLM-judge 对比。
2. **ANN 索引**：词法候选已用 FTS5 / GIN / token 索引补上，精确词不再受 2000 条新近窗口限制。稠密检索仍是有界扫描；pgvector HNSW 仍未接（`modernc` 不能加载 `vec0`，默认构建保持纯 Go）。
3. **时态 v3**：✅ `valid_from/valid_to`、`source_message_id`、`confidence`、`asserted_by` 已落地。非 user 事实默认不进入召回。
4. **多租户**：✅ `org_id/app_id` 进入 entity，同一 entity id 跨组织泄漏测试为 0。还没有数据库 RLS 策略（应用层隔离）。
5. **MCP 生产治理**：HTTP 已有 Bearer 鉴权和 TLS 入口，但没有 OAuth、按工具授权、配额与审计日志。
6. **巩固/遗忘**：✅ `Maintain` / `Restore` 按 TTL 与提及次数过期并可恢复。没有后台 LLM 摘要，也没有自动调度；调用方在空闲时显式跑一遍。
7. **本地嵌入/重排**：✅ Ollama `POST /api/embed` 与可选 HTTP rerank（默认关闭）。没有 ONNX，也没有把模型打进二进制；Ollama 需要本机已经拉好模型。
8. **隐私**：✅ `Export`（版本 1 JSON）与 `DeleteSubject` 硬删除。`MEMORI_PII_REDACT` 默认关闭，只遮邮箱、电话和通过 Luhn 的卡号。不是 crypto-shredding，也不是数据库 RLS。
9. **REST 服务层**：MCP 已覆盖 Agent 客户端，通用 HTTP API 尚未提供。

---

## 5. 优先级路线图

### P0：正确性与可证明性（接下来 4–6 周）

> 状态（2026-11-18）：P0-1 的确定性回归部分已完成（`eval/` + 15 场景 + CI）；外部数据集与三基线仍未做。P0-4 的 slot/status 覆盖已具备，但 `valid_from/valid_to` 尚未加。

| # | 事项 | Impact | Effort | 验收信号 |
|---|---|---|---|---|
| P0-1 | **评测 harness + 三基线**（✅ 确定性回归已交付） | 5 | 中 | 一条命令产出 accuracy/latency/token/$ 报告；配置哈希可复现 |
| P0-2 | **ANN 候选 + FTS5/pgvector**（✅ 词法候选：SQLite FTS5、Postgres GIN、Mongo token 索引；稠密仍是有界扫描，pgvector 未接） | 5 | 中 | recall@10 +10pt，P95 不升；10^5 事实下延迟可控 |
| P0-3 | **RRF / 可选 rerank**（✅ RRF 已交付；✅ HTTP rerank 默认关闭，失败回退加权/RRF 顺序。没有内置 cross-encoder） | 4 | 低 | 与当前加权融合 A/B；错误率下降且可回滚 |
| P0-4 | **时态 v3：valid_from/valid_to + source_message_id**（✅ 已交付） | 5 | 中 | 偏好反转、搬家、改名场景正确率 ≥90%；旧事实可追溯 |
| P0-5 | **多租户最小闭环**（✅ 应用层 org/app 隔离已交付；数据库 RLS 未做） | 5 | 中 | `org_id/app_id` 进入 schema；跨租户泄漏测试为 0 |

#### P0-2 设计草案

**SQLite**：`modernc.org/sqlite` 支持 FTS5 与 `bm25()`，但**不支持加载 `vec0` 扩展**（它是 SQLite 的 Go 翻译，不能加载 C 扩展）。因此：

- **词法**：用 FTS5 + `bm25()`；中文用 trigram 或继续用现有 Go 分词（英文 + CJK bigram）做候选打分。
- **向量**：纯 Go 默认保留暴力扫描（≤10^5 向量可接受）；需要 ANN 时可选：
  - `gosqlite` 这类在 modernc 之上提供 typed `vec0` 的封装；
  - cgo build tag 下用 `mattn/go-sqlite3` + `sqlite-vec`；
  - 或直接切到 PostgreSQL + pgvector。
- **不要把 cgo 设为默认**：CI 同时跑纯 Go 与可选 cgo 构建。

```sql
CREATE VIRTUAL TABLE memori_fact_fts USING fts5(
  content, slot, content='memori_entity_fact', content_rowid='id',
  tokenize='unicode61'
);
```

**PostgreSQL**：新增 `vector(n)` / `halfvec(n)` 列与 HNSW 索引（pgvector-go 纯 Go，无需 cgo）：

```sql
ALTER TABLE memori_entity_fact ADD COLUMN embedding vector(1536);
CREATE INDEX ON memori_entity_fact USING hnsw (embedding vector_cosine_ops);
```

**Repos 扩展**：

```go
type FactSearchRepo interface {
    SearchDense(ctx, entityID int64, vec []float32, k int) ([]FactResult, error)
    SearchLexical(ctx, entityID int64, query string, k int) ([]FactResult, error)
}
```

上层用 RRF 融合：

```text
rrf(d) = Σ_r 1 / (k + rank_r(d))    // k 通常 60
```

保留当前加权融合作为 fallback；ANN 不可用时退回候选扫描。

#### P0-4 设计草案

事实表增加：

```sql
valid_from        TIMESTAMP NOT NULL,
valid_to          TIMESTAMP NULL,          -- NULL = 仍有效
source_message_id BIGINT NULL,
confidence        REAL NOT NULL DEFAULT 1.0,
asserted_by       TEXT NOT NULL DEFAULT 'user'  -- user / assistant / inferred
```

写入规则：

- 同一互斥 slot 出现新值时：旧行 `valid_to = now()`、`status = superseded`。
- `source_message_id` 强制指向产生该事实的消息，支持“这句话从哪来”的审计。
- `asserted_by != 'user'` 的事实默认不参与自动注入，需显式允许。
- 冲突无法消解时返回“不确定”，而不是编造答案（弃答能力）。

### P1：从“记得住”到“用得上”（6–12 周）

> 状态（2026-11-18）：P1-1、P1-2 基础版与 P1-3 已交付，实际 API 见下。

| # | 事项 | Impact | Effort | 验收信号 |
|---|---|---|---|---|
| P1-1 | **上下文装配 `BuildContext(query, budget)`**（✅ 已交付） | 4 | 中 | 输出带引用、token 预算、分层片段；注入 token 可控 |
| P1-2 | **三通道上下文**：Profile + 事实 + 原始消息，MMR / 覆盖率 / 位置重排（✅ 基础版已交付） | 4 | 中 | memory tax 可量化；证据覆盖率提升 |
| P1-3 | **MCP server**（✅ stdio + HTTP + Bearer 已交付） | 4 | 中 | Claude Code / Cursor 可读写；工具有集成测试 |
| P1-4 | **框架适配**：Eino / Genkit 薄适配 | 3 | 低 | 示例可运行；不绑定框架抽象 |
| P1-5 | **巩固/遗忘**（✅ `Maintain`/`Restore`：TTL + 提及次数保护，可 dry-run、可恢复；无 LLM 摘要、无自动调度） | 3 | 中 | 过期事实离开召回，`Restore` 后回来；高频事实保留 |
| P1-6 | **本地嵌入 / reranker**（✅ Ollama HTTP 嵌入，模型名由 `MEMORI_EMBEDDING_MODEL` 锁定；HTTP rerank 默认关闭。无 API key 时仍走 hash） | 3 | 低到中 | 无 API key 可运行；模型版本由调用方固定 |
| P1-7 | **隐私与合规**（✅ 导出 v1 + 硬删除 + 可选 PII 正则。未做 crypto-shredding，未做 RLS） | 4 | 中 | 删除后本主体行数为 0，其他主体仍在；导出 JSON 版本稳定 |

#### P1-1 已交付 API

```go
type ContextOptions struct {
    TokenBudget int      // 0 -> MEMORI_CONTEXT_BUDGET (800)
    FactLimit   int      // 0 -> 召回上限
    IncludeRaw  bool
    RawLimit    int      // 默认 4
    RawScan     int      // 默认 200
}

type ContextPackage struct {
    Query     string
    Facts     []Fact
    Messages  []ContextMessage
    Citations []ContextCitation // fact:<id> / msg:<id>
    Text      string
    Tokens    int
    Budget    int
    Truncated bool
    FactCount int
    RawCount  int
}

pkg, err := m.BuildContext(ctx, "what does the user prefer?", ContextOptions{
    TokenBudget: 1200,
    IncludeRaw:  true,
})
```

装配顺序：`<memory_context>` 头 → 高相关事实 → 原始消息证据；超预算停止并置 `Truncated`。原始消息通过 `RecentForEntity` 跨 session 取同一 entity 的证据，按查询词元重叠 + 7 天新近度排序。自动注入复用同一路径，但只走事实通道。

仍未做：`Layers`（working/episodic/semantic/procedural）、reranker、按“置信度 × 相关度 / token”做更细的裁剪。

#### P1-3 已交付 MCP

工具面（stdio 与 HTTP 相同）：

```text
memory_search(query, limit?, entity_id?)
memory_context(query, budget?, include_raw?, fact_limit?, raw_limit?, entity_id?)
memory_remember(content, entity_id?)
memory_forget(id, entity_id?)
memory_profile(limit?, entity_id?)
memory_stats(entity_id?)
```

原则：

- 传输与业务解耦：`mcpserver.New` 返回 `*mcp.Server`；`cmd/memorigo-mcp` 支持 stdio 与 stateless Streamable HTTP；测试覆盖 in-memory 与 HTTP。
- HTTP 默认要求 Bearer token，`/healthz` 不鉴权；支持直接 TLS 或反向代理终止 TLS。
- 每次调用可带 `entity_id`；服务级归因切换用互斥锁串行化，避免跨作用域串数据。
- `-readonly` 禁用写工具。默认仍会执行迁移。
- 返回结构化 JSON + 文本，引用保留 fact/message id；记忆正文按不可信数据处理。
- Docker 多阶段镜像与非 root 运行已交付；与 OpenAI-compatible proxy 共用同一 core。

仍未做：OAuth、按工具授权、配额/审计日志、多租户 RLS。

### P2：更强的结构（12 周以后，需评测支撑）

| # | 事项 | Impact | Effort | 前置条件 |
|---|---|---|---|---|
| P2-1 | 实体解析 + 1-hop 图扩展 | 3 | 高 | 评测证明多跳/关联子集有增益 |
| P2-2 | 程序性记忆（规则/提示片段） | 3 | 中 | 有可回滚的写入与审计 |
| P2-3 | 多 agent 共享记忆（命名空间 + 信任分级） | 3 | 高 | 多租户与写权限先完成 |
| P2-4 | 多模态记忆（图片/音频摘要） | 2 | 高 | 有明确的存储与检索协议 |
| P2-5 | 自适应检索（是否检索、几轮、改写） | 3 | 中 | 混合 + rerank 已稳定 |

---

## 6. 明确避免

- **不要做通用 GraphRAG / Graphiti 克隆**。图只在评测证明 1-hop 有增益后做，且不做社区摘要。
- **不要把 LLM 抽取放在同步写路径**，也不要每轮全量重抽。
- **不要用摘要冒充记忆**。摘要是压缩视图，必须保留原始消息。
- **不要自研向量索引/向量数据库**，也不要默认引入 cgo（faiss/HNSW/ONNX）。
- **不要追 benchmark SOTA 数字**。协议不一致，复杂 ≠ 更好。
- **不要发明新协议**。MCP + OpenAI-compatible + REST 足够。
- **不要静默丢写**、不要在库里 panic、不要把模型推断当用户事实。
- **不要在未做租户隔离前复用全局 KG 表**；不要混用不同 embedding 模型的向量。
- **不要做“全自动人格化”而没有纠正/删除入口**。

---

## 7. 90 天执行建议

### 第 1–2 周：评测地基（✅ 确定性部分完成）

- 已建 `eval/` 与 `cmd/memorigo-eval`，15 个场景进 CI。
- 已输出表格式与 JSON 报告（accuracy / P95 / token）。
- 待做：接入 LongMemEval dev + 50 条私有场景，补 full-context 与 naive RAG 基线。

### 第 3–5 周：检索升级（下一步）

- SQLite FTS5（纯 Go）+ 可选 `gosqlite`/cgo `vec0`；PG pgvector + HNSW。
- `FactSearchRepo` + RRF；保留加权融合开关。
- 用评测对比 recall@k、P95、token/query。

### 第 6–8 周：时态与上下文

- 上下文装配已交付（`BuildContext` + 引用 + token 预算 + 原始证据）。
- 待做：`valid_from/valid_to`、`source_message_id`、`confidence`、`asserted_by`。
- 偏好反转、改名、搬家场景回归。

### 第 9–12 周：分发与治理

- MCP stdio + Streamable HTTP + Bearer 鉴权已交付（六个工具、只读、entity 作用域、集成测试）。
- 应用层 `org_id/app_id`、导出与硬删除已交付。待做：数据库 RLS。
- 待做：OAuth/按工具授权、OTel span/metrics 与成本核算。

---

## 8. 来源

访问日期均为 2026-11-18。厂商数字未独立复现。

1. Mem0, “State of AI Agent Memory 2026”, 2026-04 — https://mem0.ai/blog/state-of-ai-agent-memory-2026 `[厂商自报]`
2. Chhikara et al., “Mem0”, arXiv:2504.19413, 2025-04 — https://arxiv.org/abs/2504.19413
3. Rasmussen et al., “Zep: A Temporal Knowledge Graph Architecture for Agent Memory”, arXiv:2501.13956, 2025-01 — https://arxiv.org/abs/2501.13956
4. Graphiti — https://github.com/getzep/graphiti `[官方]`
5. Letta, “Letta's Next Phase”, 2026-03 — https://www.letta.com/blog/our-next-phase/ `[官方]`
6. Lin et al., “Sleep-time Compute”, arXiv:2504.13171, 2025-04 — https://arxiv.org/abs/2504.13171
7. LangMem — https://www.langchain.com/blog/langmem-sdk-launch `[官方]`
8. cognee — https://github.com/topoteretes/cognee `[官方]`
9. Memori — https://github.com/MemoriLabs/Memori `[官方]`
10. Memobase — https://github.com/memodb-io/memobase `[官方]`
11. Kang et al., “Memory OS of AI Agent”, arXiv:2506.06326, 2025-05 — https://arxiv.org/abs/2506.06326
12. Xu et al., “A-MEM”, arXiv:2502.12110, 2025-02 — https://arxiv.org/abs/2502.12110
13. Gutiérrez et al., “HippoRAG 2”, arXiv:2502.14802, 2025-02 — https://arxiv.org/abs/2502.14802
14. Guo et al., “LightRAG”, arXiv:2410.05779, 2024-10 — https://arxiv.org/abs/2410.05779
15. Edge et al., “GraphRAG”, arXiv:2404.16130, 2024-04 — https://arxiv.org/abs/2404.16130
16. Li et al., “MemOS”, arXiv:2507.03724, 2025-07 — https://arxiv.org/abs/2507.03724
17. Maharana et al., “LoCoMo”, arXiv:2402.17753, 2024-02 — https://arxiv.org/abs/2402.17753
18. Wu et al., “LongMemEval”, arXiv:2410.10813, 2024-10 — https://arxiv.org/abs/2410.10813
19. Hu et al., “MemoryAgentBench”, arXiv:2507.05257, 2025-07 — https://arxiv.org/abs/2507.05257
20. Tan et al., “MemBench”, arXiv:2506.21605, 2025-06 — https://arxiv.org/abs/2506.21605
21. Tavakoli et al., “BEAM”, arXiv:2510.27246, 2025-10 — https://arxiv.org/abs/2510.27246
22. Salesforce AI Research, “ConvoMem”, arXiv:2511.10523, 2025-11 — https://arxiv.org/abs/2511.10523
23. “Memory Tax”, arXiv:2610.34073, 2026-10 — https://arxiv.org/abs/2610.34073 `[论文]`
24. “Is Complex Agentic Memory Always Needed? Naive RAG Is Stronger Than We Thought”, arXiv:2610.30755, 2026-10 — https://arxiv.org/abs/2610.30755 `[论文]`
25. Anthropic, “Contextual Retrieval”, 2024-09 — https://www.anthropic.com/news/contextual-retrieval `[官方]`
26. Anthropic, “Effective context engineering for AI agents”, 2025-09 — https://www.anthropic.com/engineering/effective-context-engineering-for-ai-agents `[官方]`
27. OWASP GenAI Security Project, Agentic Top 10 2026（ASI06 Memory & Context Poisoning）— https://genai.owasp.org/ `[官方]`
28. Model Context Protocol Go SDK — https://github.com/modelcontextprotocol/go-sdk `[官方]`
29. pgvector — https://github.com/pgvector/pgvector `[官方]`
30. pgvector-go — https://github.com/pgvector/pgvector-go `[官方]`
31. modernc.org/sqlite — https://pkg.go.dev/modernc.org/sqlite `[官方]`
32. sqlite-vec — https://github.com/asg017/sqlite-vec `[官方]`
33. gosqlite（modernc 之上的 typed vec/FTS5/RRF）— https://gosqlite.com/ `[官方]`
34. OpenTelemetry Go — https://github.com/open-telemetry/opentelemetry-go `[官方]`
35. CloudWeGo Eino — https://github.com/cloudwego/eino `[官方]`
36. Genkit Go — https://github.com/firebase/genkit `[官方]`
37. LangChainGo — https://github.com/tmc/langchaingo `[官方]`
38. SAMEP, arXiv:2507.10562, 2025-07 — https://arxiv.org/abs/2507.10562
39. MemoryBank, AAAI 2024 — https://ojs.aaai.org/index.php/AAAI/article/view/29946
40. Park et al., “Generative Agents”, arXiv:2304.03442, 2023-04 — https://arxiv.org/abs/2304.03442
41. Anthropic Memory tool — https://docs.anthropic.com/en/docs/agents-and-tools/tool-use/memory-tool `[官方]`

---

## 9. 一句话建议

**评测地基、三通道上下文与 MCP 已落地；下一步用外部数据集证明收益，再做 ANN + 时态 + 多租户。** 把 Memorigo 做成“Go 生态里最可信、最可自托管的记忆层”，而不是“功能最多的记忆框架”。
