# Docs: [link](https://docs.roadrunner.dev/http/http)

## PROXY Protocol

Enable PROXY protocol v1/v2 independently for plain HTTP and HTTPS TCP listeners:

```yaml
http:
  address: 0.0.0.0:8080
  proxy_protocol:
    trusted_proxies: ["10.20.0.0/24"]
    read_header_timeout: 5s
  ssl:
    address: 0.0.0.0:8443
    cert: server.pem
    key: server-key.pem
    proxy_protocol:
      trusted_proxies: ["10.30.0.10"]
      read_header_timeout: 5s
```

Omit a block to leave that listener unchanged. When enabled, `trusted_proxies`
must explicitly list the immediate proxies' IP addresses or CIDRs. Trusted peers
must send a PROXY header, including health checks; all other connections are
dropped. There is no mixed direct/proxied mode on an enabled listener. The header
timeout defaults to 5s when omitted or zero; negative values are invalid.

For HTTPS, send the PROXY header **before** the TLS handshake. HTTP/1.1, h2c,
TLS HTTP/2, and existing Go-middleware WebSocket upgrades retain their normal
behavior. TCP4/TCP6 headers set the client address seen by handlers, access logs,
and PHP's `REMOTE_ADDR`. Valid v1 `UNKNOWN` and v2 `LOCAL` headers instead retain
the socket addresses. TLVs are ignored; they do not change TLS state or URL scheme.
PROXY metadata applies to the whole connection, so a proxy must not multiplex
different client identities onto one backend connection.

These options do not affect FastCGI, HTTP/3, or CertMagic's temporary ACME challenge
listeners. Route challenge traffic without PROXY headers to those listeners.
`proxy_ip_parser` and `http.trusted_subnets` remain separate HTTP forwarding-header
settings. With PROXY enabled, `RemoteAddr` identifies the advertised client rather
than the immediate proxy; account for that when configuring forwarding-header trust.

The parser (`go-proxyproto` v0.15.0) may reject fragmented v1 headers and limits
the v2 address/TLV payload to 4096 bytes. Prefer v2 where the proxy supports it.
