# chatai

`chatai` 是 kohme 的群聊 AI 插件。机器人被 At、回复或戳一戳时，会根据当前问题自主决定是否读取群聊记录、用户记录和印象，也可以发送消息、At 群友、戳一戳、读取网页或创建定时任务。

## 功能

- Agent 多轮工具调用，通过 `search_tools` 按需加载少量相关工具，不把全部工具或聊天记录直接发送给模型
- 群聊上下文持久化到插件数据库，重启后仍可读取
- Agent、图片解析、印象总结、入群欢迎可以使用不同模型
- 可选图片解析；没有配置图片模型时不会解析图片
- 回复含图片的历史消息时，也会把被引用图片交给图片模型解析
- 周期生成用户印象和群聊印象
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
      impression:
        provider_name: tongyi
        model_name: qwen-turbo
        max_tokens: 1200
      join:
        provider_name: tongyi
        model_name: qwen-turbo

    agent:
      max_steps: 8
      context_limit: 30
      web_max_bytes: 524288
      schedule_max_seconds: 86400
      progress_after_seconds: 15

    repeat:
      enable: true
      trigger_count: 3

    impression:
      enable: true
      interval_minutes: 60
      min_messages: 20

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
| `routes.vision` | 图片内容解析 | 不解析图片 |
| `routes.impression` | 周期生成印象 | 使用默认模型 |
| `routes.join` | 生成入群欢迎语 | 使用默认模型 |

图片模型需要支持 OpenAI 兼容的 `image_url` 消息格式。普通文本模型不要配置到 `routes.vision`，否则图片解析请求可能失败。

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
| `web_max_bytes` | `524288` | 网页工具最多读取多少字节 |
| `schedule_max_seconds` | `86400` | 定时任务允许设置的最大延迟秒数 |
| `progress_after_seconds` | `15` | 超过该秒数后 @ 触发者发送固定的处理中提示，并按此间隔继续提示 |
| `progress_tips` | 三条内置文案 | Agent 耗时较久时循环发送的固定提示文案 |
| `web_search_prefer_seconds` | `3600` | 搜索 fallback 成功后优先使用该引擎的时长（秒） |

Agent 可以使用以下能力：

- 读取当前群聊上下文
- 读取指定用户在当前群的上下文
- 按一个或多个时间区间查询持久化消息，可选限定某个用户
- 读取用户印象和群聊印象
- 读取和增量修改用户好感度
- 发送文字、引用回复，或把回复拆成 2～5 条消息依次发送
- At 指定群友
- 戳一戳指定群友
- 创建一次性定时任务
- 联网搜索公开网页
- 读取公开 HTTP/HTTPS 网页

Agent 每轮起初只看到 `search_tools`，搜索命中的少量工具才会在后续轮次暴露。最后一个决策步只提供发送消息、At 和戳一戳等群聊动作，模型必须基于已有信息立即回复，不能继续搜索；如果最后只生成了文本而没有调用动作工具，该文本会被直接发送。模型接口失败或没有产生任何可发送内容时才会发送兜底消息。

时间区间查询接受 RFC3339、`YYYY-MM-DD HH:MM[:SS]` 或日期格式。区间为左闭右开；仅填写日期作为结束时间时，会自动包含该日期全天。多个区间的重复消息会自动去重，结果按时间排序。

网页搜索默认依次尝试 DuckDuckGo、Bing 中国版和百度，当前提供方不可用或返回结果无法解析时自动降级。fallback 成功后，该可用引擎会在配置的有效期内被优先尝试，避免每次请求重复走完整降级链。网页搜索和读取会拒绝本机、内网和链路本地地址，并限制超时、响应大小和重定向次数。

定时任务保存在机器人进程内，重启机器人后未执行的任务不会恢复。

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
  interval_minutes: 60
  min_messages: 20
```

- `enable`：是否生成用户印象和群聊印象
- `interval_minutes`：检查并生成印象的周期，默认 `60` 分钟
- `min_messages`：一个周期至少积累多少条消息才生成，默认 `20`

消息数不足时不会丢弃，会继续积累到后续周期。群聊上下文、印象处理游标、长期印象和 token 用量均保存在插件数据库中；复读窗口仅保存在内存中。

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
- 旧配置中的 `providers[].model_name` 仍可作为供应商名称读取，但建议改成 `providers[].provider_name`
- 原有用户印象、群聊印象和 token 用量数据库表可以继续使用

## 常见问题

### 机器人不解析图片

确认 `routes.vision.provider_name` 和 `routes.vision.model_name` 均已配置，并且该模型支持图片输入。只配置默认模型不会自动开启图片解析。

### 机器人不参与复读

确认 `repeat.enable` 为 `true`，并检查连续相同消息数量是否达到 `trigger_count`。不支持的消息类型不会触发复读。

### 日志没有模型思考内容

先确认对应路由启用了 `thinking`，并确认模型接口会返回 `reasoning_content`。部分模型即使支持思考，也不会通过 API 返回该字段。

### 网页读取失败

网页必须是公开的 HTTP/HTTPS 地址。内网地址、本机地址、响应过大的页面、超时页面或重定向到内网的页面会被拒绝。
