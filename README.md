## plugins.yaml
```yaml
chatai:
  repo: github.com/kohmebot/chatai
  conf:
    prompt: "你是与一只猫娘..." # 全局预输入提示词
    model_name: "tongyi"                          # 使用的模型名称,目前只支持tongyi
    api_key: "your-api-key"                     # 接口调用所需的 API Key
    max_tokens: 2048                            # 单次响应最大 token 限制
    input_token: 10000                          # 每人每天输入 token 上限
    output_token: 20000                         # 每人每天输出 token 上限
    limit_tips: "你今天的使用额度已达上限，请明天再来！"   # 达到使用上限时的提示词
    error_tips: "出错了..."             # 模型触发错误时的提示词
    prompt_target:                              # 为特定 QQ 号设置专属提示词
        123456789: "你是一个女仆"
    
    online: false # 是否启用联网（需模型支持）
    
    # 暖群配置：群内长时间无人发言时自动提醒
    warm_group:
        enable: true                                # 是否启用该功能
        prompt: "你是一只猫娘...你要负责暖群了"                 # 插入模型前的提示词
        trigger: "%d 分钟没人说话了，暖下群"  # 触发语句，%d 会替换为分钟数
        duration: 60                                # 间隔时间（分钟）
        groups: [12345678,789456123]                                    # 指定启用的群，留空则默认全部

        disable_times: [0,6]        # 禁用时段（小时） 这里表示0点到6点
    
    # 加群欢迎配置：新用户加入时触发欢迎语
    join_group:
        enable: true                                # 是否启用该功能
        prompt: "你是一只猫娘...有人进群了"                     # 插入模型前的提示词
        trigger: "%s 加入了群,欢迎他"         # 触发语句，%s 会替换为新人的昵称
    
    # 戳一戳响应配置：被戳一戳时触发
    poke_group:
        enable: true                                # 是否启用该功能
        prompt: "你是一只猫娘...有人戳了一下你"                   # 插入模型前的提示词
        trigger: "%s 戳了一下你..."                  # 触发语句，%s 会替换为昵称
    
    # 启动提示配置：插件启动时发送一条消息
    on_boot:
        enable: true                                # 是否启用该功能
        prompt: "你是一只猫娘...你复活了"                     # 插入模型前的提示词
        trigger: "你现在复活了,跟大家打招呼"              # 启动后发送的提示语




```