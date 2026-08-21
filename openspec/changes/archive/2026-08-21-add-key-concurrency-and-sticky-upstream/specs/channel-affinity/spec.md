## MODIFIED Requirements

### Requirement: 分组级亲和性开关与 TTL

系统 MUST 允许在标准分组上独立开启或关闭渠道亲和性，并配置会话绑定的保留时长（TTL）。未配置 TTL 时，系统 MUST 使用 1 小时。聚合分组本身 MUST NOT 持有独立的亲和性开关；**首次**请求是否写入绑定 MUST 由被选中的子分组配置决定。入口分组上已存在的有效绑定 MUST 在后续请求中优先于子组轮询被重放。

#### Scenario: 仅开启的分组执行粘滞

- **WHEN** 管理员开启分组 A 的渠道亲和性并保持分组 B 关闭
- **THEN** 系统 MUST 只对分组 A 的请求做会话粘滞，分组 B 的请求 MUST 继续轮询

#### Scenario: 聚合组跟随子分组

- **WHEN** 客户端请求打到聚合分组，且被选中的子分组已开启渠道亲和性
- **THEN** 系统 MUST 按该子分组的 Key 池与上游做会话粘滞，且该会话后续请求 MUST 继续使用被绑定的子分组，不得改绑到其他子分组，除非原绑定已失效

#### Scenario: 聚合组后续请求跳过子组轮询

- **WHEN** 某会话已在入口聚合分组上绑定子分组 S，且绑定仍然有效
- **THEN** 系统 MUST 将后续请求直接交给子分组 S，MUST NOT 重新做加权子组选择

### Requirement: 按会话标识粘滞同一把 Key

当渠道亲和性已开启时，系统 MUST 从请求中提取会话标识，并在同一入口分组、同一模型下把后续请求绑定到同一把当时可用的 Key **以及同一条上游**。提取优先级 MUST 为：

1. Anthropic / Claude Code 的 `metadata.user_id` 中的 session 标识
2. `X-Session-ID` 请求头
3. `Session-Id` / `Session_id` 请求头
4. `X-Client-Request-Id` 请求头
5. 请求体中的 `previous_response_id`
6. 请求体中的 `prompt_cache_key`
7. 请求体中的 `conversation_id`
8. 请求体顶层的 `session_id`（以及 `metadata.session_id`）
9. 若以上皆无，则使用「模型名 + 首条 role=user 消息文本」的稳定哈希作为兜底标识

无法提取任何会话标识时，系统 MUST 回退到现有 Key 轮询，且 MUST NOT 写入会话绑定。

#### Scenario: 同一会话命中同一把 Key

- **WHEN** 开启渠道亲和性的分组连续收到带相同 `X-Session-ID` 且模型相同的两次请求，且首次绑定的 Key 仍可用
- **THEN** 系统 MUST 两次都使用同一把 Key

#### Scenario: 不同会话不共用绑定

- **WHEN** 开启渠道亲和性的分组收到两个不同 `X-Session-ID` 的请求
- **THEN** 系统 MUST NOT 因为它们打到同一分组就强制使用同一把 Key

#### Scenario: 无会话标识时回退轮询

- **WHEN** 开启渠道亲和性的分组收到无法提取会话标识的请求
- **THEN** 系统 MUST 按现有活跃 Key 轮询选择

#### Scenario: 绑定在 TTL 内续期

- **WHEN** 同一会话在 TTL 到期前再次请求且绑定 Key 仍可用
- **THEN** 系统 MUST 继续使用原绑定，并将该绑定的过期时间按 TTL 续期

#### Scenario: TTL 过期后重新绑定

- **WHEN** 同一会话在绑定过期后再次请求
- **THEN** 系统 MUST 重新选择一把可用 Key 并建立新绑定

#### Scenario: Responses 多轮用 previous_response_id 粘滞

- **WHEN** 开启渠道亲和性的分组先后收到两条请求，第二条带有与第一条响应对应的 `previous_response_id`，且绑定 Key 仍可用
- **THEN** 系统 MUST 两条请求使用同一把 Key 和同一条上游

#### Scenario: 多轮对话哈希兜底仍命中

- **WHEN** 开启渠道亲和性的分组连续收到无法提取显式会话标识、但模型相同且首条 user 消息文本相同的两次请求，且首次绑定仍可用
- **THEN** 系统 MUST 两次都使用同一把 Key，即使第二条请求的后续 messages 已增加

## ADDED Requirements

### Requirement: 绑定包含上游、Key 与子分组并记在入口组

当渠道亲和性已开启并成功选出 Key 与上游时，系统 MUST 在**入口分组**（URL 中的分组，对聚合组即聚合组本身）下写入绑定，绑定至少包含：Key、上游身份（用于校验列表是否变化）、子分组名（标准组可为空）。同一会话的后续请求 MUST 重放该上游，MUST NOT 在上游列表未变时重新加权选择另一条上游。

故障转移重试 MUST NOT 用重试选中的 Key/上游覆盖仍有效的原绑定。

#### Scenario: 多上游时同一会话不换上游

- **WHEN** 开启渠道亲和性的分组配置了至少两条不同上游，同一会话连续两次请求且绑定仍有效
- **THEN** 系统 MUST 两次转发到同一条上游 URL

#### Scenario: 标准组绑定记在自身

- **WHEN** 客户端直接请求已开启亲和性的标准分组并完成首次绑定
- **THEN** 系统 MUST 把绑定记在该标准分组下，后续请求按该绑定重放

### Requirement: 绑定上游失效时改绑

当已绑定的上游不再存在于分组上游列表中，或其地址与绑定记录不一致时，系统 MUST 丢弃该绑定，重新选择上游与 Key，并写入新绑定。Key 不可用时仍按现有改绑规则处理。

#### Scenario: 上游列表变更后重新绑定

- **WHEN** 某会话绑定的上游地址已从分组配置中移除或被替换，该会话再次请求
- **THEN** 系统 MUST 选择当前可用的上游与 Key 完成本次请求，并建立新绑定，MUST NOT 继续打到已不存在的旧上游
