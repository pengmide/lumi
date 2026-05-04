# Lumi 本地设备 v1 后端实施规格

## 0. 最高优先级约束：不得影响现有本机执行链路

本地设备 v1 只能作为新增能力实现，不能替换、迁移、弱化或重写 Lumi 当前本机执行链路。

当前本机执行链路是：

```text
Browser -> /api/chat SSE -> Lumi Backend -> local agent.Process -> 本机 Claude/Codex
```

新增远程设备执行链路是：

```text
Browser -> /api/chat SSE -> Lumi Backend -> WebSocket -> device-executor -> 设备侧 agent.Process -> 设备侧 Claude/Codex
```

硬性约束：

- `/api/chat` request 不带 `deviceId` 时，必须 100% 走当前本机执行链路。
- 不带 `deviceId` 时，SSE event type、payload shape、错误行为、权限确认体验、取消体验、conversation 持久化行为必须保持兼容。
- 当前 `agent.Manager`、`agent.Process`、`agentSessions`、`handleNotification()`、本机权限确认流程不得为了设备执行而被删除或迁移到设备侧。
- 设备连接状态、设备离线、设备 setup 失败、WebSocket 错误、`device-executor` 未安装，都不得影响不带 `deviceId` 的聊天请求。
- 设备执行分支只允许在 `deviceId != ""` 时启用。
- 任何公共重构都必须先证明本机路径行为等价，并补充无 `deviceId` 的回归测试。

推荐代码分叉形态：

```go
if req.DeviceID == "" {
    s.handleLocalChat(...)
} else {
    s.handleDeviceChat(...)
}
```

如果实施过程中发现某项改动会改变无 `deviceId` 的行为，必须停止该改动，改为把差异收敛在 `handleDeviceChat` 或设备协议层内。

## 1. 目标与范围

Lumi 当前是本地单用户应用，浏览器到后端已经使用 HTTP + SSE 完成聊天流式输出，后端通过 JSON-RPC 管理本机 ACP agent process。本地设备 v1 的目标是在不引入登录系统、不重写浏览器 SSE 链路的前提下，让后端可以把一次聊天任务下发到已连接的本地设备执行，并把设备返回的事件桥接回现有 SSE 输出。

最终链路：

```text
Browser <-- HTTP/SSE --> Lumi Backend <-- WebSocket --> Device Executor
```

更准确地说，本地设备不是替换现有执行链路，而是在 `/api/chat` 中新增一个可选执行目标分支。合并后的链路是：

```text
                         ┌── stdio JSON-RPC ──> 本机 Claude/Codex ACP agent
                         │
Browser <-- HTTP/SSE --> Lumi Backend
                         │
                         └── WebSocket ──> device-executor ── stdio JSON-RPC ──> 设备上的 Claude/Codex ACP agent
```

默认本机执行链路必须保持可用：

```text
Browser -> /api/chat SSE -> Lumi Backend -> local agent.Process -> Claude/Codex
```

设备执行链路只在请求带 `deviceId` 时启用：

```text
Browser -> /api/chat SSE -> Lumi Backend -> WebSocket -> device-executor -> remote agent.Process -> Claude/Codex
```

v1 做：

- 设备通过 WebSocket 连接 Lumi 后端。
- 设备连接后先完成本机依赖检查，检查通过后才进入可执行状态。
- 设备注册、心跳、依赖检查状态、在线/离线状态。
- 后端保留离线设备记录。
- 新增最小设备端命令 `device-executor`，负责连接后端并在设备上启动 ACP agent。
- 浏览器发送聊天时可带 `deviceId`。
- 无 `deviceId` 时保持现有本机执行路径。
- 有 `deviceId` 时后端下发任务到设备，并通过原 SSE response 返回文本、工具调用、权限请求、done/error。
- 支持设备任务取消。
- 支持设备侧权限确认。
- 每台设备同一时间只允许 1 个运行中任务。

v1 不做：

- 登录系统、多用户权限、JWT/API key 用户体系。
- Redis、CRD、云设备、远程升级。
- 多设备调度策略。
- 单设备多任务并发。
- 远程设备自动安装依赖；v1 只检查并返回安装建议。
- 替换当前 `agent.Manager` / `agent.Process` 本机执行链路。
- 独立的完整设备管理中心；v1 只在 Workspace 新增入口中提供远程设备工作区向导。

## 2. 技术决策

### 2.1 浏览器侧继续 SSE

浏览器到后端继续使用现有 `POST /api/chat` SSE。原因：

- 当前 `web/src/lib/api.ts` 的 `sendMessage()` 已经消费 SSE。
- 文本流、工具调用、权限请求、done/error 都已经围绕 SSE 事件工作。
- 改成浏览器 WebSocket 会扩大改造面，且对 v1 没有必要收益。

### 2.2 设备侧新增 WebSocket

设备侧使用 WebSocket，因为设备通道需要双向通信：

- 设备长期在线并心跳。
- 后端主动下发任务。
- 设备持续回传执行事件。
- 后端主动发送取消和权限确认。

Go WebSocket 依赖固定为：

```text
nhooyr.io/websocket
```

不使用 Socket.IO，不照搬 Wegent 的 Socket.IO namespace 设计。

### 2.3 Wegent 角色对照与部署边界

Wegent 中与 `device-executor` 对应的角色不是 Backend，而是 `wegent-executor` 的 local mode。

Wegent 的部署链路可以理解为：

```text
Wegent Backend <-- Socket.IO --> wegent-executor local mode <-- agent runtime --> Claude/OpenClaw 等
```

虽然 `wegent-executor` 的源码在 Wegent monorepo 内，但从运行位置看，它是安装并启动在“要作为本地设备执行任务”的那台机器上。Backend 只负责维护设备连接、下发任务、接收事件和做路由；真正的 agent runtime 进程由 executor 在目标设备上启动。

Lumi v1 的对应关系固定为：

```text
Lumi Backend <-- WebSocket --> device-executor <-- stdio JSON-RPC --> 目标设备上的 Claude/Codex ACP agent
```

因此 `device-executor` 的定位是：

- 它是设备侧 client，不是 Lumi Backend 的内部服务。
- 它运行在被接入为“本地设备”的目标机器上。
- 它负责和 Backend 保持 WebSocket 连接，并在目标机器上启动/复用 Claude、Codex 等 ACP agent。
- 它只服务于“通过 `deviceId` 指定远端本地设备执行”的分支。
- 如果执行目标就是运行 Lumi Backend 的同一台机器，则不需要启动 `device-executor`，继续走当前本机执行链路。

这意味着 Lumi 有两个并存的执行入口：

```text
无 deviceId：
Browser -> /api/chat SSE -> Lumi Backend -> 本机 agent.Process -> 本机 Claude/Codex

有 deviceId：
Browser -> /api/chat SSE -> Lumi Backend -> WebSocket -> device-executor -> 设备侧 agent.Process -> 设备侧 Claude/Codex
```

实现时不得把当前本机执行也强行改造成经由 `device-executor`。`device-executor` 是新增远端设备执行能力，不是现有本机 agent 执行链路的替代品。

### 2.4 远程设备连接与依赖检查流程

远程设备接入采用“设备主动连接 Backend”的模型。Lumi Backend 不主动扫描或连接远程设备。

原因：

- 目标设备通常在内网、NAT 或防火墙后面，Backend 很难稳定主动访问设备。
- 设备主动建立 WebSocket 后，Backend 可以复用这条长连接下发任务、取消任务、发送权限确认。
- 这个模型和 Wegent 的 `wegent-executor local mode` 一致，只是 Lumi 使用原生 WebSocket 而不是 Socket.IO。

完整流程固定为：

```text
1. 用户在 Lumi App 中打开“添加本地设备”。
2. Backend 生成设备连接命令：
   device-executor connect --server http://<backend-host>:<port> --token <deviceSecret>

3. 用户在目标设备上安装并启动 device-executor。
4. device-executor 主动连接 Backend：
   ws://<backend-host>:<port>/api/devices/ws
   Authorization: Bearer <deviceSecret>

5. Backend 校验 token。
6. device-executor 发送 device.register。
7. Backend 记录设备，状态先设为 setup_required。
8. device-executor 在目标设备本机执行依赖检查。
9. device-executor 通过 setup.status 把检查结果发回 Backend。
10. 如果 setup.status.ready=true，Backend 将设备状态改为 online。
11. 如果 setup.status.ready=false，Backend 保持 setup_required，并保存缺失项。
12. 用户在聊天时选择该 deviceId。
13. Backend 通过已有 WebSocket 连接发送 task.execute。
14. device-executor 在目标设备上启动或复用 Claude/Codex ACP agent。
15. 设备侧 agent 的 session/update 由 device-executor 包装成 task.event 发回 Backend。
16. Backend 将 task.event 桥接为现有 SSE event 返回 Browser。
17. task.done/task.error 后，Backend 清理 taskRun 并持久化会话状态。
```

依赖检查的运行位置必须明确区分：

```text
Backend 首次 setup 检查：
Browser -> Backend -> setupcheck 在 Backend 所在机器运行

远程设备 setup 检查：
Backend -> WebSocket -> device-executor -> setupcheck 在目标设备运行 -> setup.status -> Backend
```

规则：

- Backend 只能检查 Backend 所在机器的依赖。
- 远程设备的 `npm`、`npx`、`claude`、`codex`、ACP npm package 等依赖必须由目标设备上的 `device-executor` 检查。
- `backend/internal/setupcheck` 是可复用检查逻辑，不代表检查一定在 Backend 机器运行；调用方在哪台机器，检查就发生在哪台机器。
- 设备注册成功不等于可执行；只有远程依赖检查通过并上报 `setup.status.ready=true` 后，设备才可作为聊天执行目标。
- v1 只检查远程设备依赖并返回安装建议，不由 Backend 远程安装依赖。

### 2.5 鉴权使用本机 deviceSecret

Lumi 不引入用户登录。后端首次启动时生成本机设备接入密钥：

```text
~/.lumi/device.secret
```

要求：

- secret 至少 32 bytes 随机值，编码为 hex 或 base64url。
- 文件权限尽量设置为 `0600`。
- 读取失败时启动后端应返回明确错误日志；如果文件不存在则创建。
- WebSocket 握手只正式支持：

```text
Authorization: Bearer <deviceSecret>
```

query token 不作为正式 v1 协议，避免 secret 进入 URL 日志。

## 3. 后端文件与模块规划

本节为固定实施规划，不再使用“建议文件”。执行同学必须按以下文件边界实现，除非后续讨论明确更新本文档。

### 3.1 固定文件清单

新增文件：

```text
backend/internal/device/protocol.go
backend/internal/device/store.go
backend/internal/device/secret.go
backend/internal/device/connection.go
backend/internal/device/registry.go
backend/internal/device/ws.go
backend/internal/device/http.go
backend/internal/setupcheck/checker.go
backend/cmd/device-executor/main.go
backend/cmd/device-executor/config.go
backend/cmd/device-executor/client.go
backend/cmd/device-executor/runner.go
```

修改文件：

```text
backend/go.mod
backend/internal/api/server.go
backend/internal/api/chat.go
backend/internal/api/cancel.go 或当前包含 handleChatCancel 的文件
backend/internal/api/permission.go 或当前包含 handlePermissionConfirm 的文件
backend/internal/api/setup.go
```

禁止事项：

- 不新增第二套 chat handler 替代 `/api/chat`。
- 不把本机 `agent.Process` 迁移到 `device` 包。
- 不让 `device` 包 import `internal/api`，避免反向依赖。
- 不在 `device-executor` 中引用 `internal/api`。

### 3.2 包职责边界

#### backend/internal/api

`api` 包继续负责 HTTP API、SSE 输出、conversation 持久化、权限确认路由和取消路由。

允许新增：

- `Server.devices *device.Registry`
- `Server.remoteAgentSessions map[string]map[string]map[string]string`
- `/api/devices` REST route handler 的转发入口
- `/api/devices/ws` WebSocket route handler 的转发入口
- `handleDeviceChat(...)`
- `prepareChat(...)`

不允许新增：

- WebSocket 读写循环实现。
- 设备连接状态机。
- `devices.json` 读写逻辑。

#### backend/internal/device

`device` 包负责设备协议、连接、状态、任务运行态和设备 REST API 的底层逻辑。

它可以依赖：

```text
encoding/json
net/http
sync
time
nhooyr.io/websocket
github.com/pengmide/lumi/internal/setupcheck
```

它不可以依赖：

```text
github.com/pengmide/lumi/internal/api
github.com/pengmide/lumi/internal/conversation
```

#### backend/internal/setupcheck

`setupcheck` 包是唯一依赖检查实现。Backend 首次 setup 和远程设备 setup 都必须复用它。

职责：

- 定义 `DependencyItem`、`SetupStatus`。
- 检查 `npm`、`npx`、agent command。
- 检查通过 `npx` 使用的 ACP npm package。
- 返回结构化缺失项和安装建议。

`backend/internal/api/setup.go` 中现有检查逻辑必须迁移到这里，`api/setup.go` 只保留 HTTP handler、订阅和安装调度。

#### backend/cmd/device-executor

`device-executor` 是设备侧进程，不是后端服务。

职责：

- 读取 `connect --server <url> --token <token> [--config <path>]`。
- 建立到 `/api/devices/ws` 的 WebSocket 连接。
- 自动生成并持久化内部 `deviceId`。
- 发送 `device.register`、`device.heartbeat`、`setup.status`。
- 接收 `setup.check`、`task.execute`、`task.cancel`、`permission.confirm`。
- 在设备本机启动/复用 `agent.Process`。
- 将 agent `session/update` 包装为 `task.event`。

### 3.3 device 包文件职责

#### protocol.go

只放协议常量和 payload struct，不放业务逻辑。

必须包含第 6.5 节定义的：

- `MessageType`
- `Envelope`
- `AckPayload`
- `ErrorPayload`
- 所有设备到后端 payload
- 所有后端到设备 payload

并提供两个小工具函数：

```go
func DecodePayload[T any](env Envelope) (T, error)
func NewEnvelope(typ MessageType, id string, deviceID string, taskID string, payload any) (Envelope, error)
func AckEnvelope(id string) Envelope
func ErrorEnvelope(id string, code string, message string) Envelope
```

伪代码：

```go
func DecodePayload[T any](env Envelope) (T, error) {
    var out T
    if len(env.Payload) == 0 {
        return out, nil
    }
    err := json.Unmarshal(env.Payload, &out)
    return out, err
}

func NewEnvelope(typ MessageType, id, deviceID, taskID string, payload any) (Envelope, error) {
    raw, err := json.Marshal(payload)
    if err != nil {
        return Envelope{}, err
    }
    return Envelope{Type: typ, ID: id, DeviceID: deviceID, TaskID: taskID, Payload: raw}, nil
}

func AckEnvelope(id string) Envelope {
    env, _ := NewEnvelope(MsgAck, id, "", "", AckPayload{OK: true})
    return env
}

func ErrorEnvelope(id string, code string, message string) Envelope {
    env, _ := NewEnvelope(MsgError, id, "", "", ErrorPayload{Code: code, Message: message})
    return env
}
```

#### store.go

负责 `~/.lumi/devices.json` 持久化。

必须提供：

```go
type Store struct {
    path string
    mu   sync.Mutex
}

func NewStore(path string) *Store
func (s *Store) Load() ([]Device, error)
func (s *Store) Save(devices []Device) error
func DefaultDevicesPath() string
```

规则：

- `path == ""` 时使用 `DefaultDevicesPath()`。
- 文件不存在返回空数组，不返回错误。
- 保存时使用临时文件 + rename。
- 保存前确保目录存在。

#### secret.go

负责 `~/.lumi/device.secret`。

必须提供：

```go
func DefaultSecretPath() string
func EnsureSecret(path string) (string, error)
func ValidateBearer(header string, secret string) bool
```

规则：

- secret 不存在时生成 32 bytes 随机值。
- 文件权限尽量设置为 `0600`。
- `ValidateBearer` 只接受 `Authorization: Bearer <secret>`。
- 不支持 query token 作为正式协议。

#### connection.go

封装单个 WebSocket 连接，避免 `Registry` 直接操作 websocket.Conn。

必须提供：

```go
type Connection struct {
    DeviceID string
    conn     *websocket.Conn
    sendCh   chan Envelope
    closed   chan struct{}
}

func NewConnection(conn *websocket.Conn) *Connection
func (c *Connection) Send(ctx context.Context, env Envelope) error
func (c *Connection) Close(reason string)
func (c *Connection) ReadLoop(ctx context.Context, onMessage func(Envelope))
func (c *Connection) WriteLoop(ctx context.Context)
```

规则：

- 所有写入必须经过 `sendCh` 串行化。
- `Close` 必须幂等。
- `ReadLoop` 解码失败时发送 error envelope 或关闭连接，不能 panic。

#### registry.go

管理设备状态、连接、任务、权限映射。

必须提供：

```go
type Registry struct {
    store  *Store
    secret string
    mu     sync.RWMutex

    devices map[string]*Device
    conns   map[string]*Connection
    tasks   map[string]*TaskRun

    sessionToTask    map[string]string
    toolCallToTask   map[string]string
    deviceCurrentTask map[string]string
}

func NewRegistry(store *Store, secret string) (*Registry, error)
func (r *Registry) ListDevices() []Device
func (r *Registry) GetDevice(id string) (Device, bool)
func (r *Registry) RegisterDevice(conn *Connection, payload DeviceRegisterPayload) (Device, error)
func (r *Registry) UpdateSetupStatus(deviceID string, status setupcheck.SetupStatus) error
func (r *Registry) Heartbeat(deviceID string, runningTaskIDs []string) error
func (r *Registry) MarkDisconnected(deviceID string, reason string)
func (r *Registry) StartTask(task *TaskRun) error
func (r *Registry) FinishTask(taskID string)
func (r *Registry) SendToDevice(ctx context.Context, deviceID string, typ MessageType, taskID string, payload any) error
func (r *Registry) RegisterPermission(toolCallID string, taskID string)
func (r *Registry) TaskBySession(sessionID string) (*TaskRun, bool)
func (r *Registry) TaskByToolCall(toolCallID string) (*TaskRun, bool)
```

`registry.go` 必须定义固定错误，`api` 层用这些错误映射为用户可读 SSE error：

```go
var (
    ErrDeviceNotFound       = errors.New("device not found")
    ErrDeviceOffline        = errors.New("device is offline")
    ErrSetupNotReady        = errors.New("device setup is not ready")
    ErrDeviceBusy           = errors.New("device is busy")
    ErrTaskNotFound         = errors.New("task not found")
    ErrTaskEventBufferFull  = errors.New("task event buffer full")
)
```

`registry.go` 还必须定义 task 事件结构和构造函数：

```go
type DeviceEventType string

const (
    DeviceEventSession           DeviceEventType = "session"
    DeviceEventNotification      DeviceEventType = "notification"
    DeviceEventPermissionRequest DeviceEventType = "permission_request"
    DeviceEventDone              DeviceEventType = "done"
    DeviceEventError             DeviceEventType = "error"
)

type DeviceEvent struct {
    Type    DeviceEventType
    TaskID  string
    Payload json.RawMessage
    Err     error
}

func NewTaskRun(id string, deviceID string, conversationID string, agentID string, workspaceID string, workspacePath string) *TaskRun {
    return &TaskRun{
        ID:             id,
        DeviceID:       deviceID,
        ConversationID: conversationID,
        AgentID:        agentID,
        WorkspaceID:    workspaceID,
        WorkspacePath:  workspacePath,
        StartedAt:      time.Now().UnixMilli(),
        Events:         make(chan DeviceEvent, 64),
        Done:           make(chan struct{}),
    }
}
```

核心伪代码：

```go
func (r *Registry) RegisterDevice(conn *Connection, p DeviceRegisterPayload) (Device, error) {
    r.mu.Lock()
    defer r.mu.Unlock()

    if old := r.conns[p.DeviceID]; old != nil {
        old.Close("device reconnected")
        if taskID := r.deviceCurrentTask[p.DeviceID]; taskID != "" {
            r.failTaskLocked(taskID, "Device reconnected")
        }
    }

    now := time.Now().UnixMilli()
    d := r.devices[p.DeviceID]
    if d == nil {
        d = &Device{ID: p.DeviceID, RegisteredAt: now}
        r.devices[p.DeviceID] = d
    }

    d.Name = p.Name
    d.DefaultAgentID = p.DefaultAgentID
    d.Agents = p.Agents
    d.WorkspaceID = p.WorkspaceID
    d.Version = p.Version
    d.Status = "setup_required"
    d.SetupReady = false
    d.LastHeartbeat = now
    d.UpdatedAt = now

    conn.DeviceID = p.DeviceID
    r.conns[p.DeviceID] = conn
    r.persistLocked()
    return *d, nil
}

func (r *Registry) StartTask(task *TaskRun) error {
    r.mu.Lock()
    defer r.mu.Unlock()

    d := r.devices[task.DeviceID]
    if d == nil || d.Status == "offline" {
        return ErrDeviceOffline
    }
    if !d.SetupReady {
        return ErrSetupNotReady
    }
    if r.deviceCurrentTask[task.DeviceID] != "" {
        return ErrDeviceBusy
    }

    r.tasks[task.ID] = task
    r.deviceCurrentTask[task.DeviceID] = task.ID
    d.Status = "busy"
    d.RunningTaskIDs = []string{task.ID}
    return nil
}

func (r *Registry) FinishTask(taskID string) {
    r.mu.Lock()
    defer r.mu.Unlock()

    task := r.tasks[taskID]
    if task == nil {
        return
    }

    delete(r.tasks, taskID)
    delete(r.sessionToTask, task.SessionID)
    delete(r.deviceCurrentTask, task.DeviceID)

    if d := r.devices[task.DeviceID]; d != nil {
        d.RunningTaskIDs = nil
        if d.SetupReady && r.conns[task.DeviceID] != nil {
            d.Status = "online"
        }
    }

    close(task.Done)
}

func (r *Registry) failTaskLocked(taskID string, message string) {
    task := r.tasks[taskID]
    if task == nil {
        return
    }

    select {
    case task.Events <- DeviceEvent{Type: DeviceEventError, TaskID: taskID, Err: errors.New(message)}:
    default:
    }
}
```

#### ws.go

负责 WebSocket upgrade、鉴权、连接 read/write loop 和协议分发。

必须提供：

```go
func (r *Registry) HandleWebSocket(w http.ResponseWriter, req *http.Request)
```

伪代码：

```go
func (r *Registry) HandleWebSocket(w http.ResponseWriter, req *http.Request) {
    if !ValidateBearer(req.Header.Get("Authorization"), r.secret) {
        http.Error(w, "Unauthorized", http.StatusUnauthorized)
        return
    }

    conn, err := websocket.Accept(w, req, &websocket.AcceptOptions{
        OriginPatterns: []string{"*"},
    })
    if err != nil {
        return
    }

    dc := NewConnection(conn)
    ctx, cancel := context.WithCancel(req.Context())
    defer cancel()

    registered := make(chan string, 1)

    go dc.WriteLoop(ctx)
    go dc.ReadLoop(ctx, func(env Envelope) {
        r.handleDeviceMessage(ctx, dc, env, registered)
    })

    select {
    case deviceID := <-registered:
        <-dc.closed
        r.MarkDisconnected(deviceID, "connection closed")
    case <-time.After(10 * time.Second):
        dc.Close("register timeout")
    case <-ctx.Done():
    }
}
```

消息分发伪代码：

```go
func (r *Registry) handleDeviceMessage(ctx context.Context, c *Connection, env Envelope, registered chan<- string) {
    switch env.Type {
    case MsgDeviceRegister:
        p, err := DecodePayload[DeviceRegisterPayload](env)
        if err != nil { c.Send(ctx, ErrorEnvelope(env.ID, "invalid_payload", err.Error())); return }
        d, err := r.RegisterDevice(c, p)
        if err != nil { c.Send(ctx, ErrorEnvelope(env.ID, "internal_error", err.Error())); return }
        c.Send(ctx, AckEnvelope(env.ID))
        select { case registered <- d.ID: default: }

    case MsgSetupStatus:
        p, err := DecodePayload[setupcheck.SetupStatus](env)
        if err != nil { c.Send(ctx, ErrorEnvelope(env.ID, "invalid_payload", err.Error())); return }
        r.UpdateSetupStatus(env.DeviceID, p)
        c.Send(ctx, AckEnvelope(env.ID))

    case MsgDeviceHeartbeat:
        p, _ := DecodePayload[DeviceHeartbeatPayload](env)
        r.Heartbeat(env.DeviceID, p.RunningTaskIDs)

    case MsgTaskSession, MsgTaskEvent, MsgTaskDone, MsgTaskError, MsgPermissionRequest:
        r.forwardTaskEvent(env)

    default:
        c.Send(ctx, ErrorEnvelope(env.ID, "invalid_payload", "unsupported message type"))
    }
}
```

`forwardTaskEvent` 不直接写 SSE。它只把事件放入 `TaskRun.Events`，由 `api.handleDeviceChat` 消费并转成 SSE。

伪代码：

```go
func (r *Registry) forwardTaskEvent(env Envelope) {
    r.mu.RLock()
    task := r.tasks[env.TaskID]
    r.mu.RUnlock()
    if task == nil {
        return
    }

    event := DeviceEvent{TaskID: env.TaskID, Payload: env.Payload}
    switch env.Type {
    case MsgTaskSession:
        event.Type = DeviceEventSession
    case MsgTaskEvent:
        event.Type = DeviceEventNotification
    case MsgPermissionRequest:
        event.Type = DeviceEventPermissionRequest
    case MsgTaskDone:
        event.Type = DeviceEventDone
    case MsgTaskError:
        event.Type = DeviceEventError
    }

    select {
    case task.Events <- event:
    default:
        go func() {
            task.Events <- DeviceEvent{Type: DeviceEventError, TaskID: env.TaskID, Err: ErrTaskEventBufferFull}
        }()
    }
}
```

#### http.go

放设备相关 REST handler 的底层实现，供 `api.Server` 转发调用。

必须提供：

```go
func (r *Registry) HandleListDevices(w http.ResponseWriter, req *http.Request)
func (r *Registry) HandlePairingCommand(w http.ResponseWriter, req *http.Request)
func (r *Registry) HandleUpdateAlias(w http.ResponseWriter, req *http.Request, deviceID string)
func (r *Registry) HandleDeleteDevice(w http.ResponseWriter, req *http.Request, deviceID string)
func (r *Registry) HandleRequestSetupCheck(w http.ResponseWriter, req *http.Request, deviceID string)
```

`api.Server` 只负责路由路径解析：

```go
func (s *Server) handleDevices(w http.ResponseWriter, r *http.Request) {
    switch {
    case r.URL.Path == "/api/devices" && r.Method == "GET":
        s.devices.HandleListDevices(w, r)
    case r.URL.Path == "/api/devices/pairing-command" && r.Method == "GET":
        s.devices.HandlePairingCommand(w, r)
    case strings.HasSuffix(r.URL.Path, "/setup/check") && r.Method == "POST":
        deviceID := parseDeviceID(r.URL.Path)
        s.devices.HandleRequestSetupCheck(w, r, deviceID)
    default:
        writeError(w, "Not found", http.StatusNotFound)
    }
}
```

### 3.4 Server 初始化与路由改造

`backend/internal/api.Server` 必须扩展为：

```go
type Server struct {
    // existing fields...
    devices *device.Registry

    // conversationId -> deviceId -> agentId -> remote sessionId
    remoteAgentSessions map[string]map[string]map[string]string
    remoteSessionsMu    sync.RWMutex
}
```

`NewServer()` 必须初始化设备 registry。初始化失败时不要静默禁用设备功能，必须让问题暴露出来。

由于当前 `NewServer()` 返回 `*Server` 而不是 `(*Server, error)`，v1 采用 panic/fatal 二选一中的明确失败策略：使用 `log.Fatalf` 终止启动。不要只打印 warning。

伪代码：

```go
func NewServer(cfg *config.Config, staticFS fs.FS) *Server {
    deviceStore := device.NewStore("")
    deviceSecret, err := device.EnsureSecret("")
    if err != nil {
        log.Fatalf("failed to initialize device secret: %v", err)
    }

    devices, err := device.NewRegistry(deviceStore, deviceSecret)
    if err != nil {
        log.Fatalf("failed to initialize device registry: %v", err)
    }

    s := &Server{
        // existing fields...
        devices: devices,
        remoteAgentSessions: make(map[string]map[string]map[string]string),
    }

    // existing setup init...
    return s
}
```

`Handler()` 必须新增路由：

```go
mux.HandleFunc("/api/devices", s.handleDevices)
mux.HandleFunc("/api/devices/", s.handleDevices)
mux.HandleFunc("/api/devices/pairing-command", s.handleDevices)
mux.HandleFunc("/api/devices/ws", s.devices.HandleWebSocket)
```

注意路由顺序：`/api/devices/ws` 必须在静态文件 fallback 之前注册；如果 `ServeMux` 路由冲突，以 Go 当前 `net/http` 的精确路径优先规则为准，但实现时仍要写测试覆盖 `/api/devices/ws` 不被 `/api/devices/` 吞掉。

### 3.5 Chat 分支改造落点

`backend/internal/api/chat.go` 必须拆出三层：

```go
func (s *Server) handleChat(w http.ResponseWriter, r *http.Request)
func (s *Server) prepareChat(req chatRequest) (*chatPrepared, error)
func (s *Server) handleLocalChat(ctx chatRuntimeContext) 
func (s *Server) handleDeviceChat(ctx chatRuntimeContext)
```

`handleChat` 只做 request decode、SSE 初始化和分支选择：

```go
func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
    req := decodeChatRequest(...)
    sendEvent := setupSSE(...)

    prepared, err := s.prepareChat(req)
    if err != nil {
        sendEvent("error", map[string]string{"message": err.Error()})
        return
    }

    runtime := chatRuntimeContext{
        Request: req,
        Prepared: prepared,
        SendEvent: sendEvent,
        HTTPRequest: r,
    }

    if req.DeviceID == "" {
        s.handleLocalChat(runtime)
        return
    }
    s.handleDeviceChat(runtime)
}
```

`handleLocalChat` 必须从当前 `handleChat` 搬运原逻辑，保证无 `deviceId` 行为等价。

`handleDeviceChat` 只处理设备路径：

```go
func (s *Server) handleDeviceChat(ctx chatRuntimeContext) {
    taskID := newTaskRunID()
    task := device.NewTaskRun(
        taskID,
        ctx.Request.DeviceID,
        ctx.Prepared.ConvID,
        ctx.Prepared.AgentID,
        ctx.Prepared.WorkspaceID,
        ctx.Prepared.WorkspacePath,
    )
    if err := s.devices.StartTask(task); err != nil {
        ctx.SendEvent("error", map[string]string{"message": deviceErrorMessage(err)})
        return
    }
    defer s.devices.FinishTask(task.ID)

    sessionID := s.getRemoteSession(ctx.Prepared.ConvID, ctx.Request.DeviceID, ctx.Prepared.AgentID)

    payload := device.TaskExecutePayload{
        ConversationID: ctx.Prepared.ConvID,
        AgentID: ctx.Prepared.AgentID,
        SessionID: sessionID,
        WorkspaceID: ctx.Prepared.WorkspaceID,
        WorkspacePath: ctx.Prepared.WorkspacePath,
        Prompt: ctx.Prepared.PromptText,
        Files: toTaskFiles(ctx.Request.Files),
    }

    if err := s.devices.SendToDevice(ctx.HTTPRequest.Context(), ctx.Request.DeviceID, device.MsgTaskExecute, task.ID, payload); err != nil {
        ctx.SendEvent("error", map[string]string{"message": err.Error()})
        return
    }

    s.consumeDeviceTaskEvents(ctx, task)
}
```

### 3.6 device-executor 文件职责

#### main.go

解析命令行，只支持：

```text
device-executor connect --server <url> --token <token> [--config <path>]
```

伪代码：

```go
func main() {
    opts := parseConnectArgs(os.Args[1:])
    cfg := LoadOrCreateConfig(opts.ConfigPath)
    client := NewClient(opts.Server, opts.Token, cfg)
    if err := client.Run(context.Background()); err != nil {
        log.Fatal(err)
    }
}
```

#### config.go

负责设备端配置：

```go
type ExecutorConfig struct {
    DeviceID     string               `json:"deviceId"`
    Name         string               `json:"name"`
    Workspace    string               `json:"workspace"`
    Agents       []config.AgentConfig `json:"agents"`
    DefaultAgent string               `json:"defaultAgent"`
}
```

规则：

- `deviceId` 是内部稳定标识，用户不需要填写、不需要理解。
- 配置文件不存在或 `deviceId` 为空时，`LoadOrCreateConfig` 必须自动生成并写回。
- 对用户展示的配置模板不应包含 `deviceId`；它只在程序首次启动后出现在落盘配置中。
- `name` 为空时使用 hostname。
- `workspace` 为空时使用当前目录。
- `agents` 为空时启动失败，并提示用户编辑配置。

#### client.go

负责 WebSocket 连接、注册、心跳、消息循环。

伪代码：

```go
func (c *Client) Run(ctx context.Context) error {
    c.connect(ctx)
    c.sendRegister()
    go c.heartbeatLoop(ctx)

    c.runSetupCheckAndReport(ctx)

    for {
        env := c.readEnvelope()
        switch env.Type {
        case device.MsgSetupCheck:
            c.runSetupCheckAndReport(ctx)
        case device.MsgTaskExecute:
            c.runner.Execute(ctx, env)
        case device.MsgTaskCancel:
            c.runner.Cancel(ctx, env)
        case device.MsgPermissionConfirm:
            c.runner.ConfirmPermission(ctx, env)
        }
    }
}
```

#### runner.go

负责设备本机 agent process 生命周期。

必须维护：

```go
type Runner struct {
    cfg       *ExecutorConfig
    agents    map[string]*agent.Process
    sessions  map[string]string // taskID -> sessionID
}
```

伪代码：

```go
func (r *Runner) Execute(ctx context.Context, env device.Envelope) {
    p := device.DecodePayload[device.TaskExecutePayload](env)
    proc := r.getOrStartAgent(p.AgentID, p.WorkspacePath)
    sessionID := p.SessionID

    cleanupNotif := proc.OnNotification(func(msg *jsonrpc.Message) {
        notification := toACPNotification(msg)
        r.client.Send(device.MsgTaskEvent, env.TaskID, device.TaskEventPayload{
            SessionID: sessionID,
            Notification: notification,
        })
    })
    defer cleanupNotif()

    cleanupPerm := proc.OnPermission(func(req *agent.PermissionRequest) {
        r.client.Send(device.MsgPermissionRequest, env.TaskID, req)
    })
    defer cleanupPerm()

    if sessionID == "" {
        resp, err := proc.Request("session/new", map[string]any{"cwd": p.WorkspacePath})
        if err != nil {
            r.client.Send(device.MsgTaskError, env.TaskID, device.TaskErrorPayload{Message: err.Error()})
            return
        }

        var result struct {
            SessionID string `json:"sessionId"`
        }
        resp.ParseResult(&result)
        sessionID = result.SessionID
        if sessionID == "" {
            r.client.Send(device.MsgTaskError, env.TaskID, device.TaskErrorPayload{Message: "session/new returned empty sessionId"})
            return
        }

        r.client.Send(device.MsgTaskSession, env.TaskID, device.TaskSessionPayload{SessionID: sessionID})
    }

    _, err := proc.Request("session/prompt", map[string]any{
        "sessionId": sessionID,
        "prompt": []map[string]string{{"type": "text", "text": p.Prompt}},
    })
    if err != nil {
        r.client.Send(device.MsgTaskError, env.TaskID, device.TaskErrorPayload{Message: err.Error()})
        return
    }
    r.client.Send(device.MsgTaskDone, env.TaskID, device.TaskDonePayload{})
}
```

### 3.7 依赖变更

`backend/go.mod` 必须新增：

```text
nhooyr.io/websocket
```

引入方式：

```bash
cd backend
go get nhooyr.io/websocket
```

不要引入 Socket.IO、gorilla/websocket 或额外 Web 框架。

## 4. 数据结构

### 4.1 Device

设备持久化结构：

```go
type Device struct {
    ID             string   `json:"id"`
    Name           string   `json:"name"`
    Alias          string   `json:"alias,omitempty"`
    Status         string   `json:"status"`
    SetupReady     bool     `json:"setupReady"`
    SetupStatus    *SetupStatus `json:"setupStatus,omitempty"`
    DefaultAgentID string   `json:"defaultAgentId,omitempty"`
    Agents         []DeviceAgentInfo `json:"agents,omitempty"`
    WorkspaceID    string   `json:"workspaceId,omitempty"`
    Version        string   `json:"version,omitempty"`
    LastHeartbeat  int64    `json:"lastHeartbeat"`
    RegisteredAt   int64    `json:"registeredAt"`
    UpdatedAt      int64    `json:"updatedAt"`
    RunningTaskIDs []string `json:"runningTaskIds,omitempty"`
}

type DeviceAgentInfo struct {
    ID   string `json:"id"`
    Name string `json:"name"`
}
```

状态固定为：

```text
setup_required
online
offline
busy
error
```

展示名规则：

- API 返回 `displayName`。
- `displayName = alias` if alias 非空。
- 否则 `displayName = name`。
- 设备重新 register 时允许更新 `name`、`version`、`defaultAgentId`、`agents`、`workspaceId`，但不能覆盖 `alias`。

后端启动规则：

- 从 `devices.json` 读取的设备全部先标记为 `offline`。
- 设备 WebSocket 连接成功不等于可执行。
- 设备上报依赖检查且 `setupReady=true` 后，才允许标记为 `online`。
- 设备上报依赖检查且 `setupReady=false` 时，标记为 `setup_required`。
- 只有 `online` 设备可作为执行目标。

连接状态语义：

```text
connected != registered != executable
```

- `connected`：WebSocket 鉴权成功。
- `registered`：设备身份已上报并持久化。
- `executable`：设备依赖检查通过，状态为 `online`。

### 4.2 SetupStatus

依赖检查结构必须兼容现有 setup 页面：

```go
type DependencyItem struct {
    Name    string `json:"name"`
    Command string `json:"command,omitempty"`
    Package string `json:"package,omitempty"`
    Status  string `json:"status"`
    Message string `json:"message,omitempty"`
    Install string `json:"install,omitempty"`
}

type SetupStatus struct {
    Ready       bool             `json:"ready"`
    Environment []DependencyItem `json:"environment"`
    Agents      []DependencyItem `json:"agents"`
    ACPPackages []DependencyItem `json:"acpPackages"`
}
```

`status` 取值沿用现有实现：

```text
checking
ready
missing
not_installed
installing
error
blocked
```

v1 远程设备只使用检查状态，不触发远程自动安装。

### 4.3 TaskRun

运行中任务结构：

```go
type TaskRun struct {
    ID             string
    DeviceID       string
    ConversationID string
    AgentID        string
    SessionID      string
    WorkspaceID    string
    WorkspacePath  string
    StartedAt      int64

    Events chan DeviceEvent
    Done   chan struct{}
}
```

Registry 必须维护：

```text
taskRunId -> TaskRun
sessionId -> taskRunId
toolCallId -> taskRunId
deviceId -> current taskRunId
```

v1 每个 `deviceId` 同一时间只能有一个 current task。

## 5. REST API 契约

现有错误响应沿用 `writeError()` 风格：

```json
{
  "error": "message"
}
```

### 5.0 REST DTO 结构体

REST API 必须使用专门 DTO，不直接把 `device.Device` 持久化结构原样暴露给前端。

```go
type APIErrorResponse struct {
    Error string `json:"error"`
}

type DeviceDTO struct {
    ID             string            `json:"id"`
    Name           string            `json:"name"`
    Alias          string            `json:"alias,omitempty"`
    DisplayName    string            `json:"displayName"`
    Status         string            `json:"status"`
    SetupReady     bool              `json:"setupReady"`
    SetupStatus    *SetupStatus      `json:"setupStatus,omitempty"`
    DefaultAgentID string            `json:"defaultAgentId,omitempty"`
    Agents         []DeviceAgentInfo `json:"agents,omitempty"`
    WorkspaceID    string            `json:"workspaceId,omitempty"`
    Version        string            `json:"version,omitempty"`
    LastHeartbeat  int64             `json:"lastHeartbeat"`
    RegisteredAt   int64             `json:"registeredAt"`
    UpdatedAt      int64             `json:"updatedAt"`
    RunningTaskIDs []string          `json:"runningTaskIds,omitempty"`
}

type ListDevicesResponse struct {
    Devices []DeviceDTO `json:"devices"`
}

type PairingCommandResponse struct {
    Command    string `json:"command"`
    Server     string `json:"server"`
    ConfigPath string `json:"configPath"`
}

type UpdateDeviceAliasRequest struct {
    Alias string `json:"alias"`
}

type UpdateDeviceAliasResponse struct {
    Device DeviceDTO `json:"device"`
}

type SuccessResponse struct {
    Success bool   `json:"success"`
    Message string `json:"message,omitempty"`
}

type ChatRequest struct {
    Message        string         `json:"message"`
    ConversationID string         `json:"conversationId"`
    WorkspaceID    string         `json:"workspaceId"`
    Files          []chatFileInfo `json:"files"`
    DeviceID       string         `json:"deviceId,omitempty"`
}

type ChatCancelRequest struct {
    AgentID   string `json:"agentId"`
    SessionID string `json:"sessionId"`
}

type PermissionConfirmRequest struct {
    AgentID    string `json:"agentId"`
    ToolCallID string `json:"toolCallId"`
    OptionID   string `json:"optionId"`
}
```

DTO 规则：

- `DeviceDTO.DisplayName` 由后端计算，不持久化。
- `DeviceDTO.ID` 是内部 ID，前端只用于选择设备和调用设备 API，不提供编辑入口。
- `DeviceDTO.Agents` 表示该设备可执行的 agent；聊天选择某个设备时，后端必须校验当前 `agentId` 存在于该设备 `agents` 中。
- `ChatRequest` 只是扩展现有 request；无 `deviceId` 时不能改变原行为。

### 5.1 GET /api/devices

返回所有已注册设备，包括 offline。

Response 200:

```json
{
  "devices": [
    {
      "id": "dev_2f4b7a9c8d1e",
      "name": "MacBook Pro",
      "alias": "Office Mac",
      "displayName": "Office Mac",
      "status": "online",
      "setupReady": true,
      "setupStatus": {
        "ready": true,
        "environment": [],
        "agents": [],
        "acpPackages": []
      },
      "defaultAgentId": "claude",
      "agents": [
        {
          "id": "claude",
          "name": "Claude Code"
        }
      ],
      "workspaceId": "default",
      "version": "0.1.0",
      "lastHeartbeat": 1710000000000,
      "registeredAt": 1710000000000,
      "updatedAt": 1710000000000,
      "runningTaskIds": []
    }
  ]
}
```

### 5.2 GET /api/devices/pairing-command

返回完整配对命令，不单独强调裸 secret。命令内容按当前后端地址生成；如果无法可靠推断 host，默认使用 `http://localhost:<port>`。

Response 200:

```json
{
  "command": "device-executor connect --server http://localhost:3000 --token <deviceSecret>",
  "server": "http://localhost:3000",
  "configPath": "~/.device-executor/config.json"
}
```

注意：

- v1 可以在 response 中包含 token，因为 Lumi 当前本机 API 无登录保护；但不要在日志中打印 token。
- 后续如增加登录或配对码，可替换该接口实现，保持 response shape 尽量兼容。
- 返回的命令必须对应仓库内真实实现的 `backend/cmd/device-executor`。
- 该命令只负责连接设备，不负责自动写入 agent 配置；agent 配置由设备端配置文件提供。

### 5.3 PUT /api/devices/{id}/alias

Request:

```json
{
  "alias": "Office Mac"
}
```

规则：

- `alias` trim 后最长 100 字符。
- 空字符串表示清除 alias。
- 设备不存在返回 404。

Response 200:

```json
{
  "device": {
    "id": "dev_2f4b7a9c8d1e",
    "alias": "Office Mac",
    "displayName": "Office Mac"
  }
}
```

### 5.4 DELETE /api/devices/{id}

规则：

- 删除 `devices.json` 中的持久记录。
- 如果设备在线，不强制断开 WebSocket；下一次 heartbeat/register 会重新出现在列表。
- 如果设备有运行中任务，返回 409，要求先取消任务。
- 不存在返回 404。

Response 200:

```json
{
  "success": true
}
```

### 5.5 POST /api/chat

扩展现有 request：

```go
type chatRequest struct {
    Message        string         `json:"message"`
    ConversationID string         `json:"conversationId"`
    WorkspaceID    string         `json:"workspaceId"`
    Files          []chatFileInfo `json:"files"`
    DeviceID       string         `json:"deviceId,omitempty"`
}
```

规则：

- `deviceId == ""`：完全走现有本机 agent 流程。
- `deviceId != ""`：走设备执行流程。
- 设备不存在或 offline：SSE 返回 `error` 事件后结束。
- 设备 `status == "setup_required"` 或 `setupReady == false`：SSE 返回 `error` 事件后结束，错误信息为 `Device setup is not ready`。
- 设备 busy：SSE 返回 `error` 事件后结束，错误信息为 `Device is busy`。
- 当前聊天选中的 `agentId` 不在设备 `agents` 中：SSE 返回 `error` 事件后结束，错误信息为 `Agent not available on device: <agentId>`。

### 5.6 POST /api/devices/{id}/setup/check

触发指定在线设备重新运行依赖检查。

规则：

- 设备不存在返回 404。
- 设备 offline 返回 409。
- 后端通过 WebSocket 向设备发送 `setup.check`。
- 设备回传 `setup.status` 后更新设备 `setupReady` 和 `setupStatus`。
- v1 不通过该接口触发远程自动安装。

Response 202:

```json
{
  "success": true,
  "message": "Setup check requested"
}
```

### 5.7 POST /api/chat/cancel

沿用现有 request：

```json
{
  "agentId": "claude",
  "sessionId": "session-id"
}
```

规则：

- 如果 `sessionId` 命中远端设备运行态，发送 `task.cancel` 到对应设备。
- 否则保持现有逻辑，向本机 agent 发送 `session/cancel`。
- 远端设备不在线时，清理本地 taskRun 并返回成功。

### 5.8 POST /api/permission/confirm

沿用现有 request：

```json
{
  "agentId": "claude",
  "toolCallId": "tool-call-id",
  "optionId": "allow"
}
```

规则：

- 如果 `toolCallId` 命中远端设备权限请求，发送 `permission.confirm` 到对应设备。
- 否则保持现有逻辑，调用本机 agent 的 `ConfirmPermission()`。

### 5.9 /api/chat SSE 事件结构

`/api/chat` 的 HTTP response 继续是 SSE。无 `deviceId` 时必须保持现有事件结构；有 `deviceId` 时也必须桥接为同一组事件，前端不需要为设备路径实现第二套流式协议。

固定事件：

```go
type SSESessionEvent struct {
    ConversationID string `json:"conversationId"`
    SessionID      string `json:"sessionId"`
    Agent          string `json:"agent"`
    IsNew          bool   `json:"isNew"`
}

type SSEStatusEvent struct {
    Message string `json:"message"`
}

type SSEErrorEvent struct {
    Message string `json:"message"`
}

type SSEDoneEvent map[string]any
```

`update`、`commands`、`tool_call`、`permission_request` 事件沿用当前 `handleNotification()` 和 `agent.PermissionRequest` 的输出结构。

设备路径规则：

- 设备路径收到 `task.session` 后发送 `session` event。
- 设备路径不得在收到 `task.session` 前发送 `update`、`tool_call`、`permission_request` 或 `done`。
- 设备路径收到 `task.event.notification` 后，Backend normalize 成当前 `handleNotification()` 可处理的结构，再输出现有 SSE event。
- 设备路径收到 `permission.request` 后，输出现有 `permission_request` event。
- 设备路径收到 `task.done` 后，输出现有 `done` event。
- 设备路径收到 `task.error`、断连或取消失败后，输出现有 `error` event。

## 6. WebSocket 协议

WebSocket 路径：

```text
GET /api/devices/ws
```

握手：

```text
Authorization: Bearer <deviceSecret>
```

认证失败：

- HTTP 401，不升级 WebSocket。

### 6.1 Envelope

所有消息均为 JSON：

```json
{
  "type": "device.register",
  "id": "msg-uuid",
  "deviceId": "dev_2f4b7a9c8d1e",
  "taskId": "task-run-id",
  "payload": {}
}
```

字段规则：

- `type` 必填。
- `id` 必填，用于 ack/error 对应原始消息。
- `deviceId` 在 `device.register` 后必须和注册 deviceId 一致。
- `taskId` 仅任务相关消息必填。
- `payload` 可为空 object。

### 6.2 通用 ack

后端收到需要确认的消息后返回：

```json
{
  "type": "ack",
  "id": "original-message-id",
  "payload": {
    "ok": true
  }
}
```

错误：

```json
{
  "type": "error",
  "id": "original-message-id",
  "payload": {
    "code": "invalid_payload",
    "message": "deviceId is required"
  }
}
```

错误码固定：

```text
unauthorized
not_registered
invalid_payload
device_busy
task_not_found
internal_error
```

### 6.3 设备到后端

#### device.register

设备连接后第一条业务消息必须是 `device.register`，用于上报设备身份。注册设备身份不代表设备已经可执行任务。

Payload:

```json
{
  "deviceId": "dev_2f4b7a9c8d1e",
  "name": "MacBook Pro",
  "defaultAgentId": "claude",
  "agents": [
    {
      "id": "claude",
      "name": "Claude Code"
    },
    {
      "id": "codex",
      "name": "Codex"
    }
  ],
  "workspaceId": "default",
  "version": "0.1.0"
}
```

规则：

- 连接后 10 秒内未注册，后端关闭连接。
- `deviceId`、`name` 必填。
- `deviceId` 由 Device Executor 自动生成并持久化，Backend 只校验非空和格式，不负责替设备生成。
- `agents` 必须来自设备端配置中的 `agents`，用于后端判断该设备是否支持当前聊天选中的 agent。
- `defaultAgentId` 为空时，Backend 可使用 `agents[0].id` 作为展示默认值；但执行时仍以 `/api/chat` 准备出的 `agentId` 为准。
- 同一 `deviceId` 已在线时，新连接踢掉旧连接。
- 注册成功后设备状态为 `setup_required`，直到收到 `setup.status.ready=true`。
- 注册成功后写入或更新 `devices.json`。
- 注册成功后设备端必须立即运行依赖检查，并发送 `setup.status`。

#### setup.status

设备在本机运行依赖检查后上报结果。

Payload:

```json
{
  "ready": false,
  "environment": [
    {
      "name": "npm",
      "command": "npm",
      "status": "ready",
      "message": "Installed"
    }
  ],
  "agents": [
    {
      "name": "Claude",
      "command": "claude",
      "status": "missing",
      "message": "Not found",
      "install": "npm install -g @anthropic-ai/claude-code"
    }
  ],
  "acpPackages": []
}
```

规则：

- payload shape 必须兼容 `SetupStatus`。
- `ready=true` 时，后端将设备状态设为 `online`。
- `ready=false` 时，后端将设备状态设为 `setup_required`。
- 如果检查过程失败，设备发送 `ready=false`，并在对应 item 上使用 `status=error` 和 `message`。
- 设备每次连接后必须至少发送一次 `setup.status`。

#### device.heartbeat

设备每 15 秒发送一次。

Payload:

```json
{
  "runningTaskIds": ["task-1"]
}
```

规则：

- 后端超过 45 秒未收到 heartbeat，标记设备 offline 并关闭连接。
- heartbeat 中如包含 running task，则状态为 `busy`；否则为 `online`。

#### device.status

Payload:

```json
{
  "status": "busy"
}
```

规则：

- status 只允许 `online`、`busy`。
- offline 只能由后端断线/超时推导。

#### task.event

设备执行过程中回传 ACP agent notification。

Payload 必须包装原始 JSON-RPC notification，不重新设计一套事件结构：

```json
{
  "sessionId": "remote-session-id",
  "notification": {
    "jsonrpc": "2.0",
    "method": "session/update",
    "params": {
      "sessionId": "remote-session-id",
      "update": {
        "sessionUpdate": "agent_message_chunk",
        "content": {
          "type": "text",
          "text": "hello"
        }
      }
    }
  }
}
```

规则：

- `notification.method` v1 主要支持 `session/update`。
- `notification.params` 必须使用原始 ACP payload；Device Executor 不解析、不重组、不丢弃未知字段。
- Backend 收到后将 `notification` 还原为现有 `agent.Process` 通知结构，并复用 `handleNotification()`。
- 如果后续需要支持非 `session/update` 的 ACP notification，优先扩展 Backend normalize 层；Device Executor 仍保持外层包装、内层透传。

#### task.session

设备创建或复用远端 agent session 后回传：

```json
{
  "sessionId": "remote-session-id"
}
```

规则：

- 后端收到后更新 `remoteAgentSessions[conversationId][deviceId][agentId]`。
- 设备必须在发送第一条 `task.event`、`permission.request`、`task.done` 之前先发送 `task.session`。
- 如果后端在收到 `task.session` 前先收到 `task.event`、`permission.request` 或 `task.done`，必须视为协议错误，发送 SSE `error` 并取消该 task。
- 如果任务执行开始后 30 秒仍未收到 `task.session`，后端发送 SSE `error`，错误信息为 `Device did not create session`，并清理 taskRun。

#### task.done

Payload:

```json
{
  "result": {
    "stopReason": "end_turn"
  }
}
```

规则：

- 后端收到后发送 SSE `done`。
- 后端持久化本次 assistant message 和 tool calls。
- 后端清理 taskRun 和 device busy 状态。

#### task.error

Payload:

```json
{
  "message": "execution failed"
}
```

规则：

- 后端发送 SSE `error`。
- 后端清理 taskRun 和 device busy 状态。
- 不持久化 assistant message；已收到的 tool calls 是否持久化按现有 streamItems 聚合结果执行，v1 推荐不在 error 时新增 assistant message。

#### permission.request

Payload 复用现有前端权限请求结构：

```json
{
  "sessionId": "remote-session-id",
  "options": [
    {
      "optionId": "allow",
      "name": "Allow",
      "kind": "allow"
    }
  ],
  "toolCall": {
    "toolCallId": "tool-1",
    "rawInput": {},
    "status": "pending",
    "title": "Run command",
    "kind": "command"
  }
}
```

规则：

- 后端记录 `toolCallId -> taskRunId`。
- 后端通过 SSE `permission_request` 转发给浏览器。

### 6.4 后端到设备

#### setup.check

后端要求设备重新运行依赖检查。

Payload:

```json
{}
```

规则：

- 由 `POST /api/devices/{id}/setup/check` 触发。
- 设备收到后重新运行本机 dependency setup check。
- 设备完成后发送 `setup.status`。
- v1 不支持 `setup.install` 或远程自动安装消息。

#### task.execute

Payload:

```json
{
  "conversationId": "conv-id",
  "agentId": "claude",
  "sessionId": "remote-session-id-or-empty",
  "workspaceId": "default",
  "workspacePath": "/path/to/workspace",
  "prompt": "user prompt",
  "files": [
    {
      "name": "a.txt",
      "path": "/tmp/a.txt",
      "size": 123
    }
  ]
}
```

规则：

- 如果 `sessionId` 为空，设备负责创建 agent session。
- 设备必须 ack。
- 后端发送后 15 秒未收到 ack，SSE 返回 error，清理 taskRun。

#### task.cancel

Payload:

```json
{
  "sessionId": "remote-session-id",
  "reason": "client_cancelled"
}
```

规则：

- SSE client 断开时后端自动发送。
- `/api/chat/cancel` 命中设备 task 时后端发送。
- 设备收到后应转成 ACP `session/cancel`。

#### permission.confirm

Payload:

```json
{
  "toolCallId": "tool-1",
  "optionId": "allow"
}
```

规则：

- 设备收到后应恢复对应权限请求。

### 6.5 WebSocket 协议结构体

`backend/internal/device/protocol.go` 必须定义以下结构体。实现时以本节为准，不再从 JSON 示例反推字段。

通用 envelope：

```go
package device

import (
    "encoding/json"

    "github.com/pengmide/lumi/internal/setupcheck"
)

type MessageType string

const (
    MsgAck               MessageType = "ack"
    MsgError             MessageType = "error"
    MsgDeviceRegister    MessageType = "device.register"
    MsgDeviceHeartbeat   MessageType = "device.heartbeat"
    MsgDeviceStatus      MessageType = "device.status"
    MsgSetupStatus       MessageType = "setup.status"
    MsgSetupCheck        MessageType = "setup.check"
    MsgTaskExecute       MessageType = "task.execute"
    MsgTaskSession       MessageType = "task.session"
    MsgTaskEvent         MessageType = "task.event"
    MsgTaskDone          MessageType = "task.done"
    MsgTaskError         MessageType = "task.error"
    MsgTaskCancel        MessageType = "task.cancel"
    MsgPermissionRequest MessageType = "permission.request"
    MsgPermissionConfirm MessageType = "permission.confirm"
)

type Envelope struct {
    Type     MessageType     `json:"type"`
    ID       string          `json:"id"`
    DeviceID string          `json:"deviceId,omitempty"`
    TaskID   string          `json:"taskId,omitempty"`
    Payload  json.RawMessage `json:"payload,omitempty"`
}
```

通用响应：

```go
type AckPayload struct {
    OK bool `json:"ok"`
}

type ErrorPayload struct {
    Code    string `json:"code"`
    Message string `json:"message"`
}
```

设备到后端 payload：

```go
type DeviceRegisterPayload struct {
    DeviceID       string            `json:"deviceId"`
    Name           string            `json:"name"`
    DefaultAgentID string            `json:"defaultAgentId,omitempty"`
    Agents         []DeviceAgentInfo `json:"agents,omitempty"`
    WorkspaceID    string            `json:"workspaceId,omitempty"`
    Version        string            `json:"version,omitempty"`
}

type DeviceHeartbeatPayload struct {
    RunningTaskIDs []string `json:"runningTaskIds,omitempty"`
}

type DeviceStatusPayload struct {
    Status string `json:"status"`
}

type SetupStatusPayload = setupcheck.SetupStatus

type TaskSessionPayload struct {
    SessionID string `json:"sessionId"`
}

type ACPNotification struct {
    JSONRPC string          `json:"jsonrpc,omitempty"`
    Method  string          `json:"method"`
    Params  json.RawMessage `json:"params,omitempty"`
}

type TaskEventPayload struct {
    SessionID     string          `json:"sessionId,omitempty"`
    Notification  ACPNotification `json:"notification"`
}

type TaskDonePayload struct {
    Result json.RawMessage `json:"result,omitempty"`
}

type TaskErrorPayload struct {
    Message string `json:"message"`
}

type PermissionRequestPayload struct {
    SessionID string             `json:"sessionId"`
    Options   []PermissionOption `json:"options"`
    ToolCall  PermissionToolCall `json:"toolCall"`
}

type PermissionOption struct {
    OptionID string `json:"optionId"`
    Name     string `json:"name"`
    Kind     string `json:"kind"`
}

type PermissionToolCall struct {
    ToolCallID string          `json:"toolCallId"`
    RawInput   json.RawMessage `json:"rawInput,omitempty"`
    Status     string          `json:"status,omitempty"`
    Title      string          `json:"title,omitempty"`
    Kind       string          `json:"kind,omitempty"`
}
```

后端到设备 payload：

```go
type SetupCheckPayload struct{}

type TaskExecutePayload struct {
    ConversationID string         `json:"conversationId"`
    AgentID        string         `json:"agentId"`
    SessionID      string         `json:"sessionId,omitempty"`
    WorkspaceID    string         `json:"workspaceId,omitempty"`
    WorkspacePath  string         `json:"workspacePath"`
    Prompt         string         `json:"prompt"`
    Files          []TaskFileInfo `json:"files,omitempty"`
}

type TaskFileInfo struct {
    Name string `json:"name"`
    Path string `json:"path"`
    Size int64  `json:"size,omitempty"`
}

type TaskCancelPayload struct {
    SessionID string `json:"sessionId,omitempty"`
    Reason    string `json:"reason,omitempty"`
}

type PermissionConfirmPayload struct {
    ToolCallID string `json:"toolCallId"`
    OptionID   string `json:"optionId"`
}
```

结构体使用规则：

- `Envelope.Payload` 统一先以 `json.RawMessage` 接收，再根据 `Envelope.Type` 解码为对应 payload。
- Device Executor 对 ACP notification 只做 `TaskEventPayload` 外层包装；`ACPNotification.Params` 必须透传。
- Backend 对 `TaskEventPayload.Notification` 做 normalize，转换成当前 `handleNotification()` 可处理的内部形态。
- `PermissionRequestPayload` 使用最小强类型结构，`rawInput` 使用 `json.RawMessage` 保留工具参数扩展性。
- `TaskDonePayload.Result` v1 不参与业务判断，先保留为 `json.RawMessage`，避免锁死不同 ACP agent 的结束返回格式。

### 6.6 Device Executor 与 ACP agent 的 JSON-RPC 结构体

Device Executor 和 Claude/Codex ACP agent 之间继续使用现有 stdio JSON-RPC 模型。该层不走 WebSocket envelope。

最小 JSON-RPC 结构：

```go
type JSONRPCRequest struct {
    JSONRPC string      `json:"jsonrpc"`
    ID      any         `json:"id"`
    Method  string      `json:"method"`
    Params  any         `json:"params,omitempty"`
}

type JSONRPCResponse struct {
    JSONRPC string          `json:"jsonrpc"`
    ID      any             `json:"id"`
    Result  json.RawMessage `json:"result,omitempty"`
    Error   *JSONRPCError   `json:"error,omitempty"`
}

type JSONRPCError struct {
    Code    int             `json:"code"`
    Message string          `json:"message"`
    Data    json.RawMessage `json:"data,omitempty"`
}

type JSONRPCNotification struct {
    JSONRPC string          `json:"jsonrpc,omitempty"`
    Method  string          `json:"method"`
    Params  json.RawMessage `json:"params,omitempty"`
}
```

Device Executor 发给 agent 的最小调用：

```go
type SessionNewParams struct {
    Cwd string `json:"cwd,omitempty"`
}

type SessionPromptParams struct {
    SessionID string `json:"sessionId"`
    Prompt    string `json:"prompt"`
}

type SessionCancelParams struct {
    SessionID string `json:"sessionId"`
}
```

调用映射规则：

- 收到 `task.execute` 且 `sessionId` 为空：Device Executor 调用 `session/new`，成功后向 Backend 发送 `task.session`。
- 收到 `task.execute`：Device Executor 调用 `session/prompt`。
- 收到 agent `session/update` notification：Device Executor 包装为 `task.event`，其中 `notification.params` 原样透传。
- 收到 `task.cancel`：Device Executor 调用 `session/cancel`。
- 收到 `permission.confirm`：Device Executor 恢复本地等待中的权限请求；如当前 `agent.Process` 已有对应 helper，则复用 helper，不新增协议。

## 7. 设备生命周期规则

- WebSocket 连接未认证：HTTP 401。
- WebSocket 已连接但 10 秒未注册：关闭连接。
- 注册成功后状态为 `setup_required`。
- 注册成功后设备端必须运行依赖检查并上报 `setup.status`。
- `setup.status.ready=true` 后状态为 `online`。
- `setup.status.ready=false` 后状态保持 `setup_required`。
- `setup_required` 设备保留在设备列表中，但不能作为聊天执行目标。
- 15 秒 heartbeat，45 秒超时。
- 超时或断线：
  - 状态变 `offline`。
  - 清空连接引用。
  - 如果有运行中 task，向 SSE 返回 error：`Device disconnected`。
  - 清理 taskRun、permission 映射、busy 状态。
- 重复连接：
  - 新连接注册同一 `deviceId` 时，关闭旧连接。
  - 如果旧连接有运行中 task，task 失败，返回 `Device reconnected` error。
- 后端重启：
  - `devices.json` 中设备保留。
  - 全部状态初始化为 `offline`。
  - 上一次 `setupStatus` 可保留用于展示，但不能让设备直接进入 `online`；必须重新连接并重新上报检查结果。

## 8. 设备端 client 规格

设备端 client 是新增命令：

```text
device-executor
```

源码位置：

```text
backend/cmd/device-executor
```

### 8.1 命令行

v1 只要求一个子命令：

```text
device-executor connect --server http://localhost:3000 --token <deviceSecret> [--config ~/.device-executor/config.json]
```

参数规则：

- `--server` 必填，指向 Lumi Backend。
- `--token` 必填，用于 WebSocket Authorization。
- `--config` 可选，默认 `~/.device-executor/config.json`。
- 启动后进程保持前台运行，日志输出到 stderr/stdout。

### 8.2 设备端配置

用户需要编辑的配置文件格式沿用 Lumi 当前 agent 配置的最小子集。用户配置模板不包含 `deviceId`：

```json
{
  "name": "MacBook Pro",
  "workspace": "/Users/me/project",
  "agents": [
    {
      "id": "claude",
      "name": "Claude Code",
      "command": "npx",
      "args": ["@anthropics/claude-code", "--acp"],
      "env": {
        "ANTHROPIC_AUTH_TOKEN": "..."
      },
      "permissionMode": "default"
    }
  ],
  "defaultAgent": "claude"
}
```

`device-executor` 首次启动后会自动生成内部 `deviceId` 并写回配置文件。写回后的配置可能变为：

```json
{
  "deviceId": "dev_2f4b7a9c8d1e",
  "name": "MacBook Pro",
  "workspace": "/Users/me/project",
  "agents": [
    {
      "id": "claude",
      "name": "Claude Code",
      "command": "npx",
      "args": ["@anthropics/claude-code", "--acp"],
      "env": {
        "ANTHROPIC_AUTH_TOKEN": "..."
      },
      "permissionMode": "default"
    }
  ],
  "defaultAgent": "claude"
}
```

规则：

- `deviceId` 是内部稳定标识，必须由 `device-executor` 自动生成。
- 用户不需要填写 `deviceId`，前端也不需要让用户编辑 `deviceId`。
- 如果配置文件中已有 `deviceId`，必须复用，保证同一台设备重启后仍是同一个设备。
- 如果配置文件中没有 `deviceId` 或值为空，必须生成新 ID 并写回配置文件。
- ID 格式固定为 `dev_<12-32位随机hex或base64url>`，不要使用 hostname、MAC 地址、用户名等可识别信息。
- `name` 为空时使用主机名。
- `workspace` 为空时使用当前工作目录。
- `agents` 必须至少有一个。
- `AgentConfig` 字段语义与 `backend/internal/config.AgentConfig` 保持一致。
- v1 不要求设备端读取主 Lumi 的 `~/.lumi/lumi.config.json`；设备端有独立配置。

### 8.3 设备端 agent 执行

`device-executor` 收到 `task.execute` 后：

1. 根据 payload `agentId` 查找本地配置中的 agent。
2. 如果找不到，发送 `task.error`。
3. 如果没有对应 agent process，启动一个。
4. 如果 payload `sessionId` 为空，调用 `session/new`，cwd 使用 payload `workspacePath`；成功后发送 `task.session`。
5. 调用 `session/prompt`，prompt 使用 payload `prompt`。
6. 将 agent 的 `session/update` 通知包装成 `task.event` 回传后端。
7. `session/prompt` 返回成功后发送 `task.done`。
8. 执行失败时发送 `task.error`。

设备端可以复用或抽取 `backend/internal/agent.Process`，但不能改变后端本机执行路径对该包的使用方式。

### 8.4 设备端依赖检查

`device-executor` 必须复用 `backend/internal/setupcheck` 执行依赖检查。

检查时机：

- WebSocket `device.register` ack 后立即运行一次。
- 收到后端 `setup.check` 后重新运行一次。

检查内容：

- `npm`
- `npx`
- 设备端配置中 agent command，例如 `claude`、`codex`
- 设备端配置中通过 `npx` 使用的 ACP npm package 是否全局安装或已缓存

检查结果：

- 通过 `setup.status` 上报。
- 检查通过前，设备不能执行 `task.execute`。
- 如果设备端收到 `task.execute` 但本地检查未 ready，必须返回 `task.error`，message 为：

```text
Device setup is not ready
```

## 9. Chat 执行流程

### 9.0 核心分叉原则

`/api/chat` 的改造必须是“新增执行分支”，不是重写当前执行链路。

当前本机执行链路：

```text
POST /api/chat
  -> 创建/获取 conversation
  -> 选择 agent
  -> 创建/获取本机 agent session
  -> agentProc.Request("session/prompt")
  -> agent notification 转 SSE
```

改造后：

```text
POST /api/chat
  -> 创建/获取 conversation
  -> 选择 agent
  -> 准备 prompt/workspace/files
  -> if deviceId 为空:
       走原来的本机 agent 流程
     else:
       走设备 WebSocket 流程
```

代码结构必须体现这个分叉：

```go
if req.DeviceID == "" {
    s.handleLocalChat(...)
} else {
    s.handleDeviceChat(...)
}
```

硬性兼容要求：

- 不带 `deviceId` 的请求必须保持当前行为。
- 不带 `deviceId` 的 SSE event type、payload shape、错误行为必须保持兼容。
- 当前 `agent.Manager`、`agent.Process`、`agentSessions` 本机执行模型不能被删除或迁移到设备端。
- 设备执行分支不得要求本机 agent process 必须启动。

### 9.1 重构要求

将当前 `handleChat` 拆出公共准备逻辑，避免本机路径和设备路径复制。

建议 helper：

```go
type chatPrepared struct {
    ConvID        string
    IsNew         bool
    AgentID       string
    PreviousAgent string
    AgentChanged  bool
    WorkspaceID   string
    WorkspacePath string
    PromptText    string
    MessageFiles  []conversation.MessageFile
}
```

```go
func (s *Server) prepareChat(req chatRequest) (*chatPrepared, error)
func (s *Server) handleLocalChat(...)
func (s *Server) handleDeviceChat(...)
```

公共逻辑必须包含：

- 创建或读取 conversation。
- detect mention 并切换 active agent。
- 解析 workspace。
- 拼接文件引用。
- agent changed 时拼接历史上下文。
- 写入 user message。

公共逻辑不得发送 SSE `session`。原因是本机路径能立即得到本机 `sessionId`，但设备路径必须等待 `task.session` 才能得到远端 `sessionId`。因此：

- `handleLocalChat` 负责按现有时机发送 SSE `session` 和 `status: Processing...`。
- `handleDeviceChat` 负责在收到 `task.session` 后发送 SSE `session`，然后发送 `status: Processing...`。

### 9.2 本机路径

无 `deviceId` 时：

- 行为必须与当前 `handleChat` 等价。
- 仍使用 `s.agentSessions[convID][agentID]`。
- 仍通过 `agentProc.Request("session/prompt", ...)` 执行。
- 仍使用 `handleNotification()` 聚合文本和工具调用。
- 仍由 `agentProc.OnNotification()` 和 `agentProc.OnPermission()` 将本机 agent 事件转成 SSE。
- 仍在 `done` 前调用 `s.persistConversation(convID)`。
- 任何设备相关错误、设备离线、WebSocket 状态都不能影响本机路径。

### 9.3 设备路径

有 `deviceId` 时：

1. 检查设备存在且 online。
2. 检查设备 `setupReady == true`；否则 SSE error `Device setup is not ready`。
3. 检查当前 `agentId` 在设备 `agents` 中；否则 SSE error `Agent not available on device: <agentId>`。
4. 检查设备没有运行中 task；否则 SSE error `Device is busy`。
5. 创建 taskRunId。
6. 从 `remoteAgentSessions` 读取远端 sessionId，可为空。
7. 注册 taskRun 到 registry。
8. 发送 `task.execute`。
9. 等待设备事件并转发到 SSE。
10. 聚合 streamItems/currentText/toolCallMap。
11. done 时持久化会话并发送 SSE done。
12. error、disconnect、SSE client disconnect 时清理 taskRun。

SSE client 断开：

- 使用 `r.Context().Done()` 监听。
- 如果设备 task 仍在运行，发送 `task.cancel`。
- 清理 taskRun。

设备路径中的 `device-executor` 负责在设备本机执行类似当前后端本机路径的 agent 调用：

```text
device-executor
  -> 根据 task.execute.payload.agentId 找到本地 agent 配置
  -> 如果没有远端 sessionId，则调用 agent session/new
  -> 调用 agent session/prompt
  -> 监听 agent session/update
  -> 将 session/update 包装成 task.event 回传 Backend
  -> agent 返回后发送 task.done
```

如果设备端找不到 `agentId` 对应配置，必须返回 `task.error`，message 为：

```text
Agent not found on device: <agentId>
```

## 10. 前端入口、布局与交互模式

### 10.0 前端实施边界

前端 v1 只实施在 React / Next 代码路径中。旧 Vue 代码路径不参与本功能，不新增、不修改、不修复。

必须优先改动：

```text
web/src/features/chat/components/workspace-selector.tsx
web/src/features/chat/use-chat-controller.tsx
web/src/lib/api.ts
web/src/lib/types.ts
```

按需要可改动：

```text
web/src/features/chat/components/chat-panel.tsx
web/src/features/chat/components/sidebar.tsx
```

禁止为了本功能改动：

```text
web/src/components/*.vue
web/src/views/*.vue
web/src/stores/session.ts
```

如果实施过程中发现旧 Vue 路径仍被当前构建入口引用，必须先暂停前端实现并确认构建入口，不允许同时维护两套 UI。

### 10.1 核心 UI 模型

前端入口固定从当前侧边栏的 Workspace 区域进入。用户仍然只理解一个主入口：新增工作区。

Workspace 分为两类：

```text
Workspace
  ├── Local Workspace
  │      └── 本机目录
  │
  └── Remote Device Workspace
         └── 某台远程设备上的目录
```

远程设备不作为一个和 Workspace 平级的常驻入口。设备是远程工作区的执行承载，最终用户在聊天时选择的是 Workspace，而不是每次手动组合 `workspace + device`。

聊天链路由当前选中的 Workspace 决定：

```text
选择 Local Workspace
        │
        ▼
POST /api/chat
{
  workspaceId: "local-ws",
  deviceId: ""
}
        │
        ▼
Backend -> local agent.Process -> 本机 Claude/Codex
```

```text
选择 Remote Device Workspace
        │
        ▼
POST /api/chat
{
  workspaceId: "remote-ws",
  deviceId: "dev_xxx"
}
        │
        ▼
Backend -> WebSocket -> device-executor -> 远程 Claude/Codex
```

### 10.2 侧边栏入口

当前侧边栏保留 Workspace 区块和 `+` 按钮：

```text
Sidebar
┌─────────────────────────────┐
│ Workspace              [+]  │
│ ┌─────────────────────────┐ │
│ │ My Local Project     ▾  │ │
│ │ /Users/me/project       │ │
│ └─────────────────────────┘ │
│                             │
│ Chats                  [+]  │
│ ...                         │
└─────────────────────────────┘
```

点击 `Workspace [+]` 后打开 Add Workspace 弹窗。弹窗默认是本机工作区，不改变当前用户习惯：

```text
┌──────────────────────────────────────────┐
│ Add Workspace                            │
├──────────────────────────────────────────┤
│                                          │
│  Workspace type                          │
│  ┌──────────────┐  ┌──────────────────┐  │
│  │ Local        │  │ Remote Device    │  │
│  └──────────────┘  └──────────────────┘  │
│                                          │
│  默认进入 Local                          │
│                                          │
└──────────────────────────────────────────┘
```

### 10.3 Local Workspace 表单

Local 表单沿用当前 Add Workspace 的交互和字段：

```text
Workspace [+]
   │
   ▼
┌──────────────────────────┐
│ Add Workspace             │
│ Type: Local               │
├──────────────────────────┤
│ Name                      │
│ [ My Project           ]  │
│ Path                      │
│ [ /Users/me/project    ]  │
│                          │
│              Cancel Add  │
└──────────────────────────┘
```

规则：

- 默认打开 Local。
- `Name` 和 `Path` 必填。
- 提交后调用现有 `createWorkspace(name, path)`。
- 创建成功后选中新 workspace，并清空当前会话。
- Local workspace 发送聊天时不带 `deviceId`，必须保持当前体验。

### 10.4 Remote Device Workspace 向导

Remote Device 入口进入三步弹窗向导：

```text
Workspace [+]
   │
   ▼
┌──────────────────────────┐
│ Add Workspace             │
│ Type: Remote Device       │
├──────────────────────────┤
│ Step 1. Connect Device    │
│ Step 2. Check Setup       │
│ Step 3. Add Workspace     │
└──────────────────────────┘
```

#### Step 1: Connect Device

展示后端生成的 pairing command，引导用户在目标设备运行 `device-executor`：

```text
┌──────────────────────────────────────────────┐
│ Add Remote Workspace                         │
├──────────────────────────────────────────────┤
│ Step 1 / 3  Connect Device                   │
│                                              │
│ Run this on the remote device:               │
│ ┌──────────────────────────────────────────┐ │
│ │ device-executor connect --server ...     │ │
│ └──────────────────────────────────────────┘ │
│                                              │
│ Status: Waiting for device                   │
│                                              │
│                         Cancel        Next   │
└──────────────────────────────────────────────┘
```

规则：

- 进入该步骤时调用 `fetchDevicePairingCommand()`。
- 命令区域使用等宽字体，可复制。
- 同时轮询或刷新 `fetchDevices()`。
- 没有 online/setup_required 设备时显示 `Waiting for device`。
- 检测到设备注册后允许进入 Step 2。

#### Step 2: Check Device Setup

展示远程设备依赖检查结果：

```text
┌──────────────────────────────────────────────┐
│ Add Remote Workspace                         │
├──────────────────────────────────────────────┤
│ Step 2 / 3  Check Device Setup               │
│                                              │
│ Device: Office Mac                           │
│ Status: setup_required                       │
│                                              │
│ npm              ready                       │
│ npx              ready                       │
│ Claude Code      missing                     │
│ ACP package      ready                       │
│                                              │
│ Hint: npm install -g ...                     │
│                                              │
│              Recheck        Back       Next  │
└──────────────────────────────────────────────┘
```

规则：

- 数据来自 `/api/devices` 返回的 `setupStatus`。
- `setupReady=false` 时禁用 `Next`，展示缺失项和 install hint。
- 点击 `Recheck` 调用 `requestDeviceSetupCheck(deviceId)`。
- `setupReady=true` 后允许进入 Step 3。
- 设备 offline 时显示 offline 状态，并禁用 `Next`。

#### Step 3: Add Remote Workspace

在已通过 setup 的设备上新增远程工作区：

```text
┌──────────────────────────────────────────────┐
│ Add Remote Workspace                         │
├──────────────────────────────────────────────┤
│ Step 3 / 3  Remote Workspace                 │
│                                              │
│ Device                                       │
│ [ Office Mac                            ▾ ]  │
│                                              │
│ Workspace name                               │
│ [ Website                                ]   │
│                                              │
│ Remote path                                  │
│ [ /Users/me/site                         ]   │
│                                              │
│                         Back       Add       │
└──────────────────────────────────────────────┘
```

规则：

- `Device` 只能选择 `online && setupReady=true` 的设备。
- `Workspace name` 必填。
- `Remote path` 必填，表示目标设备上的绝对路径。
- 保存后新增一个 Remote Workspace，并在 WorkspaceSelector 中选中。
- Remote workspace 保存的数据至少包含：

```ts
type WorkspaceKind = 'local' | 'remote'

interface Workspace {
  id: string
  name: string
  path: string
  kind?: WorkspaceKind
  deviceId?: string
  deviceName?: string
  remotePath?: string
  deviceStatus?: string
  setupReady?: boolean
}
```

兼容规则：

- 旧 workspace 没有 `kind` 时按 `local` 处理。
- Local workspace 使用 `path`。
- Remote workspace 的 `path` 可以保存为 `remotePath` 的同值，前端展示时优先使用 `deviceName + remotePath`。

### 10.5 Workspace 列表展示

添加远程工作区后，Workspace 下拉按 Local / Remote 分组展示：

```text
Workspace
┌──────────────────────────────┐
│ Website                  ▾   │
│ Office Mac · /Users/me/site  │
└──────────────────────────────┘

Dropdown
┌──────────────────────────────┐
│ Local                        │
│ ┌──────────────────────────┐ │
│ │ My Project               │ │
│ │ /Users/me/project        │ │
│ └──────────────────────────┘ │
│                              │
│ Remote                       │
│ ┌──────────────────────────┐ │
│ │ Website                  │ │
│ │ Office Mac · /Users/me/site│
│ └──────────────────────────┘ │
└──────────────────────────────┘
```

展示规则：

- Local item 第一行显示 workspace name，第二行显示本机 path。
- Remote item 第一行显示 workspace name，第二行显示 `<deviceName> · <remotePath>`。
- Remote item 可显示状态：online、offline、setup_required、busy。
- offline/setup_required 的 remote workspace 可以保留在列表中，但选中后聊天输入区要禁用或发送时给出明确错误。

### 10.6 Chat 发送规则

`sendMessage()` 增加可选 `deviceId` 参数：

```ts
sendMessage(
  message,
  conversationId,
  workspaceId,
  files,
  onEvent,
  deviceId?,
)
```

发送规则：

- 当前 workspace 为 local：不传 `deviceId`。
- 当前 workspace 为 remote：传 `workspace.deviceId`。
- ChatPanel 不新增独立设备选择器。
- 前端不实现第二套流式协议，仍消费当前 SSE event。
- 远程设备错误通过现有 `error` SSE event 显示为 assistant error message。

### 10.7 新增 API client

前端需要新增：

```ts
fetchDevices()
fetchDevicePairingCommand()
updateDeviceAlias(deviceId, alias)
requestDeviceSetupCheck(deviceId)
```

并扩展：

```ts
createWorkspace(name, path, options?)
sendMessage(..., deviceId?)
```

`createWorkspace` 的 remote options 至少包含：

```ts
{
  kind: 'remote',
  deviceId: string,
  deviceName: string,
  remotePath: string
}
```

### 10.8 前端实现落点

当前主要 React/TSX 入口：

- `web/src/features/chat/components/workspace-selector.tsx`
  - 改造 Add Workspace 弹窗。
  - 增加 Local / Remote Device 模式切换。
  - 增加 Remote Device 三步向导。
- `web/src/features/chat/use-chat-controller.tsx`
  - 保存当前 workspace 的 remote metadata。
  - `sendCurrentMessage()` 根据当前 workspace 注入 `deviceId`。
  - remote workspace 创建成功后选中并清空当前 session。
- `web/src/lib/api.ts`
  - 新增 devices API client。
  - 扩展 `createWorkspace` 和 `sendMessage`。
- `web/src/lib/types.ts`
  - 扩展 `Workspace` 类型。
  - 新增 `DeviceDTO`、`SetupStatus` 等前端类型。

旧 Vue 代码路径不参与本功能；不得修改 `web/src/components/*.vue`、`web/src/views/*.vue`、`web/src/stores/session.ts`。

## 11. 测试矩阵

### 11.1 device 包单测

- `EnsureSecret`：
  - 文件不存在时创建。
  - 文件存在时复用。
  - secret 不写入日志。

- `Store`：
  - 保存新设备。
  - register 更新 name/version 但不覆盖 alias。
  - 删除设备。
  - 后端启动后设备状态为 offline。

- `Registry`：
  - 注册设备 online。
  - 注册设备后没有 setup ready 时为 setup_required。
  - setup.status ready=true 后变 online。
  - setup.status ready=false 后保持 setup_required。
  - heartbeat 刷新 lastHeartbeat。
  - heartbeat 超时变 offline。
  - 同 deviceId 新连接踢旧连接。
  - busy 设备拒绝新 task。

### 11.2 API 单测

- `GET /api/devices` 返回 offline + online 设备。
- `GET /api/devices` 返回 setupStatus。
- `GET /api/devices/pairing-command` 返回 command 且不在日志打印 token。
- `PUT /api/devices/{id}/alias` 更新 displayName。
- `DELETE /api/devices/{id}` 删除 offline 设备。
- `DELETE /api/devices/{id}` 对 running task 返回 409。
- `POST /api/devices/{id}/setup/check` 向在线设备发送 setup.check。
- `POST /api/devices/{id}/setup/check` 对 offline 设备返回 409。

### 11.3 WebSocket 集成测试

使用 mock device：

- 无 Authorization 连接返回 401。
- 正确 secret 连接成功。
- 未 register 超时关闭。
- register 后收到 ack。
- register 后设备状态为 setup_required。
- setup.status ready=false 后 `/api/devices` 显示 setup_required 和缺失项。
- setup.status ready=true 后 `/api/devices` 显示 online。
- 后端发送 setup.check 后 mock device 收到消息并回传 setup.status。
- heartbeat 后设备保持 online。
- 设备断开后 `/api/devices` 显示 offline。

### 11.4 Chat 集成测试

- 不带 `deviceId` 的 `/api/chat` 行为保持现有输出。
- 带 offline `deviceId` 返回 SSE error。
- 带 setup_required `deviceId` 返回 SSE error `Device setup is not ready`。
- 带 busy `deviceId` 返回 SSE error。
- mock device 收到 `task.execute`。
- mock device 回传 `task.event` 文本后，浏览器 SSE 收到 `update`。
- mock device 回传 `task.done` 后，浏览器 SSE 收到 `done`，会话被持久化。
- SSE client 断开时，mock device 收到 `task.cancel`。
- 设备发送 `permission.request` 后，浏览器 SSE 收到 `permission_request`。
- `/api/permission/confirm` 命中设备权限请求后，mock device 收到 `permission.confirm`。

## 12. 实施顺序

1. 添加 `nhooyr.io/websocket` 依赖。
2. 抽取 `backend/internal/setupcheck`，让现有 setup 页面继续使用同一套检查逻辑。
3. 实现 `backend/internal/device` 的 protocol、secret、store、registry。
4. 在 `Server` 初始化 registry，注册 devices REST API 和 WebSocket route。
5. 实现设备 register、setup.status、setup.check、heartbeat、disconnect、重复连接规则。
6. 增加 `/api/devices`、pairing command API、setup check API。
7. 新增最小 `backend/cmd/device-executor`，支持 connect、register、setup check、heartbeat。
8. 重构 `handleChat`，抽出公共准备逻辑，但保持本机路径行为不变。
9. 实现设备执行分支和 SSE 桥接。
10. 实现 cancel 和 permission confirm 的本机/设备双路径路由。
11. 补齐后端单测、设备端单测和 WebSocket/chat 集成测试。
12. 前端接入：在 Workspace `+` 弹窗中实现 Local / Remote Device Workspace 入口、远程设备三步向导、Workspace 列表分组展示，并在选择 remote workspace 时向 `/api/chat` 注入 `deviceId`。

## 13. 验收标准

v1 完成时必须满足：

- 后端启动后自动生成并复用 `~/.lumi/device.secret`。
- mock 设备可通过 WebSocket 注册并出现在 `/api/devices`。
- 设备注册后先进入 `setup_required`，依赖检查通过后才进入 `online`。
- 设备 setupStatus 可通过 `/api/devices` 返回并被前端展示。
- 前端可触发远程设备重新检查，设备收到 `setup.check` 并回传 `setup.status`。
- setup 未通过的设备不能执行聊天任务。
- 设备断线后状态变 offline，但列表保留设备。
- 同一设备重复连接时，新连接生效，旧连接关闭。
- 无 `deviceId` 的聊天路径与当前行为一致。
- 有 `deviceId` 的聊天任务能下发到设备。
- 设备回传文本、工具调用、权限请求、done/error 能被浏览器通过现有 SSE 接收。
- 单设备运行中时再次选择该设备执行会返回 `Device is busy`。
- 浏览器 SSE 断开会触发设备 `task.cancel`。
- 权限确认能路由回设备。
