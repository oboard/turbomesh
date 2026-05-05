# TurboMesh 快速开始

这份文档使用本地开发端口运行 TurboMesh，适合验证 CLI、权威 DNS、首页路由和
信令接口。

## 前置要求

- Go 1.25 或更新版本
- Vite+ 全局 CLI `vp`
- Bun，与 `packageManager` 声明保持一致

拉取代码后先安装前端依赖：

```sh
vp install
```

## 构建首页

Go 服务会从 `dist` 目录提供构建后的前端资源。

```sh
vp build
```

## 本地启动公网服务

本地开发建议使用高端口，避免需要 root 权限：

```sh
go run . server \
  --domain web.oboard.fun \
  --public-ip 127.0.0.1 \
  --dns :15353 \
  --http :18080 \
  --static dist
```

这个命令会启动：

- `:15353` 上的 UDP 和 TCP DNS 服务
- `:18080` 上的 HTTP 服务
- 首页与通配符域名 SPA
- `/api/client` 和 `/api/browser` WebSocket 信令接口

## 检查 DNS 结果

```sh
dig @127.0.0.1 -p 15353 web.oboard.fun A
dig @127.0.0.1 -p 15353 ns1.web.oboard.fun A
dig @127.0.0.1 -p 15353 abc12345.web.oboard.fun A
dig @127.0.0.1 -p 15353 web.oboard.fun NS
dig @127.0.0.1 -p 15353 web.oboard.fun SOA
```

在这个本地配置里，所有 `A` 记录都应该返回 `127.0.0.1`。

## 启动一个本地服务

另开一个终端，启动任意 HTTP 服务。例如：

```sh
python3 -m http.server 3000
```

## 启动 TurboMesh 客户端

```sh
go run . 3000 --server ws://127.0.0.1:18080/api/client
```

客户端会打印类似下面的 URL：

```text
https://<slug>.127.0.0.1
```

真实部署时，这个 URL 应该位于配置的域名下，例如
`https://<slug>.web.oboard.fun`。

## 验证

```sh
go test ./...
vp check
vp test
```

## 当前 MVP 限制

- WebRTC 默认只配置 STUN；项目暂未内置 TURN 中继。
- 生成的 slug 就是访问密钥。
- session 存在内存中，本地客户端断开后立即失效。
- 生产环境 TLS 由反向代理终止。
