# 1. Route DeepSeek through a network-aware local bridge

Date: 2026-09-28
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

The routing decision cannot be made once and kept, because this machine moves
between two networks with opposite requirements. At the office, a connection
that binds `en0` leaves by the physical interface and reaches DeepSeek in about
150 ms. On the `GoMobile` hotspot that same bind does not work, and the request
has to leave through an SSH tunnel that terminates in SOCKS5 on
`127.0.0.1:9902`. A fixed choice is therefore wrong on one of the two networks,
and it fails at the moment the network changes.

## How

DeepSeek is reached through a local bridge on `127.0.0.1:9905`
(`proxy-en0.js`), which makes the decision for each request. It reads the
current Wi-Fi name with the `WifiSSID` Shortcut. On `GoMobile` it forwards
through the SOCKS5 proxy on `127.0.0.1:9902`. On any other network it binds the
outbound connection to `en0` and goes direct. A name it cannot read takes the
tunnel, because the tunnel is the path that works on every network.

`networksetup` is not usable for the name: it runs without Location Services
authorization, so `airportd` answers its SSID request with an error even on a
working interface, while a Shortcut runs under a process that holds that
authorization. `shortcuts run` delivers the value only through `-o`, so the
value is written to a temporary file and read back.

The `deepseek` targets in the config point their `base_url` at
`http://127.0.0.1:9905` and set no `bind_interface`. The request to the bridge
is loopback, so binding it to a physical interface would make it fail. The
bridge binds the direct path itself, and the target's credential is sent
through it unchanged.

## Consequences

DeepSeek works on both networks with no config change and nothing to re-edit
when the network changes. Everything else stays as it was, because the rule
lives in the bridge and other targets are untouched.

The direct path costs more than a request that goes straight out of this tool.
Measured here, `GET /models` takes about 150 ms over a connection that leaves
by the physical interface, and 2.3 s through the bridge on the mobile hotspot,
where the SSH tunnel carries most of the cost. That is the price of one config
that works everywhere.

Two other costs come with it. On the direct path, traffic leaves without
corporate inspection, so that part is bought with a policy decision rather than
a technical one, and the block page names a security review ticket as the
sanctioned alternative. And the bridge is a separate process that has to be
running for any DeepSeek request to work. When it is not, the request fails
with a connection error; there is no fallback to another path.

## Alternatives considered

Per-target interface binding, which the tool still supports through
`bind_interface`. Setting it to `en0` makes the kernel scope the route lookup
to that interface, so the tunnel's aggregate routes no longer apply, and it
measured about 150 ms for `GET /models` against 1.4 s to 1.7 s through the
SOCKS proxy. It was given up for the bridge because the binding is fixed in the
config while the right path depends on the current network: a target bound to
`en0` fails on the mobile hotspot, so it would have to be re-edited on every
move. It does have one advantage the bridge does not, which is that it removes
the fixed cost of the bridge on networks where the direct path already works.

A host route through the physical gateway, such as
`sudo route add -host 3.173.21.63 192.168.1.1`. It works, but it needs root, it
needs an address list that goes stale as addresses rotate, and it is flushed
when the network changes.

Setting the interface explicitly with `IP_BOUND_IF`. This has the same effect
as the address bind, so it would add a platform-specific socket option for no
gain.

Asking the security team to re-categorise the endpoint. This is the sanctioned
path named by the block page, and it remains open.
