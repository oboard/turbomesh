# TurboMesh System Implementation

TurboMesh has two executables in one Go package:

- Server mode: `turbomesh server ...`
- Client mode: `turbomesh <port> ...`

The browser application is built with Vue and served by the Go server.

## Request Flow

1. A local user runs `turbomesh <port>`.
2. The client generates or accepts a slug.
3. The client opens `/api/client?slug=<slug>` over WebSocket.
4. A browser opens `https://<slug>.web.oboard.fun`.
5. DNS resolves the wildcard host to the TurboMesh server.
6. The Go server serves the Vue app.
7. The Vue app opens `/api/browser?slug=<slug>` over WebSocket.
8. Browser and client exchange SDP and ICE through the signaling server.
9. Browser and client establish a WebRTC DataChannel.
10. HTTP and WebSocket application traffic is carried over that DataChannel.

The signaling server does not carry application payloads.

## Server Mode

`main.go` parses CLI flags and starts:

- UDP DNS server
- TCP DNS server
- HTTP server
- In-memory signaling hub

`dns.go` implements the authoritative zone. It answers:

- `A` for the base domain, NS host, and wildcard names
- `NS` for the zone
- `SOA` for the zone

`http_server.go` implements routing:

- `/api/client` for local clients
- `/api/browser` for browsers
- `web.oboard.fun` homepage serving
- wildcard SPA serving
- homepage HTTP-to-HTTPS redirect based on `X-Forwarded-Proto`

## Signaling

`signaling.go` keeps active sessions in memory by slug.

Allowed browser-to-client messages:

- `offer`
- `ice`
- `error`

Allowed client-to-browser messages:

- `answer`
- `ice`
- `error`

The server rejects tunnel payload message types such as `http-request` or
`ws-send` on the signaling channel.

## Client Mode

`client.go` connects to the signaling server and creates one WebRTC peer
connection per browser.

For each browser:

- The browser creates the offer.
- The local client creates the answer.
- Both sides exchange ICE candidates through WebSocket signaling.
- The browser creates the `turbomesh` DataChannel.
- The local client accepts that DataChannel and attaches the tunnel proxy.

## Tunnel Protocol

`tunnel.go` defines JSON frames for traffic over the WebRTC DataChannel.

HTTP frames:

- `http-request`
- `http-response`
- `http-error`

WebSocket frames:

- `ws-open`
- `ws-opened`
- `ws-send`
- `ws-message`
- `ws-close`
- `ws-error`

Bodies are base64 encoded. Concurrent HTTP requests are multiplexed by frame
`id`.

## Browser Implementation

`src/App.vue` has two modes:

- Homepage mode on `web.oboard.fun`
- Proxy mode on `<slug>.web.oboard.fun`

Homepage mode shows a slug input and navigates to the matching wildcard host.
It preserves the current scheme: HTTPS homepage opens HTTPS wildcard URLs, HTTP
homepage opens HTTP wildcard URLs.

Proxy mode:

- opens the browser signaling WebSocket
- creates a WebRTC peer connection
- creates the `turbomesh` DataChannel
- loads the proxied local document into an iframe
- injects a small runtime to shim browser `WebSocket`

`public/turbomesh-sw.js` intercepts subresource HTTP fetches and sends them to
the Vue controller page, which forwards them over the WebRTC DataChannel.

## Security Model

- The generated slug is the access secret.
- Sessions are active only while the local client is connected.
- The server stores sessions in memory.
- Application payloads are not intentionally available to the signaling server.

## Known MVP Constraints

- TURN relay is not bundled.
- Sessions are not durable.
- There is no account system or ACL.
- The browser WebSocket shim supports the common API shape, but is not a full
  byte-for-byte browser WebSocket implementation.
