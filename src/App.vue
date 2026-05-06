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
const pendingRemoteCandidates: RTCIceCandidateInit[] = [];
const pendingHTTP = new Map<string, PendingHTTP>();
const moduleURLCache = new Map<string, Promise<string>>();

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
    return;
  }
  const registration = await navigator.serviceWorker.register("/turbomesh-sw.js", { scope: "/" });
  await registration.update();
  await navigator.serviceWorker.ready;
  if (!navigator.serviceWorker.controller) {
    if (sessionStorage.getItem("turbomesh-sw-reloaded") !== "1") {
      sessionStorage.setItem("turbomesh-sw-reloaded", "1");
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
    void tunnelHTTP(data.method, data.url, data.headers, data.body)
      .then((response) => port.postMessage(response))
      .catch((error: Error) =>
        port.postMessage({ status: 502, statusText: error.message, headers: {}, body: "" }),
      );
  };
  serviceWorkerBridge.start();
  navigator.serviceWorker.controller.postMessage({ type: "turbomesh-connect" }, [channel.port2]);
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
  replaceDocument(await prepareHTMLDocument(bytesToText(response.body ?? "")));
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

async function prepareHTMLDocument(html: string) {
  const doc = new DOMParser().parseFromString(html, "text/html");
  const head = doc.head || doc.documentElement.insertBefore(doc.createElement("head"), doc.body);

  const base = doc.createElement("base");
  base.href = "/";
  head.prepend(base);

  const runtime = doc.createElement("script");
  runtime.textContent = turbomeshRuntime;
  head.insertBefore(runtime, base.nextSibling);

  await inlineStylesheets(doc);
  await inlineScripts(doc);

  return `<!doctype html>\n${doc.documentElement.outerHTML}`;
}

async function inlineStylesheets(doc: Document) {
  const links = Array.from(doc.querySelectorAll<HTMLLinkElement>('link[rel~="stylesheet"][href]'));
  await Promise.all(
    links.map(async (link) => {
      const href = link.getAttribute("href");
      if (!href || isExternalURL(href)) {
        return;
      }
      const css = await fetchTextOverTunnel(href, "text/css,*/*;q=0.1");
      const style = doc.createElement("style");
      style.textContent = css;
      link.replaceWith(style);
    }),
  );
}

async function inlineScripts(doc: Document) {
  const scripts = Array.from(doc.querySelectorAll<HTMLScriptElement>("script[src]"));
  for (const script of scripts) {
    const src = script.getAttribute("src");
    if (!src || isExternalURL(src)) {
      continue;
    }
    const type = (script.getAttribute("type") || "").toLowerCase();
    const inline = doc.createElement("script");
    for (const attr of Array.from(script.attributes)) {
      if (attr.name !== "src" && attr.name !== "crossorigin" && attr.name !== "integrity") {
        inline.setAttribute(attr.name, attr.value);
      }
    }
    if (type === "module") {
      inline.type = "module";
      inline.textContent = await fetchModuleSource(src);
    } else {
      inline.textContent = await fetchTextOverTunnel(src, "*/*");
    }
    script.replaceWith(inline);
  }
}

async function fetchModuleSource(rawURL: string) {
  const moduleURL = normalizeTunnelURL(rawURL, "/");
  const blobURL = await buildModuleBlobURL(moduleURL);
  return `import ${JSON.stringify(blobURL)};`;
}

function buildModuleBlobURL(rawURL: string): Promise<string> {
  const moduleURL = normalizeTunnelURL(rawURL, "/");
  const cached = moduleURLCache.get(moduleURL);
  if (cached) {
    return cached;
  }
  const promise = (async () => {
    const source = await fetchTextOverTunnel(moduleURL, "text/javascript,*/*;q=0.1");
    const rewritten = await rewriteModuleImports(source, moduleURL);
    return URL.createObjectURL(new Blob([rewritten], { type: "text/javascript" }));
  })();
  moduleURLCache.set(moduleURL, promise);
  return promise;
}

async function rewriteModuleImports(source: string, importerURL: string) {
  const pattern =
    /(import\s+(?:[^'"]*?\s+from\s*)?["'])([^"']+)(["'])|(export\s+[^'"]*?\s+from\s*["'])([^"']+)(["'])|(import\s*\(\s*["'])([^"']+)(["']\s*\))/g;
  let out = "";
  let lastIndex = 0;
  for (const match of source.matchAll(pattern)) {
    const index = match.index ?? 0;
    const prefix = match[1] ?? match[4] ?? match[7] ?? "";
    const specifier = match[2] ?? match[5] ?? match[8] ?? "";
    const suffix = match[3] ?? match[6] ?? match[9] ?? "";
    out += source.slice(lastIndex, index);
    if (isTunnelModuleSpecifier(specifier)) {
      const dependencyURL = normalizeTunnelURL(specifier, importerURL);
      const blobURL = await buildModuleBlobURL(dependencyURL);
      out += `${prefix}${blobURL}${suffix}`;
    } else {
      out += match[0];
    }
    lastIndex = index + match[0].length;
  }
  out += source.slice(lastIndex);
  return out;
}

function isTunnelModuleSpecifier(specifier: string) {
  return specifier.startsWith("/") || specifier.startsWith("./") || specifier.startsWith("../");
}

function normalizeTunnelURL(rawURL: string, basePath: string) {
  const base = new URL(basePath, window.location.origin);
  const url = new URL(rawURL, base);
  if (url.origin !== window.location.origin) {
    return rawURL;
  }
  return url.pathname + url.search;
}

function isExternalURL(rawURL: string) {
  const url = new URL(rawURL, window.location.origin);
  return url.origin !== window.location.origin;
}

async function fetchTextOverTunnel(url: string, accept: string) {
  const response = await tunnelHTTP("GET", normalizeTunnelURL(url, "/"), { Accept: [accept] }, "");
  if ((response.status ?? 0) >= 400) {
    throw new Error(`failed to load ${url}: ${response.status} ${response.statusText ?? ""}`);
  }
  return bytesToText(response.body ?? "");
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
