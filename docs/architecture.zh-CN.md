# TurboMesh 系统实现介绍

TurboMesh 在同一个 Go 包中实现两个运行模式：

- Server 模式：`turbomesh server ...`
- Client 模式：`turbomesh <port> ...`

浏览器应用使用 Vue 构建，并由 Go 服务提供静态资源。

## 请求流程

1. 本地用户运行 `turbomesh <port>`。
2. 客户端生成或接收一个 slug。
3. 客户端通过 WebSocket 打开 `/api/client?slug=<slug>`。
4. 浏览器打开 `https://<slug>.web.oboard.fun`。
5. DNS 把通配符域名解析到 TurboMesh server。
6. Go server 返回 Vue 应用。
7. Vue 应用通过 WebSocket 打开 `/api/browser?slug=<slug>`。
8. 浏览器和本地客户端通过信令服务交换 SDP 和 ICE。
9. 浏览器和本地客户端建立 WebRTC DataChannel。
10. HTTP 和 WebSocket 业务流量通过 DataChannel 传输。

信令服务不承载业务 payload。

## Server 模式

`main.go` 解析 CLI 参数并启动：

- UDP DNS 服务
- TCP DNS 服务
- HTTP 服务
- 内存中的信令 hub

`dns.go` 实现权威 DNS zone，负责回答：

- base domain、NS host、wildcard name 的 `A` 记录
- zone 的 `NS` 记录
- zone 的 `SOA` 记录

`http_server.go` 实现 HTTP 路由：

- `/api/client` 给本地客户端使用
- `/api/browser` 给浏览器使用
- `web.oboard.fun` 首页
- 通配符域名 SPA
- 基于 `X-Forwarded-Proto` 的首页 HTTP 到 HTTPS 跳转

## 信令

`signaling.go` 按 slug 在内存中保存活跃 session。

允许 browser 到 client 的消息：

- `offer`
- `ice`
- `error`

允许 client 到 browser 的消息：

- `answer`
- `ice`
- `error`

server 不允许通过信令通道发送 `http-request` 或 `ws-send` 这类 tunnel
payload 消息。

## Client 模式

`client.go` 连接信令服务，并为每个浏览器创建一个 WebRTC peer connection。

对每个浏览器：

- 浏览器创建 offer。
- 本地客户端创建 answer。
- 双方通过 WebSocket 信令交换 ICE candidate。
- 浏览器创建 `turbomesh` DataChannel。
- 本地客户端接受 DataChannel，并挂载 tunnel proxy。

## Tunnel 协议

`tunnel.go` 定义了 WebRTC DataChannel 上的 JSON frame。

HTTP frame：

- `http-request`
- `http-response`
- `http-error`

WebSocket frame：

- `ws-open`
- `ws-opened`
- `ws-send`
- `ws-message`
- `ws-close`
- `ws-error`

body 使用 base64 编码。并发 HTTP 请求通过 frame `id` 复用在同一个
DataChannel 上。

## 浏览器实现

`src/App.vue` 有两种模式：

- `web.oboard.fun` 上的首页模式
- `<slug>.web.oboard.fun` 上的代理模式

首页模式展示 slug 输入框并跳转到对应通配符域名。跳转时保留当前 scheme：
HTTPS 首页打开 HTTPS 通配符 URL，HTTP 首页打开 HTTP 通配符 URL。

代理模式会：

- 打开浏览器信令 WebSocket
- 创建 WebRTC peer connection
- 创建 `turbomesh` DataChannel
- 用代理后的本地 HTML 文档替换当前 document
- 注入一个小 runtime 来 shim 浏览器 `WebSocket`

`public/turbomesh-sw.js` 拦截子资源 HTTP fetch，把请求发给 Vue controller
页面，再由 Vue 页面通过 WebRTC DataChannel 转发。

## 安全模型

- 生成的 slug 是访问密钥。
- session 只在本地客户端连接期间有效。
- server 将 session 保存在内存中。
- 业务 payload 不会被主动发送到信令服务。

## 当前 MVP 约束

- 暂未内置 TURN 中继。
- session 不持久化。
- 没有账号系统或 ACL。
- 浏览器 WebSocket shim 支持常见 API 形态，但不是完整逐字节等价的浏览器
  WebSocket 实现。
