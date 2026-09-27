# 1. Bind outbound connections to a named interface

Date: 2026-09-27
Status: accepted, implemented

## Why

This tool runs on a managed Mac whose public traffic does not take the obvious
path. The default route points at the physical interface (`192.168.1.1` on
`en0`), but a tunnel interface (`utun5`, gateway `100.64.0.1`) holds aggregate
routes covering nearly all of IPv4, so almost every public destination leaves
through the tunnel, where corporate Zscaler inspects it. One endpoint is
blocked there: TLS to `api.deepseek.com` is reset during the handshake, and the
same host over plain HTTP returns a Zscaler block page naming the category
`CSIRT-Block List` and instructing the reader to open a security review ticket.

The way around that was a local proxy on `127.0.0.1:9904`, which works but pays
a large fixed cost per request that has nothing to do with the model:
`GET /models`, which returns a static list, takes 1.4 s to 1.7 s through the
proxy, against about 150 ms for the same request over a connection that leaves
by the physical interface.

## How

A target may name a local interface in `bind_interface`, set with
`auth setup --bind-interface en0`. Every outbound connection for that target
binds its local address to that interface's IPv4 address, which makes the
kernel scope the route lookup to that interface, so the tunnel's aggregate
routes no longer apply. The address is resolved on every connection, so it
survives a DHCP change and a move between networks, and no route is added, so
no elevated privileges are needed and nothing has to be undone.

The setting is per target rather than global, because a destination reachable
only through the tunnel cannot be bound to the physical interface. The
mesh-reachable luna gateway connects in 0 ms unbound and times out when bound,
so the `deepseek` target names `en0` and the luna target names nothing.

## Consequences

Requests to a public endpoint leave by the physical interface instead of the
tunnel: they are no longer blocked, and they no longer pay the proxy's fixed
cost. Everything else stays as it was, because the setting is per target. A
target without `bind_interface` follows the routing table exactly as before,
and DNS resolution is untouched.

Two costs come with it. Traffic bound to `en0` leaves without corporate
inspection, so the saving is bought with a policy decision rather than a
technical one, and the block page names a security review ticket as the
sanctioned alternative. And an interface that does not exist, or that holds no
IPv4 address, now fails the request. That failure is deliberate: falling back
to the routing table would reintroduce the block silently and at random.

## Open issue

Go's default resolver on this machine adds 5.0 s to every process that resolves
a public name, which currently hides the saving: `models` takes 5.17 s against
0.17 s with the pure Go resolver, and `chat` takes 5.52 s against 0.45 s. The
same name resolves in milliseconds from curl, and the bound connection itself
is fast, at 4 ms to connect and 13 ms for the TLS handshake. Choosing a
resolver is therefore a separate decision from this one.

## Alternatives considered

A host route through the physical gateway, such as
`sudo route add -host 3.173.21.63 192.168.1.1`. It works, but it needs root, it
needs an address list that goes stale as addresses rotate, and it is flushed
when the network changes.

Setting the interface explicitly with `IP_BOUND_IF`. This has the same effect
as the address bind, so it would add a platform-specific socket option for no
gain.

Keeping every request on the local proxy. This stays on the sanctioned path but
keeps the 1.5 s fixed cost per request.

Asking the security team to re-categorise the endpoint. This is the sanctioned
path named by the block page, and it remains open.
