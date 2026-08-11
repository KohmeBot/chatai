# chatai

`chatai` 是 kohme 的群聊 AI 插件。机器人被 At、回复或戳一戳时，会根据当前问题自主决定是否读取群聊记录、用户记录和印象，也可以主动维护长期印象、发送消息、At 群友、戳一戳、读取网页或创建定时任务。

## 功能

- Agent 多轮工具调用，通过 `search_tools` 按需加载少量相关工具，不把全部工具或聊天记录直接发送给模型
- 全局 Skill：既可从不同群的成功任务中自动合并生成，也可直接通过配置声明
- 群聊上下文持久化到插件数据库，重启后仍可读取
- Agent、图片解析、入群欢迎可以使用不同模型
- 可选图片解析；没有配置图片模型时不会解析图片
- 回复含图片的历史消息时，也会把被引用图片交给图片模型解析
- Agent 在对话中按需维护群友印象和群聊印象
- 可开关的群聊复读，并可设置连续多少条相同消息后触发
- 日志显示 Agent 调用轮次、模型返回的思考内容、工具参数与工具结果
- 支持通义千问和 DeepSeek

机器人不会根据群聊活跃度主动插话。普通消息只用于积累短期上下文，明确触发机器人后才会调用 Agent。复读功能不受这一限制，但必须由配置单独开启。

## 配置示例

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
      max_steps: 8
      context_limit: 30
      schedule_max_seconds: 86400
      progress_after_seconds: 15

    skills:
      enable: true
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

### 多路由

| 路由 | 用途 | 不配置时 |
| --- | --- | --- |
| `routes.agent` | 对话决策和工具调用 | 使用默认模型 |
| `routes.vision` | 带图事件的完整 Agent 决策与工具调用 | 带图事件仍由文本 Agent 处理，但无法读取图片 |
| `routes.skill` | 成功任务的后台反思和 Skill 候选生成 | 使用 `routes.agent` |
| `routes.join` | 生成入群欢迎语 | 使用默认模型 |

图片模型需要支持 OpenAI 兼容的 `image_url` 消息格式和工具调用。带图提问会把完整触发事件与原图直接交给该模型，并由它完成整轮 Agent 决策，不再先生成图片描述交给另一个模型。普通文本模型不要配置到 `routes.vision`，否则请求可能失败。

配置图片模型后，Agent 还会获得 `analyze_image` 工具。它接收必填的 `url` 和可选的 `prompt`，可按需描述图片、识别文字、分析截图，或回答针对图片的问题；图片 URL 会直接作为 `image_url` 交给 `routes.vision` 模型。

每个路由都可以单独设置：

- `provider_name`：供应商
- `model_name`：模型名称
- `thinking`：是否启用模型思考模式
- `max_tokens`：该功能的最大输出长度

## Agent 配置

| 配置 | 默认值 | 说明 |
| --- | ---: | --- |
| `max_steps` | `8` | 一次对话最多执行多少轮模型决策和工具调用 |
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
- 读取用户印象和群聊印象，并在出现长期有效的新信息时主动融合更新
- 读取和增量修改用户好感度
- 发送文字、引用回复，或把回复拆成 2～5 条消息依次发送
- At 指定群友
- 戳一戳指定群友
- 创建一次性定时任务
- 联网搜索公开网页
- 读取公开 HTTP/HTTPS 网页
- 使用视觉模型理解 HTTP/HTTPS 图片 URL（仅在配置 `routes.vision` 后提供）

Agent 每轮起初只看到 `search_tools`，搜索命中的少量工具才会在后续轮次暴露。执行发送消息、At 或戳一戳等群聊动作后，Agent 仍会把动作结果交给模型继续决策，直到模型主动停止调用工具；整个决策链必须至少成功执行一次群聊动作。最后一个决策步在尚未执行群聊动作时只提供群聊动作工具，模型必须基于已有信息立即回复，不能继续搜索；如果最后只生成了文本而没有调用动作工具，该文本会被直接发送。模型接口失败或没有产生任何可发送内容时才会发送兜底消息。

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
      instructions:
        - 使用 search_web 搜索目标图片
        - 使用 browse_web 打开结果页并提取有效图片地址
        - 使用 send_image 发送相关图片
      required_tools:
        - search_web
        - browse_web
        - send_image
      success_checks:
        - 至少找到并成功发送一张相关图片
```

| 配置 | 默认值 | 说明 |
| --- | ---: | --- |
| `enable` | `false` | 是否启用成功轨迹学习和 Active Skill 召回 |
| `candidate_min_steps` | `4` | 一次成功运行至少经过多少轮决策才值得反思；使用两个以上不同非发送工具时也可触发 |
| `activation_evidence` | `3` | 候选至少积累多少次证据后才可能启用，最小为 `2` |
| `global_min_groups` | `2` | 自动生成的全局 Skill 至少需要多少个不同群提供成功证据 |
| `default_ttl_days` | `30` | 模型未给出有效期限时使用的默认天数；服务端始终限制在 `1`～`90` 天 |
| `max_active` | `20` | 全局最多同时启用多少个自动生成 Skill；配置 Skill 不占用该额度 |
| `max_candidates` | `50` | 全局最多保留多少个自动生成 Candidate/Shadow Skill |
| `reflection_workers` | `1` | 后台反思 Worker 数量；反思不会阻塞群聊回复 |
| `global` | 空 | 管理员声明的全局 Skill 列表，启动后立即启用 |

自动生成的 Skill 使用 `global/generated` 作用域。不同群产生相似描述和相同工具组合时，会合并到同一个记录并累计各群证据；达到 `activation_evidence`、`global_min_groups` 且成功率不低于 80% 后才转为 `active`。升级后的第一次启动会把旧版群级记录迁移为全局记录并合并重复项。

`skills.global` 声明的是 `global/config` Skill。它们以配置为准并立即 Active；内容变化时增加版本，配置中删除后自动停用。配置 Skill 与自动生成 Skill 重复时，保留配置内容并把已有证据合并到配置记录。配置 Skill 不设置有效期，也不会因为运行失败自动停用，但会记录使用结果。当前不支持配置群级 Skill、外部 Skill 文件或 Skill 脚本。

Active Skill 被 `search_tools` 命中时会返回流程，并自动暴露 `required_tools` 中的已注册工具；只要其中任一工具未注册，该 Skill 就不会加载。

自动生成 Skill 使用成功会续期并提高置信度；连续两次失败会转为 `stale`，随后通过相似任务重新验证，验证继续失败时退休。过期 Candidate 会退休，过期 Active Skill 会进入 `stale` 并等待相似任务重新验证。数据库只持久化 Skill、不可逆任务指纹和工具名，不保存原始聊天、工具参数或工具结果。

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

## 印象配置

```yaml
impression:
  enable: true
```

- `enable`：是否向 Agent 提供 `update_impression` 工具；读取印象不受此开关影响

启用后，Agent 会在对话中发现稳定偏好、性格特征、边界、重要经历、群氛围或长期规则时，先读取旧印象，再调用 `update_impression` 写入融合后的完整内容。工具使用 `scope: group` 更新当前群印象，使用 `scope: user` 和 `user_id` 更新群友印象，并要求把刚读取的内容通过 `previous_content` 原样带回；旧值已变化时会拒绝覆盖并要求重新读取。一次性事件、闲聊和重复信息不会写入。长期印象、群聊上下文和 token 用量均保存在插件数据库中；复读窗口仅保存在内存中。

## 日志说明

每次 Agent 对话会出现带有以下标记的日志：

```text
[Agent][run=7][开始] group=123 user=456 max_steps=8 ... prompt=...
[Agent][run=7][请求模型] step=1/8 history=0 active_tools=[] exposed_tools=[search_tools] ...
[Agent][run=7][模型决策] step=1/8 tool_calls=1 answer=...
[Agent][run=7][模型推理] step=1/8 reasoning=...
[Agent][run=7][工具搜索] step=1/8 query="联网搜索" hits=[search_web] ...
[Agent][run=7][调用工具] step=2/8 call_id=... tool=search_web args=...
[Agent][run=7][工具结果] step=2/8 tool=search_web ok=true action_done=false payload=...
[Agent][run=7][完成] step=4/8 action_done=true final_answer=...
```

每次执行都有独立的 `run` 编号，可用它串起并发场景下的完整决策链。日志会显示当前历史条数、已加载和暴露的工具、工具搜索命中、调用参数、结果以及群聊动作是否完成。只有模型接口实际返回 `reasoning_content` 时才会打印推理内容；工具结果过长时会在 6000 个字符处截断。

内置工具集中定义在 `chatai/persona/builtin_tools.go`。注册函数只维护工具元数据和处理器映射，每个工具使用独立的 `handle...` 函数，便于单独维护。

工具日志可能包含群聊上下文、用户印象或网页内容，请注意日志文件的访问权限和保存周期。

## 从旧配置升级

- `threshold`、`speak_groups` 和 `online` 已不再使用，可以删除
- 原来的自动插话已经移除
- 复读改为 `repeat.enable` 和 `repeat.trigger_count`
- 印象不再周期生成；保留 `impression.enable` 作为 Agent 写入工具开关，删除 `interval_minutes`、`min_messages` 和 `routes.impression` 即可
- 旧配置中的 `providers[].model_name` 仍可作为供应商名称读取，但建议改成 `providers[].provider_name`
- 原有用户印象、群聊印象和 token 用量数据库表可以继续使用；旧印象处理游标会保留在数据库中但不再读取

## 常见问题

### 机器人不解析图片

确认 `routes.vision.provider_name` 和 `routes.vision.model_name` 均已配置，并且该模型支持图片输入。只配置默认模型不会自动开启图片解析。

### 机器人不参与复读

确认 `repeat.enable` 为 `true`，并检查连续相同消息数量是否达到 `trigger_count`。不支持的消息类型不会触发复读。

### 日志没有模型思考内容

先确认对应路由启用了 `thinking`，并确认模型接口会返回 `reasoning_content`。部分模型即使支持思考，也不会通过 API 返回该字段。

### 网页读取失败

网页必须是公开的 HTTP/HTTPS 地址。内网地址、本机地址、响应过大的页面、超时页面或重定向到内网的页面会被拒绝。启用浏览器模式时，还需要确认本机已安装 Chrome/Chromium，或 `web_browser_address` 指向可访问的 Chrome DevTools 服务。
