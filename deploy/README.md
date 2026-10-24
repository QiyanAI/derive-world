# Reverse proxy example

This directory is an optional reference, not a production-ready deployment.
The default Compose stack publishes only `127.0.0.1:8080`; put a trusted TLS
terminator in front of it if the service is reachable from another machine.

Requirements if you use a proxy:

- Terminate HTTPS and set `PUBLIC_BASE_URL=https://your-host`.
- Preserve the `Host` header and pass `X-Forwarded-For` only if
  `TRUST_PROXY_HEADERS=true`.
- Do not cache `/api/*` responses.
- Keep the app bound to loopback and expose only the proxy.

`Caddyfile.example` is illustrative. Review it before use.
