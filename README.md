# Docs: [link](https://docs.roadrunner.dev/http/http)

## PROXY Protocol

Plain HTTP and HTTPS TCP listeners have separate PROXY protocol settings. Both support v1 and v2:

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

Omit `proxy_protocol` to leave that listener unchanged. An enabled listener accepts connections only from `trusted_proxies`. This list must contain the IP addresses or CIDR ranges of the immediate proxies. Each connection must start with a PROXY header, including health check connections. The listener drops all other connections.

If you omit `read_header_timeout` or set it to zero, the timeout is `5s`. Negative values are invalid.

For HTTPS, send the PROXY header before the TLS handshake. HTTP/1.1, h2c, TLS HTTP/2, and WebSocket upgrades through Go middleware continue to work.

TCP4 and TCP6 headers set the client address in handlers, access logs, and PHP's `REMOTE_ADDR`. Valid v1 `UNKNOWN` and v2 `LOCAL` headers retain the socket addresses. The parser ignores TLVs. They do not change TLS state or the URL scheme.

PROXY metadata applies to the whole connection. A proxy must use separate backend connections for different client identities.

These settings do not affect FastCGI, HTTP/3, or CertMagic's temporary ACME challenge listeners. Send challenge traffic to those listeners without PROXY headers.

`proxy_ip_parser` and `http.trusted_subnets` control HTTP forwarding headers separately. With PROXY enabled, `RemoteAddr` contains the advertised client address instead of the immediate proxy address. Check forwarding header trust settings for this address change.

The parser (`go-proxyproto` v0.15.0) can reject fragmented v1 headers. It limits the v2 address and TLV payload to 4096 bytes. Use v2 if the proxy supports it.
