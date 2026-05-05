# TurboMesh 部署指南

这份文档说明 `web.oboard.fun` 和 `*.web.oboard.fun` 的生产部署方式。

## 组件角色

TurboMesh 生产环境包含三个网络角色：

- 权威 DNS：Go 服务负责回答被委派的 `web.oboard.fun` zone。
- HTTP 信令应用：Go 服务提供首页和 WebRTC 信令接口。
- TLS 反向代理：Caddy、Nginx 或其他代理负责 HTTPS 终止，并转发到 Go
  HTTP 监听地址。

当前 MVP 中，Go 服务不直接终止 TLS。

## 构建

```sh
vp install
vp build
go test ./...
vp check
vp test
```

直接运行服务：

```sh
go run . server \
  --domain web.oboard.fun \
  --public-ip <server-public-ip> \
  --dns :53 \
  --http :8080 \
  --static dist
```

使用编译后的二进制：

```sh
go build -o turbomesh .
./turbomesh server \
  --domain web.oboard.fun \
  --public-ip <server-public-ip> \
  --dns :53 \
  --http :8080 \
  --static dist
```

## 父域 DNS 委派

需要在父域 DNS 服务商处，把 `web.oboard.fun` 委派给 TurboMesh DNS 服务。

父域需要配置：

```text
web.oboard.fun.      NS  ns1.web.oboard.fun.
ns1.web.oboard.fun.  A   <server-public-ip>
```

第二条在一些 DNS 服务商里叫 glue record 或 host record。

委派完成后，TurboMesh 会回答：

```text
web.oboard.fun.             A    <server-public-ip>
ns1.web.oboard.fun.         A    <server-public-ip>
*.web.oboard.fun.           A    <server-public-ip>
web.oboard.fun.             NS   ns1.web.oboard.fun.
web.oboard.fun.             SOA  ns1.web.oboard.fun.
```

## 反向代理约定

反向代理必须：

- 为 `web.oboard.fun` 和 `*.web.oboard.fun` 终止 TLS。
- 把 HTTP 流量转发到 Go 的 `--http` 地址。
- 保留 `Host`。
- 保留 `X-Forwarded-Proto`。
- 保留 WebSocket upgrade 相关头。
- 不要对 `*.web.oboard.fun` 做全局 HTTP 到 HTTPS 跳转。

首页可以从 HTTP 升级到 HTTPS，因为当 `Host` 是 `web.oboard.fun` 且
`X-Forwarded-Proto` 是 `http` 时，Go 服务会返回跳转。

通配符用户域名不能强制升级。用户服务可能明确需要 plain HTTP 或 `ws://`。

## Caddy 配置形态示例

下面只是配置形态，不是完整的证书或防火墙策略：

```caddyfile
web.oboard.fun, *.web.oboard.fun {
  reverse_proxy 127.0.0.1:8080 {
    header_up Host {host}
    header_up X-Forwarded-Proto {scheme}
  }
}
```

如果使用 Nginx，需要确保 WebSocket upgrade 头被正确转发。

## 客户端使用

暴露本地服务：

```sh
./turbomesh 3000 --server wss://web.oboard.fun/api/client
```

客户端会注册一个生成的 slug，并打印 session URL。

## 防火墙检查

- UDP 53 对公网开放
- TCP 53 对公网开放
- TCP 80 对反向代理开放
- TCP 443 对反向代理开放
- Go HTTP 监听地址能被反向代理访问

## 运行说明

- session 存在内存中，重启 server 会断开现有 session。
- 信令服务只中转 SDP 和 ICE 消息。
- 业务流量通过 WebRTC DataChannel 传输。
- 项目暂未内置 TURN；严格 NAT 环境可能需要后续版本加入 TURN 配置后才能连接。
