# 远程设备完整工作区文件能力修复计划

## 1. 背景与根因

远程设备聊天执行链路已经接通，但远程工作区文件链路没有接通。

当前远程聊天执行路径会把远程工作区路径传给 `device-executor`：

```text
Browser -> /api/chat -> Backend -> WebSocket -> device-executor -> 远程 agent
```

代码上，`backend/internal/api/chat.go` 会通过 `resolveWorkspacePath()` 得到工作区路径，并在 `handleDeviceChat()` 中作为 `TaskExecutePayload.WorkspacePath` 下发给设备。

但工作区文件 API 仍然只支持 Backend 本机文件系统。对远程工作区，目前存在直接短路逻辑：

- `/api/workspaces/tree` 对远程工作区返回空树。
- `/api/workspaces/files` 对远程工作区返回空文件列表。
- `/api/workspaces/changes` 对远程工作区返回空变更。
- `/api/workspaces/diff` 对远程工作区返回空 diff。
- `/api/workspaces/file`、`/meta`、`/file-buffer`、`/html-preview` 等仍按 Backend 本机路径读取，无法正确读取远程设备文件。

因此问题不是远程 `remotePath` 没有保存或没有传给聊天执行，而是缺少一条独立的远程工作区文件访问链路：

```text
Browser -> workspace HTTP API -> Backend -> WebSocket -> device-executor -> 远程文件系统
```

## 2. 最高优先级约束：不得影响非远程工作区

本次修复必须作为远程工作区新增能力实现，不能改变本地工作区现有行为。

固定分叉原则：

```go
if isRemoteWorkspaceConfig(ws) {
    // 只走 device-executor 远程文件协议
} else {
    // 保持现有 workspaceSvc / workspaceDiffs / os.Open / upload 逻辑
}
```

硬性要求：

- 非远程工作区的 `/api/workspaces/tree`、`/files`、`/file`、`/meta`、`/file-buffer`、`/html-preview`、`/html-asset`、`/changes`、`/diff`、`/api/upload`、`/api/upload/cleanup` 响应 shape、错误码和路径语义必须保持兼容。
- `/api/chat` 不带 `deviceId` 时仍走当前本机 agent 执行链路，不接触 `device-executor`。
- 本地工作区仍只访问 Backend 本机文件系统。
- 远程设备离线、setup 未完成、WebSocket 错误或协议超时，不得影响本地工作区文件浏览、预览、上传和聊天。
- 公共 helper 可以抽取，但所有远程特有行为必须收敛在远程分支、设备协议层或新的远程工作区访问层内。
- 必须补充本地工作区回归测试，证明非远程链路没有被改坏。

## 3. 目标范围

实现“完整远程工作区”能力，让远程设备工作区在 UI 中像本地工作区一样可识别、浏览和读取文件。

本次范围包含：

- 远程工作区文件树。
- 远程工作区文件搜索和 `@file` 候选。
- 远程文本/Markdown/HTML 文件读取。
- 远程文件 meta。
- 远程二进制文件 buffer，用于图片、PDF 等预览。
- 远程 HTML preview 与 HTML asset 代理。
- 远程工作区 changes 和 diff。
- 远程上传到 `.lumi-uploads`。
- 远程 upload cleanup。
- 分享相关的远程文件读取和校验。

不改变：

- 本地 workspace API 外部接口。
- 前端现有 workspace API 调用方式。
- 本机聊天执行链路。
- 远程聊天执行链路的基本任务协议，除非为了复用设备 request/response 基础设施做兼容扩展。

## 4. 协议设计

在设备 WebSocket 协议中新增远程工作区 request/response 消息。

新增消息类型建议：

```text
workspace.tree
workspace.files
workspace.meta
workspace.text
workspace.buffer
workspace.html
workspace.changes
workspace.diff
workspace.upload
workspace.cleanup
workspace.response
```

所有 workspace 请求必须包含：

```json
{
  "requestId": "string",
  "workspacePath": "/absolute/remote/root",
  "path": "relative/path/when-needed"
}
```

通用响应格式：

```json
{
  "requestId": "string",
  "ok": true,
  "payload": {}
}
```

通用错误格式：

```json
{
  "requestId": "string",
  "ok": false,
  "error": {
    "code": "not_found|invalid_path|path_escape|is_directory|unsupported|offline|timeout|internal",
    "message": "string"
  }
}
```

二进制内容传输：

- `workspace.buffer` v1 可使用 base64 响应，并设置明确大小上限。
- 超过上限返回 `unsupported` 或 `too_large` 类错误。
- 如果后续需要大文件流式传输，再扩展 chunk 协议；本次不要求实现无限大小文件代理。

上传传输：

- Backend 接收浏览器 multipart 后，将文件名和内容发送给设备。
- 设备写入远程 `${workspacePath}/.lumi-uploads`。
- 设备返回上传后的远程文件路径、原始文件名和 size。

## 5. 后端实现计划

### 5.1 新增远程工作区访问层

新增一个后端内部访问层，用于隐藏本地/远程分支差异。

建议职责：

- 接收 `WorkspaceConfig` 和请求参数。
- 若是本地工作区，调用现有 `workspaceSvc`、`workspaceDiffs`、`os.Open`、upload 逻辑。
- 若是远程工作区，校验 `DeviceID`、设备在线状态和 setup 状态，然后通过 `device.Registry` 发 workspace 请求。
- 将设备错误码映射为现有 workspace HTTP 错误。

这样 API handler 保持简洁，并且本地分支可以尽量少动。

### 5.2 扩展 device.Registry request/response 能力

当前设备协议主要围绕 task 执行和事件桥接。远程工作区需要通用请求响应能力。

实现要求：

- Backend 发送 workspace 请求时生成 request id。
- Registry 维护 pending request map。
- 设备返回 `workspace.response` 后按 request id 唤醒等待方。
- 每个请求有超时，默认建议 10-30 秒，文件 tree 和 diff 可使用较长但有限的超时。
- 设备断开时，所有 pending request 失败并返回明确错误。
- 不能占用或污染现有 task run 路由。

### 5.3 改造 workspace HTTP handlers

以下 handler 对远程工作区改为调用远程访问层，不再返回空或读取 Backend 本机路径：

- `handleWorkspaceTree`
- `handleWorkspaceFiles`
- `handleWorkspaceChanges`
- `handleWorkspaceDiff`
- `handleWorkspaceFile`
- `handleWorkspaceFileMeta`
- `handleWorkspaceFileBuffer`
- `handleWorkspaceHTMLPreview`
- `handleWorkspaceHTMLAsset`
- `handleFileUpload`
- `handleFileCleanup`
- `CleanupUploads`

本地工作区分支保留现有实现。

### 5.4 HTML preview 处理

远程 HTML preview 仍由 Backend 返回给浏览器，但 HTML 文件内容和 asset 内容来自远程设备。

规则：

- `html-preview` 读取远程 HTML 文本后，继续复用现有 HTML rewrite 逻辑。
- `html-asset` 根据 workspace id 和 relative path 向设备读取远程资源。
- CSS asset 需要继续做 root-relative URL rewrite。
- 非 HTML/CSS 资源按 buffer 代理返回。

### 5.5 分享能力接入

分享逻辑目前多处按 `workspacePath` 读取本机文件。远程工作区需要改为使用同一工作区访问层。

涉及行为：

- 创建 share 时校验选择的文件是否存在。
- 读取 public share 文件 meta。
- 读取 public share 文本内容。
- 读取 public share HTML preview 和 asset。
- 从 tool call 中提取并补充文件 metadata 时，对远程 workspace 通过设备查询。

分享接口的外部响应 shape 不变。

## 6. device-executor 实现计划

### 6.1 新增 workspace request handler

`device-executor` 在 WebSocket client 消息分发中新增 workspace 消息处理。

每个请求在目标设备本机执行：

- `workspace.tree`：调用 `internal/workspace.Service.ListTree(workspacePath)`。
- `workspace.files`：按现有后端 `listWorkspaceFiles` 行为搜索文件。
- `workspace.meta`：调用 `StatFile(workspacePath, path)`。
- `workspace.text`：调用 `ReadTextFile(workspacePath, path)`。
- `workspace.buffer`：解析文件、限制大小、读取 bytes、base64 返回。
- `workspace.changes`：调用 `ChangesService.ListChanges(workspacePath)`。
- `workspace.diff`：调用 `ChangesService.UnifiedDiff(workspacePath, path)`。
- `workspace.upload`：写入 `${workspacePath}/.lumi-uploads`。
- `workspace.cleanup`：删除 `${workspacePath}/.lumi-uploads`。

### 6.2 路径安全

设备侧必须把 `workspacePath` 当作 root，所有 relative path 都必须经过现有 workspace resolver 或等价逻辑校验。

要求：

- 禁止 `..` 路径逃逸。
- 禁止 symlink 逃逸。
- 目录当文件读返回 `is_directory`。
- 不存在返回 `not_found`。
- 不支持的 preview 类型返回 `unsupported`。
- 不允许 Backend 传任意绝对文件路径让设备读取 root 外文件。

### 6.3 并发与任务隔离

远程工作区读操作可以与聊天任务并发，但必须满足：

- 不改变“单设备同一时间只允许 1 个运行中聊天任务”的规则。
- workspace request 不占用 task slot。
- 每个 workspace request 独立超时。
- 设备关闭或重连时 pending request 返回错误。

## 7. 前端影响

前端 API shape 尽量不变，因此大部分 UI 不需要改。

可能需要的小改动：

- 远程 workspace 请求失败时，显示明确错误，而不是误认为空工作区。
- workspace preview 的 loading/error 文案可以复用现有机制。
- `WorkspaceSelector` 继续使用已有 `deviceStatus` 和 `setupReady` 展示状态。

不需要改：

- `sendMessage()` 的 deviceId 选择逻辑。
- workspace tree 组件的数据结构。
- text/image/PDF/HTML preview 的 URL 构造函数。

## 8. 测试计划

### 8.1 后端远程集成测试

新增或更新测试：

- 远程 workspace tree 返回设备侧真实文件树。
- 远程 workspace files 返回匹配文件。
- 远程 workspace text file 返回内容和 meta。
- 远程 workspace meta 返回 size、modifiedAt、mime、previewKind。
- 远程 workspace buffer 返回图片或二进制内容。
- 远程 HTML preview 能读取 HTML 并重写 asset URL。
- 远程 HTML asset 能代理 CSS、图片等资源。
- 远程 changes 返回设备侧 git/worktree 变更。
- 远程 diff 返回设备侧 diff。
- 远程 upload 写入远程 `.lumi-uploads` 并返回文件列表。
- 远程 cleanup 删除远程上传目录。

### 8.2 错误测试

覆盖：

- 远程设备不存在。
- 远程设备离线。
- 远程设备 setup 未 ready。
- workspace request 超时。
- 远程路径不存在。
- relative path 非法。
- symlink/path escape。
- 目录当文件读取。
- 不支持的文本预览类型。
- 文件超过 buffer 上限。

### 8.3 本地回归测试

必须补充或保留以下本地工作区测试：

- 本地 workspace tree 行为不变。
- 本地 workspace files 搜索行为不变。
- 本地 text/meta/buffer/html-preview 行为不变。
- 本地 changes/diff 行为不变。
- 本地 upload/cleanup 行为不变。
- `/api/chat` 不带 `deviceId` 仍走本机 agent，不触发设备协议。

### 8.4 device-executor 单元测试

覆盖：

- 临时目录下 tree/files/meta/text/buffer。
- `.git`、`node_modules` 等目录跳过规则。
- 文本截断和 UTF-8 处理。
- binary base64 响应。
- upload 唯一文件名和 cleanup。
- path escape 和 symlink escape。

## 9. 验收标准

功能验收：

- 创建远程工作区后，右侧工作区面板能看到远程设备真实文件树。
- `@file` 能搜索到远程设备工作区文件。
- 点击远程文本/Markdown/图片/PDF/HTML 文件可以预览。
- 远程设备中 agent 修改文件后，changes/diff 能反映远程变更。
- 上传附件时文件写入远程设备工作区的 `.lumi-uploads`。
- 远程设备离线时，远程工作区 API 返回明确错误。

回归验收：

- 本地工作区所有文件浏览、预览、上传、diff 行为保持不变。
- 本地聊天不带 `deviceId` 时仍走本机执行链路。
- 远程设备异常不影响本地工作区 API。

## 10. 实施顺序

1. 扩展设备协议类型和通用 request/response 路由。
2. 在 `device-executor` 实现 workspace request handler。
3. 在 Backend 新增远程工作区访问层。
4. 改造 workspace HTTP handlers，让远程分支走设备访问层，本地分支保持现有代码路径。
5. 接入 HTML preview 和 HTML asset 远程读取。
6. 接入 upload、cleanup 和 share 相关读取。
7. 补远程集成测试、device-executor 测试和本地回归测试。
8. 手动验证本地工作区和远程工作区 UI 行为。

## 11. 默认决策

- 使用现有 `WorkspaceConfig.Kind == "remote"`、`DeviceID`、`RemotePath` 判定远程工作区，不新增配置字段。
- 远程文件读取由 `device-executor` 在目标设备上执行，Backend 不对 `RemotePath` 做本机文件访问。
- 前端 API 保持兼容，优先不改调用 shape。
- 二进制文件 v1 设置大小上限，避免 WebSocket JSON 消息无限膨胀。
- 远程 workspace request 不占用聊天 task slot。
