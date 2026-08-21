# channel-affinity Specification

## Purpose

让标准分组按需把同一会话粘滞到同一把上游 Key，并在该 Key 因限流或失效不可用时自动改绑，同时按时间冷却恢复 429。

## Requirements

### Requirement: 渠道亲和性默认关闭

系统 MUST 将分组的渠道亲和性视为默认关闭。关闭时，系统 MUST 继续使用现有活跃 Key 轮询，不得按会话绑定 Key。

#### Scenario: 未开启时仍轮询 Key

- **WHEN** 同一客户端会话连续向未开启渠道亲和性的分组发送两次请求
- **THEN** 系统 MUST 按现有活跃 Key 轮询选择，不得因为属于同一会话而强制使用同一把 Key

### Requirement: 分组级亲和性开关与 TTL

系统 MUST 允许在标准分组上独立开启或关闭渠道亲和性，并配置会话绑定的保留时长（TTL）。未配置 TTL 时，系统 MUST 使用 1 小时。聚合分组本身 MUST NOT 持有独立的亲和性开关；一次请求是否亲和 MUST 由被选中的子分组配置决定。

#### Scenario: 仅开启的分组执行粘滞

- **WHEN** 管理员开启分组 A 的渠道亲和性并保持分组 B 关闭
- **THEN** 系统 MUST 只对分组 A 的请求做会话粘滞，分组 B 的请求 MUST 继续轮询

#### Scenario: 聚合组跟随子分组

- **WHEN** 客户端请求打到聚合分组，且被选中的子分组已开启渠道亲和性
- **THEN** 系统 MUST 按该子分组的 Key 池做会话粘滞，且绑定不得串用到其他子分组

### Requirement: 按会话标识粘滞同一把 Key

当渠道亲和性已开启时，系统 MUST 从请求中提取会话标识，并在同一分组、同一模型下把后续请求绑定到同一把当时可用的 Key。提取优先级 MUST 为：

1. Anthropic / Claude Code 的 `metadata.user_id` 中的 session 标识
2. `X-Session-ID` 请求头
3. `Session-Id` / `Session_id` 请求头
4. `X-Client-Request-Id` 请求头
5. 请求体中的 `conversation_id`
6. 若以上皆无，则使用前若干条消息内容的稳定哈希作为兜底标识

无法提取任何会话标识时，系统 MUST 回退到现有 Key 轮询。

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

### Requirement: 绑定 Key 不可用时自动改绑

当渠道亲和性已开启且会话已绑定的 Key 不再可用时，系统 MUST 为该会话重新选择一把可用 Key，并在本次请求使用新 Key。不可用至少包括：Key 已被拉黑、Key 处于 429 冷却中、Key 已从活跃池移除。

#### Scenario: 绑定 Key 被拉黑后改绑

- **WHEN** 某会话绑定的 Key 已被标记为无效，该会话再次请求
- **THEN** 系统 MUST 选择另一把可用 Key 完成本次请求，并把该会话改绑到新 Key

#### Scenario: 绑定 Key 处于 429 冷却时改绑

- **WHEN** 某会话绑定的 Key 因 429 处于冷却且尚未到期，该会话再次请求
- **THEN** 系统 MUST 跳过该 Key，改绑到另一把可用 Key

### Requirement: 亲和性开启时 429 按时间冷却并自动恢复

当渠道亲和性已开启且某把 Key 的上游响应为 429 时，系统 MUST 将该 Key 暂时移出可选集合，而不是只把它当作普通失败计数。冷却截止时间 MUST 优先使用上游 `Retry-After`；若无该头，系统 MUST 使用短时间退避。冷却到期后，系统 MUST 自动将该 Key 重新视为可选，无需等待后台探活任务。

亲和性关闭时，429 的处理 MUST 保持现有失败计数、黑名单和定时探活行为。

#### Scenario: 429 后立即换 Key 且进入冷却

- **WHEN** 开启渠道亲和性的分组使用某把 Key 收到上游 429
- **THEN** 系统 MUST 在本次请求的重试中改用另一把可用 Key，并且在冷却期内不再选中原来那把 Key

#### Scenario: 带 Retry-After 的 429 到期后恢复

- **WHEN** 一把 Key 因 429 进入冷却，且上游提供了 `Retry-After`
- **THEN** 系统 MUST 在该时间到达后重新允许选择这把 Key，即使后台探活尚未执行

#### Scenario: 无 Retry-After 时短退避后恢复

- **WHEN** 一把 Key 因 429 进入冷却，且上游未提供 `Retry-After`
- **THEN** 系统 MUST 在短退避结束后重新允许选择这把 Key

#### Scenario: 未开启亲和性时 429 行为不变

- **WHEN** 未开启渠道亲和性的分组收到上游 429
- **THEN** 系统 MUST 继续按现有故障转移、失败计数和黑名单规则处理

### Requirement: 视频任务粘滞到创建时的 Key

当渠道亲和性已开启且分组成功创建视频任务时，系统 MUST 把返回的 `video_id` 或 `request_id` 与创建所用 Key 绑定。后续对该 ID 的查询、拉内容请求 MUST 使用同一把 Key，直到 TTL 到期或该 Key 不可用。Key 不可用时 MUST 按改绑规则处理，而不是把任务查到另一把无关 Key 上静默 404。

#### Scenario: 查询视频命中创建 Key

- **WHEN** 开启渠道亲和性的分组用某把 Key 创建了视频，客户端随后用返回的视频 ID 查询状态或拉取内容
- **THEN** 系统 MUST 使用创建时的那把 Key 访问上游

#### Scenario: 视频绑定 Key 不可用时改绑并暴露失败

- **WHEN** 创建视频所用 Key 已被拉黑或处于冷却，客户端再查询该视频 ID
- **THEN** 系统 MUST 不把请求静默打到另一把从未创建该任务的 Key 上充当成功；若无法继续使用原 Key，MUST 向客户端返回错误
