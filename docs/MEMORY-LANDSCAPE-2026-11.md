# Memorigo 记忆系统调研（2026-11 第二轮）

> 研究快照：2026-11-18。方法：优先官方文档、论文与仓库；厂商自报数字单独标注。
>
> 标记：`[论文]` 论文/预印本；`[官方]` 官方文档或仓库；`[厂商自报]` 未见独立复现；`[推断]` 本文推理。
>
> **重要提醒**：2026 年 memory 领域的 benchmark 协议差异很大，LoCoMo / LongMemEval / MemoryAgentBench 的分数不可直接横向比较。本文把“可借鉴的机制”和“营销数字”分开看。
>
> **落地状态（2026-11-18）**：本文推荐的 Profile Card 与 Context Engine v2 已实现，进入自动注入和 MCP 默认路径；Fact metadata v3、巩固/观察层仍是建议。

---

## 0. 结论先行

这一轮调研最重要的变化，不是又出现了更强的图数据库，而是行业开始同时承认两件事：

1. **“记得更多”不等于“答得更对”**。Memory Tax、Raw-RAG、Obsolete Facts 三篇 2026 论文都指向同一个结论：经过改写/压缩的记忆会丢失证据，复杂记忆结构经常打不过“原始历史 + 强检索 + 重排”。
2. **真正稀缺的是“不靠 query 也能稳定拿到的高信号上下文”**。Supermemory、Memobase、Honcho、LangMem 都在做 profile / peer card；Hindsight 在做 observations / mental models；Anthropic 在做 wiki + dreaming。它们解决的是同一类问题：用户问“帮我推荐个餐厅”时，姓名、所在地、饮食偏好这些事实不会出现在 query 里，纯向量检索天然漏掉。

对 Memorigo 的判断：

- **不要做 GraphRAG / Graphiti 克隆**。现有 `slot/status` + 双通道已经覆盖了大部分“知识更新”需求；图只在多跳/全局问题上可能有增益，且 memory tax 很高。
- **最值得先做的是 always-on profile（常驻画像）**。它不需要改 schema，直接复用现有 slot 分类，能立刻让 MCP / SDK 的“懂用户”体感上一个台阶。
- **第二个是 Context Engine v2**：事实 + 原始消息联合重排、MMR 去重、覆盖度/弃答、lost-in-the-middle 排序。它是“降低 memory tax”的直接手段，也不需要 schema。
- **第三个是 Fact metadata v3**：`valid_from/valid_to + confidence + asserted_by + trust`。它和信任分级共用一次迁移，是后续审计、时态查询、巩固/观察层的地基。
- **巩固/观察层（observations / dreaming）放到最后**。它很吸引人，但需要分页扫描、合并/过期原语、后台任务和跨进程协调；在 profile 与时态地基之前做，收益/风险不划算。

---

## 1. 2026 年值得关注的系统与特色

| 系统 | 核心机制 | 最值得借鉴 | 不要照搬 |
|---|---|---|---|
| **Mem0** | LLM 抽取 + ADD/UPDATE/DELETE 调和；语义 + 关键词 + 实体 + 时态多信号；2026 增加 procedural memory、decay、scoped memory | 多信号融合、异步写入、按 user/agent/run 作用域 | 营销分数；每轮全量重抽；把图当默认 |
| **Zep / Graphiti** | episode → entity/fact edge → community；**双时态**（valid time + transaction time）；冲突失效而非删除；cosine + BM25 + 图遍历 | 失效不删除、时态区间、episode 作为 provenance、查询期不调 LLM | 每个 episode 都跑 LLM 的成本；社区摘要；Neo4j 依赖 |
| **Letta / MemGPT** | core/recall/archival 三层；memory blocks；MemFS；sleep-time / dreaming 后台巩固；propose → verify | 常驻 core memory、后台巩固、审计/doctor、可回滚的变更 | 整套 agent runtime / git-backed MemFS |
| **Hindsight** | world / experience / observation / opinion 四网络；retain / recall / reflect；TEMPR（语义+BM25+图+时态）+ RRF + rerank + token budget；observations 带证据链/置信度/历史；mental models | observation 层、证据链、按 token 预算填充、mental models 的刷新策略 | 四网络全量复制；一开始就做 mental models |
| **Supermemory** | 自动维护 user profile：static / dynamic facts + topic buckets；profile 与 search 并行 | **常驻 profile + 按主题分桶**、profile 不依赖 query | 连接器/多模态；厂商延迟数字 |
| **Memobase** | profile topics/sub-topics + event timeline（summary + tags + **profile delta** + time）；buffer flush | 事件时间线、profile delta、token 上限、`only_topics/prefer_topics` | 重后端（Postgres+Redis）；把摘要当唯一记忆 |
| **LangMem** | semantic / episodic / procedural；profiles vs collections；hot-path vs background formation；namespace | profile（单文档当前状态）与 collection（可检索记录）分开；三类记忆 | 纯向量、无混合检索；Python 运行时 |
| **Honcho** | Peer 为中心；Deriver / Dreamer / Dialectic；Peer Card（≤40 核心事实）；directional representations | **Peer Card** 的“少而精”、Dreamer 周期巩固、按 observer/observed 的视角 | 完整 theory-of-mind / 多 agent 视角；AGPL 依赖 |
| **Anthropic** | transcripts → notes → representations（wiki）→ injected context；dreaming 离线重读/纠错/建 wiki | 常驻注入层要小；wiki + search > 单纯 search；离线 dreaming 能纠错 | 直接把 413 个文件式笔记搬进数据库；无治理的自动改写 |
| **A-MEM / MemoryOS / MemOS** | Zettelkasten 链接 + 记忆演化；短/中/长三层；MemCube + plaintext/activation/parametric | 分层、链接、演化这些“概念”可借鉴 | 构建成本、无遗忘、in-place mutation、activation/parametric 记忆 |

### 1.1 三条被 2026 论文反复验证的结论

1. **复杂记忆经常输给 naive RAG + raw history**  
   Memory Tax 在 LongMemEval-s 上报告：naive RAG 81.4，A-Mem 79.4，HippoRAG2 78.2（Qwen3.6-27B）；Raw-RAG 在 7 个 benchmark-model 组合中 6 个第一。\([论文]\)  
   含义：Memorigo 的“事实 + 原始消息双通道”方向是对的，但**必须联合重排**，不能事实先占满预算。

2. **过期事实的伤害大于无关证据**  
   Obsolete Facts 报告：证据预算从 64 增到 512 token，BM25 在 6K 历史上从 45% 降到 32%；把一条无关证据换成旧版本事实，Qwen2.5 从 100% 降到 44.6%。\([论文]\)  
   含义：`superseded` 不够，检索时必须**退役/降权旧事实**，并保留 `valid_from/valid_to` 供审计。

3. **MemoryAgentBench 的冲突消解大量是“last-write bookkeeping”**  
   2026 的复现分析指出，一个冻结的“最新事实胜出”解析器就能答对约 80% 的 Conflict Resolution 题；很多失败来自旧事实被一起检索出来，而不是记不住新事实。\([论文]\)  
   含义：Memorigo 的 slot/status 已经做对了一半；下一步是把“最新/有效”语义扩展到非互斥事实和上下文装配层。

---

## 2. Memorigo 现状：优势与真实缺口

### 2.1 已经很强的地方

- 写路径幂等、SQL 事务、异步增强、Flush/Close、panic recover。
- slot / status / superseded / 软删除；互斥 slot 的 last-write 语义。
- 稠密 + BM25 + 新近度 + 频率混合召回；有相关度闸门。
- `BuildContext` 的 token 预算、`fact:<id>` / `msg:<id>` 引用、原始消息证据通道。
- MCP stdio + HTTP + Bearer；SQLite / PostgreSQL / Mongo；纯 Go 默认路径。
- 15 个确定性评测场景 + CI；离线 hash 嵌入可零依赖跑通。

这些是“生产级记忆层”的真本事，不应该为了追新而推翻。

### 2.2 与 2026 主流能力的差距

| 缺口 | 现状 | 影响 |
|---|---|---|
| **常驻 profile** | 只有 query-driven recall；`Facts()` / `memory_profile` 能列事实，但不会每轮注入 | 用户问“推荐个餐厅”时，姓名/所在地/偏好可能完全召回不到 |
| **时态有效性** | 只有 `status` + `date_last_time`；没有 `valid_from/valid_to` | 不能回答“去年我在哪工作”；审计只能靠 conversation id |
| **来源/置信度** | `source_conversation_id` 有，`source_message_id/asserted_by/confidence/trust` 没有 | 无法区分用户明说 vs 模型推断；无法做低信任隔离 |
| **上下文装配** | 事实先占预算，原始消息后填；没有联合重排/MMR/弃答 | 旧事实可能挤掉正确原文；近重复事实浪费 token |
| **事件/情景记忆** | 有 conversation summary，但不可检索、无时间线 | “上周我们决定了什么”只能靠原始消息扫 |
| **巩固/观察** | 只有手动 Forget；没有合并、过期、观察层 | 记忆会越积越多，但没有“越用越准” |
| **查询路由** | 每个 turn 都尝试事实召回 | “帮我算个积分”也会触发记忆检索，增加噪声和延迟 |
| **多租户/共享** | 只有 entity_id；没有 org/app/agent 作用域 | 不能做团队共享记忆、按 agent 隔离 |

### 2.3 代码级硬约束（来自当前 1.0.0 树）

- `UNIQUE(entity_id, uniq)` + content-only hash 是最大的 schema 约束：同一内容只能有一行，无法天然表达“同一事实的多个有效期”或“不同来源的同一句话”。  
  → MVP 先做“一行 + 最新信任/有效期”，真正的多版本要改唯一键，SQLite 需要重建表。
- `MessageRepo.Create` 只返回 `error`；`AugmentationInput.Messages` 没有消息 ID。  
  → `source_message_id` 需要改 `storage.MessageRepo`、Writer 和两个 SQL/Mongo 实现，建议第二阶段做。
- `prepareMessages` 只重新渲染 `pkg.Facts`，不消费 `pkg.Text`。  
  → 如果只在 `packContext` 里加 profile/observation/abstention，自动注入看不到。必须先统一渲染路径。
- `storage.Repos` 是导出接口，直接加必选方法会破坏外部 driver。  
  → 新能力用可选接口（`ProfileRepo`、`FactTemporalRepo`、`EpisodeRepo`、`MaintenanceRepo`）+ type assertion，保留 fallback。
- `status` 字符串在 SQL/Mongo 多处硬编码。  
  → 信任隔离优先用 `trust` 数值过滤，不要急着加 `quarantined` 状态，避免全量 sweep。
- 没有 `EntityRepo.List`、没有分页 fact/message 扫描；`List` 是 top-N，`Candidates` 上限 2000。  
  → 巩固/时间线任务需要新的可选 `MaintenanceRepo`，不能靠现有 API 硬凑。

---

## 3. 候选功能排序

评分：适配度 = 与现有架构/定位的契合；价值 = 对“生产级记忆层”叙事的提升；工作量 S/M/L。

| 优先级 | 方向 | 参考 | 适配 | 价值 | 工作量 | 主要风险 |
|---|---|---|---|---|---|---|
| **P0** | Always-on Profile Card（static/dynamic + buckets） | Supermemory / Memobase / Honcho / LangMem | 5 | 5 | S/M | profile 过期、重复、占预算 |
| **P0** | Context Engine v2：联合重排 + MMR + coverage + abstention + LITM | Memory Tax / Raw-RAG / Obsolete Facts | 5 | 5 | M | 排序回归、误弃答 |
| **P1** | Fact metadata v3：`valid_from/valid_to/confidence/asserted_by/trust` + as-of | Zep/Graphiti / Memobase | 5 | 5 | M | `uniq` 约束、迁移语义 |
| **P1** | 查询意图路由（profile / fact / temporal / episodic / none） | Mem0 / Supermemory | 4 | 4 | S/M | 误路由、语言覆盖 |
| **P1** | 事件/情景时间线（summary + tags + profile delta） | Memobase / MemoryOS | 4 | 4 | M | 摘要损失、重复通道 |
| **P2** | 巩固/观察层（observations + evidence links + 刷新） | Hindsight / Anthropic dreaming / Letta | 4 | 5 | L | LLM 成本、漂移、并发 |
| **P2** | 程序性记忆（规则/技能/提示片段 + 版本） | LangMem / Mem0 | 3 | 3 | M | 适用面窄、审计复杂 |
| **P2** | Mental models / knowledge pages | Hindsight / Anthropic wiki | 3 | 4 | L | 幻觉、维护成本 |
| **P3** | 实体链接 + 1-hop 图扩展 | Graphiti / A-MEM | 2 | 3 | L | memory tax、评测未证明 |
| **P3** | 多 agent 共享 / directional representations | Honcho | 2 | 3 | L | 多租户前置、作用域复杂 |
| **P3** | 多模态记忆 | Supermemory / MemOS | 2 | 2 | L | 存储/检索协议不成熟 |

**结论**：先做两个 P0（profile + context v2），再做 P1 的 fact v3；巩固/观察层放最后。P3 不建议现在碰。

---

## 4. 推荐设计

### 4.1 P0-1：Always-on Profile Card

**目标**：每一轮都把一小块“关于用户是谁”的高信号画像注入上下文，不依赖 query 命中；事实召回仍然按 query 走。

**数据来源（不改 schema）**

复用现有 slot 约定：

| 分组 | slot | 静态/动态 |
|---|---|---|
| Identity | `profile.name`、`profile.location`、`profile.origin`、`profile.birthday`、`profile.role`、`profile.employer` | 静态 |
| Preferences | `favorite.*` | 静态 |
| Health | `health.allergy.*` | 静态 |
| Skills | `skill.*` | 静态 |
| Opinions | `sentiment.*` | 动态 |
| Current focus | `goal.*`、`project.*`、`status.*`（若抽取器产生） | 动态 |
| Other | 最近 30 天、非互斥事实 | 动态 |

- 静态事实优先；动态事实只在 `IncludeDynamic` 时加入。
- 按 slot 优先级排序，而不是纯新近度。
- 最多 40 条（参考 Honcho Peer Card 的“少而精”），默认预算 120–200 token，最多不超过总预算的 25%。
- 与 query recall 结果按 fact ID 去重；profile 已包含的事实不重复出现在 “Long-term memories”。

**API**

```go
type ProfileOptions struct {
    TokenBudget   int      // 0 -> 默认 200
    MaxFacts      int      // 0 -> 40
    IncludeDynamic bool    // 默认 false
    Buckets       []string // 可选，限制分组
}

type ProfileSection struct {
    Name  string `json:"name"`  // Identity / Preferences / ...
    Facts []Fact `json:"facts"`
}

type Profile struct {
    EntityID string           `json:"entity_id"`
    Sections []ProfileSection `json:"sections"`
    Facts    []Fact           `json:"facts,omitempty"` // 兼容旧调用
    Tokens   int              `json:"tokens"`
    Budget   int              `json:"budget"`
    Truncated bool            `json:"truncated"`
}

func (m *Memori) Profile(ctx context.Context, opts ProfileOptions) (Profile, error)
```

`ContextOptions` 增加：

```go
IncludeProfile bool
ProfileBudget  int
ProfileLimit   int
```

`ContextPackage` 增加：

```go
Profile      []Fact `json:"profile,omitempty"`
ProfileCount int    `json:"profile_count,omitempty"`
```

**渲染**

```text
<memory_profile>
Identity:
- [fact:12] My name is Max
- [fact:18] I live in Shanghai
Preferences:
- [fact:21] I prefer concise Chinese answers
Current focus:
- [fact:36] I am migrating the billing service this week
</memory_profile>
```

`DefaultMemoryPrompt` 增加可选的 `{{profile}}` 段；`prepareMessages` 改为统一渲染 `profile + facts`，不要只渲染 `pkg.Facts`。如果调用方模板没有 `{{profile}}`，自动在 `{{facts}}` 前插入 profile 段，保持向后兼容。

**MCP**

- `memory_profile`：返回 `sections` + `facts`，保留旧字段；默认 50 条，上限 500。
- `memory_context`：新增 `include_profile`（默认 `true`）和 `profile_budget`。
- `memory_search` 不返回 profile，避免搜索结果被画像污染。

**测试**

- 画像包含姓名/所在地/偏好；不含 superseded/deleted。
- 搬家后画像只显示新城市。
- 画像与 query 召回按 ID 去重。
- profile 预算裁剪后 `Tokens <= Budget`。
- 无 query 时仍能注入 profile（或明确配置关闭）。
- 跨 entity 不泄漏。
- eval 新增场景：用户先自述，再问一个与画像无词面重叠的问题，断言 profile 事实进入上下文。

**工作量**：S/M。无迁移；主要是 `context.go`、`memory_prompt.go`、`memori.go`、`mcpserver/server.go` 和测试。

---

### 4.2 P0-2：Context Engine v2

**目标**：降低 memory tax；让原始证据有机会战胜过期/错误事实；让上下文更短、更全、更靠前。

**四个改动**

1. **事实 + 原始消息联合重排**
   - 当前：`BuildContext` 分别取 facts 和 messages，facts 先占预算。
   - 改为：两路候选先各取 top-N（如 facts 50、messages 50），用 RRF 或归一化加权融合；保留 channel prior（facts 0.6 / messages 0.4，可配）。
   - 为原始证据保留至少 20–30% 预算（`IncludeRaw` 时），防止 facts 把证据挤掉。
   - 旧事实/低信任事实在联合排序中降权；`valid_to` 非空的事实直接排除。

2. **MMR 去重**
   - 在 `rankFacts` 截断 top-k 之前做 MMR：`λ * score - (1-λ) * max_sim_to_selected`，默认 λ=0.7。
   - 需要保留内部 `rankedFact.fact.Embedding`；不要只把 `Fact` 暴露给上层。
   - 原始消息按 conversation/时间做多样性约束，避免同一段对话占满。

3. **Coverage + Abstention**
   - 计算 query token 覆盖度（可用现有 BM25 token 集）。
   - `ContextPackage` 增加 `Coverage`、`TopScore`、`InsufficientEvidence`。
   - 当 top score 低于阈值且覆盖度为 0 时，不注入记忆（或只注入 profile），避免“为了用记忆而用记忆”。
   - 自动注入根据 `InsufficientEvidence` 跳过，而不是渲染空块。

4. **Lost-in-the-middle 排序**
   - 同一 section 内，最强项放开头，次强项放结尾；中间放补充证据。
   - 不要打乱 citation 的可追溯性；`Facts`/`Citations` 顺序与文本一致。
   - 通过配置开关，默认开启；评测对比。

**查询意图路由（可同批做）**

新增 `memori/intent.go`，先做确定性规则（英文 + 中文）：

- `profile`：`what do you know about me` / `我的偏好` / `我是谁` → profile 优先。
- `temporal`：`when` / `last week` / `以前` / `去年` → 时态/时间线（等 v3）。
- `episodic`：`what did we decide` / `上次讨论` → episode（等 v5）。
- `procedural`：`how should you respond` / `按我的规则` → procedure（可选）。
- `none`：纯计算/代码/工具调用，明显不需要个人记忆 → 跳过召回。
- 其余 → 混合事实召回。

路由结果写入 `ContextOptions.Intent`，MCP `memory_context` 可显式指定；默认规则可关闭，保持现有行为。

**测试**

- 旧事实 + 新原始消息：新证据进入上下文，旧事实降权/排除。
- 近重复事实只占一份预算。
- 弱 query 返回 `InsufficientEvidence`，自动注入不产生噪声。
- LITM 排序后强项在首/尾。
- 路由：profile/temporal/episodic/none 各一条；中英文都覆盖。
- eval 增加“memory tax”场景：同一问题分别跑 facts-only、raw-only、联合三种，报告 accuracy/token。

**工作量**：M（联合重排 + MMR + 路由），S（coverage/abstention/LITM 单独）。无迁移。

---

### 4.3 P1：Fact metadata v3（时态 + 来源 + 信任）

**目标**：让“这条记忆什么时候为真、谁说的、有多可信”成为一等字段；支持 point-in-time 查询和低信任隔离。

**建议一次迁移加齐**（SQLite/Postgres v3；Mongo 加字段 + 索引）：

```sql
ALTER TABLE memori_entity_fact ADD COLUMN valid_from TIMESTAMP;
ALTER TABLE memori_entity_fact ADD COLUMN valid_to TIMESTAMP;
ALTER TABLE memori_entity_fact ADD COLUMN confidence REAL NOT NULL DEFAULT 1.0;
ALTER TABLE memori_entity_fact ADD COLUMN asserted_by VARCHAR(20) NOT NULL DEFAULT 'user';
ALTER TABLE memori_entity_fact ADD COLUMN trust REAL NOT NULL DEFAULT 1.0;
```

- 回填：SQL `valid_from = date_created`，`valid_to = NULL`；Mongo 读取时缺省按 `date_created` 处理。
- `SupersedeSlot` 在把旧行标为 `superseded` 时同时写 `valid_to = now`。
- `Forget` 也写 `valid_to = now`（软删除 + 时态关闭）。
- `Candidates` 保持只取 active；新增可选 `FactTemporalRepo.CandidatesAsOf(ctx, entityID, asOf, limit)`：
  - `valid_from <= asOf AND (valid_to IS NULL OR valid_to > asOf)`；
  - 包含 `superseded` 行，用于历史查询；
  - 默认 `asOf = now` 时行为与当前一致。
- `Fact` / `FactResult` 增加 `ValidFrom/ValidTo/Confidence/AssertedBy/Trust`；MCP `factView` 同步。
- 信任来源：
  - `Remember` / 用户明说：`asserted_by=user`，`trust=1.0`；
  - 启发式抽取：`user`，`0.9`；
  - LLM 抽取：`user`，`0.8`；
  - assistant/tool 推断：`assistant`/`tool`，`0.4–0.6`；
  - MCP agent 写入：`agent`，`0.6–0.8`（可配）。
- Upsert 时取 `max(trust)`、`asserted_by` 取更高信任来源；自动注入默认 `MinTrust=0.5`，低信任事实只可通过显式 `IncludeLowTrust` 召回。
- **不要**新增 `quarantined` 状态；先用 trust 数值过滤，避免 status 字符串全量 sweep。

**`uniq` 约束的现实处理**

MVP 接受“一条内容一行”：

- 同一内容重复出现：`num_times++`、`date_last_time` 更新、`trust=max`；不创建第二段有效期。
- `valid_from` 表示首次进入系统的时间；`valid_to` 表示被覆盖/遗忘的时间。
- 真正需要“同一事实多段有效期”时，再引入 `version` 或去掉 `UNIQUE(entity_id, uniq)`；SQLite 需要重建表，SQLite/Postgres/Mongo 都要迁移。不要在 v3 里偷偷改唯一键。

**`source_message_id` 第二阶段**

- 需要 `MessageRepo.Create` 返回 ID、`AugmentationInput` 携带存储后的消息 ID、extractor 把候选事实映射到来源消息。
- 用可选接口 `MessageIDRepo`，避免破坏外部 driver。
- 在拿到 `source_message_id` 之前，`ContextCitation` 仍用 `source_conversation_id`。

**验收**

- 搬家/改名：当前召回只返回新值；`RecallAsOf(过去时间)` 返回旧值。
- 旧事实不会出现在当前上下文；`valid_to` 有值。
- 低信任事实不进入自动注入；显式 `IncludeLowTrust` 可召回。
- 迁移在 SQLite/Postgres/Mongo 三后端通过；schema version 3；文档更新。
- eval 新增“as-of”“stale suppression”“trust gate”三个场景。

**工作量**：M（不含 `source_message_id`）；L（含真正多版本 + 消息溯源）。

---

### 4.4 P1：事件/情景时间线（Episodic Timeline）

**目标**：让“上周我们决定了什么”“这个项目什么时候改过方案”可检索。

**最小设计**

- 新表 `memori_episode`（或 conversation 扩展列）：
  ```text
  id, uuid, entity_id, conversation_id,
  summary, tags_json, profile_delta_json,
  started_at, ended_at, embedding, embedding_model, embedding_dim,
  status, date_created
  ```
- 生成：会话结束或 `Maintain` 时，用现有 conversation summary + slot delta + 消息时间范围生成 episode。
  - 确定性 fallback：summary + changed slots + 首尾消息；
  - 可选 LLM：严格 JSON（summary/tags/profile_delta），失败回退。
- 检索：`RecallEpisodes(query, limit)`，复用 BM25 + 余弦 + 时间衰减；`BuildContext` 增加 `IncludeEpisodes`，在 facts 之后、raw messages 之前。
- 引用：`episode:<id>`，并带 `conversation_id`。
- MCP：`memory_timeline`（query/time range/limit），`memory_context` 增加 `include_episodes`。

**为什么放 P1 而不是 P0**：它依赖较好的 summary 和时态字段；先把 profile + context v2 做好，episode 的收益更容易验证。

**工作量**：M（表 + 生成 + 检索）；LLM episode 生成 L。

---

### 4.5 P2：巩固 / 观察层（Observations / Dreaming-lite）

**目标**：从“事实堆积”变成“越用越准”：去重、过期、合并、生成带证据的观察；可选 LLM 巩固。

**为什么最后做**

- 需要分页扫描（当前 `List` top-N、`Candidates` 2000 上限）。
- 需要合并/过期/重写原语（当前没有 `UpdateContent`/`MergeFacts`/`ExpireFacts`）。
- 需要后台任务/调度；MCP `state.mu` 会阻塞长任务，必须异步或独立进程。
- Mongo standalone 无事务，需要幂等/补偿。
- 没有评测证明“巩固后 accuracy 不降”之前，不应默认开启。

**建议落地顺序**

1. **确定性维护**：重复合并、TTL 过期、profile 重建、报告；`DryRun` 默认 true。
2. **观察层**：`memori_observation` 表（topic/content/embedding/confidence/evidence_fact_ids/valid_from/valid_to/status/version）；确定性 fallback 先做，LLM 可选。
3. **反思/mental models**：高频问题预计算答案，带 `based_on` 证据和版本历史；必须支持删除/回滚。
4. **调度**：由调用方 cron / 独立 worker 触发；不在请求路径内跑 LLM。

**关键约束**

- observation 只能从 raw facts 生成，**不能读其他 observation**，避免自我强化。
- 空结果不能覆盖旧内容（Hindsight 的 empty guard）。
- 每次刷新保留历史版本和证据链；软删除可回滚。
- 巩固前后必须报告：条数、token、accuracy、stale-answer rate。

**工作量**：L。

---

### 4.6 现在不建议做

| 方向 | 原因 |
|---|---|
| 通用 GraphRAG / Graphiti 克隆 | 构建成本高、memory tax 高；图只在多跳/全局子集可能有益，需评测证明 |
| A-MEM 式全量链接 + 反向演化 | 构建成本极高，且会改写旧记忆；审计困难 |
| MemOS 的 activation/parametric memory | 研究前沿，和 Go SDK/自托管定位不匹配 |
| Letta MemFS / 完整 agent runtime | 产品形态不同；Memorigo 是记忆层，不是 agent 框架 |
| Honcho directional representations | 多 agent 视角很酷，但需要多租户/权限先完成；当前场景收益低 |
| 多模态记忆 | 存储/检索协议不成熟，偏离核心 |
| 自动人格化 / 无纠正入口的画像 | 风险高；必须先有 profile 的查看/纠正/删除 |

---

## 5. 验收：每个功能都要能证明“benefit > tax”

2026 的评测结论很清楚：不能只和“其他记忆系统”比，必须和 **naive RAG / raw history** 比。

建议每个新功能都同时报告：

- accuracy（LLM-judge + 人工抽检）
- recall@k / evidence coverage
- token/query、P95 延迟、构建成本
- **memory tax**：同一问题，记忆系统答错而 raw RAG 答对的比率
- stale-answer rate（旧事实导致的错误）
- 删除/隔离：跨 entity 泄漏 = 0，Forget 后不可召回
- 配置哈希 + 随机种子，保证可复现

`eval/` 可以先加确定性场景，再接外部数据集；不要把 15/15 当成 LoCoMo/LongMemEval 分数。

---

## 6. 建议的 90 天顺序

| 阶段 | 内容 | 交付信号 |
|---|---|---|
| **第 1–2 周** | Profile Card + 统一渲染 + MCP `memory_profile` 分组 | 无 schema；eval 新增 profile 场景；`memory_context` 带 profile |
| **第 3–4 周** | Context Engine v2：联合重排 + MMR + coverage/abstention + LITM | 旧事实/原始证据对比场景通过；token 下降或持平；无排序回归 |
| **第 5–7 周** | Fact metadata v3：时态 + 信任 + as-of | 三后端迁移；as-of/stale/trust 场景通过；文档/CHANGELOG |
| **第 8–9 周** | Episodic timeline + 查询路由 | `memory_timeline`；profile/temporal/episodic/none 路由测试 |
| **第 10–12 周** | 确定性巩固 + observation 原型（默认 DryRun/关闭） | 条数下降、accuracy 不降；可回滚；报告 memory tax |

如果只能做一件事：**Profile Card**。  
如果只能做一组：**Profile Card + Context Engine v2**，两周内可交付、无迁移、用户体感最强。

---

## 7. 来源

访问日期均为 2026-11-18。厂商数字未独立复现。

1. Anthropic, “The components of agent memory” — https://www.anthropic.com/research/components-of-agent-memory `[官方]`
2. Rasmussen et al., “Zep: A Temporal Knowledge Graph Architecture for Agent Memory”, arXiv:2501.13956 — https://arxiv.org/abs/2501.13956 `[论文]`
3. Graphiti README — https://github.com/getzep/graphiti `[官方]`
4. Chhikara et al., “Mem0”, arXiv:2504.19413 — https://arxiv.org/abs/2504.19413 `[论文]`
5. Mem0 memory types docs — https://docs.mem0.ai/core-concepts/memory-types `[官方]`
6. Hindsight, ACL 2026 System Demonstrations — https://aclanthology.org/2026.acl-demo.27/ `[论文]`
7. Hindsight docs, “How Hindsight works” — https://docs.hindsight.vectorize.io/enterprise/how-hindsight-works/ `[官方]`
8. Hindsight, “Mental models deep dive” — https://hindsight.vectorize.io/blog/2026/06/05/mental-models-deep-dive `[官方]`
9. Supermemory, “User profiles” — https://supermemory.ai/docs/concepts/user-profiles `[官方]`
10. Memobase, “Event Fundamentals” — https://docs.memobase.io/features/event/event `[官方]`
11. Memobase, “Best Practices & Tips” — https://docs.memobase.io/practices/tips `[官方]`
12. Letta, “Memory & dreaming” — https://docs.letta.com/configuration/memory/index.md `[官方]`
13. LangMem, “Long-term Memory in LLM Applications” — https://langchain-ai.github.io/langmem/concepts/conceptual_guide/ `[官方]`
14. Honcho, “Architecture & Intuition” — https://honcho.dev/docs/v3/documentation/core-concepts/architecture `[官方]`
15. Honcho, “Peer Representations” — https://honcho.dev/docs/v3/documentation/core-concepts/representation `[官方]`
16. He et al., “Memory Tax: Dissecting the Net Value of Agent Memory”, arXiv:2610.34073 — https://arxiv.org/abs/2610.34073 `[论文]`
17. Bao et al., “Is Complex Agentic Memory Always Needed? Naive RAG Is Stronger Than We Thought”, arXiv:2610.30755 — https://arxiv.org/abs/2610.30755 `[论文]`
18. Babu Poikarayil et al., “When Retrieval Is Not Enough: Obsolete Facts in Agent Memory”, arXiv:2610.36308 — https://arxiv.org/abs/2610.36308 `[论文]`
19. Hu et al., “MemoryAgentBench”, arXiv:2507.05257 — https://arxiv.org/abs/2507.05257 `[论文]`
20. “Bookkeeping, Composition, or Unreachable Gold? Reading MemoryAgentBench’s Conflict-Resolution Scores…”, arXiv:2610.09193 — https://arxiv.org/abs/2610.09193 `[论文]`
21. Xu et al., “A-MEM”, arXiv:2502.12110 — https://arxiv.org/abs/2502.12110 `[论文]`
22. Kang et al., “Memory OS of AI Agent”, arXiv:2506.06326 — https://arxiv.org/abs/2506.06326 `[论文]`
23. Li et al., “MemOS”, arXiv:2507.03724 — https://arxiv.org/abs/2507.03724 `[论文]`
24. OWASP GenAI Security Project, Agentic App Security — https://genai.owasp.org/initiatives/agentic-security-initiative/ `[官方]`
25. Lin et al., “Sleep-time Compute”, arXiv:2504.13171 — https://arxiv.org/abs/2504.13171 `[论文]`

---

## 8. 一句话

**Memorigo 的下一步不是“更多结构”，而是“更少但更准的常驻上下文 + 更诚实的双通道证据”。** 先做 Profile Card，再做 Context Engine v2；时态/信任紧随其后；图与重型巩固留到评测证明收益之后。
