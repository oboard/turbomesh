<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { baseDomain, buildSessionURL, getSessionSlug, isValidSlug } from "./domain";
import turbomeshRuntime from "./turbomesh-runtime.js?raw";

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
  protocols?: string[];
  protocol?: string;
};

type PendingHTTP = {
  resolve: (frame: TunnelFrame) => void;
  reject: (error: Error) => void;
};

declare global {
  interface Window {
    __turbomesh?: {
      openWS: (id: string, url: string, protocols: string[]) => void;
      sendWS: (id: string, body: string, opcode: number) => void;
      closeWS: (id: string) => void;
      fetchHTTP: (
        method: string,
        url: string,
        headers: Record<string, string[]>,
        body: string,
      ) => Promise<TunnelFrame>;
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
let serviceWorkerBridge: MessagePort | undefined;
let cleaningUp = false;
let reloadingForServiceWorker = false;
let tunnelOpenResolve: (() => void) | undefined;
const pendingRemoteCandidates: RTCIceCandidateInit[] = [];
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
  try {
    await registerServiceWorker();
  } catch (error) {
    status.value = "Service worker unavailable";
    detail.value = error instanceof Error ? error.message : String(error);
    return;
  }

  peer = new RTCPeerConnection({
    iceServers: [{ urls: "stun:stun.l.google.com:19302" }],
  });
  channel = peer.createDataChannel("turbomesh", { ordered: true });
  channel.addEventListener("open", () => {
    connected.value = true;
    status.value = "Connected";
    detail.value = "HTTP and WebSocket traffic are using WebRTC DataChannels.";
    tunnelOpenResolve?.();
    tunnelOpenResolve = undefined;
    void loadInitialDocument();
  });
  channel.addEventListener("message", (event) => {
    void parseDataChannelJSON(event.data)
      .then((frame) => handleTunnelFrame(frame as TunnelFrame))
      .catch((error: Error) => {
        status.value = "Tunnel error";
        detail.value = error.message;
      });
  });
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
      await flushRemoteCandidates();
      return;
    }
    if (message.type === "ice" && message.candidate) {
      if (peer.remoteDescription) {
        await peer.addIceCandidate(message.candidate);
      } else {
        pendingRemoteCandidates.push(message.candidate);
      }
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
    throw new Error("This browser does not support service workers.");
  }
  if (!window.isSecureContext) {
    throw new Error("Service workers require HTTPS. Use the https session URL.");
  }
  const registration = await navigator.serviceWorker.register("/turbomesh-sw.js", { scope: "/" });
  await registration.update();
  await navigator.serviceWorker.ready;
  if (!navigator.serviceWorker.controller) {
    if (sessionStorage.getItem("turbomesh-sw-reloaded") !== "1") {
      sessionStorage.setItem("turbomesh-sw-reloaded", "1");
      reloadingForServiceWorker = true;
      window.location.reload();
      await new Promise<never>(() => {});
    }
    await waitForServiceWorkerController();
  }
  sessionStorage.removeItem("turbomesh-sw-reloaded");
  connectServiceWorkerBridge();
}

function waitForServiceWorkerController() {
  if (navigator.serviceWorker.controller) {
    return Promise.resolve();
  }
  return new Promise<void>((resolve, reject) => {
    const timeout = window.setTimeout(() => {
      navigator.serviceWorker.removeEventListener("controllerchange", onControllerChange);
      reject(new Error("service worker did not control the page"));
    }, 5_000);
    function onControllerChange() {
      window.clearTimeout(timeout);
      navigator.serviceWorker.removeEventListener("controllerchange", onControllerChange);
      resolve();
    }
    navigator.serviceWorker.addEventListener("controllerchange", onControllerChange);
  });
}

function installRuntimeListeners() {
  window.addEventListener("pagehide", cleanupSession, { once: true });
  window.__turbomesh = {
    openWS(id, url, protocols) {
      sendTunnel({ type: "ws-open", id, url, headers: {}, protocols });
    },
    sendWS(id, body, opcode) {
      sendTunnel({ type: "ws-send", id, body, opcode });
    },
    closeWS(id) {
      sendTunnel({ type: "ws-close", id });
    },
    fetchHTTP(method, url, headers, body) {
      return tunnelHTTP(method, url, headers, body);
    },
  };
}

function connectServiceWorkerBridge() {
  if (!navigator.serviceWorker.controller) {
    throw new Error("service worker is not controlling the page");
  }
  serviceWorkerBridge?.close();
  const channel = new MessageChannel();
  serviceWorkerBridge = channel.port1;
  serviceWorkerBridge.onmessage = (event) => {
    const data = event.data as {
      type: string;
      id: string;
      method: string;
      url: string;
      headers: Record<string, string[]>;
      body: string;
    };
    const port = event.ports[0];
    if (data.type !== "turbomesh-fetch" || !port) {
      return;
    }
    void waitForTunnelOpen()
      .then(() => tunnelHTTP(data.method, data.url, data.headers, data.body))
      .then((response) => port.postMessage(response))
      .catch((error: Error) =>
        port.postMessage({ status: 502, statusText: error.message, headers: {}, body: "" }),
      );
  };
  serviceWorkerBridge.start();
  navigator.serviceWorker.controller.postMessage({ type: "turbomesh-connect" }, [channel.port2]);
}

function waitForTunnelOpen() {
  if (channel?.readyState === "open") {
    return Promise.resolve();
  }
  if (channel?.readyState === "closing" || channel?.readyState === "closed") {
    return Promise.reject(new Error("WebRTC tunnel is closed"));
  }
  return new Promise<void>((resolve, reject) => {
    const timeout = window.setTimeout(() => {
      reject(new Error("WebRTC tunnel is not open"));
    }, 10_000);
    tunnelOpenResolve = () => {
      window.clearTimeout(timeout);
      resolve();
    };
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

async function flushRemoteCandidates() {
  if (!peer?.remoteDescription) {
    return;
  }
  while (pendingRemoteCandidates.length > 0) {
    const candidate = pendingRemoteCandidates.shift();
    if (candidate) {
      await peer.addIceCandidate(candidate);
    }
  }
}

async function parseDataChannelJSON(data: string | Blob | ArrayBuffer) {
  if (typeof data === "string") {
    return JSON.parse(data) as unknown;
  }
  if (data instanceof Blob) {
    return JSON.parse(await data.text()) as unknown;
  }
  return JSON.parse(new TextDecoder().decode(new Uint8Array(data))) as unknown;
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
  const closeScript = "</scr" + "ipt>";
  const runtime = `<script>${turbomeshRuntime}\n${closeScript}`;
  if (html.includes("<head")) {
    return html.replace(/<head([^>]*)>/i, `<head$1>${base}${runtime}`);
  }
  return `${base}${runtime}${html}`;
}

function cleanupSession() {
  if (reloadingForServiceWorker) {
    return;
  }
  if (cleaningUp) {
    return;
  }
  cleaningUp = true;
  navigator.serviceWorker.controller?.postMessage({ type: "turbomesh-disconnect" });
  serviceWorkerBridge?.close();
  signalSocket?.close();
  channel?.close();
  peer?.close();
  if ("serviceWorker" in navigator) {
    void navigator.serviceWorker
      .getRegistration("/")
      .then((registration) => registration?.unregister());
  }
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
