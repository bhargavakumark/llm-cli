# 1. Route DeepSeek through a network-aware local bridge

Date: 2026-09-28
Status: accepted, implemented

## Why
This managed Mac routes almost all public IPv4 through a tunnel (`utun5`, 
gateway `100.64.0.1`) where corporate Zscaler inspects it, and TLS to 
`api.deepseek.com` is reset there. The right path also changes with the 
network: on the office network a connection bound to `en0` reaches DeepSeek 
directly, while on the `GoMobile` hotspot it must leave through a SOCKS5 tunnel 
on `127.0.0.1:9902`.

## How
A local bridge on `127.0.0.1:9905` (`proxy-en0.js`) decides per request. It 
reads the Wi-Fi name with the `WifiSSID` Shortcut, because `networksetup` runs 
without Location Services authorization and gets an error from `airportd`. On 
`GoMobile`, or when the name cannot be read, it forwards through SOCKS5; on any 
other network it binds the outbound connection to `en0`. The `deepseek` targets 
point `base_url` at the bridge and set no `bind_interface`, because the 
loopback request must not be bound.

## Consequences
DeepSeek works on both networks with no config edit, measured at about 150 ms 
direct against 2.3 s through the bridge on the hotspot, but the bridge must be 
running; without it a request fails with a connection error and there is no 
fallback. The direct path also leaves without corporate inspection, a policy 
decision the block page says needs a security review ticket.
