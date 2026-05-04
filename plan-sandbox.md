# Lumi V1 Docker 沙箱细化方案

## 1. 目标

本方案定义 Lumi 引入 Docker 沙箱能力的 V1 最终实现方式。实现必须以当前仓库真实结构为准，目标是让后续实现者不再做运行时、接口、生命周期和用户交互层面的决策，直接按文档完成实现。

V1 的实现结果固定为：

- Lumi 新增 1 类工作区：`sandbox workspace`。
- `sandbox workspace` 的聊天执行运行在 Docker 容器中，而不是 Backend 本机进程中。
- `sandbox workspace` 的文件访问、HTML preview、公开 share 都访问同一容器化 runtime。
- 主 Backend 继续复用现有 `/api/chat`、workspace HTTP API、`device` WebSocket 协议和 `device-executor`。
- 沙箱容器在 Lumi 内部表现为隐藏 device，而不是新的公开设备类型。
- 前端继续通过 `workspaceId` 使用沙箱工作区，不直接感知 Docker 或隐藏 `deviceId`。

本方案参考项目固定为：

- 参考项目：`/Users/pengmd/c/Wegent`
- 参考笔记：`/Users/pengmd/c/Wegent/learn/sandbox-hybrid-runtime/lesson-01.html`

---

## 2. 明确取舍

### 2.1 借鉴 Wegent 的部分

Wegent 只借鉴下面这些核心思路，不借整套基础设施和语言选型：

- 把“隔离运行时”抽成独立生命周期域，而不是在聊天逻辑里临时 `docker run`。
- 把“容器生命周期”和“聊天执行 / 文件访问”拆成不同职责层。
- 为沙箱定义明确状态模型，而不是只有“起了 / 没起”。
- 允许容器复用、keep-alive、健康检查、垃圾回收和启动恢复。
- 让聊天执行和文件访问都挂到同一运行时，而不是各自维护一套状态。

### 2.2 不采用 Wegent 的部分

Lumi V1 不引入下面这些结构：

- 不引入 E2B 兼容层。
- 不引入 Wegent 的 Python `executor_manager`。
- 不引入 Redis 持久化模型。
- 不引入独立 HTTP executor 执行协议。
- 不引入 Kubernetes / Kruise / 多节点调度语义。
- 不引入前端专用沙箱 API 作为主路径。

### 2.3 固定结论

结论固定为：

- 借鉴 Wegent 的“独立生命周期服务 + 容器复用 + 统一运行时入口”。
- 不照搬 Wegent 的协议兼容层和基础设施选型。
- Lumi 首期以单机本地 Docker 为实现边界。
- Lumi 首期以复用现有 `device-executor` 为优先，不重写第二套执行器。

---

## 3. 当前仓库约束

### 3.1 聊天执行主链约束

当前 `backend/internal/api/chat.go` 已经具备两条聊天主链：

- 本机聊天：`/api/chat -> local agent process`
- 远程设备聊天：`/api/chat -> device.Registry -> device-executor`

当前代码已经有成熟的：

- conversation 准备
- SSE 事件流
- permission request 转发
- session 复用
- task cancel

固定要求：

- `/api/chat` 外部接口不改。
- 现有 SSE 事件名不改。
- 本机聊天链路保持原样。
- 已有 remote device 聊天链路保持原样。

### 3.2 设备协议约束

当前仓库已经存在一套可用的设备协议基础设施：

- `backend/internal/device/protocol.go`
- `backend/internal/device/registry.go`
- `backend/internal/device/connection.go`
- `backend/cmd/device-executor`

这套协议已经支持：

- `task.execute`
- `task.session`
- `task.event`
- `task.done`
- `task.error`
- `task.cancel`
- `permission.request`
- `permission.confirm`
- `workspace.tree/files/meta/text/buffer/changes/diff/upload/cleanup`

因此 V1 的固定结论是：

- Docker 沙箱不新发明一套运行时协议。
- 沙箱容器内部继续运行现有 `device-executor`。
- 主 Backend 把沙箱容器视为受管隐藏设备。

### 3.3 现有设备列表暴露约束

当前 `/api/devices` 直接把 `Registry.ListDevices()` 返回值映射为 `DeviceDTO` 输出。

这意味着：

- 沙箱容器一旦注册为普通 device，就会污染当前设备列表。
- “隐藏设备”不能只靠命名约定或内部 map 暗示，必须在 `device.Device` 层有显式语义。

### 3.4 文件访问与分享链路约束

当前远程工作区文件访问已经具备基础设施：

- `backend/internal/api/remote_workspace.go`
- `device.Registry.SendWorkspaceRequest(...)`
- `backend/cmd/device-executor/workspace.go`

但 `share` 与公开 HTML preview 仍有大量本机读盘逻辑：

- `backend/internal/api/shares.go`
- `backend/internal/api/html_preview.go`

这意味着 V1 如果只改 workspace API，不改 share / HTML preview，后续一定会返工。

固定结论：

- V1 一次性抽统一 runtime 文件访问层。
- workspace API、HTML preview、share 都走同一层。

### 3.5 工作区与前端创建入口约束

当前工作区模型已经支持 `kind` 分叉，但前端 `WorkspaceSelector` 和后端 `createWorkspace` 只支持：

- `local`
- `remote`

这意味着 `sandbox workspace` 如果要进 V1 UI，就必须把：

- 前端创建向导
- 后端创建接口
- workspace 列表状态展示

一起扩展。

### 3.6 启动模型约束

当前桌面入口只启动一个 `api.Server`，没有现成第二个 OS 进程的编排和生命周期管理。

固定结论：

- V1 的 `sandbox-manager` 不做独立 OS 进程。
- 改为主 Backend 内嵌模块，同时提供本地 HTTP 接口。

### 3.7 并发约束

当前 `device-executor` 一次只接受一个运行中的聊天 task，再来的聊天任务会返回 busy。

固定结论：

- 同一 `sandbox workspace` 的聊天执行维持单活语义。
- 并发聊天返回 busy，不做排队。
- 文件访问请求不受聊天 busy 限制，仍可继续执行。

### 3.8 运行环境约束

V1 依赖本机 Docker daemon。当前开发环境检查结果显示 Docker socket 尚未连接成功，因此后续联调默认需要先恢复 Docker 本地运行环境。

这同时意味着 V1 必须把 Docker 不可用定义成明确的产品状态，而不是让用户在聊天失败时看到底层 socket、SDK 或容器错误。

固定要求：

- 后端必须提供 sandbox Docker preflight 能力，用于创建表单和首次 runtime access 前的环境检查。
- Docker daemon 未运行、Docker socket 无权限、镜像不存在 / 拉取失败、宿主机回连地址推导失败、`device-executor` 回连超时，都必须归一化为稳定错误码。
- 前端必须把这些错误码翻译成用户可理解的提示和恢复动作。
- 不允许把原始 Docker SDK error、socket path error 或容器启动日志作为主错误文案直接展示给用户。
- 创建 `sandbox workspace` 不强制要求 Docker 当前可用；如果 Docker 不可用，允许用户保存 workspace，但必须显示该 workspace 需要修复运行环境后才能启动。

---

## 4. V1 固定架构

### 4.1 总体结构

V1 引入一个内嵌的 `sandbox-manager` 模块，由主 Backend 启动并持有。

主链路固定为：

```text
Browser
  |
  +--> /api/chat
  |
  +--> /api/workspaces/*
  |
  +--> /api/public/shares/*
          |
          v
      Backend
          |
          v
   RuntimeResolver
      |      |      |
      |      |      +--> sandbox -> sandbox-manager -> Docker container -> hidden device -> device-executor
      |      +---------> remote  -> device.Registry -> remote device-executor
      +----------------> local   -> local services / local agent
```

### 4.2 沙箱工作区语义

`sandbox workspace` 在用户视角上仍是普通工作区，但运行时固定为：

- 宿主机目录 `Path`
- 容器内固定工作目录 `/workspace`
- 容器内运行的 `device-executor`
- 主 Backend 维护的隐藏 `deviceId`

对前端来说：

- 仍然只选择 `workspaceId`
- 仍然调用 `/api/chat`、`/api/workspaces/*`、公开 share API
- 不直接理解 `deviceId`、容器名或 Docker 细节

这里的 `hidden device` 是 Backend 为复用现有 `device` 协议和 `device-executor` 而引入的内部运行时抽象，不是对用户公开的设备对象。

用户和前端始终面向 `workspace` 操作：创建、打开、聊天、文件访问、share 都只通过 `workspaceId` 进行；前端不需要也不应该感知对应的 `deviceId`、容器名或 Docker 运行细节。

对 Backend 来说，`sandbox workspace` 的实际执行目标仍然是一个由容器内 `device-executor` 注册回来的隐藏 device，只是这层映射完全封装在服务端内部。

### 4.3 文件持久化语义

V1 固定规则：

- `WorkspaceConfig.Path` 对 `sandbox workspace` 表示宿主机上的真实项目目录。
- 该目录通过 Docker bind mount 挂到容器内固定路径 `/workspace`。
- 容器中的 agent 和 `device-executor` 只看到容器内路径，例如 `/workspace`。
- Backend 在 `sandbox workspace` 模式下不得直接读取宿主机 `Path` 作为主文件访问路径。

路径映射固定如下：

```text
Host machine                           Docker container
+-------------------------------+      +-------------------------+
| WorkspaceConfig.Path          |      | fixed container path    |
| /Users/me/project             |=====>| /workspace              |
| real persisted project files  | bind | same files in container |
+-------------------------------+ mount+-------------------------+
            ^                                       |
            |                                       |
            | host persistence                      | agent / device-executor
            |                                       | only sees container path
            +------------------ Backend ------------+
                               sandbox file access
                                       |
                               hidden device request
                                       |
                              always uses /workspace/...
```

这样同时满足：

- 文件持久化到宿主机
- 执行环境与 Backend 进程隔离
- 容器重建后仍能看到同一工作区文件

### 4.4 不采用的架构

V1 明确不采用：

- 独立 OS 进程的 `sandbox-manager`
- Backend handler 内直接 `docker run`
- 容器内部额外暴露第二套文件 API
- 不经 `device-executor` 直接向容器 POST prompt
- 单独的“先启动沙箱再聊天”用户流程

---

## 5. 配置与公开接口变更

### 5.1 WorkspaceConfig 扩展

扩展 `backend/internal/config/WorkspaceConfig`，新增字段：

```go
type WorkspaceConfig struct {
    ID             string   `json:"id"`
    Name           string   `json:"name"`
    Path           string   `json:"path"`
    Kind           string   `json:"kind,omitempty"`
    DeviceID       string   `json:"deviceId,omitempty"`
    DeviceName     string   `json:"deviceName,omitempty"`
    RemotePath     string   `json:"remotePath,omitempty"`

    Image          string   `json:"image,omitempty"`
    IdleTimeoutSec int      `json:"idleTimeoutSec,omitempty"`
    Agents         []string `json:"agents,omitempty"`
}
```

字段语义固定为：

- `Kind == "sandbox"`：表示工作区运行在 Docker 沙箱中
- `Path`：宿主机绝对路径
- `Image`：Docker 镜像，允许覆盖默认镜像
- `IdleTimeoutSec`：空闲超时，默认 1800 秒
- `Agents`：该工作区允许使用的 agent ID 列表；为空表示使用全局 agent 集合

额外固定规则：

- V1 容器内工作目录固定为 `/workspace`
- `mountPath` 不作为用户可配置字段，也不写入 `WorkspaceConfig`

### 5.2 默认工作区限制

V1 固定限制：

- `sandbox workspace` 不能成为默认工作区

原因：

- 当前应用启动和首页加载阶段不应隐式拉起容器
- 避免“打开应用就自动起 Docker runtime”的复杂启动路径

### 5.3 /api/workspaces 响应扩展

`GET /api/workspaces` 对 sandbox workspace 返回额外状态字段：

- `sandboxStatus`: `pending | running | failed | terminating | terminated`
- `sandboxStage`: `checking_docker | preparing_image | starting_container | connecting_executor` 可选
- `sandboxReady`: `bool`
- `sandboxExpiresAt`: `int64`
- `sandboxError`: `string` 可选

本地和 remote workspace 不需要伪造这些字段。

额外固定规则：

- 前端冷启动和失败恢复所需的启动阶段信息，统一通过公开 workspace 状态字段获取。
- 前端不直接调用内部 `/sandboxes/*` 接口获取启动细节。
- 方案中的“检查 Docker / 准备镜像 / 启动容器 / 连接执行器”分步 UI，以 `sandboxStage` 为唯一公开数据来源。

### 5.4 createWorkspace 扩展

`POST /api/workspaces` 需要支持 `kind=sandbox`。

创建请求至少支持：

- `name`
- `path`
- `kind`
- `image`
- `idleTimeoutSec`
- `agents`

校验规则固定为：

- `name`、`path` 必填
- `path` 必须为宿主机绝对路径
- `image` 为空时回落到默认镜像
- `idleTimeoutSec <= 0` 时回落到默认值
- 若 `agents` 非空，则所有 agent ID 必须存在于全局配置
- 不允许把 sandbox workspace 设为默认 workspace
- 容器内工作目录固定为 `/workspace`，创建请求不接收 `mountPath`

创建与首次启动的关系固定如下：

```text
Frontend create form
  -> POST /api/workspaces {kind=sandbox, path, image, idleTimeoutSec, agents}
  -> Backend validates request
  -> persist WorkspaceConfig
  -> return workspaceId
  -> no Docker container is created here

First runtime access later
  -> /api/chat or /api/workspaces/* or /api/public/shares/*
  -> RuntimeResolver
  -> sandbox-manager.ensure(workspaceId)
  -> create or reuse sandbox runtime
```

### 5.5 sandbox preflight 接口

新增面向前端的轻量 preflight 接口，用于 sandbox workspace 创建表单和首次进入前的环境提示：

- `POST /api/workspaces/sandbox/preflight`

请求字段：

- `path`：宿主机绝对路径，可选；创建表单中已填写时传入
- `image`：Docker 镜像，可选；为空时检查默认镜像
- `checkImagePull`：可选，默认 `false`

响应字段固定为：

```json
{
  "ok": true,
  "code": "ready",
  "message": "",
  "recoverable": true,
  "details": ""
}
```

`code` 固定枚举：

- `ready`：Docker 环境可用
- `path_invalid`：`path` 不是合法宿主机绝对路径，或不可访问
- `docker_unavailable`：未连接到 Docker daemon，例如 Docker Desktop 未启动
- `docker_permission_denied`：当前 Backend 进程无权访问 Docker socket
- `image_missing`：镜像本地不存在；当 `checkImagePull=false` 时这是 warning，不代表不可创建
- `image_pull_failed`：镜像拉取失败
- `host_connect_unresolved`：无法自动推导容器回连 Backend 的宿主机地址
- `executor_registration_timeout`：容器已启动，但容器内 `device-executor` 未在限定时间内注册为 hidden device；该错误只会出现在 runtime ensure，不会出现在快速 preflight
- `unknown`：未归类错误

行为规则固定为：

- 创建表单默认调用 `checkImagePull=false` 的快速 preflight，避免打开表单时触发耗时镜像拉取。
- 如果用户显式点击“检查镜像”或首次 runtime ensure 需要拉取镜像，才允许进入 `image_pull_failed`。
- preflight 失败不直接创建或销毁容器。
- preflight 不替代 `sandbox-manager.ensure` 的运行时检查；`ensure` 必须复用同一套错误码和用户提示语义。
- 前端展示 `message` 时必须优先使用本地化的产品文案，`details` 只用于展开调试信息或日志。

---

## 6. 隐藏 Device 语义

### 6.1 Device 结构扩展

扩展 `device.Device` 和 `device.DeviceDTO`：

```go
type Device struct {
    ...
    Hidden bool `json:"hidden,omitempty"`
}
```

### 6.2 固定行为

规则固定为：

- 沙箱容器注册出来的 device 一律 `Hidden=true`
- `/api/devices` 默认过滤隐藏 device
- 用户不能对隐藏 device 执行 alias、删除、配对等普通设备操作
- Backend 内部通过 `workspaceId -> sandbox runtime -> hidden deviceId` 路由到沙箱

### 6.3 不采用的策略

V1 不采用：

- 只用命名规则暗示“这是隐藏设备”
- 只靠 `WorkspaceID` 推断是否隐藏
- 只在 runtime resolver 里维护内部 map 而不扩展 `Device`

---

## 7. sandbox-manager 服务设计

### 7.1 形态

`sandbox-manager` 作为主 Backend 内嵌模块启动，同时暴露本地 HTTP 接口，供内部调用、调试和测试。

它不是独立 OS 进程，也不需要新的桌面进程编排。

代码组织固定按下面的结构实现：

```text
backend/internal/sandbox/
  manager.go
    生命周期主入口：ensure / recover / terminate / reuse
  store.go
    sandboxes.json 持久化
  health.go
    容器就绪 / hidden device 在线检查
  scheduler.go
    GC / keepalive / 启动恢复
  docker/
    client.go
      官方 Docker Go SDK 封装
    containers.go
      create / start / inspect / stop / remove
    images.go
      image resolve / pull / check
    labels.go
      label build / query / recovery helpers
```

固定边界为：

- `backend/internal/sandbox/*` 负责沙箱控制面
- `backend/internal/sandbox/docker/*` 是唯一允许直接调用 Docker Go SDK 的位置
- `/api/chat`、workspace API、share API 只允许通过 `RuntimeResolver -> sandbox-manager` 进入沙箱，不得直接调用 Docker SDK

### 7.2 本地 HTTP 接口

提供如下接口：

- `POST /sandboxes/ensure`
- `GET /sandboxes/{workspaceId}`
- `POST /sandboxes/{workspaceId}/keepalive`
- `DELETE /sandboxes/{workspaceId}`

这些接口只供 Backend 内部调用和本地调试使用，不给前端直接使用。

### 7.3 职责

职责固定为：

- 维护 `workspaceId` 级别的容器生命周期
- 创建 / 复用 / 终止容器
- 状态持久化与启动恢复
- 健康检查、keep-alive、GC、orphan container 清理
- 生成并注入容器内 `device-executor` 配置
- 通过 `backend/internal/sandbox/docker/*` 统一封装 Docker Go SDK 调用

### 7.4 状态持久化

V1 不引入 Redis，状态持久化固定为本地文件：

- `~/.lumi/runtime/sandboxes.json`

每条记录至少包含：

- `workspaceId`
- `deviceId`
- `containerName`
- `image`
- `hostPath`
- `workspacePath`
- `status`
- `createdAt`
- `startedAt`
- `lastActivityAt`
- `expiresAt`
- `errorMessage`

### 7.5 容器标签

所有沙箱容器必须带如下 Docker labels：

- `lumi.runtime=sandbox`
- `lumi.workspace_id=<workspaceId>`
- `lumi.device_id=<deviceId>`

用途固定为：

- 启动恢复
- orphan container 清理
- 调试定位

### 7.6 状态模型

内部状态枚举固定为：

- `pending`
- `running`
- `failed`
- `terminating`
- `terminated`

状态含义固定为：

- `pending`：容器已创建，隐藏 device 尚未完成注册
- `running`：容器已启动，隐藏 device 已在线
- `failed`：启动失败或健康检查失败
- `terminating`：正在销毁
- `terminated`：已销毁

### 7.7 复用规则

复用粒度固定为：

- `workspaceId`

固定行为：

- 首次访问 `sandbox workspace` 时创建容器
- 同一 `workspaceId` 后续访问复用同一容器
- 每次聊天或文件访问都会刷新 `LastActivityAt`
- 超时或失活后，下次访问自动重建

---

## 8. 沙箱生命周期与管理

### 8.1 生命周期拥有者

沙箱生命周期固定由 `sandbox-manager` 统一持有和管理。

管理粒度固定为：

- `workspaceId`

这意味着：

- 生命周期归属的是 `workspaceId` 对应的 runtime，而不是 conversation
- 一个 runtime 同时包含容器、隐藏 `deviceId`、状态记录和过期时间
- Backend 内部不得绕过 `sandbox-manager` 直接创建、复用或销毁沙箱容器
- Docker 控制面能力固定经由 `backend/internal/sandbox/docker/*` 进入官方 Docker Go SDK
- 聊天、workspace 文件访问、share 访问继续走现有 device 协议，不直接调用 Docker SDK

从前端入口看沙箱 runtime 的完整链路如下：

```text
Frontend
  |
  | open chat / tree / preview / share
  | request only carries workspaceId
  v
Backend API
  |
  | /api/chat
  | /api/workspaces/*
  | /api/public/shares/*
  v
RuntimeResolver
  |
  | resolve workspaceId -> workspace.kind == sandbox
  v
sandbox-manager.ensure(workspaceId)
  |
  +--> runtime already running
  |      -> reuse existing hidden device
  |
  +--> runtime missing / expired / failed
         -> create or rebuild Docker container
         -> inject device-executor config
         -> start device-executor connect --skip-setup
         -> wait hidden device register online
  |
  v
Backend runtime routing
  |
  | handleDeviceChat / workspace request / share request
  v
Docker container
  |
  +--> hidden device = device-executor
  +--> agent task execution
  +--> workspace file access under /workspace
  |
  v
response / SSE / file payload back to Frontend
  |
  v
refresh LastActivityAt / ExpiresAt
  |
  +--> idle timeout / DELETE / GC / failure -> terminate container
  \--> next frontend access -> ensure again
```

生命周期状态总览如下：

```text
first ensure / first access
          |
          v
    +-----------+
    |  pending  |
    +-----------+
      |       |
      |       +------------------------------+
      | startup / health failure             |
      v                                      |
 +-----------+                               |
 |  failed   |                               |
 +-----------+                               |
      |                                      |
      | next ensure / rebuild                |
      v                                      |
    +-----------+  hidden device online  +-----------+
    |  pending  | ----------------------> |  running  |
    +-----------+                         +-----------+
                                               |
                                               | idle timeout / delete / GC /
                                               | unrecoverable runtime failure
                                               v
                                         +-------------+
                                         | terminating |
                                         +-------------+
                                               |
                                               v
                                         +------------+
                                         | terminated |
                                         +------------+
                                               |
                                               +--> next access -> pending
```

### 8.2 创建与进入运行态

V1 不做应用启动预热，也不做“打开工作区即后台自动起容器”。

创建触发点固定为：

- `POST /sandboxes/ensure`
- `/api/chat`
- `/api/workspaces/*`
- `/api/public/shares/*`

前提是命中的 `workspace.kind == sandbox`。

`ensure` 流程固定为：

```text
workspaceId
  -> 读取 workspace 配置（image / idleTimeoutSec / agents）
  -> 使用固定容器工作目录 /workspace
  -> 查询持久化 runtime 记录与 Docker labels
  -> 若已有可复用且健康的 runtime，则直接返回
  -> 否则创建或重建容器
  -> 注入 device-executor 配置并启动容器
  -> 等待 hidden device 注册成功
  -> 状态从 pending 进入 running
```

如果容器启动、回连或健康检查失败，则该次 `ensure` 进入 `failed`，由下一次访问重新触发恢复或重建。

### 8.3 复用、Keep-Alive 与过期

复用规则固定为同一 `workspaceId` 复用同一 runtime。

Keep-Alive 与过期规则固定为：

- 每次成功命中 sandbox runtime 的聊天、文件访问或 share 访问都会刷新 `LastActivityAt`
- 刷新后同步更新 `ExpiresAt`
- 空闲超时取 `WorkspaceConfig.IdleTimeoutSec`，默认 1800 秒
- runtime 超时或失活后不做 pause / resume，统一在下次访问时按重建处理

换句话说，V1 的空闲策略是“过期后丢弃，下次再建”，而不是长期保活或后台预热。

### 8.4 终止、销毁与清理

终止触发来源固定为：

- `DELETE /sandboxes/{workspaceId}`
- 空闲超时后的 GC 回收
- 启动恢复时发现 orphan container
- 运行时进入不可恢复失败，需要销毁后重建

终止流程固定为：

```text
running | failed
  -> terminating
  -> 停止并删除 Docker container
  -> 清理 runtime 状态与关联映射
  -> terminated
```

`terminated` runtime 不再复用；后续再次访问同一 `workspaceId` 时，按全新 runtime 重新创建。

### 8.5 启动恢复与管理入口

主 Backend 启动后，`sandbox-manager` 必须基于以下两类信息执行恢复：

- `~/.lumi/runtime/sandboxes.json`
- Docker labels

恢复用途固定为：

- 恢复已有 runtime 的状态
- 识别仍然存在的容器与其 `workspaceId` / `deviceId`
- 清理没有有效状态归属的 orphan container

管理入口固定为：

- `POST /sandboxes/ensure`：确保 runtime 可用，不可用时创建或重建
- `GET /sandboxes/{workspaceId}`：查询当前 runtime 状态
- `POST /sandboxes/{workspaceId}/keepalive`：显式刷新存活时间
- `DELETE /sandboxes/{workspaceId}`：主动销毁 runtime

这些入口只供 Backend 内部调用和本地调试使用，不对前端暴露为公开产品接口。

管理动作的整体闭环如下：

```text
1. Backend startup
   -> load ~/.lumi/runtime/sandboxes.json
   -> inspect Docker labels
   -> recover known runtimes
   -> cleanup orphan containers

2. Frontend access
   -> open chat / tree / preview / share
   -> call Backend with workspaceId

3. Backend ensure
   -> RuntimeResolver
   -> sandbox-manager.ensure(workspaceId)
   -> reuse running runtime, or create/rebuild container

4. Container online
   -> device-executor registers as hidden device
   -> downstream chat / workspace / share is routed into container

5. Runtime maintenance
   -> refresh LastActivityAt / ExpiresAt on every successful access
   -> GC expired runtimes
   -> handle DELETE /sandboxes/{workspaceId}

6. Runtime end
   -> sandbox-manager terminates container
   -> next frontend access restarts from step 2
```

这里的职责分界固定为：

- Docker SDK 只负责 image / container / inspect / labels / lifecycle 管理
- `device-executor` 和现有 device 协议负责 chat、workspace request、permission、session 与事件流

---

## 9. 容器与镜像策略

### 9.1 单一通用镜像

V1 使用单一通用镜像，默认由 Lumi 维护一份：

- 预装 `device-executor`
- 预装 `npm` / `npx`
- 预装常用 agent CLI
- 预装 ACP 依赖

默认镜像名固定为：

- `lumi/sandbox:latest`

`device-executor` 必须作为镜像内容预装在沙箱镜像内，不允许在容器启动后通过临时下载、安装脚本或入口命令动态安装。

`WorkspaceConfig.Image` 允许覆盖默认镜像，但默认路径固定走通用镜像。

### 9.2 默认镜像 Dockerfile

V1 必须在仓库中提供默认 sandbox 镜像 Dockerfile。

固定位置：

- `docker/sandbox/Dockerfile`
- `docker/sandbox/build-device-executor.sh`

固定构建上下文为仓库根目录。镜像构建前先在宿主机编译 Linux 版 `device-executor`：

```bash
./docker/sandbox/build-device-executor.sh
docker build -f docker/sandbox/Dockerfile -t lumi/sandbox:latest .
```

如果显式构建 amd64 镜像，则先编译 amd64 二进制并指定平台：

```bash
./docker/sandbox/build-device-executor.sh amd64
docker build --platform linux/amd64 -f docker/sandbox/Dockerfile -t lumi/sandbox:latest .
```

Dockerfile 固定如下：

```Dockerfile
# syntax=docker/dockerfile:1

FROM node:22-bookworm-slim AS runtime

ARG DEVICE_EXECUTOR_BIN=docker/sandbox/bin/device-executor

ENV NODE_ENV=production
ENV NPM_CONFIG_UPDATE_NOTIFIER=false
ENV NPM_CONFIG_FUND=false
ENV NPM_CONFIG_AUDIT=false

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
       bash \
       ca-certificates \
       curl \
       git \
       openssh-client \
       tini \
    && rm -rf /var/lib/apt/lists/*

RUN npm install -g \
      @anthropic-ai/claude-code \
      @openai/codex \
      @agentclientprotocol/claude-agent-acp@0.30.0 \
      @zed-industries/codex-acp \
    && npm cache clean --force

COPY ${DEVICE_EXECUTOR_BIN} /usr/local/bin/device-executor

RUN mkdir -p /workspace /lumi/device-executor \
    && chmod 0755 /usr/local/bin/device-executor

WORKDIR /workspace

ENTRYPOINT ["/usr/bin/tini", "--", "device-executor"]
CMD ["--help"]
```

固定说明：

- V1 镜像不在 Docker build 内编译 Go；`device-executor` 由宿主机预编译后复制进镜像。
- 预编译二进制的 `GOARCH` 必须与镜像平台一致；本地默认使用宿主机架构，发布 amd64 时显式传 `amd64`。
- V1 镜像优先保证可用性和启动稳定性，不做极致瘦身。
- npm 包版本除 `@agentclientprotocol/claude-agent-acp@0.30.0` 外暂不固定；后续发布流程可以再引入版本锁定。
- V1 容器内以 root 运行，优先降低宿主机 bind mount 写入失败风险。
- 镜像不得挂载 Docker socket，也不得在容器内访问宿主机 Docker daemon。

### 9.3 容器启动模型

固定模型：

- 宿主机 `Path` bind mount 到容器固定目录 `/workspace`
- 容器内运行 `device-executor connect --server <backend-url> --token <token> --config /lumi/device-executor/config.json --skip-setup`
- `device-executor` 配置中的 `workspace` 固定为 `/workspace`

鉴权模型固定为：

- V1 复用当前 Backend `device.Registry` 的全局 bearer secret。
- `sandbox-manager` 为容器注入的 `--token <token>`，固定使用该全局 device secret。
- V1 不为每个 sandbox runtime 生成独立注册 token。
- V1 不新增第二套 sandbox 专用 websocket 鉴权协议。

### 9.4 device-executor 配置注入

`sandbox-manager` 负责生成并注入容器内 `device-executor` 配置。

配置至少包含：

- `deviceId`
- `name`
- `workspace`
- `agents`
- `defaultAgent`

规则固定为：

- `workspace = /workspace`
- 配置文件在容器内固定写入 `/lumi/device-executor/config.json`
- `agents` 取全局 agent 配置与 workspace `agents` 白名单交集；为空则使用全部全局 agent
- 若全局默认 agent 不在允许列表中，则取允许列表第一个作为 `defaultAgent`
- 容器回连 Backend 时使用的 bearer token 不写入 workspace 配置，而由 `sandbox-manager` 在启动命令层注入当前全局 device secret

### 9.5 Backend 连通策略

V1 按“宿主机直连 Backend”设计：

- 容器通过宿主机地址回连主 Backend
- 优先支持当前桌面 / 单机场景
- 按不同 OS 封装宿主地址解析逻辑

V1 不要求用户手动填写回连地址，除非自动推导失败。

---

## 10. 统一 RuntimeResolver

### 10.1 返回模型

Backend 新增统一 `RuntimeResolver`，返回例如：

```go
type ResolvedRuntime struct {
    Mode          string // local | remote | sandbox
    WorkspaceID   string
    WorkspacePath string
    DeviceID      string
    Ready         bool
    Status        string
    ExpiresAt     int64
}
```

### 10.2 行为规则

固定分支：

- `local`：继续走本机 agent 和本机文件系统
- `remote`：继续走现有 remote device
- `sandbox`：先 `ensure` runtime，再走隐藏 device

分流逻辑固定如下：

```text
workspaceId
  -> load WorkspaceConfig
  -> kind == local   -> local services / local agent
  -> kind == remote  -> device.Registry -> remote device-executor
  -> kind == sandbox -> sandbox-manager.ensure -> hidden device -> device-executor
```

### 10.3 Keep-Alive 触发点

只要命中了 `sandbox workspace` 且 runtime 可用，就刷新 keep-alive，包括：

- `/api/chat`
- `/api/workspaces/*`
- `/api/public/shares/*`

---

## 11. 聊天执行语义

### 11.1 /api/chat 接入

`sandbox workspace` 在 `/api/chat` 中被解析为特殊的 device chat：

- 先 `ensure` 对应 runtime
- 再复用现有 `handleDeviceChat` 路径
- 继续复用现有 SSE 事件流、permission request、cancel、session 回传

请求链路固定为：

```text
workspaceId
  -> 查到 workspace.kind == sandbox
  -> sandbox-manager ensure 对应 runtime
  -> 找到或恢复该 workspace 绑定的 hidden deviceId
  -> 复用现有 handleDeviceChat / workspace request 流程
```

一次 chat session 的端到端时序如下：

```text
Frontend chat UI                     Backend /api/chat                     Docker container
      |                                      |                                     |
      | POST /api/chat {workspaceId, prompt} |                                     |
      |------------------------------------->|                                     |
      |                                      | prepare conversation + SSE          |
      |                                      | RuntimeResolver.ensure              |
      |                                      |------------------------------------>|
      |                                      | reuse runtime or start container    |
      |                                      |<------------------------------------|
      |                                      | hidden device ready                 |
      |                                      | task.execute ---------------------->|
      |                                      |<------------------------- task.session
      |<------------------------- SSE session|                                     |
      |                                      |<-------------------------- task.event*
      |<-------------------------- SSE event*|                                     |
      |                                      |<------------------- permission.request
      |<------------------- SSE permission --|                                     |
      |-- permission.confirm ---------------->|------------------------------------>|
      |                                      |<---------------------- task.done/error
      |<---------------------- SSE done/error|                                     |
```

也就是说，`sandbox workspace` 对外仍是 workspace 语义，对内才被解析为 hidden device 语义；前端不会直接参与 `deviceId` 选择或 Docker 生命周期管理。

### 11.2 并发语义

V1 固定采用单活聊天任务语义：

- 同一 `sandbox workspace` 一次只允许一个运行中的聊天 task
- 第二个并发聊天请求直接返回 busy
- 不做队列
- 不按 conversation 拆多个 runtime

### 11.3 文件访问与聊天冲突

即使聊天 task busy，以下文件访问仍然允许继续执行：

- tree
- files
- meta
- text
- buffer
- html-preview
- html-asset
- changes
- diff
- share 相关只读访问

原因：

- 当前 workspace 请求走独立 request-response 通道
- 文件预览不应被聊天 busy 阻塞

---

## 12. 统一 Runtime 文件访问层

### 12.1 目标

V1 一次性抽统一 runtime 文件访问层，供以下三类功能共享：

- workspace API
- HTML preview
- share / public share

统一访问层的汇聚关系如下：

```text
/api/workspaces/*        /api/workspaces/html-*       /api/public/shares/*
         \                       |                             /
          \                      |                            /
           +---------------------+---------------------------+
                                 |
                       runtime file access layer
                       /            |             \
                      /             |              \
                   local          remote         sandbox
                     |              |               |
              local filesystem  remote device   hidden device
```

### 12.2 三类 runtime 的后端分支

固定分支为：

- `local`：走现有 `workspaceSvc`、`workspaceDiffs`、本机读写文件
- `remote`：走现有 `device.Registry.SendWorkspaceRequest`
- `sandbox`：走隐藏 device 的同一套 workspace request-response

### 12.3 覆盖能力

统一访问层至少覆盖：

- `tree`
- `files`
- `meta`
- `text`
- `buffer`
- `changes`
- `diff`
- `upload`
- `cleanup`

### 12.4 路径语义

路径安全规则保持与现有 workspace 语义一致：

- 禁止 `..` 路径逃逸
- 禁止 symlink 逃逸
- 目录当文件读返回 `is_directory`
- 不存在返回 `not_found`
- 超大或不支持文本预览文件返回现有错误码

---

## 13. HTML Preview 与 Share

### 13.1 HTML Preview

V1 继续复用现有 HTML rewrite 逻辑，但底层文件读取改为统一 runtime 文件访问层。

覆盖：

- `/api/workspaces/html-preview`
- `/api/workspaces/html-asset`

### 13.2 公开 Share

V1 完整支持 sandbox workspace 的公开 share：

- public share file meta
- public share text file
- public share file buffer
- public share HTML preview
- public share HTML asset

### 13.3 Share 创建与文件校验

`share` 创建时以下逻辑都必须改为统一 runtime 访问：

- 文件是否存在校验
- 文件元数据获取
- tool call 输出文件的元数据补全

固定结论：

- `shares.go` 中所有基于 `workspacePath` 直接读盘的主路径逻辑都要替换

---

## 14. 前端与用户反馈

### 14.1 WorkspaceSelector 扩展

前端 `WorkspaceSelector` 扩展为三类创建流程：

- `local`
- `remote`
- `sandbox`

### 14.2 sandbox 创建表单

创建 `sandbox workspace` 的表单至少包含：

- `name`
- `path`
- `image`
- `idleTimeoutSec`
- `agents`

默认值可以由后端返回或前端内置：

- 默认镜像
- 默认 `idleTimeoutSec=1800`

表单交互固定为：

- `name`、`path` 是主表单字段。
- `image`、`idleTimeoutSec`、`agents` 默认放在高级设置中，不作为普通用户创建 sandbox workspace 的必经理解成本。
- 当用户选择 `sandbox` 类型或修改 `path` / `image` 后，前端调用 `POST /api/workspaces/sandbox/preflight` 做快速检查。
- 快速检查使用 `checkImagePull=false`，不得因为打开表单或输入镜像名就自动拉取镜像。
- `path_invalid` 阻止提交，并在 `path` 字段旁显示原因。
- `docker_unavailable`、`docker_permission_denied`、`image_missing`、`host_connect_unresolved` 不阻止保存 workspace，但提交按钮旁必须显示 warning，提示该 workspace 创建后可能无法立即启动。
- 如果用户仍然提交，则保存 workspace；首次访问时继续通过 runtime ensure 给出完整状态。

### 14.3 状态展示

前端工作区列表显示 sandbox 状态：

- `pending`
- `running`
- `failed`
- `terminated`

首次访问未启动的 sandbox workspace 时：

- 聊天入口显示“启动中 / 重试中”
- 文件预览入口也显示“启动中 / 重试中”

V1 不要求用户手动点击“启动沙箱”。

### 14.4 Docker 不可用与启动失败反馈

前端必须把 sandbox runtime 启动失败展示为工作区级状态，而不是只作为某一次聊天消息的失败。

固定展示规则：

- `docker_unavailable`：显示“无法连接 Docker。请启动 Docker Desktop 后重试。”
- `docker_permission_denied`：显示“Lumi 无权访问 Docker。请检查 Docker 权限后重试。”
- `image_missing`：显示“镜像尚未在本机找到，首次启动时可能需要拉取。”
- `image_pull_failed`：显示“镜像拉取失败。请检查镜像名称、网络或登录状态后重试。”
- `host_connect_unresolved`：显示“无法自动配置容器回连地址。请检查本机网络设置后重试。”
- `executor_registration_timeout`：显示“沙箱已启动，但执行器未连接。请重试或重建运行环境。”

恢复动作固定为：

- 所有 recoverable 错误都显示“重试启动”。
- 已创建过容器但进入 `failed` 的 runtime 显示“重建运行环境”。
- Docker 相关错误显示“重新检查 Docker”。
- 展开调试信息时可以显示后端返回的 `details`，但默认主视图不展示原始 Docker 错误。

交互位置固定为：

- 工作区列表状态 badge 显示简短状态。
- 聊天输入区上方显示可操作的错误 banner。
- 文件树 / preview 区域显示同一错误状态和重试入口。
- public share 页面显示只读错误页，不暴露本机路径、Docker socket 路径、容器 ID 或 hidden `deviceId`。

### 14.5 前端 UI 与交互示意

#### 14.5.1 创建入口

```text
+--------------------------------------------------+
| Workspace Selector                               |
+--------------------------------------------------+
| [ Local ]  [ Remote ]  [ Sandbox ]               |
|                              selected --------^  |
|                                                  |
| Name        [ my-sandbox-project              ]  |
| Path        [ /Users/me/project              ]   |
|                                                  |
| Docker check:                                    |
|   [warning] Docker is not running                |
|   You can save this workspace, but it cannot     |
|   start until Docker is available.               |
|                                                  |
| Advanced settings                                |
|   Image       [ lumi/sandbox:latest        ]   |
|   Idle time   [ 1800 sec                     ]   |
|   Agents      [ All agents                   ]   |
|                                                  |
|                         [ Cancel ] [ Create ]    |
+--------------------------------------------------+
```

#### 14.5.2 创建表单 preflight 流程

```text
User selects "Sandbox"
        |
        v
fills name / path / optional image
        |
        v
POST /api/workspaces/sandbox/preflight
        |
        +--> ready
        |       -> show "Ready"
        |       -> allow create
        |
        +--> path_invalid
        |       -> inline path error
        |       -> block create
        |
        +--> docker_unavailable / permission / image_missing
                -> show warning
                -> allow create
                -> workspace may fail on first start
```

#### 14.5.3 首次进入 sandbox 的冷启动

```text
+--------------------------------------------------+
| Sandbox App                         starting...  |
+--------------------------------------------------+
| Chat                                             |
|                                                  |
|  Starting sandbox runtime                        |
|  +--------------------------------------------+  |
|  | 1. Checking Docker              done       |  |
|  | 2. Preparing image              running    |  |
|  | 3. Starting container           waiting    |  |
|  | 4. Connecting executor          waiting    |  |
|  +--------------------------------------------+  |
|                                                  |
| [ message input disabled while starting...    ]  |
+--------------------------------------------------+
```

#### 14.5.4 启动状态机

```text
not_started
     |
     | first chat / file tree / preview / share
     v
starting
     |
     +--> checking_docker
     |
     +--> pulling_image
     |
     +--> starting_container
     |
     +--> connecting_executor
     |
     +--> running
     |
     +--> failed
            |
            +--> retry_start
            |
            +--> rebuild_runtime
```

固定实现语义：

- 上述 `starting -> checking_docker / pulling_image / starting_container / connecting_executor` 阶段必须映射到 `GET /api/workspaces` 返回的 `sandboxStage`。
- 前端工作区列表、聊天区、文件树和 preview 区域展示启动进度时，只读取 workspace 状态，不直接读取内部 sandboxes 管理接口。

#### 14.5.5 Docker 不可用错误态

```text
+--------------------------------------------------+
| Sandbox App                          failed      |
+--------------------------------------------------+
| Cannot connect to Docker                         |
|                                                  |
| Docker Desktop may not be running, or Lumi     |
| may not have permission to access Docker.         |
|                                                  |
| [ Recheck Docker ]  [ Retry Start ]              |
|                                                  |
| Debug details                                    |
|   hidden by default                              |
+--------------------------------------------------+
```

#### 14.5.6 文件树 / Preview 同步错误态

```text
+----------------------+---------------------------+
| Files                | Preview                   |
+----------------------+---------------------------+
| Sandbox unavailable  | Sandbox unavailable       |
|                      |                           |
| Cannot connect       | Cannot render preview     |
| to Docker.           | until the sandbox starts. |
|                      |                           |
| [ Retry Start ]      | [ Retry Start ]           |
+----------------------+---------------------------+
```

#### 14.5.7 并发 busy 体验

```text
Tab A
+--------------------------------------------------+
| Running task in Sandbox App                      |
| Streaming response...                            |
| [ Cancel Task ]                                  |
+--------------------------------------------------+

Tab B
+--------------------------------------------------+
| Sandbox App is busy                              |
|                                                  |
| Another task is already running in this          |
| workspace. File browsing is still available.     |
|                                                  |
| [ View Running Task ]  [ Retry Later ]           |
+--------------------------------------------------+
```

#### 14.5.8 public share 失败页

```text
+--------------------------------------------------+
| Shared file unavailable                          |
+--------------------------------------------------+
| This shared file belongs to a sandbox workspace, |
| but the sandbox runtime could not be started.    |
|                                                  |
| Please try again later.                          |
|                                                  |
| Error: sandbox_unavailable                       |
+--------------------------------------------------+

Forbidden in public page:
- host path
- Docker socket path
- container ID
- hidden deviceId
```

### 14.6 默认工作区限制

前端不允许把 sandbox workspace 设为默认工作区。

---

## 15. 测试与验收

### 15.1 Backend / Runtime

- `RuntimeResolver` 正确区分 `local / remote / sandbox`
- sandbox workspace 首次访问会触发 ensure，复用时不会重复建容器
- 容器重建后聊天和文件访问可恢复
- 并发聊天命中同一 sandbox 时返回 busy
- busy 时 tree/read/meta/preview/share 仍可访问
- Docker daemon 未运行、socket 无权限、镜像拉取失败时，后端返回稳定错误码，不返回未归一化的 Docker SDK 错误作为主错误
- `POST /api/workspaces/sandbox/preflight` 不创建容器，不销毁容器，并能区分 `docker_unavailable`、`docker_permission_denied`、`path_invalid`、`image_missing`

### 15.2 Device / Container

- `docker build -f docker/sandbox/Dockerfile -t lumi/sandbox:latest .` 可以成功构建默认 sandbox 镜像
- `docker run --rm lumi/sandbox:latest --help` 可以正常输出 `device-executor` usage
- 默认镜像内 `node`、`npm`、`npx`、`claude`、`codex` 命令可用
- 默认镜像内已缓存 V1 默认 ACP package：`@agentclientprotocol/claude-agent-acp@0.30.0` 与 `@zed-industries/codex-acp`
- 沙箱容器注册为 `Hidden=true` 的 device
- `/api/devices` 不返回隐藏 device
- 容器内 `device-executor` 能正常处理 task 与 workspace 请求
- 宿主机 bind mount 后，生成文件能在宿主与容器间保持一致
- 启动恢复可从持久化状态和 Docker labels 恢复 runtime

### 15.3 Files / Share / Preview

- workspace tree/files/meta/text/buffer/changes/diff 在 sandbox 下可用
- HTML preview 与 HTML asset 在 sandbox 下可用
- share 的 meta/text/buffer/html-preview/html-asset 在 sandbox 下可用
- share 创建时的文件选择与 tool call 文件元数据补全在 sandbox 下正确

### 15.4 Frontend

- WorkspaceSelector 可创建 sandbox workspace
- 工作区列表显示 sandbox 状态
- 首次进入 sandbox workspace 时显示显式启动中
- sandbox workspace 不能设为默认工作区
- sandbox 创建表单会执行快速 preflight，并对 Docker 不可用、无权限、镜像缺失给出 inline warning
- 首次启动失败时，聊天区、文件树、preview 显示一致的工作区级错误状态和重试入口
- public share 访问 sandbox workspace 失败时显示安全的只读错误页，不泄露本机路径、容器 ID 或 hidden `deviceId`

---

## 16. 实施顺序

建议固定按以下顺序实施：

1. 扩展 `WorkspaceConfig`、`createWorkspace` 和 `/api/workspaces` 响应字段。
2. 扩展 `device.Device` / `DeviceDTO` 的 `Hidden` 语义，并让 `/api/devices` 默认过滤隐藏设备。
3. 在 Backend 中搭建固定的 `backend/internal/sandbox/` 目录结构，实现 `manager/store/health/scheduler` 与 `docker/*` 分层。
4. 在 Backend 中实现内嵌 `sandbox-manager` 模块和本地 HTTP 接口。
5. 在 `backend/internal/sandbox/docker/*` 中使用官方 Docker Go SDK 完成容器创建、查询、销毁、label 管理。
6. 实现 `sandboxes.json` 状态持久化、启动恢复、GC 和 orphan container 清理。
7. 新增 `docker/sandbox/Dockerfile`，按文档构建并验证默认镜像 `lumi/sandbox:latest`。
8. 实现容器内 `device-executor` 配置注入、镜像默认值和宿主机回连策略。
9. 新增统一 `RuntimeResolver`。
10. 新增统一 runtime 文件访问层。
11. 接入 `/api/chat`，把 sandbox workspace 转成隐藏 device 聊天。
12. 接入 workspace API、HTML preview 与公开 share。
13. 实现 `POST /api/workspaces/sandbox/preflight` 与 runtime ensure 的统一错误码。
14. 扩展前端 WorkspaceSelector 的 sandbox 创建流程、preflight warning、启动失败展示与重试入口。
15. 补 Backend、container、share、frontend 回归测试。

---

## 17. 固定假设与非目标

固定假设：

- V1 只支持单机 Docker，不支持分布式调度。
- V1 只支持 bind mount，不支持纯临时 volume 模式。
- V1 不实现 E2B 兼容层。
- V1 不引入 Redis、额外数据库或消息队列。
- V1 的沙箱镜像默认由 Lumi 维护一份通用镜像。
- V1 默认沙箱镜像先提供 `linux/amd64`，多架构镜像发布不作为 V1 必需项。
- V1 的沙箱 runtime 只对 Backend 内部可见，用户不直接操作 hidden device。

V1 非目标：

- 不实现按 conversation 的多 runtime 并发模型。
- 不实现容器 pause / resume。
- 不实现把 sandbox workspace 设为默认工作区。
- 不实现新的前端专用沙箱页面或独立控制台。
