# TurboMesh

TurboMesh exposes a local HTTP service through WebRTC. The public Go server is
authoritative DNS for `web.oboard.fun`, serves the homepage, and relays WebRTC
signaling only. Application HTTP and WebSocket traffic travels peer-to-peer over
WebRTC DataChannels between the browser and the local client.

TurboMesh 通过 WebRTC 暴露本地 HTTP 服务。公网 Go 服务同时作为
`web.oboard.fun` 的权威 DNS、首页服务和 WebRTC 信令中转服务；业务 HTTP
与 WebSocket 数据不经过公网服务转发，而是通过浏览器和本地客户端之间的
WebRTC DataChannel 点对点传输。

## Documentation

- Quick start: [English](docs/quickstart.en.md) / [中文](docs/quickstart.zh-CN.md)
- Deployment guide: [English](docs/deployment.en.md) / [中文](docs/deployment.zh-CN.md)
- System implementation: [English](docs/architecture.en.md) /
  [中文](docs/architecture.zh-CN.md)

## Validation

```sh
go test ./...
vp check
vp test
```
