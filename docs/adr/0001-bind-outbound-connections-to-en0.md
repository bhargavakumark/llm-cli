# 1. Bind every outbound connection to the physical interface

Date: 2026-09-27
Status: accepted, not implemented
Supersedes: nothing

## Context

`llm-cli` runs on a managed Mac and talks to OpenAI-compatible endpoints over
HTTPS. The machine's routing makes a direct connection to a public endpoint
unusual, in two ways that matter here.

First, although the default route still points at the physical interface
(`default 192.168.1.1` on `en0`), a tunnel interface (`utun5`, gateway
`100.64.0.1`) holds aggregate routes that cover essentially all of the IPv4
address space: `2/7`, `4/6`, `8/5`, `1/8`, `11/8`, `16/4`, `32/3`, `64/2`,
`128/2` and `192.0.0/2`. Longest-prefix match therefore sends nearly every
public destination through the tunnel, where corporate Zscaler inspects it.
A second set of more-specific routes already exists: 519 public prefixes are
routed direct via `192.168.1.1` on `en0`. Those exclusions do not cover the
endpoints this tool uses. Their origin could not be attributed from the
routing table alone: no launch agent and no running process adds them, so one
of the installed network agents most likely installed them when it connected.
The Zscaler client and the Cisco Secure Client are both installed and active
on the machine.

Second, Zscaler blocks at least one of those endpoints. A plain HTTPS request
to `api.deepseek.com` is reset during the TLS handshake:

```
curl: (35) Recv failure: Connection reset by peer
```

and the same host over plain HTTP returns the block page:

```
HTTP/1.1 403 Forbidden
Server: Zscaler/6.2
```

The page reports category `CSIRT-Block List` and states that access is blocked
in compliance with SFDC Security policy, and directs the reader to open a
review ticket with SFDC Security. The block page is the sanctioned remedy for
a miscategorised destination.

The working path today is a local proxy on `127.0.0.1:9904`, which reaches the
model by another route. It works, but it pays a large fixed cost per request
that has nothing to do with generation: `GET /models`, which returns a static
list, takes 1.37 s to 1.66 s through the proxy, while a direct TLS connection
to the same public endpoint completes its handshake in 17 ms.

## Decision

Every outbound connection that `llm-cli` opens binds its local address to the
IPv4 address of `en0`, resolved at the moment the connection is dialled. The
tool therefore egresses through the physical interface rather than the tunnel,
for every configured target, without changing the routing table and without
requiring elevated privileges.

Binding the source address is sufficient on macOS. The kernel scopes the route
lookup to the interface that owns the bound address, so the tunnel's aggregate
routes no longer apply. The explicit `IP_BOUND_IF` option (option 25) produces
the same result but is not required.

## Evidence

Measured on the machine, against `api.deepseek.com` (`3.173.21.63`, a CloudFront
address), with no route changes in place:

| Connection style | Result |
|---|---|
| no bind, default route | reset by peer after 48 ms |
| bound to `192.168.1.101` (address of `en0`) | TCP handshake 3 ms, TLS handshake 17 ms, peer certificate issued by `Amazon RSA 2048 M01` |
| `IP_BOUND_IF` set to the index of `en0` | TCP handshake 4 ms, TLS handshake 21 ms, same issuer |
| address bind and `IP_BOUND_IF` together | TCP handshake 5 ms, TLS handshake 16 ms, same issuer |

Because the certificate is CloudFront's own rather than an interception
certificate, the connection reaches the real endpoint.

The same comparison for a full request, `GET /models`, three samples each:

| Path | First byte | Response |
|---|---|---|
| direct over `en0` | 252 ms, 149 ms, 140 ms | 401, `x-ds-trace-id` present, so the request reached DeepSeek |
| via the local proxy | 1365 ms, 1658 ms, 1605 ms | 401 |

Both requests were unauthenticated, so the comparison is transport only. The
direct path is roughly ten times faster on the fixed cost, which is the whole
of the difference the tool has been fighting on this machine.

The measurements can be repeated. Route ownership and the block are visible
with `route -n get api.deepseek.com`, `curl -sS -o /dev/null -w '%{http_code}'`
against that address, and `curl` against the same host over plain HTTP for the
block page. The bind comparison needs a socket bound to the interface address
before it connects:

```python
import socket, ssl
s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
s.bind(("192.168.1.101", 0))                # the address of en0
s.connect(("api.deepseek.com", 443))
ssl.create_default_context().wrap_socket(s, server_hostname="api.deepseek.com")
```

Removing the `bind` call reproduces the reset.

## Consequences

The fixed per-request cost for public endpoints drops by roughly an order of
magnitude, and generation time is no longer buried under it.

The approach binds by interface rather than by address, so it keeps working
when a name resolves to a different address, when CloudFront rotates its
addresses, and when the machine moves between networks. There is no route to
add, nothing to revert, and no `sudo`.

Two costs come with it.

The first is policy. Binding to `en0` deliberately routes around the corporate
Zscaler control, so this traffic leaves without inspection. The block page
names a CSIRT block list and a Security review ticket as the way to change a
categorisation. This decision was taken deliberately by the repo owner, and the
sanctioned alternative remains a request to SFDC Security.

The second is reachability. A destination that is only reachable through the
tunnel cannot be bound to `en0` and stops working:

| Destination | unbound | bound to `en0` |
|---|---|---|
| luna gateway `100.64.1.15`, a mesh peer | connects in 0 ms | times out after 6 s |
| local proxy `127.0.0.1:9904` | connects in 0 ms | connects in 0 ms |
| `3.173.21.63` | connects in 20 ms | connects in 4 ms |

The local proxy survives because the kernel short-circuits loopback traffic.
The mesh peer does not, which directly conflicts with applying the bind to
every target: a target whose base URL is a mesh address will fail.

Further consequences to handle in the implementation:

- `en0` must exist and hold an IPv4 address. When it does not, the connection
  must fail with an error naming the interface, never fall back to the tunnel,
  because a silent fall back would reintroduce the block at unpredictable
  moments.
- A destination reached over IPv6 cannot take a bind to an IPv4 address.
- DNS is untouched. Resolution still goes through the corporate resolver, so a
  name that resolver refuses, such as `api-docs.deepseek.com`, stays refused.

## Open question

Whether the bind applies unconditionally, as instructed, or is skipped for
destinations that are only reachable through the tunnel. The candidates for an
exclusion rule are loopback addresses, `100.64.0.0/10` mesh addresses, and the
RFC 1918 private ranges. The recommended reading is that the bind applies to
public destinations only, which preserves both the local proxy and any
mesh-reachable target such as the luna gateway while still keeping public
endpoint traffic off the tunnel. This needs a decision before implementation,
because the two readings differ in whether the luna target keeps working.

## Alternatives considered

Adding a host route through `192.168.1.1`, for example
`sudo route add -host 3.173.21.63 192.168.1.1`. This works, but it needs root,
it needs an address list that goes stale as CloudFront rotates addresses, and
the entry is flushed when the network changes.

Setting `IP_BOUND_IF` explicitly on each socket. This is equivalent in effect,
but because the address bind already scopes the lookup, it would add a
dependency on platform-specific socket options for no gain.

Doing nothing and continuing to route every request through the local proxy.
This keeps the sanctioned path, but pays roughly 1.5 s of fixed cost per
request on the current machine.

Asking SFDC Security to re-categorise the endpoint. This is the sanctioned
path named by the block page, and it is not mutually exclusive with the
decision above.

## Implementation notes

`pkg/api/client.go` builds the one HTTP transport the tool uses, in
`newHTTPClient`. The dial function there becomes one that resolves `en0`'s
IPv4 address per connection and dials with `net.Dialer.LocalAddr` set to that
address. Resolution happens per connection rather than once at startup, so a
change of address is picked up without restarting the process.
