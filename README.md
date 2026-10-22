# chatai

`chatai` 是 kohme 的群聊 AI 插件。机器人被 At、回复或戳一戳时，会自动带入当前用户的核心记忆，并根据问题查询更多群聊记录。启用长期记忆维护后，默认在与 Agent 的会话过期时提取结构化用户记忆与关系，Agent 可以发送消息、At 群友、戳一戳、读取网页或创建定时任务。

## 功能

- Agent 多轮工具调用，通过 `search_tools` 按需加载少量相关工具，不把全部工具或聊天记录直接发送给模型
- 全局 Skill：既可从不同群的成功任务中自动合并生成，也可直接通过配置声明
- 群聊上下文持久化到插件数据库，重启后仍可读取
- 保留 OneBot 原始 `raw_message` 和消息段数组，支持 `send_raw_message`，并可读取、重建合并转发节点
- 发送返回 `message_id=0` 时回到 Agent 修正参数，同一次运行累计三次发送失败后报错；已成功的消息不自动重发
- Agent、图片解析、入群欢迎可以使用不同模型
- 可选图片解析；没有配置图片模型时不会解析图片
- 回复含图片的历史消息时，也会把被引用图片交给图片模型解析
- 会话过期后由独立后台模型提取用户记忆与关系，可选全群消息维护；Agent 自动读取核心记忆，支持明确记住、更正和遗忘
- 可开关的群聊复读，并可设置连续多少条相同消息后触发
- 日志显示 Agent 调用轮次、模型决策输出、供应商返回的 `reasoning_content`、工具名称与执行状态；提示词、工具参数和结果正文不展开
- 支持通义千问和 DeepSeek

机器人不会根据群聊活跃度主动插话。首次仍需 At、回复机器人或戳机器人；AI 成功回复后，为当前群的当前用户开启续聊窗口（默认 60 秒）。窗口内该用户无需再次 At：模型先结合最近群聊判断是否在与 AI 对话且值得回复，是则进入 Agent，成功回复后重新计时；否则保持沉默，将判断完成时的剩余时间减半。不同群、不同用户互不影响；过期后需重新明确触发。判断失败时保持沉默，窗口自然到期。复读功能独立配置。

`agent.continuation_seconds` 配置续聊存留秒数，未填写或小于等于 0 时使用 60 秒。续聊判断使用 Agent 路由模型单独调用，不调用工具或发送进度提示；成功发送对话消息时才续期，发送失败、进度提示、定时任务和复读不会开启窗口。窗口保存在内存中，重启后清空，独立于 `agent.conversation_memory` 的开关与过期时间。

同群同用户已有消息正在判断或执行回复时，后续普通消息只记录上下文，不再调用续聊判断或启动新 Agent，不排队补跑，也不因此缩短窗口。明确 At、回复机器人或戳机器人仍可触发；若存在多个明确触发的并行任务，全部结束后才恢复普通续聊。不同用户、不同群互不阻塞。`ask_user_and_wait` 正在等待的回答仍优先交给原任务。

## 配置示例

### OneBot 原始消息与发送重试

当前事件、历史上下文和追问回复仅提供消息摘要和 ID。原始消息段仍完整持久化，按需调用 `read_raw_message`（参数 `{"message_id":123}`，引用消息使用 `quoted_message_id`）读取。优先读取当前群数据库，缺失时查询 OneBot 并验证群归属。结果包含 `raw_segments`，以及按转发 ID 索引的 `forwards` 原始节点及元数据；嵌套转发自动展开（最多 8 层、32 个 ID），失败或超限通过 `forward_errors` 明确返回。

模型通过工具搜索加载 `send_raw_message`，参数示例：

```json
{"message":[{"type":"at","data":{"qq":"123456"}},{"type":"text","data":{"text":"你好"}},{"type":"face","data":{"id":"1"}}]}
```

工具使用 `Message []Segment`，`data` 参数值为字符串，普通消息段直接发送到当前群，不自动加引用或改写内容。合并转发先使用 `read_raw_message` 按消息 ID 读取原始段与节点，再构造全部为 `node` 的数组；此时自动使用 `send_group_forward_msg`。自定义节点的 `content` 是消息数组的 JSON 字符串，发送前展开为真正嵌套数组：

```json
{"message":[{"type":"node","data":{"uin":"10000","name":"匿名用户","content":"[{\"type\":\"text\",\"data\":{\"text\":\"原记录内容\"}}]"}}]}
```

这提供了修改转发记录展示署名的基础能力；具体节点、语音等消息段能否发送仍取决于 OneBot 实现。修改署名不等于平台级匿名，也不会自动移除正文中的身份信息。

普通文字、图片附文、@、分条消息和追问会在发送前检查字面量 `\n`/`\r`，拒绝发送并提示模型修正重复转义。确实需要原样展示代码或路径时使用 `send_raw_message`，避免自动替换损坏原文。

文字、图片、@、原始消息、分条消息和追问均检查返回 ID。ID 为零时打印 WARN，失败及已发送部分会反馈给模型；第三次失败打印 ERROR 并终止（总计最多三次失败，非首次发送后再重试三次）。修正发送有独立补充的决策轮次和工具调用额度，仍受运行超时约束。分条消息失败后停止剩余发送，反馈成功 ID、失败位置和未发送内容。欢迎语、用量回复、复读失败也进入群 Agent；进度提示失败反馈给正在运行的 Agent。私聊管理命令没有群 Agent，最多直接发送三次。

### YAML 配置

在 `plugins.yaml` 中配置：

```yaml
chatai:
  repo: github.com/kohmebot/chatai
  conf:
    system: |
      你是群里的猫娘，说话自然、简短，不要使用书面腔

    # 默认模型。没有单独指定模型的功能会使用这里的配置
    provider_name: tongyi
    model_name: qwen-plus
    max_tokens: 2048
    thinking: false

    providers:
      - provider_name: tongyi
        api_key: your-dashscope-key
      - provider_name: deepseek
        api_key: your-deepseek-key

    # 不同功能可以选择不同模型
    routes:
      agent:
        provider_name: deepseek
        model_name: deepseek-chat
        max_tokens: 2048
      vision:
        provider_name: tongyi
        model_name: qwen-vl-plus
        max_tokens: 1024
      # 可选；不配置时使用 routes.agent
      skill:
        provider_name: deepseek
        model_name: deepseek-chat
        max_tokens: 1024
      join:
        provider_name: tongyi
        model_name: qwen-turbo

    agent:
      continuation_seconds: 60
      conversation_memory:
        enable: true
        window_seconds: 600
        max_chars: 200
      max_steps: 8
      max_tool_calls: 16
      run_timeout_seconds: 180
      context_limit: 30
      schedule_max_seconds: 86400
      progress_after_seconds: 15

    skills:
      enable: true
      auto_activate_generated: false
      candidate_min_steps: 4
      activation_evidence: 3
      global_min_groups: 2
      default_ttl_days: 30
      max_active: 20
      max_candidates: 50
      reflection_workers: 1

    repeat:
      enable: true
      trigger_count: 3

    impression:
      enable: true
      observe_group_chat: false

    join_group:
      enable: true
      trigger: "%s 加入了群，欢迎他"
```

## 模型配置

### 默认模型

`provider_name` 和 `model_name` 是默认模型配置。某个路由没有完整配置时，会回退到默认模型。

支持的供应商名称：

| 名称 | 供应商 |
| --- | --- |
| `tongyi` | 阿里云百炼兼容接口 |
| `deepseek` | DeepSeek API |

Tongyi 模型名包含 `image`（大小写不敏感）时，会改用阿里云百炼的 `dashscope.aliyuncs.com/api/v1/services/aigc/multimodal-generation/generation` 图像生成接口；其他 Tongyi 模型仍使用 OpenAI 兼容接口。图生图可通过模型请求的 `ImageURL` 或最多三个 `image_url` content part 传入图片，生成成功后模型响应的 `Answer` 为首张图片 URL，完整图片列表保留在 `Response.Content` 中。图片 URL 仅临时有效，调用方应及时下载保存。

### 多路由

| 路由 | 用途 | 不配置时 |
| --- | --- | --- |
| `routes.agent` | 对话决策和工具调用 | 使用默认模型 |
| `routes.vision` | 带图事件的完整 Agent 决策与工具调用 | 带图事件仍由文本 Agent 处理，但无法读取图片 |
| `routes.skill` | 成功任务的后台反思和 Skill 候选生成 | 使用 `routes.agent` |
| `routes.impression` | 后台结构化用户记忆与关系维护 | 使用 `routes.agent` |
| `routes.memory` | 短期对话压缩 | 使用 `routes.agent` |
| `routes.join` | 生成入群欢迎语 | 使用默认模型 |

图片模型需要支持 OpenAI 兼容的 `image_url` 消息格式和工具调用。带图提问会把完整触发事件与原图直接交给该模型，并由它完成整轮 Agent 决策，不再先生成图片描述交给另一个模型。普通文本模型不要配置到 `routes.vision`，否则请求可能失败。

配置图片模型后，Agent 还会获得 `analyze_image` 工具。它接收必填的 `url` 和可选的 `prompt`，可按需描述图片、识别文字、分析截图，或回答针对图片的问题；图片 URL 会直接作为 `image_url` 交给 `routes.vision` 模型。

每个路由都可以单独设置：

- `provider_name`：供应商
- `model_name`：模型名称
- `thinking`：是否启用模型思考模式
- `max_tokens`：该功能的最大输出长度

## Agent 配置

### 短期对话记忆

`agent.conversation_memory` 为可选功能，默认关闭。启用后按「群 ID + 触发用户 ID」维护近期对外对话：包括触发消息、Agent 成功发送的回复、`ask_user_and_wait` 的问题与收到的回答；不包含模型推理、工具调用历史、工具结果、进度提示或其他群友的普通闲聊。失败发送不记入，分条发送仅记入成功部分。追问其他群友所得的回答保留实际用户 ID，归入发起本轮任务的用户会话。

- `enable`：是否启用，默认 `false`。
- `window_seconds`：自上次对话记录更新起的空闲过期时间，默认 `600` 秒；互动会续期。
- `max_chars`：压缩阈值及注入内容上限，默认 `200`，最小 `64`；按 Unicode 字符计数，包含摘要、第一步提示及对话标签，不是 token 数或字节数。小于最小值时回退为默认值。

未超过阈值时，下次触发会直接附带近期对话。超过阈值时，在本轮结束后单独调用压缩模型，将旧摘要与新增对外对话整理为 `summary` 和 `first_action`，自动放入下次事件的 `conversation_memory` 字段。摘要保留话题、约束、结果和未完成事项；第一步可提示先核实并读取某个 Skill，或先搜索并读取 `read_group_context`。不从隐藏的调用轨迹推断 Skill；对话无法确定具体 Skill 时应查询上下文。提示供 Agent 判断续聊时优先参考，不是强制工具执行，也不会覆盖用户的新话题或产生新的操作授权。

压缩模型可通过 `routes.memory` 单独配置，不配置则沿用 `routes.agent`，使用独立的压缩提示词。压缩有最多 10 秒超时，输入对话片段最多保留最近 16000 个字符；失败、非法输出或超长摘要会回退为受限片段及「先查群聊上下文」提示。压缩会额外消耗一次模型请求，200 字是较积极的压缩阈值，可按实际用量调大。

短期摘要保存在进程内，重启后清空；已有群聊数据库记录仍按原机制保存，可通过上下文工具查询。每轮结束才合并该轮对话；重叠运行可能暂时看到尚未压缩的受限片段，较晚返回的旧压缩结果不会覆盖新一轮记忆。定时任务不读取或更新此短期会话。启用长期记忆维护时，默认另外持久暂存这些实际对话的消息来源，在同一 `window_seconds` 空闲窗口到期后提取长期记忆，详见下文。

### 运行参数

| 配置 | 默认值 | 说明 |
| --- | ---: | --- |
| `max_steps` | `8` | 一次对话最多执行多少轮模型决策和工具调用 |
| `max_tool_calls` | `16` | 一次对话最多接受多少个工具调用请求；包括搜索、失败和被策略跳过的调用 |
| `run_timeout_seconds` | `180` | 一次对话的总超时；会传递到模型 HTTP 请求、等待用户和支持取消的工具 |
| `context_limit` | `30` | 上下文工具一次最多返回多少条消息 |
| `web_browser_enable` | `false` | 网页搜索和读取是否改用 chromedp 驱动 Chrome |
| `web_browser_address` | 空 | Chrome 远程调试 HTTP/WS 地址；留空时启动本机 Chrome |
| `schedule_max_seconds` | `86400` | 定时任务允许设置的最大延迟秒数 |
| `progress_after_seconds` | `15` | 超过该秒数后 @ 触发者发送固定的处理中提示，并按此间隔继续提示 |
| `progress_tips` | 三条内置文案 | Agent 耗时较久时循环发送的固定提示文案 |
| `web_search_prefer_seconds` | `3600` | 搜索 fallback 成功后优先使用该引擎的时长（秒） |

Agent 可以使用以下能力：

- 读取当前群聊上下文，可按最近分钟数、起止时间、关键词筛选并分页
- 读取指定用户在当前群的上下文，可按最近分钟数、起止时间、关键词筛选并分页
- 按一个或多个时间区间查询持久化消息，可选限定某个用户
- 读取结构化用户记忆，按来源和版本更新偏好、边界、近期状态、共同经历及关系
- 读取和维护群聊印象；旧用户印象、好感度工具兼容访问统一用户记忆
- 发送文字、引用回复，或把回复拆成 2～5 条消息依次发送
- At 指定群友
- 在需要补充信息时 @ 指定群友追问，监听其接下来 30 秒内的第一条群消息，并把回复交回同一条 Agent 决策链
- 戳一戳指定群友
- 创建一次性定时任务
- 联网搜索公开网页
- 读取公开 HTTP/HTTPS 网页
- 使用视觉模型理解 HTTP/HTTPS 图片 URL（仅在配置 `routes.vision` 后提供）

触发事件会以结构化 `EventEnvelope` 交给模型，身份、目标用户、引用消息与正文彼此分离；消息、网页、图片文字、工具结果和 Skill Markdown 都按不可信数据处理。Agent 默认只看到 `search_tools`；启用目录 Skills 时还会提供技能摘要和读取、搜索、列表工具，其他能力在搜索命中后才会暴露。搜索结果同时给出命名空间、只读性、幂等性、副作用、`group_action` 标签和风险等级，中文任务会使用本地字符语义匹配补充精确关键词匹配。

每次 Agent 运行都必须成功调用至少一个 `group_action=true` 的工具；普通文字也由模型调用 `send_message` 发送，宿主不会直发模型的普通 assistant 文本。群聊动作成功后，其结果仍会交回模型继续决策，直到模型主动停止调用工具；已经成功完成群聊动作后，后续重复的群聊动作会被拒绝，避免重复发送。模型轮数、工具调用总数和整轮超时分别受独立预算约束；模型未完成群聊动作或接口失败时，本轮返回错误而不绕过工具协议直发文本。

内置 Agent Prompt 按“角色与目标、群聊个性、上下文与信任边界、决策与证据、工具规则、完成与停止”组织。它保留接梗、吐槽、卖萌、发图和戳一戳等群聊娱乐空间，但要求严肃场景收住玩笑，并把事实、权限边界和用户目标放在表演人格之前。

时间区间查询接受 RFC3339、`YYYY-MM-DD HH:MM[:SS]` 或日期格式。区间为左闭右开；仅填写日期作为结束时间时，会自动包含该日期全天。多个区间的重复消息会自动去重，结果按时间排序。

群聊和用户上下文工具支持 `last_minutes`、`since`、`until`、`keyword`、`limit` 与 `offset`。例如“总结半小时前到现在的聊天内容”可直接用 `last_minutes: 30` 查询；结果会返回 `has_more` 和 `next_offset`，消息较多时 Agent 可以继续翻页。

网页搜索默认依次尝试 DuckDuckGo、Bing 中国版和百度，当前提供方不可用或返回结果无法解析时自动降级。fallback 成功后，该可用引擎会在配置的有效期内被优先尝试，避免每次请求重复走完整降级链。网页搜索和读取会拒绝本机、内网和链路本地地址，并限制超时、响应大小和重定向次数。

`browse_web` 需要同时提供网页 URL 和查询内容。较短网页会返回完整的结构化 Markdown；网页超过内置大小上限时，只返回与查询内容最相关的若干上下文片段。该上限由工具内部管理，无需配置。

遇到依赖 JavaScript、校验普通 HTTP 客户端或需要浏览器渲染的页面时，可以启用 chromedp：

```yaml
agent:
  web_browser_enable: true
  web_browser_address: http://127.0.0.1:9222
```

`web_browser_address` 接受 Chrome DevTools 的 HTTP 地址（如上）或 `ws://`/`wss://` 浏览器 WebSocket 地址。留空时会在插件所在机器启动本机 Chrome。启用后，`search_web` 与 `browse_web` 都通过浏览器加载页面；Chrome 发起的 HTTP(S) 请求仍会经过公网地址校验。远程调试端口能够控制浏览器，请只在受信任网络内开放。

定时任务保存在机器人进程内，重启机器人后未执行的任务不会恢复。

## 自生成 Skill 配置

```yaml
skills:
  enable: true
  auto_activate_generated: false
  candidate_min_steps: 4
  activation_evidence: 3
  global_min_groups: 2
  default_ttl_days: 30
  max_active: 20
  max_candidates: 50
  reflection_workers: 1

  # 配置 Skill 当前只支持全局作用域；启动后立即 Active
  global:
    - name: search_and_send_images
      description: 当用户请求特定角色或主题的网络图片时搜索并发送，不用于资料查询或生成式绘图
      triggers:
        - 我要看某个角色的图
        - 找张某个主题的图片
        - 发一张图片
      non_triggers:
        - 搜索相关资料
        - 生成一张图片
      markdown: |
        # 搜索并发送网络图片

        在用户明确想查看某个角色或主题的现有网络图片时，寻找内容相关、地址有效的图片并发送。

        根据请求选择合适的搜索词。检查候选结果与主题是否一致，并确认最终图片地址可以直接访问；不要把网页缩略图、失效地址或无关图片当作结果。需要执行能力时，通过 `search_tools` 加载相关搜索、网页读取和图片发送工具。

        如果没有找到能够验证的图片，应如实说明，不要为了完成发送动作而发送低相关内容。
```

| 配置 | 默认值 | 说明 |
| --- | ---: | --- |
| `enable` | `false` | 是否启用成功轨迹学习和 Active Skill 召回 |
| `auto_activate_generated` | `false` | 是否允许自动生成的候选在证据达标后自动启用；关闭时仍会生成和验证候选，便于先人工观察 |
| `candidate_min_steps` | `4` | 一次成功运行至少经过多少轮决策才值得反思；使用两个以上不同非发送工具时也可触发 |
| `activation_evidence` | `3` | 候选至少积累多少次证据后才可能启用，最小为 `2` |
| `global_min_groups` | `2` | 自动生成的全局 Skill 至少需要多少个不同群提供成功证据 |
| `default_ttl_days` | `30` | 模型未给出有效期限时使用的默认天数；服务端始终限制在 `1`～`90` 天 |
| `max_active` | `20` | 全局最多同时启用多少个自动生成 Skill；配置 Skill 不占用该额度 |
| `max_candidates` | `50` | 全局最多保留多少个自动生成 Candidate/Shadow Skill |
| `reflection_workers` | `1` | 后台反思 Worker 数量；反思不会阻塞群聊回复 |
| `global` | 空 | 管理员声明的全局 Skill 列表，启动后立即启用 |

自动生成的 Skill 使用 `global/generated` 作用域。不同群产生相似描述和相同工具组合时，会合并到同一个记录并累计各群证据。默认只生成和影子验证候选；启用 `auto_activate_generated` 后，候选达到 `activation_evidence`、`global_min_groups` 且成功率不低于 80% 才转为 `active`。关闭该开关不会自动停用数据库中已经 Active 的 Skill。升级后的第一次启动会把旧版群级记录迁移为全局记录并合并重复项。

`skills.global` 声明的是 `global/config` Skill。`name` 和 `description` 用于标识与召回，`triggers`、`non_triggers` 保留显式的正反触发条件，`markdown` 是命中后完整交给 Agent 的 Markdown 行为说明。它们以配置为准并立即 Active；内容变化时增加版本，配置中删除后自动停用。配置 Skill 与自动生成 Skill 重复时，保留配置内容并把已有证据合并到配置记录。配置 Skill 不设置有效期，也不会因为运行失败自动停用，但会记录使用结果。旧配置继续兼容；新技能推荐使用下面的目录格式。当前不支持群级目录 Skill。目录中的 Python 脚本可由 Agent 显式调用 run_skill_script，通过 pyrunner SDK 执行。

数据库中的 Active Skill 被 `search_tools` 命中时仍返回完整 Markdown；目录 Skill 只返回路由摘要和入口路径。配置 Skill 可以只是行为规范，不要求绑定工具；需要某种执行能力时，Markdown 应指导 Agent 通过 `search_tools` 加载。自动生成 Skill 仍会在内部记录成功轨迹实际使用过的工具，用于候选验证、合并和可选的自动激活，但该工具索引不是 Skill 内容，也不需要管理员配置。Skill 的技术成功要求群聊动作已执行、用户可见回复确实送达且整轮无终止错误；某个可选只读工具失败后若仍通过群聊动作正确完成回复，不会误判整轮失败。

旧版 `instructions`、`required_tools`、`success_checks` 配置仍可读取，并会在启动时转换为 Markdown；新配置应只使用 `markdown`。

自动生成 Skill 使用成功会续期并提高置信度；连续两次失败会转为 `stale`，随后通过相似任务重新验证，验证继续失败时退休。过期 Candidate 会退休，过期 Active Skill 会进入 `stale` 并等待相似任务重新验证。反思模型生成的是一段完整 Markdown 经验说明，而不是固定的步骤、工具和检查项数组。数据库只持久化 Skill Markdown、触发条件、学习状态、不可逆任务指纹和内部工具索引，不保存原始聊天、工具参数或工具结果。

### 目录式 Skill 与渐进读取

启用 `skills.enable` 后，插件通过 `env.FilePath()` 获取数据目录，用 `filepath.Join` 创建其下的 `skills` 文件夹。所有群共享该目录；不需要在 YAML 中逐个注册。管理员可以直接放置文件，Agent 也可以通过工具创建：

```text
<插件数据目录>/skills/
  chat-summary/
    SKILL.md
    references/
      examples.md
```

`SKILL.md` 示例（文件夹名与 `name` 必须一致）：

```markdown
---
name: chat-summary
description: 当用户需要总结最近群聊、整理讨论结论时使用。
triggers: [总结群聊, 整理讨论结论]
non_triggers: [总结外部文章]
---
# 群聊总结
先确定时间范围，通过 search_tools 查找读取聊天记录的工具。
按主题整理结论，区分共识、分歧和待办。
需要输出示例时，再读取 references/examples.md。
```

`name`、`description` 必填，`triggers`、`non_triggers` 可选。名称以小写字母开头，可包含小写字母、数字、`-`、`_`，最多 64 字符。路由索引只解析 YAML 头部（最多 16 KiB），不会把正文和参考文件预先放入模型上下文。格式错误的目录会记录警告并跳过。

渐进式加载过程：

1. Agent 首轮可见目录 Skill 的名称、适用场景、入口路径和正反触发条件（总预算 8000 字符，超出会明确提示），同时可直接调用 `read_skill`、`search_skills`、`list_skills`。只读取元数据，不预加载正文或执行脚本；每次运行刷新目录。
2. Agent 根据用户明确点名与适用场景，选择覆盖当前任务所需的最小技能集合，可以组合多个，也可以不用。分别调用 `read_skill(name="chat-summary")` 读取选中技能的 `SKILL.md`。
3. 仅在需要时调用 `read_skill(name="chat-summary", file="references/examples.md")` 读取辅助文件，`file` 相对于该技能文件夹。

`search_skills(query, limit)` 独立检索目录技能摘要；`search_tools` 仍可同时召回目录技能和数据库经验。默认返回 5 个 Skill 候选，最多 20 个，不再限制为一个；混合来源按匹配分排序并共享数量预算。明确的完整技能名（含 `$name`）优先于模糊描述匹配，负触发条件仍生效。搜索只使用本次查询，不拼接整段历史消息。未命中或首轮摘要被截断时，Agent 可以用 `list_skills` 浏览完整目录，根据描述选择后直接读取。

此发现方式参考 [Codex 的渐进式技能加载](https://learn.chatgpt.com/docs/customization/overview)：先展示元数据，再按需读取正文、参考文件与脚本。建议 description 写清楚“何时使用 / 何时不使用”，便于模型判断，而不是只堆关键词。

新增工具均可通过 `search_tools` 发现：

| 工具 | 功能 |
| --- | --- |
| `list_skills` | 列出目录技能的元数据，不读取正文 |
| `search_skills` | 按目标或完整名称搜索多个目录技能摘要，默认 5 个，最多 20 个 |
| `read_skill` | 读取指定技能内的一个 UTF-8 文本文件，默认 `SKILL.md` |
| `save_skill` | 保存一个技能文件，参数为 `name`、`content`，可选 `file` 和 `overwrite` |
| `list_skill_files` | 递归列出指定 `name` 下的文件相对路径和大小，不读取正文 |
| `search_skill_files` | 按路径关键词 `query` 搜索文件或文件夹；可选 `directory`（skills 相对目录）、`type`（file/directory）、`limit`（默认50，最多200）；返回 skills 相对 `path`、`absolute_path`、类型、大小及 `truncated` |
| `run_skill_script` | 通过 pyrunner 运行 `name` 内的 `.py` 入口 `file`，可选 `args` 和 `timeout_seconds` |

资源搜索与技能路由元数据独立。例如 `search_skill_files(query="开心", directory="meme/assets", type="file")` 返回的 `path` 为 `meme/assets/开心.png`，相对于整个 skills 目录；`absolute_path` 为本机绝对路径。两者均可直接传给 `send_image(url=...)`；相对路径会在发送前校验并转换为绝对路径，拒绝不存在的文件、目录和越界路径。`list_skill_files` 的路径仍相对于指定 Skill，传给 `send_image` 时需加上技能文件夹名前缀。

Agent 先用 `save_skill` 创建完整的 `SKILL.md`，再保存参考文件。保存后下次搜索立即可见，无需重启。默认拒绝覆盖已有文件；更新时先读取旧内容，再显式传入 `overwrite=true`。文件读写限定在对应技能目录内，拒绝路径穿越及越界符号链接，每个文本文件最多 5 MiB。保存和读取不会触发执行；脚本须通过 run_skill_script 显式运行。

目录 Skill 保存后立即可用，不参与数据库经验的候选验证、TTL 或激活额度。原有经验总结、影子验证和自动学习机制继续保留；`skills.global` 也仍然有效。关闭 `skills.enable` 会同时关闭目录技能工具和经验学习。插件 `OnBoot` 输出已注册 tools，以及目录 skills 的摘要、数据库 skills 的来源与状态。

## 复读配置

```yaml
repeat:
  enable: true
  trigger_count: 3
```

- `enable`：是否允许机器人参与复读，默认为 `false`
- `trigger_count`：连续出现多少条相同消息后触发，最小值为 `2`，未填写或小于 `2` 时使用 `3`

机器人在一轮连续相同消息中只复读一次。文字、图片、At 和引用回复可以触发复读；戳一戳、语音、转发和 JSON 分享不会触发。
复读使用每个群对应 Persona 的内存滑动窗口，并通过 `ContentEqual` 比较连续消息；机器人重启后窗口会重新开始积累。

## 结构化用户记忆与关系

```yaml
impression:
  enable: true
  observe_group_chat: false # 默认：仅从与 Agent 的对话提取；true 恢复全群消息维护
  maintenance_seconds: 60
  batch_size: 40

# 可选；省略则使用 routes.agent，仍为独立提示词和独立调用
routes:
  impression:
    provider_name: deepseek
    model_name: deepseek-chat
```

- `enable`：启用长期记忆维护和 Agent 记忆写入工具，默认关闭。读取和自动带入已有记忆不受此开关影响。
- `observe_group_chat`：默认 `false`，只从用户与 Agent 的实际对话提取长期记忆。设为 `true` 才恢复旧的全群消息自动维护。已有配置未填写此项时也采用新的对话模式。
- `maintenance_seconds`：后台检查间隔，默认 60 秒，最低 10 秒。默认模式只有会话过期且存在待处理对话才调用模型；单个 worker 顺序处理，模型慢时可能延迟。
- `batch_size`：每个过期会话每轮最多处理的消息数；全群模式则为每群每轮的消息数。默认 40，上限 100；每次最多涉及 10 位用户，剩余消息留待下一批。

默认对话模式按「群 ID + 发起用户 ID」暂存触发消息、Agent 成功发送的回复、追问及收到的回答。普通群聊（包括同一用户在期间的闲聊）、进度提示、工具结果、内部推理及定时任务不会进入提取输入。机器人回复仅提供上下文，用户记忆仍必须以本人消息为依据。

每轮对话结束后续期，空闲窗口使用 `agent.conversation_memory.window_seconds`（默认 600 秒）。窗口到期后由后台主动提取，不需要用户再发消息；正在运行或等待回答的会话暂不提取。短期摘要开关 `agent.conversation_memory.enable` 仅控制回复时携带摘要及压缩，关闭时仍可暂存对话并在到期后提取长期记忆，不额外调用短期压缩模型。长会话超过 `batch_size` 时分批处理，并非保证整个会话只有一次提取请求。

暂存来源保存在 `pending_memory_messages`，重启后可继续处理已完成轮次；提取成功后删除已处理的暂存项，失败保留以便重试。提取使用原始对话消息来源，而非有损的短期摘要，以保留来源校验。没有值得保存的信息时模型可返回空更新。明确要求记住、更正、遗忘仍可由 Agent 即时调用工具处理，无须等待过期。

群友印象与好感度合并存储在 `user_memories` 中。结构化画像包括：

| 字段 | 用途 |
| --- | --- |
| `summary` | 核心认识，最多 600 字；迁移时保留旧印象 |
| `items[].kind` | `fact` 事实、`preference` 偏好、`boundary` 边界、`state` 近期状态、`experience` 共同经历 |
| `items[].key/text` | 稳定键与内容；同键更新，最多 30 条，每条最多 200 字 |
| `items[].source_id/group_id/observed_at` | 来源消息记录、来源群和消息时间 |
| `items[].confidence` | 明确陈述 `explicit` 或谨慎推断 `inferred` |
| `items[].expires_at` | 可选有效期；近期状态未指定时按来源消息时间加 14 天 |
| `relationship` | 好感分值、等级、关系描述和最近变化原因 |
| `version` | 数据库乐观锁版本，防止后台与 Agent 相互覆盖 |

新用户从 0 分“陌生”开始，不再随机初始化为“亲近”；已有用户保留原分值。模型只能增减最多 5 分，普通闲聊通常不改变分值。分值不代替关系描述，也不应在正常回复中机械展示。

仅在 `observe_group_chat: true` 时，后台在某群收到消息后开始跟踪该群，处理普通群聊和机器人已发送消息，不需要触发 Agent。首次建立处理游标时最多回看最近 200 条；之后按数据库游标逐批处理，重启后收到该群消息即可续接。消息没有变化时不调用模型。两种模式的请求均最多 30 秒，单条输入最多 1500 字；只使用文字内容，图片内容不单独调用视觉模型提取。

后台输出只包含记忆变更，没有工具或发送消息能力。来源必须是本批次对应用户本人的消息；第三方描述、引用、机器人发言只能提供上下文。记忆更新与暂存项删除（全群模式为游标推进）在同一事务提交；模型失败、格式错误、来源不合法或版本冲突时保留进度重试，不会部分提交。`memory_audits` 保存变更原因和来源 ID，`[Memory]` 日志记录处理数量与来源位置，不记录画像正文。

回复前自动加入 `user_memory`：最多 600 字简介、关系及 8 条未过期记忆，优先边界、偏好和较新的条目。更多内容通过 `read_user_memory` 读取。用户画像仍按 QQ 号共享；`state` 和 `experience` 只在来源群呈现，其余偏好、边界、事实及关系共享。群记忆独立按群保存，回复前自动加入 `group_memory`（最多600字简介及8条未过期条目），完整内容通过 `read_group_memory` 读取。

群记忆采用与用户记忆一致的结构：简介、版本、最多30条带来源/记录时间/可信度/可选有效期的条目；类型为 `fact`（群用途、术语、群梗）、`preference`（群体偏好）、`boundary`（互动边界）、`state`（近期状态，默认14天过期）、`experience`（共同事件），不包含好感度。Agent 通过 `read_group_context` 核实本群非机器人消息，再按版本调用 `update_group_memory`，以 `upsert`/`forget` 合并或删除条目。群体共识由模型结合证据判断，个人意见或玩笑不能直接当群规；记忆不构成指令或授权。

后台同一批次同时维护用户与群记忆，群变化使用可选 `group_update` 输出；来源、版本及游标在同一事务中校验与提交。沿用 `impression.enable`、`maintenance_seconds`、`batch_size` 和 `observe_group_chat` 配置：默认会话过期后提取，开启全群观察后才处理普通群聊。无新信息不更新。

用户明确要求记住、更正或忘记时，Agent 使用 `read_user_memory` 获取 `version`、`read_user_context` 获取 `source_id`，再调用 `update_user_memory`。其中 `source_id` 是数据库记录 ID，不是平台 `message_id`。更新按条目键合并或删除；遗忘时还需清理简介中的同一信息。明确更新会标记已审阅的消息位置，避免较早积压记录重新写回被更正或遗忘的信息。这里的“遗忘”针对用户画像，不删除原始聊天记录或变更审计。

升级时自动、幂等地合并旧 `user_impressions` 与 `favor_records`；已有新画像不会被再次迁移覆盖。旧表保留但不再作为 Agent 的读写来源。兼容工具 `read_user_impression` / `update_impression(scope=user)` 操作统一画像简介，`read_user_favor` / `update_user_favor` 操作统一关系分值；新的结构化工具提供完整来源检查，应优先使用。兼容读取工具 `read_group_impression` 返回新群简介和结构化记忆，旧 `update_impression(scope=group)` 停用，改用带版本和来源检查的 `update_group_memory`。关闭 `impression.enable` 会同时关闭后台及这些写入工具。

## 日志说明

每次 Agent 对话会出现带有以下标记的日志：

```text
[Agent][run=7][开始] group=123 user=456 max_steps=8 max_tool_calls=16 ... prompt=<redacted chars=320>
[Agent][run=7][请求模型] step=1/8 history=0 active_tools=[] exposed_tools=[search_tools] question=<redacted chars=320>
[Agent][run=7][模型决策] step=1/8 tool_calls=[search_tools] answer=""
[Agent][run=7][模型推理] step=1/8 reasoning="需要先搜索近期活动信息"
[Agent][run=7][工具搜索] step=1/8 query=<redacted chars=4> hits=[search_web] ...
[Agent][run=7][调用工具] step=2/8 call_id=... tool=search_web args=<redacted chars=24>
[Agent][run=7][工具结果] step=2/8 tool=search_web ok=true action_done=false
[Agent][run=7][完成] step=5/8 action_done=true final_answer=<redacted chars=0>
```

每次执行都有独立的 `run` 编号，可用它串起并发场景下的完整决策链。日志显示当前历史条数、已加载和暴露的工具、工具搜索命中、调用状态以及群聊动作是否完成；模型的 `answer` 和接口明确返回的 `reasoning_content` 会完整打印，便于排查决策过程。提示词、工具查询和参数只记录字符数，工具结果正文不写日志；供应商请求与响应日志也只保留模型名、消息/工具数量、HTTP 状态和字节数。模型日志可能包含用户信息，请限制日志访问权限与保存周期。

### Agent 管理后台

网页随 Go 插件一起编译，无需安装 Node.js 或单独部署前端。在 chatai 配置的顶层增加：

```yaml
monitor:
  enable: true
  listen: "127.0.0.1:9086"
  access_token: "替换为随机长口令"
  max_runs: 200
```

重启宿主后访问 `http://127.0.0.1:9086`，输入配置的口令即可查看。默认关闭；启用时口令必填，端口占用会让初始化明确报错。需要让其他机器访问时，可将 `listen` 设为 `0.0.0.0:9086`，或使用 HTTPS 反向代理转发到本机端口。公网访问请使用 HTTPS，并关闭代理的 SSE 缓冲；代理需保留 `/api/` 路径，建议使用独立域名的根路径。

- 对话列表展示触发语句、群号、触发者昵称与用户 ID、实际模型路由、开始时间、总耗时、输入/输出 token 汇总、运行中/成功/错误状态；支持文本搜索和状态筛选。
- 详情按 Agent Loop 实际顺序展示用户触发、每轮模型请求与返回、Skill 加载、工具调用与结果以及结束状态。参数、结果、错误和模型返回的 `reasoning_content` 可以分别展开。模型未返回推理内容时明确标注，不生成或猜测思维链。
- 使用 SSE 推送更新通知，断线自动重连，并每 10 秒补充刷新。模型返回后更新该次调用的 token、耗时与推理内容，不是逐 token 流式展示。
- 覆盖通过 Persona 执行的文本、视觉及定时 Agent 运行；独立 SDK 请求、短期记忆压缩、Skill 后台反思及入群欢迎的独立模型调用不计入这份 Agent 时间线。
- 最近运行保存在内存中，默认 200 条、最多 2000 条，重启清空。达到上限时优先淘汰最早结束的记录；全部槽位都在运行时，新运行不会被收录。每次已收录运行完整保留流程事件和时间线，不因事件数量或累计大小丢弃后续事件。文本字段（包括工具参数、结果和模型返回内容）超过 8 KiB 时截断并追加标记；截断仅影响监控展示，不影响实际执行，token 总计和最终状态正常更新。
- 后台分为运行日志、Skills、Tools 和记忆中心。选择群聊后可查看实际注册的工具及 JSON Schema、目录技能的 SKILL.md、当前可检索的已激活经验技能。未激活、退役或已过期的经验技能不展示。
- 记忆中心展示有效的短期对话摘要、统一用户长期记忆（简介、事实、偏好、边界、近期状态、共同经历与关系）和结构化群记忆；用户条目每页 50 人。旧用户印象、旧好感度表仅用于迁移，不单独展示。短期记忆遵循当前有效窗口，长期记忆过滤过期条目，状态与共同经历按当前群隔离。
- 记忆中心支持“开颅手术”：编辑群简介，新增、修改或删除群记忆条目、清空当前群记忆，编辑用户简介与关系，逐条修改或删除结构化长期记忆，以及确认后清空某用户全部长期记忆。可为已有用户新增结构化条目，也可为当前群建立记忆；人工新增条目的 `source_id=0`，不伪造聊天来源。条目键、类型、来源和所属群不可修改；其他群不可见的条目不会被逐条编辑覆盖。简介与关系跨群共享；“清空长期记忆”影响该用户所有群的记忆。删除条目不会自动重写简介，请同步检查重复内容。
- 用户记忆修改采用版本校验，旧页面保存会报错并要求刷新；清空保留内部版本记录，防止旧数据迁移和积压消息恢复已删除内容，新消息仍可形成记忆。群记忆同样采用版本校验及清空保留记录，所有条目严格属于当前群；旧群印象修改接口停用，需刷新页面使用新接口。写接口为 `PATCH /api/inspect/memory?group=<群号>` 和 `DELETE /api/inspect/memory?group=<群号>`，要求登录 Cookie 与 `application/json`，不支持 POST 添加。
- 后台采用半透明玻璃材质、柔和高光、按钮与弹窗动效，支持移动端、键盘操作和系统“减少动态效果”。CSS 方案参考 [dpawlikowski/liquid-glass](https://github.com/dpawlikowski/liquid-glass)，没有引入第三方运行库。运行列表、详情、群选择区和记忆卡片的头像由前端使用 QQ 头像地址直接加载；失败时显示占位。
- Agent Loop 不记录模型请求原文或鉴权请求头；时间线保留模型请求开始与返回事件，工具参数默认折叠，长文本截断展示。记忆和技能页面可手动刷新，运行日志仍实时更新。
- 监控详情包含群聊内容、工具原始参数/结果和模型返回内容，仅向可信运维人员提供口令。口令不放入 URL 或浏览器本地存储，登录后使用 HttpOnly、SameSite Cookie；网页把所有运行内容作为纯文本呈现。

只读接口：`GET /api/runs`（不含事件正文）、`GET /api/runs/{id}`（含时间线）、`GET /api/events`（SSE 刷新通知）。先以 `Authorization: Bearer <access_token>` 调用 `POST /api/session` 获取会话 Cookie；`DELETE /api/session` 退出。SDK 可在 `agent.Runner.Observer` 接入其他事件消费者；观察器应快速返回。

内置工具集中定义在 `chatai/persona/builtin_tools.go`。注册函数只维护工具元数据和处理器映射，每个工具使用独立的 `handle...` 函数，便于单独维护。

错误信息和部分业务日志仍可能包含外部服务或数据库返回的细节，请注意日志文件的访问权限和保存周期。

## 从旧配置升级

- `threshold`、`speak_groups` 和 `online` 已不再使用，可以删除
- 原来的自动插话已经移除
- 复读改为 `repeat.enable` 和 `repeat.trigger_count`
- `impression.enable` 同时启用长期记忆维护和 Agent 写入工具；默认只在 Agent 会话过期后提取，需显式设置 `observe_group_chat: true` 才恢复全群消息自动维护。`interval_minutes`、`min_messages` 仍被忽略，改用 `maintenance_seconds`、`batch_size`。`routes.impression` 用于独立后台维护模型。
- 旧配置中的 `providers[].model_name` 仍可作为供应商名称读取，但建议改成 `providers[].provider_name`
- 原有用户印象与好感度自动合并到新的统一用户记忆；旧群印象幂等迁移至 `group_memories` 简介，完整旧文保留，已有新群记忆（含清空记录）不会被覆盖；旧群印象表仅供迁移，token 用量继续保留。新的记忆处理游标与旧版印象游标独立。

## 常见问题

### 机器人不解析图片

确认 `routes.vision.provider_name` 和 `routes.vision.model_name` 均已配置，并且该模型支持图片输入。只配置默认模型不会自动开启图片解析。

### 机器人不参与复读

确认 `repeat.enable` 为 `true`，并检查连续相同消息数量是否达到 `trigger_count`。不支持的消息类型不会触发复读。

### 日志没有模型思考内容

先确认对应路由启用了 `thinking`，并确认模型接口会返回 `reasoning_content`。部分模型即使支持思考，也不会通过 API 返回该字段。

### 网页读取失败

网页必须是公开的 HTTP/HTTPS 地址。内网地址、本机地址、响应过大的页面、超时页面或重定向到内网的页面会被拒绝。启用浏览器模式时，还需要确认本机已安装 Chrome/Chromium，或 `web_browser_address` 指向可访问的 Chrome DevTools 服务。

### Skill Python 脚本执行

在 `plugins.yaml` 同时启用 `pyrunner`，并开启 chatai 的 `skills.enable`：

```yaml
pyrunner:
  repo: github.com/kohmebot/pyrunner
chatai:
  repo: github.com/kohmebot/chatai
  conf:
    skills:
      enable: true
```

SDK 在工具调用时查找 pyrunner，避免插件初始化顺序影响其他技能工具。未启用 pyrunner 时，执行工具返回明确错误，技能读写仍然可用。

示例目录：

```text
skills/report/
  SKILL.md
  requirements.txt
  scripts/
    main.py
    helper.py
  assets/
    input.json
```

`SKILL.md` 使用 `name: report` 的 front matter，并说明脚本入口、参数和输出。Agent 先读取说明及相关脚本，再调用：

```json
{"name":"report","file":"scripts/main.py","args":["--format","json"],"timeout_seconds":60}
```

调用会创建整个 Skill 的临时快照，再通过 `pyrunnersdk.Invoker.Run` 以 `Path` 和 `EntryPoint` 运行。工作目录是项目根目录，保留辅助模块及资源的相对路径；脚本可按标准 Python 规则导入模块。项目根目录的 `requirements.txt`、入口 PEP 723 依赖或 `pyproject.toml` 交由 pyrunner 安装。无需将源代码拼入 shell，也不暴露任意宿主路径参数。

快照拒绝路径穿越、符号链接和特殊文件，最多 1000 个文件、合计 10 MiB。执行默认超时 60 秒，可设置 1–180 秒，并受 Agent 本轮剩余时间限制；依赖安装也计入超时。沙箱后端、网络、资源和输出限制由 pyrunner 配置决定，Python 依赖安装阶段可能联网。依赖与脚本应来自可信来源。

返回 `success`、`stdout`、`stderr`、`output`、`exit_code`、`timed_out`、`output_truncated`、`duration_ms`、`environment`、`sandbox_backend` 和 `dependencies_installed`。失败时仍保留 SDK 提供的诊断信息。运行结束清理临时副本，不改写原 Skill，也不持久化生成文件；需交给 Agent 的结果应输出至 stdout。
