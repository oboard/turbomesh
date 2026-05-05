let controllerClientId = "";

self.addEventListener("install", (event) => {
  event.waitUntil(self.skipWaiting());
});

self.addEventListener("activate", (event) => {
  event.waitUntil(self.clients.claim());
});

self.addEventListener("message", (event) => {
  if (event.data && event.data.type === "turbomesh-controller" && event.source) {
    controllerClientId = event.source.id;
  }
});

self.addEventListener("fetch", (event) => {
  const url = new URL(event.request.url);
  if (url.pathname === "/turbomesh-sw.js" || url.pathname.startsWith("/api/")) {
    return;
  }
  if (event.request.mode === "navigate") {
    return;
  }
  event.respondWith(proxyFetch(event.request));
});

async function proxyFetch(request) {
  const client = await findControllerClient();
  if (!client) {
    return fetch(request);
  }

  const body = await request.arrayBuffer();
  const channel = new MessageChannel();
  const responsePromise = new Promise((resolve) => {
    channel.port1.onmessage = (event) => resolve(event.data);
  });

  client.postMessage(
    {
      type: "turbomesh-fetch",
      method: request.method,
      url: new URL(request.url).pathname + new URL(request.url).search,
      headers: headersToObject(request.headers),
      body: arrayBufferToBase64(body),
    },
    [channel.port2],
  );

  const response = await responsePromise;
  return new Response(base64ToArrayBuffer(response.body || ""), {
    status: response.status || 502,
    statusText: response.statusText || "",
    headers: objectToHeaders(response.headers || {}),
  });
}

async function findControllerClient() {
  if (controllerClientId) {
    const client = await self.clients.get(controllerClientId);
    if (client) {
      return client;
    }
  }
  const clients = await self.clients.matchAll({ type: "window", includeUncontrolled: true });
  return clients[0];
}

function headersToObject(headers) {
  const out = {};
  headers.forEach((value, key) => {
    out[key] = out[key] || [];
    out[key].push(value);
  });
  return out;
}

function objectToHeaders(values) {
  const headers = new Headers();
  Object.entries(values).forEach(([key, list]) => {
    for (const value of list) {
      headers.append(key, value);
    }
  });
  return headers;
}

function arrayBufferToBase64(buffer) {
  const bytes = new Uint8Array(buffer);
  let binary = "";
  for (const byte of bytes) {
    binary += String.fromCharCode(byte);
  }
  return btoa(binary);
}

function base64ToArrayBuffer(value) {
  const binary = atob(value);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i += 1) {
    bytes[i] = binary.charCodeAt(i);
  }
  return bytes;
}
