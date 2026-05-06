(() => {
  const NativeEventTarget = EventTarget;
  const nativeFetch = window.fetch.bind(window);
  const sockets = new Map();
  let wsSeq = 0;

  class TurboMeshWebSocket extends NativeEventTarget {
    constructor(url) {
      super();
      this.url = String(url);
      this.readyState = 0;
      this.id = `ws-${Date.now().toString(36)}-${(++wsSeq).toString(36)}`;
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
    if (!data.id || !data.type) {
      return;
    }
    const socket = sockets.get(data.id);
    if (!socket) {
      return;
    }

    if (data.type === "turbomesh-ws-opened") {
      socket.readyState = 1;
      const open = new Event("open");
      socket.dispatchEvent(open);
      socket.onopen?.(open);
    }

    if (data.type === "turbomesh-ws-message") {
      const message = new MessageEvent("message", {
        data: data.opcode === 2 ? decodeBytes(data.body || "").buffer : decodeText(data.body || ""),
      });
      socket.dispatchEvent(message);
      socket.onmessage?.(message);
    }

    if (data.type === "turbomesh-ws-close" || data.type === "turbomesh-ws-error") {
      socket.readyState = 3;
      const close = new CloseEvent("close");
      socket.dispatchEvent(close);
      socket.onclose?.(close);
      sockets.delete(data.id);
    }
  });

  const Original = TurboMeshWebSocket;
  window.WebSocket = function WebSocket(url) {
    const socket = new Original(url);
    sockets.set(socket.id, socket);
    return socket;
  };
  Object.assign(window.WebSocket, Original);

  window.fetch = async function turboMeshFetch(input, init) {
    const request = new Request(input, init);
    const url = new URL(request.url);
    if (url.origin !== location.origin || !window.__turbomesh?.fetchHTTP) {
      return nativeFetch(input, init);
    }
    const body = await request.arrayBuffer();
    const response = await window.__turbomesh.fetchHTTP(
      request.method,
      url.pathname + url.search,
      headersToObject(request.headers),
      encodeBytes(new Uint8Array(body)),
    );
    return new Response(decodeBytes(response.body || ""), {
      status: response.status || 502,
      statusText: response.statusText || "",
      headers: objectToHeaders(response.headers || {}),
    });
  };

  function encodePayload(data) {
    if (data instanceof ArrayBuffer) {
      return encodeBytes(new Uint8Array(data));
    }
    if (ArrayBuffer.isView(data)) {
      return encodeBytes(new Uint8Array(data.buffer, data.byteOffset, data.byteLength));
    }
    return encodeBytes(new TextEncoder().encode(String(data)));
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

  function decodeText(value) {
    return new TextDecoder().decode(decodeBytes(value));
  }

  function decodeBytes(value) {
    const binary = atob(value);
    const bytes = new Uint8Array(binary.length);
    for (let i = 0; i < binary.length; i += 1) {
      bytes[i] = binary.charCodeAt(i);
    }
    return bytes;
  }

  function encodeBytes(bytes) {
    let binary = "";
    for (const byte of bytes) {
      binary += String.fromCharCode(byte);
    }
    return btoa(binary);
  }
})();
