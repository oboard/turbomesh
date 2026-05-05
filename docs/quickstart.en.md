# TurboMesh Quick Start

This guide runs TurboMesh locally with development ports. It is enough to test
the CLI, authoritative DNS behavior, homepage routing, and signaling endpoints.

## Prerequisites

- Go 1.25 or newer
- Vite+ global CLI `vp`
- Bun, as declared by `packageManager`

After pulling changes, install frontend dependencies:

```sh
vp install
```

## Build the Homepage

The Go server serves the built frontend from `dist`.

```sh
vp build
```

## Start the Public Server Locally

Use high ports locally so root privileges are not required:

```sh
go run . server \
  --domain web.oboard.fun \
  --public-ip 127.0.0.1 \
  --dns :15353 \
  --http :18080 \
  --static dist
```

What this starts:

- UDP and TCP DNS listeners on `:15353`
- HTTP server on `:18080`
- Homepage and wildcard SPA serving
- WebSocket signaling endpoints at `/api/client` and `/api/browser`

## Check DNS Answers

```sh
dig @127.0.0.1 -p 15353 web.oboard.fun A
dig @127.0.0.1 -p 15353 ns1.web.oboard.fun A
dig @127.0.0.1 -p 15353 abc12345.web.oboard.fun A
dig @127.0.0.1 -p 15353 web.oboard.fun NS
dig @127.0.0.1 -p 15353 web.oboard.fun SOA
```

All `A` answers should return `127.0.0.1` in this local setup.

## Start a Local Service

In another terminal, run any HTTP service. For example:

```sh
python3 -m http.server 3000
```

## Start the TurboMesh Client

```sh
go run . 3000 --server ws://127.0.0.1:18080/api/client
```

The client prints a URL like:

```text
https://<slug>.127.0.0.1
```

For real deployment, the printed URL should be under your configured domain,
such as `https://<slug>.web.oboard.fun`.

## Validate

```sh
go test ./...
vp check
vp test
```

## Current MVP Limits

- WebRTC uses STUN by default; TURN relay is not bundled.
- The generated slug is the access secret.
- Sessions are stored in memory and expire when the local client disconnects.
- TLS is expected to be terminated by a reverse proxy in production.
