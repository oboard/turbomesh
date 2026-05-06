<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { baseDomain, buildSessionURL, getSessionSlug, isValidSlug } from "./domain";

type SignalMessage = {
  type: "browser-ready" | "offer" | "answer" | "ice" | "error" | "session-expired";
  sdp?: string;
  candidate?: RTCIceCandidateInit;
  error?: string;
};

type TunnelFrame = {
  type:
    | "http-request"
    | "http-response"
    | "http-error"
    | "ws-open"
    | "ws-opened"
    | "ws-send"
    | "ws-message"
    | "ws-close"
    | "ws-error"
    | "error";
  id: string;
  method?: string;
  url?: string;
  headers?: Record<string, string[]>;
  body?: string;
  status?: number;
  statusText?: string;
  opcode?: number;
};

type PendingHTTP = {
  resolve: (frame: TunnelFrame) => void;
  reject: (error: Error) => void;
};

declare global {
  interface Window {
    __turbomesh?: {
      openWS: (id: string, url: string) => void;
      sendWS: (id: string, body: string, opcode: number) => void;
      closeWS: (id: string) => void;
    };
  }
}

const slugInput = ref("");
const status = ref("Ready");
const detail = ref("");
const connected = ref(false);
const mode = computed(() => (currentSlug() ? "proxy" : "home"));

let signalSocket: WebSocket | undefined;
let peer: RTCPeerConnection | undefined;
let channel: RTCDataChannel | undefined;
let requestSeq = 0;
const pendingHTTP = new Map<string, PendingHTTP>();

onMounted(() => {
  const slug = currentSlug();
  if (slug) {
    void startProxy(slug);
  }
});

function currentSlug() {
  return getSessionSlug(window.location.hostname);
}

function openSlug() {
  const slug = slugInput.value.trim().toLowerCase();
  if (!isValidSlug(slug)) {
    status.value = "Use an 8-63 character slug with lowercase letters, numbers, or hyphens.";
    return;
  }
  window.location.href = buildSessionURL(slug, window.location.protocol);
}

async function startProxy(slug: string) {
  status.value = "Preparing browser tunnel";
  detail.value = `Connecting to ${slug}.${baseDomain}`;
  installRuntimeListeners();
  await registerServiceWorker();

  peer = new RTCPeerConnection({
    iceServers: [{ urls: "stun:stun.l.google.com:19302" }],
  });
  channel = peer.createDataChannel("turbomesh", { ordered: true });
  channel.addEventListener("open", () => {
    connected.value = true;
    status.value = "Connected";
    detail.value = "HTTP and WebSocket traffic are using WebRTC DataChannels.";
    void loadInitialDocument();
  });
  channel.addEventListener("message", (event) =>
    handleTunnelFrame(JSON.parse(event.data as string) as TunnelFrame),
  );
  channel.addEventListener("close", () => {
    connected.value = false;
    status.value = "Disconnected";
  });

  peer.addEventListener("icecandidate", (event) => {
    if (event.candidate) {
      sendSignal({ type: "ice", candidate: event.candidate.toJSON() });
    }
  });
  peer.addEventListener("connectionstatechange", () => {
    if (!peer) {
      return;
    }
    detail.value = `WebRTC state: ${peer.connectionState}`;
  });

  const wsScheme = window.location.protocol === "https:" ? "wss" : "ws";
  signalSocket = new WebSocket(
    `${wsScheme}://${window.location.host}/api/browser?slug=${encodeURIComponent(slug)}`,
  );
  signalSocket.addEventListener("open", async () => {
    status.value = "Negotiating WebRTC";
    const offer = await peer.createOffer();
    await peer.setLocalDescription(offer);
    sendSignal({ type: "offer", sdp: offer.sdp });
  });
  signalSocket.addEventListener("message", async (event) => {
    const message = JSON.parse(event.data as string) as SignalMessage;
    if (message.type === "answer" && message.sdp) {
      await peer.setRemoteDescription({ type: "answer", sdp: message.sdp });
      return;
    }
    if (message.type === "ice" && message.candidate) {
      await peer.addIceCandidate(message.candidate);
      return;
    }
    if (message.type === "session-expired") {
      status.value = "Session is not active";
      detail.value = message.error ?? "Start the local turbomesh client and try again.";
    }
    if (message.type === "error") {
      status.value = "Signaling error";
      detail.value = message.error ?? "";
    }
  });
  signalSocket.addEventListener("close", () => {
    if (!connected.value) {
      status.value = "Signaling disconnected";
    }
  });
}

function sendSignal(message: SignalMessage) {
  if (signalSocket?.readyState === WebSocket.OPEN) {
    signalSocket.send(JSON.stringify(message));
  }
}

async function registerServiceWorker() {
  if (!("serviceWorker" in navigator)) {
    return;
  }
  const registration = await navigator.serviceWorker.register("/turbomesh-sw.js", { scope: "/" });
  await navigator.serviceWorker.ready;
  const worker = registration.active ?? registration.waiting ?? registration.installing;
  worker?.postMessage({ type: "turbomesh-controller" });
  navigator.serviceWorker.controller?.postMessage({ type: "turbomesh-controller" });
}

function installRuntimeListeners() {
  window.__turbomesh = {
    openWS(id, url) {
      sendTunnel({ type: "ws-open", id, url, headers: {} });
    },
    sendWS(id, body, opcode) {
      sendTunnel({ type: "ws-send", id, body, opcode });
    },
    closeWS(id) {
      sendTunnel({ type: "ws-close", id });
    },
  };

  navigator.serviceWorker?.addEventListener("message", (event) => {
    const port = event.ports[0];
    const data = event.data as {
      type: string;
      id: string;
      method: string;
      url: string;
      headers: Record<string, string[]>;
      body: string;
    };
    if (data.type !== "turbomesh-fetch" || !port) {
      return;
    }
    void tunnelHTTP(data.method, data.url, data.headers, data.body)
      .then((response) => port.postMessage(response))
      .catch((error: Error) =>
        port.postMessage({ status: 502, statusText: error.message, headers: {}, body: "" }),
      );
  });
}

async function loadInitialDocument() {
  const response = await tunnelHTTP(
    "GET",
    window.location.pathname + window.location.search,
    {},
    "",
  );
  const contentType = headerValue(response.headers, "content-type");
  if (!contentType.includes("text/html")) {
    replaceDocument(`<pre>${escapeHTML(bytesToText(response.body ?? ""))}</pre>`);
    return;
  }
  replaceDocument(injectRuntime(bytesToText(response.body ?? "")));
}

function tunnelHTTP(method: string, url: string, headers: Record<string, string[]>, body: string) {
  const id = nextID();
  sendTunnel({ type: "http-request", id, method, url, headers, body });
  return new Promise<TunnelFrame>((resolve, reject) => {
    pendingHTTP.set(id, { resolve, reject });
    window.setTimeout(() => {
      if (pendingHTTP.delete(id)) {
        reject(new Error("request timed out"));
      }
    }, 60_000);
  });
}

function sendTunnel(frame: TunnelFrame) {
  if (channel?.readyState !== "open") {
    throw new Error("WebRTC tunnel is not open");
  }
  channel.send(JSON.stringify(frame));
}

function handleTunnelFrame(frame: TunnelFrame) {
  if (frame.type === "http-response" || frame.type === "http-error") {
    const pending = pendingHTTP.get(frame.id);
    pendingHTTP.delete(frame.id);
    if (!pending) {
      return;
    }
    if (frame.type === "http-error") {
      pending.reject(new Error(frame.statusText ?? "HTTP tunnel error"));
    } else {
      pending.resolve(frame);
    }
    return;
  }

  if (frame.type.startsWith("ws-")) {
    window.postMessage({ ...frame, type: `turbomesh-${frame.type}` }, "*");
  }
}

function replaceDocument(html: string) {
  document.open();
  document.write(html);
  document.close();
}

function nextID() {
  requestSeq += 1;
  return `${Date.now().toString(36)}-${requestSeq.toString(36)}`;
}

function headerValue(headers: Record<string, string[]> | undefined, key: string) {
  if (!headers) {
    return "";
  }
  const lower = key.toLowerCase();
  for (const [name, values] of Object.entries(headers)) {
    if (name.toLowerCase() === lower) {
      return values.join(",").toLowerCase();
    }
  }
  return "";
}

function bytesToText(encoded: string) {
  const bytes = Uint8Array.from(atob(encoded), (char) => char.charCodeAt(0));
  return new TextDecoder().decode(bytes);
}

function escapeHTML(value: string) {
  return value.replace(/[&<>"']/g, (char) => {
    const entities: Record<string, string> = {
      "&": "&amp;",
      "<": "&lt;",
      ">": "&gt;",
      '"': "&quot;",
      "'": "&#39;",
    };
    return entities[char] ?? char;
  });
}

function injectRuntime(html: string) {
  const base = `<base href="/" />`;
  const runtime = `<script>
(() => {
  const NativeEventTarget = EventTarget;
  let wsSeq = 0;
  class TurboMeshWebSocket extends NativeEventTarget {
    constructor(url) {
      super();
      this.url = String(url);
      this.readyState = 0;
      this.id = "ws-" + Date.now().toString(36) + "-" + (++wsSeq).toString(36);
      window.__turbomesh.openWS(this.id, this.url);
    }
    send(data) {
      const binary = data instanceof ArrayBuffer;
      window.__turbomesh.sendWS(this.id, encodePayload(data), binary ? 2 : 1);
    }
    close() {
      window.__turbomesh.closeWS(this.id);
    }
  }
  TurboMeshWebSocket.CONNECTING = 0;
  TurboMeshWebSocket.OPEN = 1;
  TurboMeshWebSocket.CLOSING = 2;
  TurboMeshWebSocket.CLOSED = 3;
  addEventListener("message", (event) => {
    const data = event.data || {};
    if (!data.id || !data.type) return;
    const sockets = TurboMeshWebSocket._sockets || (TurboMeshWebSocket._sockets = new Map());
    let socket = sockets.get(data.id);
    if (!socket) return;
    if (data.type === "turbomesh-ws-opened") {
      socket.readyState = 1;
      socket.dispatchEvent(new Event("open"));
      socket.onopen && socket.onopen(new Event("open"));
    }
    if (data.type === "turbomesh-ws-message") {
      const message = new MessageEvent("message", { data: data.opcode === 2 ? decodeBytes(data.body || "").buffer : decodeText(data.body || "") });
      socket.dispatchEvent(message);
      socket.onmessage && socket.onmessage(message);
    }
    if (data.type === "turbomesh-ws-close" || data.type === "turbomesh-ws-error") {
      socket.readyState = 3;
      const close = new CloseEvent("close");
      socket.dispatchEvent(close);
      socket.onclose && socket.onclose(close);
      sockets.delete(data.id);
    }
  });
  const Original = TurboMeshWebSocket;
  window.WebSocket = function(url) {
    const socket = new Original(url);
    Original._sockets = Original._sockets || new Map();
    Original._sockets.set(socket.id, socket);
    return socket;
  };
  Object.assign(window.WebSocket, Original);
  function encodePayload(data) {
    if (data instanceof ArrayBuffer) return encodeBytes(new Uint8Array(data));
    if (ArrayBuffer.isView(data)) return encodeBytes(new Uint8Array(data.buffer, data.byteOffset, data.byteLength));
    return encodeBytes(new TextEncoder().encode(String(data)));
  }
  function decodeText(value) {
    return new TextDecoder().decode(decodeBytes(value));
  }
  function decodeBytes(value) {
    const binary = atob(value);
    const bytes = new Uint8Array(binary.length);
    for (let i = 0; i < binary.length; i += 1) bytes[i] = binary.charCodeAt(i);
    return bytes;
  }
  function encodeBytes(bytes) {
    let binary = "";
    for (const byte of bytes) binary += String.fromCharCode(byte);
    return btoa(binary);
  }
})();
<\\/script>`;
  if (html.includes("<head")) {
    return html.replace(/<head([^>]*)>/i, `<head$1>${base}${runtime}`);
  }
  return `${base}${runtime}${html}`;
}
</script>

<template>
  <main v-if="mode === 'home'" class="home-shell">
    <section class="hero-panel">
      <div class="brand">TurboMesh</div>
      <h1>Open a local web service through WebRTC.</h1>
      <p>
        Enter an active session slug to visit the matching
        <code>*.web.oboard.fun</code> service without routing app data through the server.
      </p>
      <form class="slug-form" @submit.prevent="openSlug">
        <input
          v-model="slugInput"
          autocomplete="off"
          spellcheck="false"
          placeholder="session slug"
        />
        <button type="submit">Open</button>
      </form>
      <p class="status">{{ status }}</p>
    </section>
  </main>

  <main v-else class="proxy-shell">
    <header class="proxy-bar">
      <div>
        <strong>{{ currentSlug() }}.{{ baseDomain }}</strong>
        <span>{{ detail }}</span>
      </div>
      <span :class="['dot', { connected }]" />
    </header>
    <section class="connect-state">
      <h1>{{ status }}</h1>
      <p>{{ detail }}</p>
    </section>
  </main>
</template>
