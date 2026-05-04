# Lumi 微信入口 V1 最终实施方案

## 1. 目标

本方案定义 Lumi 微信入口 V1 的最终实现方式。实现必须以当前仓库真实结构为准，目标是让看到这份文档的同学不再做产品、接口或数据结构层决策，直接按文档完成代码实现。

V1 的实现结果固定为：

- Lumi 新增 1 个微信入口，调用现有本机 agent 聊天能力。
- 微信入口固定绑定 1 个本机 `local` workspace。
- 微信入口固定绑定 1 个默认 agent。
- 微信会话独立持久化，不进入现有 Web `sessions` 列表。
- 微信配置独立存储，不写回主 `lumi.config.json`。
- 微信前端管理入口接入当前 React 设置弹窗，不接入旧 Vue 设置页。

---

## 2. 明确取舍

### 2.1 借鉴参考项目的部分

参考项目 `/Users/pengmd/c/Ditto/main` 只借鉴下面这些能力，不借整套架构：

- `src/process/channels/plugins/weixin/WeixinLogin.ts`
  - 直接调用微信二维码接口和二维码状态接口完成扫码登录。
- `src/process/channels/plugins/weixin/WeixinMonitor.ts`
  - 使用 `getupdates` 长轮询。
  - 使用 `get_updates_buf` 记录同步进度。
  - 支持附件下载、媒体上传和媒体回传。
- `src/process/channels/utils/channelSendProtocol.ts`
  - 使用显式协议块声明“需要发回微信的图片或文件”。

### 2.2 不采用参考项目的部分

Lumi V1 不引入下面这些结构：

- 不引入 `ChannelManager`。
- 不引入 `PluginManager`。
- 不引入 `BasePlugin`。
- 不引入 `PairingService`。
- 不引入多平台统一 `channels` 框架。
- 不引入 Electron 专用登录窗口或隐藏窗口二维码渲染。
- 不引入多用户配对和授权流程。

结论固定为：

- 借微信接口能力。
- 不借参考项目的通用平台骨架。

### 2.3 参考项目会话维护调研结论

参考项目 `/Users/pengmd/c/Ditto/main` 的微信链路不是简单的 `from_user_id -> sha1 -> 隐藏会话` 模型。

实际调研结论固定记录如下：

- `src/process/channels/plugins/weixin/WeixinMonitor.ts`
  - 将微信 `msg.from_user_id` 作为插件层 `conversationId`。
- `src/process/channels/plugins/weixin/WeixinAdapter.ts`
  - 将插件层 `conversationId` 同时映射为统一消息的 `id`、`chatId`、`user.id`。
- `src/process/channels/gateway/ActionExecutor.ts`
  - 使用 `SessionManager.getSession(channelUser.id, chatId)` 查找当前 channel session。
  - 如果没有 session，则按 `source + channelChatId + type + backend` 查找已有 conversation。
  - 找不到已有 conversation 时才创建新 conversation。
- `src/process/channels/actions/SystemActions.ts`
  - 通用 channel 框架存在 `session.new` action，可清理当前 session 并创建新的 conversation。
- 当前微信插件没有发现把微信文本或按钮转换成 `session.new` 的入口。
  - Telegram/Lark/DingTalk 有按钮或菜单能触发 `session.new`。
  - 微信 monitor 当前只把消息包装成普通 text，因此微信用户发 `session.new` 也会被当成普通聊天文本。

因此参考项目微信链路的语义是：

```text
WeixinMonitor
    |
    v
from_user_id
    |
    v
plugin conversationId / unified chatId / unified user.id
    |
    v
ActionExecutor
    |
    +--> find active channel session by channelUser.id + chatId
    |
    +--> if missing, find latest conversation by:
    |       source=weixin
    |       channelChatId=from_user_id
    |       type=current agent type
    |       backend=current backend
    |
    +--> if still missing, create a normal conversation
```

和 Lumi V1 的差异固定为：

```text
Reference project
-----------------
from_user_id -> channelChatId
channel session -> normal conversationId
conversation lookup depends on source/channelChatId/type/backend
generic channel framework supports session.new
current WeixinPlugin does not expose session.new to WeChat users

Lumi V1
---------
from_user_id -> conversationKey
conversationID = "wx_" + sha1(conversationKey)[:16]
conversation is hidden and stored under ~/.lumi/wechat/sessions/
no channel framework
no pairing
no WeChat-side session.new command
no WeChat-side manual conversation switch
```

结论固定为：

- Lumi V1 不照搬参考项目的 channel session 模型。
- Lumi V1 不支持微信用户在微信里手动新建或切换会话。
- Lumi V1 使用 `from_user_id` 派生稳定隐藏 `conversationID`，同一个微信用户默认长期复用同一个隐藏会话。
- 如果未来要支持 `/new` 或按时间窗口开新会话，需要进入 V2，重新设计 `conversationKey` 或新增显式映射表。

### 2.4 取舍边界图

```text
Ditto reference project
|
|-- WeixinLogin API calls ----------+
|-- WeixinMonitor getupdates -------+--> Lumi V1 backend/internal/wechat
|-- channelSendProtocol idea -------+
|
|-- ChannelManager      X
|-- PluginManager       X
|-- BasePlugin          X
|-- PairingService      X
|-- multi-channel core  X
|-- channel session     X
|-- session.new action  X

Legend:
  +--> borrowed capability
  X    intentionally not imported
```

---

## 3. 当前仓库约束

### 3.1 后端聊天主链约束

当前 `backend/internal/api/chat.go` 同时承担：

- HTTP 请求解析
- 会话准备
- local/device 分发
- SSE 输出
- conversation 持久化

直接先抽共享 Chat Runtime 会改到 Web 聊天主链，风险高于 V1 目标。

V1 固定改为稳定优先方案：

- 不先抽 Web Chat Runtime。
- 不重构 `/api/chat` 主链。
- 微信先新增一条私有聊天运行器，复制当前 local chat 必要逻辑。
- 微信私有运行器只服务微信入口，不服务 Web。
- V1 稳定后，再进入 V2 评估把 Web 和微信的重复代码合并。

结论固定为：

- V1 允许有受控代码冗余。
- V1 以隔离 Web 风险为优先级最高目标。
- V1 不追求提前消除重复代码。

### 3.2 事件语义约束

当前 Web 前端已经依赖这些 SSE 事件名：

- `session`
- `status`
- `update`
- `tool_call`
- `permission_request`
- `error`
- `done`

V1 不允许改动这些事件名，不允许重新发明新的 Web 事件协议。

### 3.3 会话持久化约束

当前 `backend/internal/storage/session.go` 的 `SessionStore.List()` 直接驱动 Web sidebar。微信会话如果写进当前 `SessionStore`，一定会污染现有 Web 会话列表。

V1 固定规则：

- 微信会话不复用 `SessionStore` 目录。
- 微信会话不进入 `/api/sessions`。
- Web 删除普通 session 时不能影响微信隐藏会话。

### 3.4 配置保存约束

当前 `backend/internal/config/save.go` 保存主配置时只回写已知顶层结构。微信配置如果硬塞进主配置文件，会被未来保存操作覆盖。

V1 固定规则：

- 微信配置单独存储。
- 不修改主配置结构。

### 3.5 本地 agent session 约束

当前本地 agent session 只保存在内存 `agentSessions` 中。服务重启后无法保证继续复用同一个 `agentSessionID`。

V1 固定规则：

- 只承诺恢复 conversation history。
- 不承诺重启后复用同一个 `agentSessionID`。

### 3.6 前端真实入口约束

当前 Go 服务嵌入的是 `web/dist`，前端构建脚本是 `next build`。因此当前实际生效的前端链路是 React/Next，不是旧 Vue 页面。

V1 的前端实现目标固定为：

- `web/src/features/settings/settings-modal.tsx`
- `web/src/lib/api.ts`
- `web/src/lib/types.ts`

旧文件 `web/src/components/SettingsModal.vue` 不作为本功能实施入口。

### 3.7 约束到设计决策映射图

```text
current repo constraint
    |
    v
+------------------------------+     +------------------------------+
| chat.go mixes HTTP/SSE/runtime|---->| keep Web chat path unchanged |
+------------------------------+     +------------------------------+

+------------------------------+     +------------------------------+
| WeChat needs agent chat       |---->| add private WeChat runner    |
+------------------------------+     +------------------------------+

+------------------------------+     +------------------------------+
| Web depends on SSE names      |---->| keep existing event names     |
+------------------------------+     +------------------------------+

+------------------------------+     +------------------------------+
| SessionStore drives sidebar   |---->| store WeChat sessions apart   |
+------------------------------+     +------------------------------+

+------------------------------+     +------------------------------+
| main config save is strict    |---->| store WeChat config apart     |
+------------------------------+     +------------------------------+

+------------------------------+     +------------------------------+
| agentSessionID is in memory   |---->| restore history only          |
+------------------------------+     +------------------------------+

+------------------------------+     +------------------------------+
| active frontend is React/Next |---->| edit settings-modal.tsx       |
+------------------------------+     +------------------------------+
```

---

## 4. V1 范围

### 4.1 必做范围

- 微信作为 Lumi 新入口，调用现有本机 agent 聊天能力。
- 固定绑定 1 个本机 `local` workspace。
- 固定绑定 1 个默认 agent。
- 支持扫码登录。
- 支持手工录入微信凭据。
- 支持微信文本消息。
- 支持微信图片和文件入站。
- 支持 agent 输出图片和文件回传微信。
- 支持自动权限。
- 支持隐藏会话恢复。

### 4.2 明确不做

- 不做通用 channels 平台。
- 不做 Telegram/Lark/DingTalk 抽象。
- 不做群聊支持。
- 不做 pairing。
- 不做用户白名单。
- 不做取消执行入口。
- 不做微信侧切换 workspace。
- 不做微信侧切换 agent。
- 不做 remote workspace / device 聊天。
- 不做 crash 场景严格 exactly-once。

### 4.3 V1 范围边界图

```text
IN SCOPE
========
WeChat personal message
    |
    v
fixed local workspace
    |
    v
fixed default agent
    |
    +--> text input
    +--> image/file input
    +--> hidden conversation persistence
    +--> auto permission handling
    +--> image/file reply protocol
    `--> React settings entry

OUT OF SCOPE
============
X generic channels platform
X group chat
X pairing / whitelist
X workspace switch from WeChat
X agent switch from WeChat
X remote workspace / device chat
X strict crash-time exactly-once
```

---

## 5. 总体架构

V1 固定拆成三块能力：

- 现有 Web `/api/chat` 主链，保持不重构。
- `backend/internal/api` 内新增微信私有聊天运行器。
- `backend/internal/wechat` 微信子系统。

### 5.0 总体架构图

```text
                         +----------------------+
Web React chat          |                      |
------------------------>|  backend/internal/api |
        /api/chat        |                      |
                         |  existing chat.go    |
                         |  Web path unchanged  |
                         +----------+-----------+
                                    |
                                    v
                         +----------------------+
                         | existing Web agent   |
                         | manager + sessions   |
                         +----------------------+

WeChat user
    |
    v
+----------------------+      +----------------------+      +----------------------+
| WeChat HTTP API      |<---->| backend/internal/    |----->| api WeChat private  |
| login/getupdates/    |      | wechat               |      | chat runner         |
| send media/text      |      | monitor + gateway    |      | copied local logic  |
+----------------------+      +----------------------+      +----------+-----------+
                                                                  |
                                                                  v
                                                       +----------------------+
                                                       | separate WeChat      |
                                                       | agent manager        |
                                                       | hidden sessions      |
                                                       +----------------------+
```

### 5.1 微信私有聊天运行器

V1 固定不抽共享 Chat Runtime。微信聊天先走一条新增私有运行器，复制当前 local chat 必要逻辑，等 V1 稳定后再进入 V2 合并重复代码。

固定原则：

- Web `/api/chat` 是既有行为基线。
- V1 不拆 `handleChat()`、`prepareChat()`、`handleLocalChat()`、`handleDeviceChat()`。
- V1 不新增 `backend/internal/chatruntime` 中立 runtime 包。
- 微信私有运行器不能服务 Web 请求。
- 微信私有运行器不能写普通 `SessionStore`。
- 微信私有运行器不能使用 Web 的 `agentSessions`、`initialized`、`conversations`。
- V1 允许复制代码，复制范围必须明确、可删除、可测试。

### 5.1.1 包边界与依赖方向

微信包定义自己需要的最小聊天接口，`api` 实现该接口：

- `backend/internal/wechat/chat_contract.go`
  - 定义 `ChatRunner`、`ChatRunInput`、`ChatEvent`、`ChatEventSink`、`HiddenConversationStore`。
  - 不 import `backend/internal/api`。
- `backend/internal/api/wechat_chat.go`
  - 实现 `wechat.ChatRunner`。
  - 只服务微信入口。
  - 复制当前 local chat 的必要逻辑。
- `backend/internal/wechat/gateway.go`
  - 只依赖 `ChatRunner` 接口。
  - 不 import `backend/internal/api`。

依赖方向固定为：

```text
backend/internal/api
    `-- imports backend/internal/wechat

backend/internal/wechat
    `-- no api dependency
```

`api.NewServer()` 创建微信 service 时，把微信私有运行器作为 `wechat.ChatRunner` 注入给 `wechat.Service`。微信侧只能调用接口，不能直接访问 `api.Server` 内部字段。

合约接口固定为：

```go
package wechat

import (
    "context"

    "github.com/pengmide/lumi/internal/storage"
)

type ChatFileInfo struct {
    Name string
    Path string
    Size int64
}

type HiddenConversationStore interface {
    Load(id string) (*storage.StoredSession, error)
    Save(session *storage.StoredSession) error
}

type ChatRunInput struct {
    Message             string
    ConversationID      string
    WorkspaceID         string
    WorkspacePath       string
    AgentID             string
    Files               []ChatFileInfo
    PromptPrefix        string
    SessionModeOverride string
    ConversationStore   HiddenConversationStore
}

type ChatEvent struct {
    Name string
    Data any
}

type ChatEventSink interface {
    Emit(ChatEvent) error
}

type ChatRunner interface {
    RunWeChatChat(ctx context.Context, input ChatRunInput, sink ChatEventSink) error
}
```

### 5.1.2 私有运行器内部状态

微信私有运行器固定使用独立状态：

```go
type wechatChatRuntime struct {
    config        *config.Config
    agents        *agent.Manager
    conversations *conversation.Manager

    agentSessions map[string]map[string]string // conversationID -> agentID -> sessionID
    initialized   map[string]bool
    mu            sync.Mutex
}
```

规则固定为：

- `agents` 必须是独立 `agent.Manager`，不能复用 `Server.agents`。
- `conversations` 必须是独立 `conversation.Manager`，不能复用 `Server.conversations`。
- `agentSessions` 必须是微信私有 map，不能复用 `Server.agentSessions`。
- `initialized` 必须是微信私有 map，不能复用 `Server.initialized`。
- 微信 agent process 与 Web agent process 分离，避免 notification/permission handler 串流互相污染。
- 代价是微信会为绑定 agent 额外启动一个 agent 进程；V1 接受该资源开销，换取 Web 主链隔离。
- `Server.Shutdown()` 时先停止微信 monitor，再 shutdown 微信私有 agent manager，最后 shutdown Web agent manager。
- `/api/agents/update` 更新 agent 配置后，必须停止对应微信私有 agent process，并清理该 agent 的微信 session 映射和 initialized 状态。

### 5.1.3 允许复制的代码范围

V1 允许从当前 `backend/internal/api/chat.go` 和 `notification.go` 复制以下逻辑到 `api/wechat_chat.go`：

- agent initialize。
- `session/new`。
- `session/set_mode`。
- `session/prompt`。
- notification text chunk 聚合。
- tool call 聚合成 `conversation.ToolCallInfo`。
- assistant stream 持久化。
- `storage.GenerateTitle()` 和 `storage.StoredSession` 结构复用。

V1 禁止复制或接入以下 Web 行为：

- HTTP/SSE adapter。
- Web `@agent` 检测。
- Web agent switch context 注入。
- device chat path。
- remote workspace `@file` 展开。
- `/api/permission/confirm` Web 交互流程。
- 普通 `SessionStore` 读写。
- Web sidebar/session/share 逻辑。
- Web `agentCommands` 缓存。

如果复制 notification 解析逻辑，必须加注释标记来源：

```go
// V1 WeChat-private copy of api notification aggregation.
// Keep Web notification.go unchanged; dedupe in V2 only after WeChat V1 stabilizes.
```

### 5.1.4 微信私有运行器步骤

`RunWeChatChat()` 内部固定步骤：

1. 校验 `ConversationID`、`WorkspaceID`、`WorkspacePath`、`AgentID`、`ConversationStore` 非空。
2. 从微信私有 `conversations` 查找 `ConversationID`。
3. 如果内存不存在，从 `ConversationStore` 恢复隐藏会话。
4. 如果磁盘也不存在，用传入 `ConversationID` 创建隐藏会话。
5. 使用独立微信 `agent.Manager` 获取 agent process。
6. 设置工作目录为 `WorkspacePath`。
7. 确保 agent 初始化。
8. 创建或复用微信私有 agent session。
9. 新建 session 时按 `SessionModeOverride` 设置 mode。
10. 将 `PromptPrefix` 拼到发给 agent 的 prompt 前面。
11. 用户原始消息和文件 metadata 写入隐藏 conversation。
12. 调用 `session/prompt`。
13. 聚合 notification text/tool call。
14. 自动处理 permission request。
15. assistant 输出写入隐藏 conversation。
16. 用 `ConversationStore.Save()` 保存隐藏会话。
17. 通过 sink 发出 `session`、`status`、`update`、`tool_call`、`permission_request`、`error`、`done` 事件。

错误语义固定为：

- 业务错误优先通过 `event:error` 发给 sink。
- `RunWeChatChat()` 返回 error 只表示 context 取消、sink 写失败或无法继续的内部错误。

### 5.1.5 与 Web 主链的隔离

微信私有运行器必须满足：

- 不调用 `s.handleChat()`。
- 不调用 `s.prepareChat()`。
- 不调用 `s.handleLocalChat()`。
- 不调用 `s.handleDeviceChat()`。
- 不调用 `s.getOrCreateConversation()`。
- 不调用 `s.persistConversation()`。
- 不读取或写入 `s.sessionStore`。
- 不读取或写入 `s.conversations`。
- 不读取或写入 `s.agentSessions`。
- 不读取或写入 `s.initialized`。
- 不读取或写入 `s.remoteAgentSessions`。

因此 Web 即使传入 `wx_xxx` conversationId，也不会命中微信隐藏 conversation，因为微信隐藏 conversation 不存在于 Web 的 `s.conversations` 中。

### 5.1.6 session mode override 与自动权限

微信私有 `createAgentSession()` 固定接收显式 mode：

```go
func (r *wechatChatRuntime) createAgentSession(agentID, cwd, modeID string) (string, error)
```

规则固定为：

- 微信 session mode 不读取 Web agent permission mode。
- 微信 session mode 由 gateway 按 agentID 推导后传入。
- `agentID == "codex"` -> `auto`。
- `strings.HasPrefix(agentID, "claude")` -> `bypassPermissions`。
- 其他 agent -> `default`。
- 不修改 `s.config`。
- 不调用 `s.config.Save()`。
- 不停止或重启 Web agent process。

自动权限有一个实现陷阱：当前 agent permission request 是 agent process 先通知 handler，再把 pending permission 放入 map。如果微信在 handler 里立即确认，可能确认不到 pending permission。

固定处理方式：

- V1 允许修改 `agent.Process.handlePermissionRequest()` 这一处共享底层逻辑：
  1. 解析 permission request。
  2. 先把 `toolCallID -> PendingPermission` 写入 `p.permissions`。
  3. 再通知 permission handlers。
  4. 等待确认结果。
- 这个改动必须配测试，确保 Web `/api/permission/confirm` 仍然有效。
- 微信自动权限只作用于微信私有 agent process。
- 自动选择顺序固定为 `allow_once` -> `allow_always`。
- 没有 allow 类选项时，微信本次请求发 `event:error` 并结束。

### 5.1.7 并发与串行

并发规则固定为：

- 微信同一 conversation 的串行执行由 `backend/internal/wechat/gateway.go` 的 per-conversation lock 负责。
- 微信私有运行器内部 map 必须由自己的 mutex 保护。
- 不新增全局聊天大锁。
- 不改 Web 多会话并发行为。
- 微信 agent manager 与 Web agent manager 分离后，不需要改 Web `agentSessions`、`initialized` 的锁策略。

### 5.1.8 V2 合并条件

V1 不做共享 runtime。只有满足以下条件后，才能进入 V2 合并：

- 微信纯文本、附件、文件回传、自动权限全部验收通过。
- Web 主链回归测试稳定通过。
- 微信私有 runner 和 Web local chat 的重复代码点已列清单。
- 已确认 notification/permission/session 并发模型不会让 Web 和微信互相串流。
- 有明确回滚方案。

V2 合并优先级固定为：

1. 先抽纯函数，例如 prompt 拼接、session mode 推导、文件 metadata 转换。
2. 再抽无状态 notification 解析器。
3. 最后才考虑共享 Chat Runtime。

V2 之前不得为了减少重复代码而重构 Web `/api/chat`。

### 5.1.9 私有运行器流向

```text
Web /api/chat
    |
    v
existing chat.go path
    |
    v
existing Web agent manager/session store

WeChat gateway
    |
    | wechat.ChatRunInput
    v
api/wechat_chat.go
    |
    +--> private conversation manager
    +--> private agent manager
    +--> private agent sessions
    +--> hidden conversation store
    +--> same event names via sink
    |
    v
WeChat reply aggregator
```

### 5.1.10 Phase 1 必测用例

Phase 1 完成后，必须先通过以下测试，才能继续微信后续阶段：

- 现有 `backend/internal/api` 测试全部通过。
- `/api/chat` Web local chat 行为不变。
- `/api/chat` Web device chat 行为不变。
- Web `@agent` 切换行为不变。
- Web remote workspace `@file` 展开行为不变。
- Web 手工 `/api/permission/confirm` 仍然有效。
- 微信私有 runner 创建的 `wx_` conversation 不出现在普通 `SessionStore`。
- Web 请求同名 `wx_` conversationId 不能读到微信隐藏会话。
- 微信私有 runner 使用独立 agent manager，不复用 `Server.agents`。
- 微信自动权限能立即确认 allow option。

### 5.2 微信子系统

新增 `backend/internal/wechat/`，目录和职责固定如下：

- `service.go`
  - 微信服务单例生命周期。
  - 负责 `Start()`、`Stop()`、`Status()`。
- `config_store.go`
  - 读写 `~/.lumi/wechat/config.json`。
- `runtime_store.go`
  - 读写 `~/.lumi/wechat/runtime.json`。
- `conversation_store.go`
  - 读写 `~/.lumi/wechat/sessions/<conversationID>.json`。
- `client.go`
  - 封装微信 HTTP API。
- `login.go`
  - 扫码登录任务和 SSE 事件。
- `monitor.go`
  - `getupdates` 长轮询、buf 落盘、消息解析、附件下载、回复发送。
- `gateway.go`
  - 微信入站消息转 runtime，请求结果聚合成微信回复。
- `send_protocol.go`
  - 解析 `LUMI_WECHAT_SEND` 协议块。
- `handlers.go`
  - `/api/wechat/*` 管理接口。

微信子系统职责图：

```text
backend/internal/wechat/
|
|-- handlers.go
|     |-- config/status/test/enable/disable/login HTTP APIs
|
|-- service.go
|     |-- owns monitor lifecycle
|     |-- exposes Start/Stop/Status
|
|-- config_store.go --------> ~/.lumi/wechat/config.json
|-- runtime_store.go -------> ~/.lumi/wechat/runtime.json
|-- conversation_store.go --> ~/.lumi/wechat/sessions/*.json
|
|-- login.go ---------------> WeChat QR login API
|-- monitor.go -------------> WeChat getupdates long polling
|-- client.go --------------> low level WeChat HTTP calls
|
|-- gateway.go -------------> api WeChat private chat runner
|-- send_protocol.go -------> media/file reply actions
```

---

## 6. 文件存储与持久化

### 6.1 微信配置目录

微信数据根目录固定为：

```text
~/.lumi/wechat/
```

### 6.2 微信配置文件

配置文件固定为：

```text
~/.lumi/wechat/config.json
```

结构固定为：

```go
type WeChatConfig struct {
    Enabled     bool   `json:"enabled"`
    LoginMode   string `json:"loginMode"` // qr | manual
    AccountID   string `json:"accountId"`
    BotToken    string `json:"botToken"`
    BaseURL     string `json:"baseUrl"`
    WorkspaceID string `json:"workspaceId"`
    AgentID     string `json:"agentId"`
}
```

默认值固定为：

- `enabled = false`
- `loginMode = "qr"`
- `baseUrl = "https://ilinkai.weixin.qq.com"`

存储权限固定为：

- `~/.lumi/wechat/` 目录权限尽量设置为 `0700`。
- `config.json`、`runtime.json`、隐藏会话文件权限尽量设置为 `0600`。
- `botToken` V1 明文存储在 `config.json` 中，不引入系统钥匙串或本地加密。
- Windows 或不支持 POSIX permission 的平台允许降级为普通写入，但不得额外扩散 token。

### 6.3 微信运行状态文件

运行状态文件固定为：

```text
~/.lumi/wechat/runtime.json
```

结构固定为：

```go
type WeChatRuntimeState struct {
    Running             bool     `json:"running"`
    LastError           string   `json:"lastError,omitempty"`
    LastSyncAt          int64    `json:"lastSyncAt,omitempty"`
    LastLoginAt         int64    `json:"lastLoginAt,omitempty"`
    LastMessageAt       int64    `json:"lastMessageAt,omitempty"`
    Buf                 string   `json:"buf,omitempty"`
    ProcessedMessageIDs []string `json:"processedMessageIds,omitempty"`
}
```

`get_updates_buf` 固定写入 `runtime.json.buf`，不额外拆分 `buf.txt`。

`processedMessageIds` 固定保存最近 500 个已处理微信 `msg_id`：

- 收到重复 `msg_id` 时直接跳过，不回复微信，不调用 agent。
- 消息开始处理后，无论最终 agent 成功、agent 失败、发送微信回复失败，均写入去重记录。
- 同一 conversation 忙碌时，回复固定忙碌文案后，也写入去重记录。
- 如果微信消息没有 `msg_id`，使用 `from_user_id + ":" + receivedAt` 生成临时去重 key。
- 去重只用于避免重复执行，不承诺 crash 场景 exactly-once。

### 6.4 微信隐藏会话文件

微信隐藏会话固定存储为：

```text
~/.lumi/wechat/sessions/<conversationID>.json
```

文件结构固定复用 `storage.StoredSession`：

- `id`
- `title`
- `messages`
- `activeAgent`
- `workspaceId`
- `createdAt`
- `updatedAt`

不在隐藏会话文件中持久化 `agentSessionID`。

恢复隐藏会话时必须原样恢复 `storage.StoredSession` 中的 `Messages`、`ActiveAgent`、`WorkspaceID`、`CreatedAt`：

- 不调用 Web 的 `s.restoreConversation()`。
- 不通过逐条 `AddAssistantMessage()` 重建，以免丢失 `ToolCall`、`Files` 或原 timestamp。
- 微信私有 conversation manager 如缺少原样恢复 API，V1 可在 `api/wechat_chat.go` 内部写私有恢复函数，禁止改 Web session 恢复语义。

### 6.5 存储布局图

```text
~/.lumi/
|
|-- lumi.config.json              # main config, unchanged by WeChat
|
`-- wechat/
    |
    |-- config.json                 # account, token, binding config
    |-- runtime.json                # running status, timestamps, buf
    |
    `-- sessions/
        |-- wx_0123456789abcdef.json
        |-- wx_89abcdef01234567.json
        `-- ...

workspace root/
|
`-- .lumi-uploads/
    `-- wechat/
        `-- wx_0123456789abcdef/
            |-- 1712345678000-photo.png
            `-- 1712345680000-spec.pdf
```

---

## 7. 会话模型

### 7.1 conversationKey

微信单聊场景下，入站会话键固定为：

```text
conversationKey = from_user_id
```

V1 不支持群聊消息，不解析群聊上下文。

### 7.2 隐藏 conversationID

隐藏会话 ID 固定生成规则为：

```text
conversationID = "wx_" + sha1(conversationKey)[:16]
```

V1 不维护单独的 `wechat_session_map`。

### 7.3 恢复规则

收到同一微信用户的新消息时，固定按以下顺序处理：

1. 如果内存中已存在该 `conversationID`，直接复用。
2. 如果内存中不存在，但 `~/.lumi/wechat/sessions/<conversationID>.json` 存在，先恢复 history，再创建新的本地 agent session。
3. 如果隐藏会话文件不存在，创建新的隐藏 conversation。

按方案，微信链路不让用户在微信里手动新建或切换会话。同一个微信用户永远映射到同一个隐藏 `conversationID`，所以默认就是复用会话。

### 7.4 新建与复用判断流程

判断流程固定为：

```text
收到微信消息
  |
  v
取 from_user_id
  |
  v
conversationKey = from_user_id
  |
  v
conversationID = "wx_" + sha1(conversationKey)[:16]
  |
  v
按这个 conversationID 查找/恢复会话
```

具体什么时候复用：

```text
同一个 from_user_id 再次发消息
  |
  v
生成同一个 wx_xxx conversationID
  |
  +-- 内存里已有这个 conversationID
  |     -> 直接复用当前会话和当前 agentSessionID
  |
  +-- 内存里没有，但 ~/.lumi/wechat/sessions/wx_xxx.json 存在
        -> 恢复历史消息
        -> 创建新的本地 agentSessionID
        -> 继续同一个隐藏 conversation
```

具体什么时候新建：

```text
收到一个以前没见过的 from_user_id
  |
  v
生成一个新的 wx_xxx conversationID
  |
  v
内存里没有，磁盘上也没有 ~/.lumi/wechat/sessions/wx_xxx.json
  |
  v
创建新的隐藏 conversation
```

V1 固定不提供以下能力：

- 不提供微信侧 `/new` 或 `session.new` 命令。
- 不提供微信侧会话列表。
- 不提供微信侧手动切换历史 conversation。
- 不按天、按时间窗口或按主题自动开新会话。

### 7.5 串行执行

同一微信会话固定串行执行。

实现规则固定为：

- `conversationID` 维度维护互斥锁。
- 正在执行时，同一 `conversationID` 再收到新消息，直接回复：

```text
上一条消息还在处理中，请稍后再发。
```

V1 不排队，不覆盖，不合并。

### 7.6 会话恢复与串行执行图

```text
Inbound WeChat message
    |
    v
conversationKey = from_user_id
    |
    v
conversationID = "wx_" + sha1(conversationKey)[:16]
    |
    v
+------------------------------+
| lock exists and is running ? |
+------------------------------+
    | yes                         | no
    v                             v
reply busy message                acquire conversation lock
                                  |
                                  v
                       +----------------------+
                       | in-memory session ?  |
                       +----------------------+
                           | yes        | no
                           v            v
                        reuse     hidden file exists ?
                                      | yes          | no
                                      v              v
                              restore history   create new hidden
                              create agent      conversation
                              session
                                  \              /
                                   v            v
                               call WeChat private chat runner
                                   |
                                   v
                              persist hidden session
                                   |
                                   v
                              release lock
```

---

## 8. 自动权限

### 8.1 session mode override

微信来源的本地 session 创建后，固定立刻设置 mode：

- `agentID == "codex"` -> `auto`
- `strings.HasPrefix(agentID, "claude")` -> `bypassPermissions`
- 其他 agent -> `default`

### 8.2 兜底自动确认

如果微信 runtime 仍收到 `permission_request`，固定在后端直接处理，不向微信暴露交互。

处理顺序固定为：

1. 优先选择 `allow_once`
2. 如果没有 `allow_once`，选择 `allow_always`
3. 如果没有 allow 类选项，本次请求失败

失败时固定向微信回复错误文本，且 runtime 结束。

### 8.3 Web 语义不变

微信自动权限不能影响 Web 现有行为：

- `/api/permission/confirm` 继续服务 Web 和 device 场景
- Web 当前 agent 配置不因为微信请求而被改写
- Web 会话的人工确认流程保持原样

### 8.4 自动权限处理图

```text
WeChat private runner event stream
    |
    v
permission_request ?
    |
    +-- no ----------------------------------> aggregate normal events
    |
    +-- yes
         |
         v
   options contain allow_once ?
         | yes                         no
         v                             v
   confirm allow_once        options contain allow_always ?
                                       | yes                 no
                                       v                     v
                               confirm allow_always     fail this run
                                                           |
                                                           v
                                                 reply error to WeChat

Web event stream
    |
    v
permission_request stays visible to Web UI
```

---

## 9. 微信入站消息模型

入站消息结构固定为：

```go
type WeChatAttachment struct {
    Kind string `json:"kind"` // image | file
    Name string `json:"name"`
    Path string `json:"path"`
    Size int64  `json:"size"`
}

type WeChatInboundMessage struct {
    ConversationKey string             `json:"conversationKey"`
    MessageID       string             `json:"messageId"`
    ContextToken    string             `json:"contextToken"`
    Text            string             `json:"text"`
    Attachments     []WeChatAttachment `json:"attachments"`
    ReceivedAt      int64              `json:"receivedAt"`
}
```

固定解析字段：

- `conversationKey = from_user_id`
- `messageID = msg_id`
- `contextToken = context_token`
- `text` 从文本 item 或语音转文字 item 中提取
- `attachments` 从 image/file item 中提取

如果消息没有 `from_user_id`，直接丢弃并记录日志。

### 9.1 入站消息解析图

```text
raw getupdates item
    |
    v
+--------------------+
| has from_user_id ? |
+--------------------+
    | no                         | yes
    v                            v
drop + log              conversationKey = from_user_id
                             |
                             v
                    +---------------------+
                    | parse message parts |
                    +---------------------+
                       | text item
                       | voice-to-text item
                       | image item
                       | file item
                       v
                    WeChatInboundMessage
                       |
                       |-- MessageID    = msg_id
                       |-- ContextToken = context_token
                       |-- Text         = text or voice text
                       `-- Attachments  = image/file metadata
```

---

## 10. 入站附件处理

### 10.1 存储路径

微信附件固定下载到绑定 workspace 内：

```text
<workspacePath>/.lumi-uploads/wechat/<conversationID>/
```

### 10.2 文件命名

文件名固定格式为：

```text
<unix_ms>-<sanitized_basename>
```

清洗规则固定为：

- 只保留 `[a-zA-Z0-9._-]`
- 连续非法字符合并为 `_`
- 空文件名回退为 `file`

### 10.3 类型判定

文件类型固定按内容探测，不信任微信返回的扩展名：

- 图片识别为 `image`
- 其他识别为 `file`

### 10.4 prompt 注入格式

微信 runtime 在用户原始消息前固定注入以下说明块：

```text
[WeChat attachments]
- image: .lumi-uploads/wechat/wx_xxx/1712345678-photo.png
- file: .lumi-uploads/wechat/wx_xxx/1712345678-spec.pdf
[/WeChat attachments]
```

规则固定为：

- 路径写相对 workspace root 的相对路径
- 多个附件按收到顺序列出
- 如果消息没有文本但有附件，prompt 只包含附件块
- 如果部分附件下载失败，仍调用 agent，并在附件块中追加失败说明。
- 如果全部附件下载失败但消息有文本，仍调用 agent，并在附件块中说明附件处理失败。
- 如果消息没有文本且全部附件下载失败，不调用 agent，直接回复微信：`附件处理失败，请重新发送。`

附件失败说明格式固定为：

```text
[WeChat attachments]
- image: .lumi-uploads/wechat/wx_xxx/1712345678-photo.png
- failed: original-name.dat (download failed: <reason>)
[/WeChat attachments]
```

### 10.5 限制与清理

固定限制为：

- 单文件大小上限：`200MB`
- 上传目录总容量上限：`200MB`
- TTL：`72h`

每次有新附件写入后，固定执行一次轻量清理：

- 先清理过期文件
- 再按修改时间从旧到新删除，直到总容量不超过上限

### 10.6 附件处理图

```text
WeChatInboundMessage.Attachments
    |
    v
for each attachment
    |
    +--> check size <= 200MB
    |
    +--> download via WeChat client
    |
    +--> sniff content type
    |       |-- image -> Kind=image
    |       `-- other -> Kind=file
    |
    +--> sanitize basename
    |
    +--> write to:
    |       <workspace>/.lumi-uploads/wechat/<conversationID>/
    |
    +--> record workspace-relative path
            |
            v
    inject prompt block
            |
            v
    run lightweight cleanup
```

---

## 11. 微信文件回传协议

### 11.1 协议格式

agent 需要把图片或文件发回微信时，必须输出以下协议块：

```text
[LUMI_WECHAT_SEND]
{"type":"image","path":"output/chart.png","fileName":"chart.png","caption":"生成的图表"}
[/LUMI_WECHAT_SEND]
```

```text
[LUMI_WECHAT_SEND]
{"type":"file","path":"reports/summary.pdf","fileName":"summary.pdf"}
[/LUMI_WECHAT_SEND]
```

### 11.2 协议规则

规则固定为：

- 支持多段协议块
- 每段协议块只允许 1 个 JSON object
- 仅在微信私有聊天运行器输出链路下解析
- `type` 只允许 `image` 或 `file`
- `path` 必填
- `fileName` 可选
- `caption` 可选

### 11.3 路径解析规则

每个 action 固定按以下规则校验：

1. 相对路径按绑定 workspace root 解析。
2. 绝对路径允许使用，但 `realpath` 后必须仍位于 workspace root 内。
3. 软链允许存在，但 `realpath` 后必须仍位于 workspace root 内。
4. 路径必须指向普通文件。
5. 文件大小必须不超过 `200MB`。

不满足任何一条时，该 action 直接跳过。

### 11.4 可见文本与失败说明

协议块从最终可见文本中删除。删除协议块后剩余内容记为 `visibleText`。

如果某个 action 跳过或发送失败，在最终可见文本末尾逐条追加：

```text
文件回传失败：<path>（<reason>）
```

如果 `visibleText` 为空且没有任何媒体成功发送，固定回复：

```text
已完成。
```

### 11.5 发送顺序

同一条微信回复固定按以下顺序发送：

1. 发送每个媒体自己的 `caption`
2. 发送媒体文件
3. 发送剩余 `visibleText`

所有发送请求都带回本次入站消息的 `context_token`。

### 11.6 文件回传图

```text
agent final text
    |
    v
extract [LUMI_WECHAT_SEND] blocks
    |
    +--> visibleText = final text without protocol blocks
    |
    `--> actions[]
          |
          v
     validate each action
          |
          |-- type in {image,file}
          |-- path present
          |-- realpath under workspace root
          |-- regular file
          `-- size <= 200MB
          |
          v
     send in fixed order
          |
          |-- caption
          |-- media file
          `-- visibleText
          |
          v
     append failure lines for skipped/failed actions
```

---

## 12. 微信登录

### 12.1 登录方式

V1 固定支持 2 种登录方式：

- `qr`
- `manual`

### 12.2 扫码登录执行方式

扫码登录固定由后端直接调用微信接口完成。前端只负责建立 SSE 和渲染二维码。

不使用 Electron 隐藏窗口，不使用服务端渲染二维码图片。

### 12.3 登录任务管理

登录任务规则固定为：

- 同一时刻只允许 1 个活动登录任务
- 新的 `login/start` 请求会取消旧任务并创建新任务
- SSE 断开后，当前登录任务直接取消
- 前端如果需要继续登录，重新发起 `login/start`

### 12.4 SSE 事件

后端登录 SSE 事件固定为：

- `qr`
- `scanned`
- `confirmed`
- `expired`
- `error`
- `done`

事件数据固定为：

```json
event: qr
data: {"ticket":"xxx","imageUrl":"https://..."}
```

```json
event: scanned
data: {}
```

```json
event: confirmed
data: {"accountId":"xxx","baseUrl":"https://ilinkai.weixin.qq.com","hasToken":true}
```

```json
event: expired
data: {}
```

```json
event: error
data: {"message":"具体错误信息"}
```

```json
event: done
data: {}
```

### 12.5 登录成功后的落盘行为

扫码登录成功后，后端固定立即执行以下操作：

1. 保存 `accountId`
2. 保存 `botToken`
3. 保存 `baseUrl`
4. 更新 `runtime.json.lastLoginAt`

登录成功后不自动启动 monitor，`enabled` 字段保持原值，是否启动仍由用户显式点击启用。

`qr` 事件不返回 `expiresAt`。微信接口没有稳定返回过期时间，前端只根据 `expired`、`error`、`done` 事件更新状态。

### 12.6 扫码登录时序图

```text
React settings UI        backend login.go         WeChat API          config/runtime store
       |                       |                       |                       |
       | POST /login/start     |                       |                       |
       |---------------------->| create login task     |                       |
       |<----------------------| loginId               |                       |
       |                       |                       |                       |
       | GET /login/events?id  |                       |                       |
       |---------------------->| request QR ticket     |                       |
       |                       |---------------------->|                       |
       |                       |<----------------------| ticket/imageUrl       |
       |<----------------------| event: qr             |                       |
       |                       |                       |                       |
       |                       | poll QR status        |                       |
       |                       |---------------------->|                       |
       |<----------------------| event: scanned        |                       |
       |                       |                       |                       |
       |                       | poll QR status        |                       |
       |                       |---------------------->|                       |
       |                       |<----------------------| account/token/baseURL |
       |<----------------------| event: confirmed      |                       |
       |                       |----------------------------------------------->|
       |                       | save credentials + lastLoginAt                |
       |<----------------------| event: done           |                       |
```

### 12.7 微信 HTTP API 合同

V1 直接实现以下微信 iLink Bot API。实现不得让开发者重新查参考项目决定字段。

通用规则：

- `baseUrl` 默认 `https://ilinkai.weixin.qq.com`，拼接 endpoint 前先去掉末尾 `/`。
- 登录二维码接口使用 `GET`，请求头固定带 `iLink-App-ClientVersion: 1`。
- 登录后的 Bot API 使用 `POST` JSON，请求头固定带：
  - `Content-Type: application/json`
  - `AuthorizationType: ilink_bot_token`
  - `Authorization: Bearer <botToken>`
  - `X-WECHAT-UIN: <accountId>`
- POST 响应 HTTP 非 2xx 视为错误。
- JSON 响应中 `ret != 0` 或 `errcode != 0` 视为微信业务错误。
- 普通 API 超时固定 15s，长轮询 `getupdates` 超时固定 35s，CDN 上传/下载超时固定 30s。

扫码登录：

```text
GET /ilink/bot/get_bot_qrcode?bot_type=3
response: {"qrcode":"<ticket>","qrcode_img_content":"<imageUrl>"}
```

```text
GET /ilink/bot/get_qrcode_status?qrcode=<ticket>
response: {
  "status": "wait" | "scaned" | "expired" | "confirmed",
  "bot_token": "...",
  "ilink_bot_id": "...",
  "ilink_user_id": "...",
  "baseurl": "..."
}
```

确认成功时：

- `accountId = ilink_bot_id`
- `botToken = bot_token`
- `baseUrl = baseurl || defaultBaseUrl`
- `scaned` 原样映射为 SSE `scanned`

长轮询：

```text
POST /ilink/bot/getupdates
request: {"get_updates_buf":"<runtime.buf>","base_info":{}}
response: {"ret":0,"errcode":0,"get_updates_buf":"...","msgs":[...]}
```

发送文本：

```text
POST /ilink/bot/sendmessage
request: {
  "msg": {
    "to_user_id": "<from_user_id>",
    "client_id": "<uuid>",
    "message_type": 2,
    "message_state": 2,
    "item_list": [{"type": 1, "text_item": {"text": "<text>"}}],
    "context_token": "<context_token>"
  },
  "base_info": {}
}
```

获取上传地址：

```text
POST /ilink/bot/getuploadurl
request: {
  "filekey": "<random hex>",
  "media_type": 1 | 3,
  "to_user_id": "<from_user_id>",
  "rawsize": <raw bytes>,
  "rawfilemd5": "<md5 hex>",
  "filesize": <aes padded bytes>,
  "no_need_thumb": true,
  "aeskey": "<aes key hex>",
  "base_info": {}
}
response: {"upload_full_url":"...","upload_param":"..."}
```

媒体类型固定为：`1=image`，`3=file`。

CDN 上传：

- 如果 `upload_full_url` 非空，直接 POST 到该 URL。
- 否则 POST 到 `https://novac2c.cdn.weixin.qq.com/c2c/upload?encrypted_query_param=<upload_param>&filekey=<filekey>`。
- body 为 AES-128-ECB 加密后的文件 bytes，`Content-Type: application/octet-stream`。
- HTTP 200 且响应头 `x-encrypted-param` 非空才算成功。
- 4xx 不重试；其他错误最多重试 3 次。

发送媒体：

```text
POST /ilink/bot/sendmessage
request image item: {
  "msg": {
    "to_user_id": "<from_user_id>",
    "client_id": "<uuid>",
    "message_type": 2,
    "message_state": 2,
    "item_list": [{
      "type": 3,
      "image_item": {
        "media": {
          "encrypt_query_param": "<x-encrypted-param>",
          "aes_key": "<base64 aes key hex string>",
          "encrypt_type": 1
        },
        "mid_size": <ciphertext bytes>
      }
    }],
    "context_token": "<context_token>"
  },
  "base_info": {}
}
```

```text
POST /ilink/bot/sendmessage
request file item: {
  "msg": {
    "to_user_id": "<from_user_id>",
    "client_id": "<uuid>",
    "message_type": 2,
    "message_state": 2,
    "item_list": [{
      "type": 4,
      "file_item": {
        "media": {
          "encrypt_query_param": "<x-encrypted-param>",
          "aes_key": "<base64 aes key hex string>",
          "encrypt_type": 1
        },
        "file_name": "<fileName>",
        "len": "<raw bytes as string>"
      }
    }],
    "context_token": "<context_token>"
  },
  "base_info": {}
}
```

附件下载：

- 图片 item type 固定为 `3`，文件 item type 固定为 `4`，文本 item type 固定为 `1`，语音转文本 item type 固定为 `2`。
- 入站媒体读取 `image_item.media.encrypt_query_param` 或 `file_item.media.encrypt_query_param`。
- 下载 URL 固定为 `https://novac2c.cdn.weixin.qq.com/c2c/download?encrypted_query_param=<encrypt_query_param>`。
- 如果 item 中存在 AES key，按 AES-128-ECB 解密；没有 key 时按原始 bytes 保存。
- AES key 兼容两种来源：`itemData.aeskey` hex，或 `itemData.media.aes_key` base64。

Typing：

```text
POST /ilink/bot/getconfig
request: {"ilink_user_id":"<from_user_id>","context_token":"<context_token>","base_info":{}}
response: {"ret":0,"errcode":0,"typing_ticket":"..."}
```

```text
POST /ilink/bot/sendtyping
request: {"ilink_user_id":"<from_user_id>","typing_ticket":"...","status":"TYPING" | "CANCEL","base_info":{}}
```

Typing 失败只记录日志，不影响聊天、附件或回复发送。

---

## 13. 微信 monitor

### 13.1 启停规则

monitor 行为固定为：

- `POST /api/wechat/enable` 启动 monitor
- `POST /api/wechat/disable` 停止 monitor
- `Server.Shutdown()` 时先停止 monitor，再关闭 agent manager
- 服务启动时，如果 `config.json.enabled=true` 且配置校验通过，后台自动启动 monitor。
- 服务启动时自动启动失败不得阻塞 Lumi 启动，只写入 `runtime.json.lastError`，并在 `/api/wechat/status` 暴露。

### 13.2 单例规则

一个进程内只允许 1 个 monitor goroutine。

- 已运行时再次 `enable`，直接返回成功，不重复启动
- 已停止时再次 `disable`，直接返回成功

### 13.3 长轮询规则

固定使用：

- `ilink/bot/getupdates`
- 当前 `runtime.json.buf`

处理规则固定为：

- 请求成功后，更新 `lastSyncAt`
- 响应中包含新的 `get_updates_buf` 时，立即持久化到 `runtime.json.buf`
- 每条消息处理前先检查 `runtime.json.processedMessageIds`
- 正常运行优先，不追求 crash 场景严格 exactly-once

### 13.4 错误与退避

monitor 发生错误时固定处理为：

- 更新 `lastError`
- 保持服务处于已启用状态
- 退避后重试

退避规则固定为：

- 连续失败少于 3 次：2 秒后重试
- 连续失败达到 3 次：30 秒后重试

### 13.5 Typing 规则

V1 实现基础 typing：

- gateway 调用 agent 前，monitor 为当前 `from_user_id` 获取 `typing_ticket` 并发送 `TYPING`。
- agent 执行期间每 10 秒补发一次 `TYPING`。
- 回复发送完成、agent 失败、context 取消或 monitor 停止时，尽量发送一次 `CANCEL`。
- `getconfig` 或 `sendtyping` 失败只记录日志，不影响主聊天链路。
- 同一 `from_user_id` 新 typing 会先取消旧 typing。

### 13.6 monitor 循环图

```text
POST /api/wechat/enable
    |
    v
service.Start()
    |
    v
single monitor goroutine
    |
    v
load runtime.json.buf
    |
    v
call ilink/bot/getupdates
    |
    +-- success -------------------------------+
    |                                          |
    |  update lastSyncAt                       |
    |  persist new get_updates_buf if present  |
    |  parse messages                          |
    |  dispatch to gateway                     |
    |  reset failure count                     |
    |                                          |
    +-- error ---------------------------------+
       update lastError
       failure count += 1
       wait 2s or 30s
       |
       v
repeat until disable/shutdown
```

---

## 14. 微信网关链路

微信入站处理链路固定为：

1. monitor 长轮询获取微信消息
2. 解析为 `WeChatInboundMessage`
3. 校验微信功能已启用且配置完整
4. 校验 `workspaceId` 仍存在且为 `local`
5. 校验 `agentId` 仍存在
6. 生成隐藏 `conversationID`
7. 处理附件并注入附件 prompt
8. 组装 `wechat.ChatRunInput`
9. 调用微信私有聊天运行器
10. 聚合运行器事件为 `WeChatReply`
11. 解析并发送协议块中的媒体
12. 发送剩余可见文本

微信来源的 `ChatRunInput` 固定为：

```go
wechat.ChatRunInput{
    Message:             messageWithAttachmentBlock,
    ConversationID:      conversationID,
    WorkspaceID:         wechatConfig.WorkspaceID,
    WorkspacePath:       workspacePath,
    AgentID:             wechatConfig.AgentID,
    Files:               files,
    PromptPrefix:        wechatSourceInstruction,
    SessionModeOverride: derivedMode,
    ConversationStore:   wechatConversationStore,
}
```

`messageWithAttachmentBlock` 是用户原文加附件说明块，不包含 `wechatSourceInstruction`。`PromptPrefix` 只用于发给 agent，不作为用户原始消息写入隐藏会话。

其中 `wechatSourceInstruction` 固定包含：

- 微信附件说明
- `LUMI_WECHAT_SEND` 协议说明

### 14.1 端到端网关图

```text
WeChat user
    |
    v
WeChat API getupdates
    |
    v
monitor.go
    |
    v
parse WeChatInboundMessage
    |
    v
gateway.go
    |
    +--> validate enabled/config/workspace/agent
    |
    +--> derive hidden conversationID
    |
    +--> download attachments
    |
    +--> build prompt prefix
    |
    +--> call WeChat private chat runner
    |        ConversationID=wx_xxx
    |        WorkspaceID=config.workspaceId
    |        AgentID=config.agentId
    |        Store=wechat hidden store
    |
    +--> aggregate update/done/error events
    |
    +--> auto-confirm permission_request if needed
    |
    +--> parse LUMI_WECHAT_SEND blocks
    |
    +--> send media + visible text with context_token
    |
    v
WeChat user receives reply
```

---

## 15. 后端 API

### 15.1 `GET /api/wechat/config`

返回结构固定为：

```json
{
  "enabled": false,
  "loginMode": "qr",
  "accountId": "xxx",
  "baseUrl": "https://ilinkai.weixin.qq.com",
  "workspaceId": "default",
  "agentId": "claude",
  "hasToken": true,
  "maskedToken": "abcd********wxyz"
}
```

规则固定为：

- 不返回明文 `botToken`
- `maskedToken` 少于 8 位时，直接返回 `********`

### 15.2 `POST /api/wechat/config`

请求结构固定为：

```json
{
  "enabled": true,
  "loginMode": "manual",
  "accountId": "xxx",
  "botToken": "optional-or-empty",
  "baseUrl": "https://ilinkai.weixin.qq.com",
  "workspaceId": "default",
  "agentId": "claude"
}
```

字段语义固定为：

- `botToken` 字段缺失：保持原值
- `botToken` 为非空字符串：覆盖原值
- `botToken` 为 `""`：清空原值

校验规则固定为：

- `loginMode` 只允许 `qr` 或 `manual`
- `workspaceId` 必须存在
- `workspace.kind` 必须为空或 `local`
- `agentId` 必须存在
- `baseUrl` 为空时回退默认值
- 保存配置只落盘，不启动 monitor，不停止 monitor。
- 如果保存时 `enabled=false` 且 monitor 当前正在运行，不自动停止；停止必须由 `POST /api/wechat/disable` 完成。
- 如果保存时 `enabled=true`，也不自动启动；启动必须由 `POST /api/wechat/enable` 或服务重启自动恢复完成。

成功返回：

```json
{"success":true,"config":{...sanitized config...}}
```

### 15.3 `GET /api/wechat/status`

返回结构固定为：

```json
{
  "running": false,
  "configured": true,
  "configError": "",
  "lastError": "",
  "lastSyncAt": 0,
  "lastLoginAt": 0,
  "lastMessageAt": 0
}
```

规则固定为：

- `configured` 表示当前微信配置是否通过校验
- `configError` 在 `configured=false` 时返回明确错误文案

### 15.4 `POST /api/wechat/test`

该接口固定执行：

1. 配置校验
2. 使用当前 `accountId`、`botToken`、`baseUrl` 发起一次短超时 `getupdates` 请求
3. 不持久化测试返回的 `get_updates_buf`

成功返回：

```json
{"success":true,"message":"connection ok"}
```

失败返回：

```json
{"success":false,"error":"具体错误文案"}
```

### 15.5 `POST /api/wechat/enable`

成功返回：

```json
{"success":true}
```

如果配置非法，返回错误并且不启动 monitor。

成功后固定同时保存 `config.json.enabled=true`。

### 15.6 `POST /api/wechat/disable`

成功返回：

```json
{"success":true}
```

该接口幂等。

成功后固定同时保存 `config.json.enabled=false`。

### 15.7 `POST /api/wechat/login/start`

返回结构固定为：

```json
{"loginId":"wxlogin_xxx"}
```

### 15.8 `GET /api/wechat/login/events?id=<loginId>`

该接口返回 SSE 流，事件定义见第 12 节。

### 15.9 错误状态码

微信管理 API 错误语义固定如下：

| 场景 | HTTP status | body |
|---|---:|---|
| JSON 解析失败 | 400 | `{"error":"Invalid request"}` |
| 参数非法 | 400 | `{"error":"具体错误文案"}` |
| 配置非法导致无法 enable | 400 | `{"error":"具体错误文案"}` |
| `test` 连接微信失败 | 200 | `{"success":false,"error":"具体错误文案"}` |
| `login/events` 的登录流程失败 | 200 SSE | `event:error` |
| store 读写失败 | 500 | `{"error":"具体错误文案"}` |
| monitor 启停内部错误 | 500 | `{"error":"具体错误文案"}` |

普通成功响应均为 2xx。前端 API 封装必须同时支持非 2xx error body 和 `test` 的 `success:false`。

### 15.10 API 分组图

```text
/api/wechat
|
|-- config
|     |-- GET   read sanitized config
|     `-- POST  validate and save config
|
|-- status
|     `-- GET   config validation + runtime state
|
|-- test
|     `-- POST  short getupdates probe, no buf persistence
|
|-- enable
|     `-- POST  validate config, start monitor
|
|-- disable
|     `-- POST  stop monitor, idempotent
|
`-- login
      |-- start
      |     `-- POST create/cancel login task
      |
      `-- events?id=<loginId>
            `-- GET SSE: qr/scanned/confirmed/expired/error/done
```

---

## 16. 前端实现

### 16.1 页面入口

微信设置固定接入：

- `web/src/features/settings/settings-modal.tsx`

不修改旧 Vue 设置弹窗作为本功能入口。

### 16.2 前端类型

在 `web/src/lib/types.ts` 中新增：

- `WeChatConfig`
- `WeChatStatus`
- `WeChatLoginEvent`

固定定义为：

```ts
export interface WeChatConfig {
  enabled: boolean
  loginMode: 'qr' | 'manual'
  accountId: string
  baseUrl: string
  workspaceId: string
  agentId: string
  hasToken: boolean
  maskedToken?: string
}

export interface WeChatStatus {
  running: boolean
  configured: boolean
  configError?: string
  lastError?: string
  lastSyncAt?: number
  lastLoginAt?: number
  lastMessageAt?: number
}

export type WeChatLoginEvent =
  | { type: 'qr'; ticket: string; imageUrl: string }
  | { type: 'scanned' }
  | { type: 'confirmed'; accountId: string; baseUrl: string; hasToken: boolean }
  | { type: 'expired' }
  | { type: 'error'; message: string }
  | { type: 'done' }
```

### 16.3 前端 API 封装

在 `web/src/lib/api.ts` 中新增：

- `fetchWeChatConfig()`
- `saveWeChatConfig()`
- `fetchWeChatStatus()`
- `startWeChatLogin()`
- `subscribeWeChatLogin()`
- `testWeChatConnection()`
- `enableWeChat()`
- `disableWeChat()`

### 16.4 前端交互规则

前端界面固定提供：

- 启用 / 禁用微信入口
- 扫码登录
- 手工填写 `accountId` / `botToken` / `baseUrl`
- 选择绑定 workspace
- 选择默认 agent
- 展示运行状态
- 展示最近错误
- 展示最后同步时间
- 展示最后登录时间
- 展示是否已保存 token
- 测试连接

布局规则固定为：

- 在现有 `SettingsModal` 滚动内容中新增一个 `WeChat` 分区，不新增独立页面，不重构为 tabs。
- `WeChat` 分区显示运行状态、配置错误、启用/禁用、扫码登录、测试连接。
- 手工 token、baseUrl、workspace、agent 选择放在高级配置区域，可折叠。
- `qr` 事件优先展示 `imageUrl`；`ticket` 只用于调试和兼容，不要求前端生成二维码。
- `POST /api/wechat/config` 保存成功后刷新 config/status，但不假设 monitor 已启动。

workspace 下拉框固定只展示：

- 当前 `fetchWorkspaces()` 返回的 workspace
- 且 `kind !== "remote"`

### 16.5 前端交互图

```text
settings-modal.tsx
    |
    +--> fetchWeChatConfig()
    |        |
    |        v
    |   render account/login/binding form
    |
    +--> fetchWeChatStatus()
    |        |
    |        v
    |   render running/configured/errors/timestamps
    |
    +--> saveWeChatConfig()
    |        |
    |        v
    |   POST /api/wechat/config
    |
    +--> startWeChatLogin()
    |        |
    |        v
    |   POST /api/wechat/login/start
    |        |
    |        v
    |   subscribeWeChatLogin(loginId)
    |        |
    |        v
    |   render QR and login progress
    |
    +--> testWeChatConnection()
    |
    +--> enableWeChat() / disableWeChat()

workspace selector
    |
    v
fetchWorkspaces() -> filter kind != "remote"
```

### 16.6 Settings UI 结构

微信设置只作为当前 Settings 弹窗内的一个纵向 section，不新增 route、不新增独立弹窗、不把 Settings 改成 tabs。

整体结构固定为：

```text
+--------------------------------------------------+
| Settings                                         |
+--------------------------------------------------+
|                                                  |
| [Agents]                                         |
|   agent cards / permission / env                 |
|                                                  |
| ------------------------------------------------ |
|                                                  |
| [WeChat]                 Status: Running/Stopped |
|                                                  |
|   Config status / last error                     |
|   Enable / Disable / Test                        |
|                                                  |
|   Login                                          |
|   QR login / manual token summary                |
|                                                  |
|   Binding                                        |
|   Workspace select / Agent select                |
|                                                  |
|   Advanced config          [Show/Hide]           |
|   accountId / botToken / baseUrl                 |
|                                                  |
| ------------------------------------------------ |
|                                                  |
| [Appearance / Language]                          |
|                                                  |
+--------------------------------------------------+
```

WeChat section 首屏固定展示：

```text
[WeChat]                                      [Stopped]

Config: [Config error]

+--------------------------------------------------+
| Error: workspace is required                     |
|                                                  |
| Status                                           |
| Running       No                                 |
| Last sync     -                                  |
| Last login    2026-04-28 14:20                   |
| Last message  -                                  |
|                                                  |
| Actions                                          |
| [Save] [Test connection] [Enable] [Disable]      |
+--------------------------------------------------+
```

状态 badge 固定含义：

- `[Running]`：`status.running=true`
- `[Stopped]`：`status.running=false`
- `[OK]`：`status.configured=true`
- `[Config error]`：`status.configured=false`
- `[Testing...]`：前端正在执行 `POST /api/wechat/test`

时间格式固定为本地短格式：

```text
YYYY-MM-DD HH:mm
```

空时间显示为 `-`。

按钮启用规则固定为：

| Button | Enabled when |
|---|---|
| Save | 表单已加载，且当前没有保存中请求 |
| Test connection | `configured=true` 且 `hasToken=true` 且当前没有测试中请求 |
| Enable | `configured=true` 且 `running=false` 且当前没有启用中请求 |
| Disable | `running=true` 且当前没有禁用中请求 |

### 16.7 Login UI

扫码登录放在 WeChat section 内，不开新窗口。

```text
Login
+--------------------------------------------------+
| Mode:  (x) QR login   ( ) Manual token           |
|                                                  |
| [Start QR Login]                                 |
|                                                  |
| QR status: waiting / scanned / confirmed / error |
|                                                  |
| +----------------------+                         |
| |                      |                         |
| |      QR IMAGE        |                         |
| |                      |                         |
| +----------------------+                         |
|                                                  |
| ticket: wxlogin...                               |
+--------------------------------------------------+
```

登录事件处理固定为：

```text
POST /api/wechat/login/start
        |
        v
GET /api/wechat/login/events?id=...
        |
        +-- qr        -> 显示 imageUrl，保存 ticket 到小字调试区
        +-- scanned   -> 状态显示 scanned
        +-- confirmed -> 更新 accountId/baseUrl/hasToken UI 状态
        +-- expired   -> 清空 QR，提示重新开始
        +-- error     -> 显示错误
        `-- done      -> 关闭 EventSource，刷新 config/status
```

Settings 弹窗关闭或组件 unmount 时必须执行：

```text
eventSource.close()
```

### 16.8 Binding UI

绑定区域固定为：

```text
Binding
+--------------------------------------------------+
| Workspace     [ Default workspace        v ]     |
| Agent         [ Claude Code              v ]     |
+--------------------------------------------------+
```

workspace 下拉框过滤规则固定为：

```text
fetchWorkspaces()
  -> keep workspace where kind is empty or kind == "local"
  -> exclude kind == "remote"
```

如果已保存的 workspace 或 agent 已不存在，后端 `/api/wechat/status` 返回 `configError`，前端只展示错误，不自行猜默认值覆盖配置。

### 16.9 Advanced Config UI

手工配置放在折叠区，默认收起。

```text
Advanced config                         [ Show ]
```

展开后固定为：

```text
Advanced config                         [ Hide ]
+--------------------------------------------------+
| Account ID    [ wx_xxxxxxxxx                 ]   |
| Bot Token     [                            ]     |
|               saved token: abcd********wxyz     |
| Base URL      [ https://ilinkai.weixin.qq.com ]  |
| Login Mode    [ qr v ]                           |
|                                                  |
| [Clear saved token]                              |
+--------------------------------------------------+
```

`botToken` 输入规则固定为：

- 普通保存时，如果 token input 为空，不发送 `botToken` 字段。
- token input 非空时，发送 `botToken=<input>`，覆盖旧 token。
- 点击 `Clear saved token` 时，发送 `botToken=""`，清空旧 token。

输入 placeholder 固定为：

```text
token saved, leave blank to keep
```

### 16.10 保存与启用流程

保存配置和启用 monitor 是两个独立动作。

```text
User edits form
   |
   v
[Save]
   |
   v
POST /api/wechat/config
   |
   v
refresh config/status
   |
   v
User clicks [Enable]
   |
   v
POST /api/wechat/enable
   |
   v
refresh status
```

前端禁止假设 `Save` 会启动 monitor。

### 16.11 错误展示

WeChat section 必须有局部错误展示，不复用 agent 设置的全局错误作为唯一错误出口。

错误来源包括：

- `GET /api/wechat/status` 的 `configError`
- `GET /api/wechat/status` 的 `lastError`
- `POST /api/wechat/config` 非 2xx
- `POST /api/wechat/test` 的 `success:false`
- `POST /api/wechat/enable` 非 2xx
- login SSE 的 `event:error`

展示优先级固定为：

```text
operationError > status.configError > status.lastError
```

局部错误样式固定为当前 Settings 中已有错误样式的同类实现，不新增全局 toast 依赖。

### 16.12 API 封装形态

`subscribeWeChatLogin()` 固定返回 cleanup function：

```ts
const unsubscribe = subscribeWeChatLogin(loginId, {
  onQR,
  onScanned,
  onConfirmed,
  onExpired,
  onError,
  onDone,
})

unsubscribe()
```

`saveWeChatConfig()` 的输入类型必须允许 `botToken` 字段缺失：

```ts
type SaveWeChatConfigInput = Omit<WeChatConfig, 'hasToken' | 'maskedToken'> & {
  botToken?: string
}
```

### 16.13 最终 UI 草图

```text
+----------------------------------------------------------------+
| Settings                                                       |
+----------------------------------------------------------------+
|                                                                |
| AGENTS                                                         |
| +------------------------------------------------------------+ |
| | Claude Code                                                | |
| | Permission: [Default] [Bypass]                             | |
| | Command: npx @anthropics/claude-code --acp                 | |
| | Env: [3 variables v]                                       | |
| +------------------------------------------------------------+ |
|                                                                |
| WECHAT                                      [Stopped] [Config error]
| +------------------------------------------------------------+ |
| | Config error: workspace is required                        | |
| |                                                            | |
| | Status                                                     | |
| | Running       No                                           | |
| | Last sync     -                                            | |
| | Last login    2026-04-28 14:20                             | |
| | Last message  -                                            | |
| |                                                            | |
| | Actions                                                    | |
| | [Save] [Test connection] [Enable] [Disable]                | |
| |                                                            | |
| | Login                                                      | |
| | Mode: [QR login v]                                         | |
| | [Start QR Login]                                           | |
| |                                                            | |
| | +----------------------+                                   | |
| | |                      |                                   | |
| | |       QR IMAGE       |                                   | |
| | |                      |                                   | |
| | +----------------------+                                   | |
| | Login status: waiting for scan                             | |
| |                                                            | |
| | Binding                                                    | |
| | Workspace  [Default workspace        v]                    | |
| | Agent      [Claude Code              v]                    | |
| |                                                            | |
| | Advanced config                                    [Show]   | |
| +------------------------------------------------------------+ |
|                                                                |
| APPEARANCE                                                     |
| Language: [English v]      Theme: [Dark/Light]                 |
|                                                                |
+----------------------------------------------------------------+
```

---

## 17. 实施顺序

### 阶段 1：建立微信私有聊天运行器

阶段 1 固定不抽共享 runtime，不拆 Web `chat.go` 主链。必须拆成 5 个小提交级步骤：

1. 新增 `backend/internal/wechat/chat_contract.go`
   - 定义 `ChatRunner`、`ChatRunInput`、`ChatEvent`、`ChatEventSink`、`HiddenConversationStore`。
   - 不 import `backend/internal/api`。
   - 不改变现有 `/api/chat`。
2. 新增 `backend/internal/api/wechat_chat.go`
   - 实现 `wechat.ChatRunner`。
   - 内部使用独立 `agent.Manager`。
   - 内部使用独立 `conversation.Manager`。
   - 内部使用微信私有 `agentSessions` 和 `initialized`。
   - 只复制 local chat 必要逻辑，不接 device path。
3. 调整 permission pending 顺序
   - 只改 `agent.Process.handlePermissionRequest()`：先登记 pending，再通知 handler。
   - 加测试保证 Web `/api/permission/confirm` 仍然有效。
4. 接入隐藏会话 store 接口
   - 私有运行器只通过 `HiddenConversationStore` 接口读写隐藏会话。
   - Phase 1 测试可使用 stub store。
   - 真实 `conversation_store.go` 在 Phase 2 落地。
   - 不读写普通 `SessionStore`。
   - 不读写 Web `s.conversations`。
5. 补 Phase 1 回归测试
   - 确认 Web local/device/permission/tool_call/session 行为不变。
   - 确认微信 hidden conversation 不进入普通 `SessionStore`。
   - 确认 Web 不能读取微信 hidden conversation。
   - 确认微信私有 runner 使用独立 agent manager。

阶段 1 完成前，不允许开始真实 login、monitor、附件或前端 UI。

### 阶段 2：落地微信 store 和后端 API

- 完成 `config_store.go`
- 完成 `runtime_store.go`
- 完成 `conversation_store.go`
- 完成 `/api/wechat/config`
- 完成 `/api/wechat/status`

### 阶段 3：落地扫码登录和 monitor

- 完成 `login.go`
- 完成 `client.go`
- 完成 `monitor.go`
- 完成 `/api/wechat/login/start`
- 完成 `/api/wechat/login/events`
- 完成 `/api/wechat/enable`
- 完成 `/api/wechat/disable`

### 阶段 4：打通隐藏聊天链路

- 完成 `gateway.go`
- gateway 调用阶段 1 的微信私有聊天运行器
- 接入隐藏 conversation
- 接入 session mode override
- 接入自动权限
- 打通纯文本微信聊天

### 阶段 5：打通附件和文件回传

- 下载入站附件
- 注入附件 prompt
- 解析 `LUMI_WECHAT_SEND`
- 回传图片和文件
- 完成越界和失败处理

### 阶段 6：完成 React 设置页

- 扩展 `settings-modal.tsx`
- 增加微信设置 UI
- 接入登录 SSE
- 接入状态刷新和测试连接

### 17.1 实施依赖图

```text
Phase 1.1
wechat chat contract only
    |
    v
Phase 1.2
api/wechat_chat.go private runner
    |
    v
Phase 1.3
permission pending order fix
    |
    v
Phase 1.4
hidden-store interface hooks
    |
    v
Phase 1.5
Web and private-runner tests
    |
    v
Phase 2
wechat stores + config/status APIs
    |
    v
Phase 3
client + login + monitor lifecycle APIs
    |
    v
Phase 4
gateway + hidden conversation + auto permission
    |
    v
Phase 5
attachments + LUMI_WECHAT_SEND reply protocol
    |
    v
Phase 6
React settings UI

Critical dependency:
  WeChat gateway must call the private runner from Phase 1.4+.
  Hidden conversations must use stores from Phase 2.
  UI can be completed after backend APIs are stable.
```

### 17.2 文件级实施清单

阶段实现时按下面文件边界拆分，避免开发者自行重组：

- `backend/internal/wechat/chat_contract.go`
  - 只放微信聊天接口和 DTO。
  - 不 import `backend/internal/api`。
- `backend/internal/api/wechat_chat.go`
  - 实现微信私有 runner。
  - 拥有独立 agent manager、conversation manager、agent session map、initialized map。
  - 复制 Web local chat 必要逻辑，不接 `/api/chat`、device path、普通 `SessionStore`。
- `backend/internal/wechat/config_store.go`
  - 读写 `~/.lumi/wechat/config.json`。
  - 负责默认值、token masking、0600 写入。
- `backend/internal/wechat/runtime_store.go`
  - 读写 `runtime.json`。
  - 负责 `buf`、`last*`、`processedMessageIds` 最近 500 条裁剪。
- `backend/internal/wechat/conversation_store.go`
  - 读写隐藏会话文件。
  - 不依赖普通 `storage.SessionStore` 的目录。
- `backend/internal/wechat/client.go`
  - 封装第 12.7 节所有微信 HTTP API、CDN 上传下载、AES-128-ECB。
  - 只返回结构化错误，不直接操作 conversation。
- `backend/internal/wechat/login.go`
  - 管理单例登录任务和 login SSE 事件。
  - `qr` 事件返回 `ticket` 和 `imageUrl`。
- `backend/internal/wechat/monitor.go`
  - 单例长轮询、buf 持久化、消息去重、typing 生命周期、发送回复。
- `backend/internal/wechat/gateway.go`
  - 校验配置、workspace、agent。
  - 生成 `wx_` conversationID，下载附件，调用 `ChatRunner`，解析回传协议。
- `backend/internal/wechat/send_protocol.go`
  - 只解析 `[LUMI_WECHAT_SEND]`，校验 workspace 内路径。
- `backend/internal/wechat/handlers.go`
  - 注册 `/api/wechat/*` 管理接口。
  - 遵守第 15.9 节 HTTP status 语义。
- `backend/internal/api/server.go`
  - `NewServer()` 初始化 WeChat service。
  - `Handler()` 注册 `/api/wechat/*`。
  - `Shutdown()` 先停 WeChat monitor/private agent manager，再停 Web agent manager。
- `backend/internal/api/handlers.go`
  - `/api/agents/update` 同时停止对应微信私有 agent process，清理微信私有 session/initialized 状态。
- `web/src/lib/types.ts`、`web/src/lib/api.ts`、`web/src/features/settings/settings-modal.tsx`
  - 只接 React 设置弹窗，不改旧 Vue。

### 17.3 测试清单

V1 测试以单元测试为主，不强制完整 fake WeChat E2E 或 Playwright。

必须补充：

- `go test ./internal/...` 必须通过。
- `agent.Process.handlePermissionRequest()`：handler 内立即确认也能成功返回 selected outcome。
- Web `/api/permission/confirm` 回归：仍可确认本机 agent 和 device permission。
- 微信 config store：默认值、token masking、缺失 token 保持、空 token 清空、0600 写入。
- 微信 runtime store：`buf` 持久化、最近 500 个 `processedMessageIds` 裁剪、重复判断。
- 微信 conversation store：隐藏会话不进入普通 `/api/sessions`，恢复时保留 tool call/files/timestamp。
- 微信 client：用 `httptest` 覆盖 QR、status、getupdates、sendmessage、getuploadurl 的 request/response shape。
- send protocol：多段协议块、非法 JSON、路径越界、软链越界、文件缺失、文件过大。
- gateway：纯文本入站、附件部分失败继续、全部附件失败且无文本直接回复错误、忙碌消息写入去重。
- private runner：不调用 Web `/api/chat` 相关方法，不读写普通 `SessionStore`，不复用 `Server.agents`。

可手工验收：

- 真实扫码登录。
- 真实微信纯文本消息。
- 真实图片/文件入站。
- 真实图片/文件回传。
- React settings UI 的视觉和基本操作。

---

## 18. 验收标准

### 18.1 Web 主链零回归

- `/api/chat` 的 SSE 事件名保持不变
- 现有 Web 聊天行为不回归
- 现有 tool call 展示不回归
- 现有 permission 行为不回归
- 现有 device chat 行为不回归
- 现有 remote workspace `@file` 行为不回归
- 现有 `@agent` 切换行为不回归
- Web 未知 `conversationId` 的处理行为不回归
- Web 手工 `/api/permission/confirm` 行为不回归
- Web 不能读取或复用微信 hidden conversation

### 18.2 配置与状态

- 微信配置可正常 round-trip
- `GET /api/wechat/config` 不泄露明文 token
- 绑定 remote workspace 时保存失败
- agent 或 workspace 删除后，`GET /api/wechat/status` 返回明确 `configError`

### 18.3 登录与 monitor

- 扫码登录能收到 `qr -> scanned -> confirmed -> done`
- `qr` 事件包含 `ticket` 和 `imageUrl`，不包含 `expiresAt`
- 登录成功后凭据自动保存
- 登录成功后 monitor 不自动启动
- `enable` 后 monitor 启动
- `disable` 后 monitor 停止
- 服务重启时，如果 `enabled=true` 且配置合法，monitor 自动恢复
- 服务关闭时 monitor 必停
- typing 发送失败不影响聊天回复

### 18.4 微信聊天

- 微信纯文本消息能创建隐藏 conversation 并收到最终回复
- 同一微信用户再次发消息时复用同一隐藏 `conversationID`
- 重复 `msg_id` 不会重复调用 agent 或重复回复
- agent 失败、发送失败、忙碌回复后，该 `msg_id` 均进入去重记录
- 同一进程内可复用本地 `agentSessionID`
- 服务重启后可恢复 conversation history
- 服务重启后允许新的 `agentSessionID`
- 同一会话并发消息返回固定忙碌提示

### 18.5 自动权限

- 微信会话默认自动权限
- Web 会话权限行为不受影响
- 没有 allow 选项时，微信请求稳定失败并返回错误文案

### 18.6 入站附件

- 微信图片能下载到 `workspace/.lumi-uploads/wechat/...`
- 微信文件能下载到受控目录
- prompt 中能看到明确附件路径
- 部分附件下载失败时仍处理文本和成功附件，并在 prompt 中说明失败附件
- 全部附件失败且无文本时，不调用 agent，直接回复附件处理失败
- 非法文件名会被清洗
- 超大附件会被拒绝

### 18.7 文件回传

- workspace 内图片能正常回传
- workspace 内普通文件能正常回传
- 相对路径和绝对路径都能正确解析
- 路径越界会被拒绝
- 软链越界会被拒绝
- 文件缺失不会导致整条消息失败

### 18.8 会话隔离

- `/api/sessions` 看不到微信隐藏会话
- 删除普通 Web session 不影响微信隐藏会话
- 微信隐藏会话不参与分享逻辑

### 18.9 验收覆盖图

```text
                 +----------------+
                 | Web zero       |
                 | regression     |
                 +--------+-------+
                          |
                          v
+-------------+    +------+-------+    +----------------+
| config and  |--->| WeChat V1    |<---| login and      |
| status      |    | acceptance   |    | monitor        |
+-------------+    +------+-------+    +----------------+
                          |
                          v
+-------------+    +------+-------+    +----------------+
| hidden      |--->| private      |<---| auto           |
| sessions    |    | runner path  |    | permissions    |
+-------------+    +------+-------+    +----------------+
                          |
                          v
                 +--------+-------+
                 | attachments and|
                 | file replies   |
                 +----------------+
```

---

## 19. 最终结论

Lumi 微信入口 V1 的最终实现原则固定为：

- 不造通用 channels 平台
- V1 不先抽共享 runtime，先做微信私有聊天运行器
- 允许受控冗余，V2 再评估合并重复代码
- 微信配置独立存储
- 微信会话独立落盘
- 自动权限只作用于微信独立 session
- 附件和文件回传必须走显式路径和显式协议
- 前端只改当前 React 设置页

后续实现必须以本方案为准，不再在代码实现阶段新增产品决策。

---

## 20. 参考项目能力对齐说明

本节固定说明 Lumi 微信 V1 与参考项目 `/Users/pengmd/c/Ditto/main` 的微信对接能力关系，避免实现同学误以为需要照搬参考项目。

### 20.1 参考项目微信能力清单

参考项目微信相关能力集中在：

- `WeixinLogin.ts`
  - 调用 `get_bot_qrcode` 获取二维码。
  - 调用 `get_qrcode_status` 轮询扫码状态。
  - 登录成功后得到 `accountId`、`botToken`、`baseUrl`。
- `WeixinLoginHandler.ts`
  - 管理 Electron 登录窗口和登录状态事件。
- `WeixinMonitor.ts`
  - 使用 `getupdates` 长轮询。
  - 持久化 `get_updates_buf`。
  - 解析文本、语音转文字、图片、文件消息。
  - 下载微信 CDN 附件。
  - 调用 channel agent 获取回复。
  - 发送文本回复。
  - 通过 `getuploadurl`、CDN AES 上传、`sendmessage` 回传图片/文件。
- `WeixinTyping.ts`
  - 调用 `getconfig` 获取 `typing_ticket`。
  - 调用 `sendtyping` 周期性发送正在输入状态。
- `WeixinPlugin.ts`
  - 接入通用 `BasePlugin` 生命周期。
  - 把微信入站消息桥接到通用 channel message。
  - 聚合通用 channel 的文本和媒体输出，再交回 `WeixinMonitor` 发送。
  - 维护 pending response、active user count、plugin stop 清理。
- `WeixinAdapter.ts`
  - 将微信消息转换为通用 `IUnifiedIncomingMessage`。
  - 清理 HTML 文本。
- `channelSendProtocol.ts`
  - 解析通用 `[AIONUI_CHANNEL_SEND]` 协议块。
  - 校验 workspace 内文件路径、软链、文件大小。

### 20.2 完整对齐的能力

以下能力按本方案实现完成后，与参考项目能力完整对齐：

| 参考项目能力 | Lumi V1 对齐方式 |
|---|---|
| 扫码登录二维码获取 | `wechat/login.go` 调用 `get_bot_qrcode`，SSE `qr` 返回 `ticket` 和 `imageUrl` |
| 扫码状态轮询 | `wechat/login.go` 调用 `get_qrcode_status`，输出 `scanned`、`confirmed`、`expired`、`error`、`done` |
| 手工凭据配置 | `/api/wechat/config` 支持 `accountId`、`botToken`、`baseUrl` |
| `getupdates` 长轮询 | `wechat/monitor.go` 使用 `runtime.json.buf` 调用 `ilink/bot/getupdates` |
| `get_updates_buf` 持久化 | `runtime.json.buf` 固定保存最新 buf |
| 文本消息解析 | `monitor.go` 解析 text item |
| 语音转文本解析 | `monitor.go` 解析 voice text item 并合并为文本 |
| 图片/文件附件下载 | `client.go` 通过微信 CDN 下载并按 AES-128-ECB 解密 |
| 文本回复发送 | `client.go` 调用 `ilink/bot/sendmessage` 文本 item |
| 图片/文件回传 | `client.go` 调用 `getuploadurl`、CDN 上传、`sendmessage` media item |
| 正在输入提示 | `monitor.go`/typing helper 调用 `getconfig` 和 `sendtyping` |
| 文件回传路径安全校验 | `send_protocol.go` 校验 workspace 内 realpath、软链、普通文件和大小 |

### 20.3 部分对齐的能力

以下能力只对齐核心功能，不照搬参考项目完整框架：

| 参考项目能力 | Lumi V1 状态 | 差异 |
|---|---|---|
| 通用 `BasePlugin` 生命周期 | 部分对齐 | Lumi 不引入 `BasePlugin`，改为 `wechat.Service Start/Stop/Status` |
| 通用 channel message 桥接 | 部分对齐 | Lumi 不生成 `IUnifiedIncomingMessage`，直接生成 `wechat.ChatRunInput` |
| pending response 聚合 | 部分对齐 | Lumi 不走 channel pending map，改为私有 runner event sink 聚合 |
| active user count | 部分对齐 | Lumi 只需要 monitor/status，不实现参考项目 plugin 统计接口 |
| channel media send protocol | 部分对齐 | Lumi 使用 `[LUMI_WECHAT_SEND]`，不使用 `[AIONUI_CHANNEL_SEND]` |
| testConnection | 部分对齐 | 参考项目检查 buf 文件；Lumi V1 实际调用短超时 `getupdates` 探测 |
| typing ticket 缓存 | 部分对齐 | Lumi V1 做基础 typing，缓存策略可简单实现；typing 失败不影响主链 |

### 20.4 不实现的参考项目能力

以下能力 V1 明确不实现：

- 不引入 `ChannelManager`。
- 不引入 `PluginManager`。
- 不引入 `BasePlugin`。
- 不引入多平台统一 channel 框架。
- 不引入通用 channel session。
- 不实现 channel action `session.new`。
- 不实现微信侧手动新建或切换会话。
- 不实现 Electron 隐藏窗口或 Electron 专用二维码渲染。
- 不实现参考项目的多 channel pairing/authorization。
- 不实现参考项目数据库 conversation lookup：`source + channelChatId + type + backend`。
- 不实现参考项目的 HTML message adapter；Lumi 只处理 agent 输出文本和显式协议块。

### 20.5 Lumi 本地化适配

以下设计是基于 Lumi 当前项目结构做的本地化适配，不是参考项目原样搬运：

- **隐藏会话模型**
  - 参考项目：微信消息进入通用 conversation/channel session。
  - Lumi：`from_user_id -> wx_<sha1>`，隐藏会话落盘到 `~/.lumi/wechat/sessions/`，不进入 Web sidebar。
- **私有聊天运行器**
  - 参考项目：微信通过通用 channel 框架调用应用内部会话系统。
  - Lumi：新增 `api/wechat_chat.go` 私有 runner，复制 local chat 必要逻辑，避免改 `/api/chat`。
- **独立 agent manager**
  - 参考项目：插件接入统一后端。
  - Lumi：微信独立 `agent.Manager`、`conversation.Manager`、`agentSessions`、`initialized`，避免 notification/permission 串流污染 Web。
- **配置存储**
  - 参考项目：走 channel/plugin 配置。
  - Lumi：微信配置单独保存到 `~/.lumi/wechat/config.json`，不写主 `lumi.config.json`。
- **运行状态存储**
  - 参考项目：buf 文件按账号存储。
  - Lumi：`runtime.json` 同时保存 `buf`、last timestamps、`processedMessageIds`。
- **去重策略**
  - 参考项目：主要依赖 `get_updates_buf`。
  - Lumi：额外持久化最近 500 个 `msg_id`，避免重复调用 agent。
- **回传协议名**
  - 参考项目：`[AIONUI_CHANNEL_SEND]`。
  - Lumi：`[LUMI_WECHAT_SEND]`，只在微信私有输出链路解析。
- **权限策略**
  - 参考项目：由统一 channel/backend 行为决定。
  - Lumi：微信 session mode 固定按 agent 推导，并在微信私有 agent process 内自动确认 allow option。
- **前端入口**
  - 参考项目：Electron/插件 UI。
  - Lumi：只接入当前 React `settings-modal.tsx`，不改旧 Vue。

---

## 21. 对现有功能影响与隔离说明

本节固定说明微信 V1 是否是全新逻辑、哪些旧代码会被复用或修改、以及如何保证不影响原有 Web 功能。

### 21.1 结论

微信 V1 是一条新增入口和新增运行链路，但不是绝对零触碰旧代码。

固定结论为：

- 微信聊天数据面是全新链路。
- Web `/api/chat` 主链不重构、不抽 shared runtime、不改 SSE event shape。
- 微信隐藏会话不进入 Web `SessionStore`。
- 微信 agent process 与 Web agent process 分离。
- 旧功能影响面只允许出现在明确列出的共享挂点。
- 这些共享挂点都有固定实现方式和测试要求，实施同学不需要再做架构决策。

### 21.2 全新链路范围

以下链路是微信新增逻辑，不复用 Web 主链状态：

```text
WeChat API
  -> backend/internal/wechat/monitor.go
  -> backend/internal/wechat/gateway.go
  -> backend/internal/api/wechat_chat.go private runner
  -> WeChat private agent.Manager
  -> WeChat private conversation.Manager
  -> ~/.lumi/wechat/sessions/
  -> WeChat sendmessage/media reply
```

微信私有 runner 固定禁止：

- 不调用 `s.handleChat()`。
- 不调用 `s.prepareChat()`。
- 不调用 `s.handleLocalChat()`。
- 不调用 `s.handleDeviceChat()`。
- 不调用 `s.getOrCreateConversation()`。
- 不调用 `s.persistConversation()`。
- 不读写 `s.sessionStore`。
- 不读写 `s.conversations`。
- 不读写 `s.agentSessions`。
- 不读写 `s.initialized`。
- 不读写 `s.remoteAgentSessions`。
- 不接入 Web `agentCommands` 缓存。

### 21.3 会复用的旧代码和类型

允许复用的旧代码和类型固定如下：

| 复用对象 | 用途 | 约束 |
|---|---|---|
| `config.Config` / `AgentConfig` / `WorkspaceConfig` | 查找绑定 agent/workspace | 不把微信配置写入主 config |
| `agent.Manager` / `agent.Process` | 启动微信私有 agent process | 必须新建独立 manager，不复用 `Server.agents` |
| `conversation.Manager` / `conversation.Message` / `ToolCallInfo` | 管理微信隐藏 conversation 内存结构 | 必须新建独立 manager，不复用 `Server.conversations` |
| `storage.StoredSession` / `storage.GenerateTitle()` | 隐藏会话文件结构和 title 生成 | 只复用结构，不复用普通 `SessionStore` 目录 |
| `jsonrpc.Message` | 解析 agent notification/request | 微信复制必要解析逻辑，不调用会写 Web 状态的 handler |
| `sessionModeID` 的语义 | 推导 agent mode 的参考 | 微信使用显式 `SessionModeOverride`，不读取 Web permission mode |
| `setupSSE` 风格 | 登录 SSE 输出格式参考 | 登录 SSE 独立实现，不影响 `/api/chat` SSE |

### 21.4 必须修改的旧代码

V1 只允许修改以下旧代码挂点：

| 文件/位置 | 修改内容 | 是否会影响旧功能 | 消除影响的约束 |
|---|---|---:|---|
| `backend/internal/agent/rpc.go` `handlePermissionRequest()` | 改成先登记 pending permission，再通知 permission handlers | 会影响 Web permission 底层顺序 | 必须补“handler 内立即确认”和 Web `/api/permission/confirm` 回归测试 |
| `backend/internal/api/server.go` `Server` struct/NewServer/Handler/Shutdown | 增加 WeChat service 字段、初始化、路由注册、关闭顺序 | 会影响服务生命周期 | 微信初始化失败不得阻塞 Lumi；Shutdown 先停微信再停 Web agent |
| `backend/internal/api/handlers.go` `/api/agents/update` | 同步停止微信私有 agent process 并清理微信私有 session/initialized | 只影响 agent 配置更新 | 不改 Web 原有 stop/clear 行为，只追加微信私有清理 |
| `web/src/features/settings/settings-modal.tsx` | 增加 WeChat 设置区块 | 影响设置弹窗 UI | 不改现有 agent/env/theme/lang 行为 |
| `web/src/lib/api.ts` / `web/src/lib/types.ts` | 增加 WeChat API 封装和类型 | 不影响旧调用 | 只新增 export，不改旧函数签名 |

除此之外，不允许为了微信 V1 修改：

- `/api/chat` request/response/SSE 语义。
- Web session list、session detail、session delete 行为。
- share 逻辑。
- device chat path。
- remote workspace `@file` 展开。
- Web permission card 和 confirm API 的前端交互。

### 21.5 共享改动是否足够详细

上述共享改动已经在本方案中固定到实现级别，实施时不需要再做决策：

- permission pending 顺序：
  - 第 5.1.6 节固定了处理顺序。
  - 第 17.3 节固定了测试。
- WeChat service 生命周期：
  - 第 13.1 节固定了启动、重启恢复、关闭行为。
  - 第 17.2 节固定了 `server.go` 修改点。
- agent update：
  - 第 5.1.2 节固定必须停止微信私有 agent process。
  - 第 17.2 节固定修改位置。
- 前端设置：
  - 第 16.4 节固定布局，不改 tabs，不开独立页。
- API 错误语义：
  - 第 15.9 节固定 HTTP status 和 body。
- 测试要求：
  - 第 17.3 节固定必须补的单元测试和可手工验收项。

如果实施同学发现需要新增共享改动，必须先回到方案评审，不允许在代码实现阶段自行决定。

### 21.6 不影响原功能的保证方式

不影响原功能不是靠“完全不改旧代码”，而是靠以下约束保证：

1. **数据隔离**
   - 微信配置不进入主 config。
   - 微信会话不进入普通 `SessionStore`。
   - 微信上传附件放在 workspace `.lumi-uploads/wechat/`。

2. **运行时隔离**
   - 微信使用独立 agent manager。
   - 微信使用独立 conversation manager。
   - 微信使用独立 session map 和 initialized map。
   - 微信自动权限只作用于微信私有 agent process。

3. **入口隔离**
   - Web 仍走 `/api/chat`。
   - 微信只走 `/api/wechat/*` 和 monitor/gateway。
   - Web 即使传入 `wx_` conversationId，也不能读到微信隐藏会话。

4. **协议隔离**
   - Web SSE event 名称和 payload 不变。
   - `[LUMI_WECHAT_SEND]` 只在微信私有输出链路解析。
   - Web 不解析微信回传协议。

5. **回归测试**
   - `go test ./internal/...` 必须通过。
   - Web local chat、device chat、permission confirm、session list/delete、remote workspace `@file` 必须保留现有行为。
   - 微信 hidden conversation 隔离、私有 runner 隔离、permission pending 顺序必须有新增测试覆盖。

最终判断固定为：

- 微信 V1 不是对旧 Web 聊天的替换。
- 微信 V1 是新增入口、新增 service、新增私有 runner。
- 旧代码只在少数生命周期和底层 permission 顺序处做受控修改。
- 这些修改点已经有明确实现和测试约束，足够交给实施同学执行，不需要他再做产品或架构决策。
