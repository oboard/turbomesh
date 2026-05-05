# TurboMesh Deployment Guide

This guide describes the production deployment model for
`web.oboard.fun` and `*.web.oboard.fun`.

## Roles

TurboMesh production has three network roles:

- Authoritative DNS: the Go server answers the delegated `web.oboard.fun` zone.
- HTTP signaling app: the Go server serves the homepage and WebRTC signaling.
- TLS reverse proxy: Caddy, Nginx, or another proxy terminates HTTPS and forwards
  traffic to the Go HTTP listener.

The Go server does not terminate TLS in the current MVP.

## Build

```sh
vp install
vp build
go test ./...
vp check
vp test
```

Then run the server:

```sh
go run . server \
  --domain web.oboard.fun \
  --public-ip <server-public-ip> \
  --dns :53 \
  --http :8080 \
  --static dist
```

For a compiled binary:

```sh
go build -o turbomesh .
./turbomesh server \
  --domain web.oboard.fun \
  --public-ip <server-public-ip> \
  --dns :53 \
  --http :8080 \
  --static dist
```

## Parent DNS Delegation

Delegate `web.oboard.fun` from the parent zone to the TurboMesh DNS server.

Required records at the parent DNS provider:

```text
web.oboard.fun.      NS  ns1.web.oboard.fun.
ns1.web.oboard.fun.  A   <server-public-ip>
```

Some providers call the second record a glue record or host record.

After delegation, TurboMesh answers:

```text
web.oboard.fun.             A    <server-public-ip>
ns1.web.oboard.fun.         A    <server-public-ip>
*.web.oboard.fun.           A    <server-public-ip>
web.oboard.fun.             NS   ns1.web.oboard.fun.
web.oboard.fun.             SOA  ns1.web.oboard.fun.
```

## Reverse Proxy Contract

The reverse proxy must:

- Terminate TLS for `web.oboard.fun` and `*.web.oboard.fun`.
- Forward HTTP traffic to the Go `--http` address.
- Preserve `Host`.
- Preserve `X-Forwarded-Proto`.
- Preserve WebSocket upgrade headers.
- Avoid a global HTTP-to-HTTPS redirect for `*.web.oboard.fun`.

Homepage HTTP may be upgraded to HTTPS because Go returns a redirect when
`Host` is `web.oboard.fun` and `X-Forwarded-Proto` is `http`.

Wildcard hosts must not be force-upgraded. User services may intentionally need
plain HTTP or `ws://`.

Because `web.oboard.fun` is delegated to the Go DNS server, wildcard DNS-01
certificate issuance is not available through the parent DNS provider. For Caddy,
use On-Demand TLS for individual session hostnames and the TurboMesh ask endpoint
at `/api/tls-ask`.

## Example Caddy Shape

This is a shape, not a complete certificate or firewall policy:

```caddyfile
{
  auto_https disable_redirects
  on_demand_tls {
    ask http://127.0.0.1:8080/api/tls-ask
  }
}

(turbomesh_proxy) {
  reverse_proxy 127.0.0.1:8080 {
    header_up Host {host}
    header_up X-Forwarded-Proto {scheme}
  }
}

http://web.oboard.fun,
https://web.oboard.fun,
http://*.web.oboard.fun {
  import turbomesh_proxy
}

https://*.web.oboard.fun {
  tls {
    on_demand
  }
  import turbomesh_proxy
}
```

If you use Nginx, make sure WebSocket upgrade headers are forwarded.

Do not use a single `web.oboard.fun, *.web.oboard.fun` Caddy site without
On-Demand TLS. That usually gives the base domain a certificate but makes random
slug HTTPS fail during the TLS handshake.

## Client Usage

Expose a local service:

```sh
./turbomesh 3000 --server wss://web.oboard.fun/api/client
```

The client registers a generated slug and prints the session URL.

## Firewall Checklist

- UDP 53 open to the internet
- TCP 53 open to the internet
- TCP 80 open to the reverse proxy
- TCP 443 open to the reverse proxy
- Go HTTP listener reachable from the reverse proxy

## Operational Notes

- Sessions are in-memory. Restarting the server drops active sessions.
- The signaling server only relays SDP and ICE messages.
- Application traffic flows through WebRTC DataChannels.
- TURN is not bundled; hard NAT environments may fail until TURN is configured
  in a future version.
