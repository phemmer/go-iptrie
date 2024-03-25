# iptrie
Highly performant IP storage & lookup using a trie in Golang.

The trie is a compressed IP radix trie implementation, similar to what is described at
https://vincent.bernat.im/en/blog/2017-ipv4-route-lookup-linux. Path compression is used to merge nodes with only one child into their parent, decreasing the amount of traversals needed when
looking up a value.

This project was originally derived from [cidranger](https://github.com/yl2chen/cidranger).

### Path compressed trie

This is visualization of a trie storing CIDR blocks `128.0.0.0/2` `192.0.0.0/2` `200.0.0.0/5` without path compression, the 0/1 number on the path indicates the bit value of the IP address at specified bit position, hence the path from root node to a child node represents a CIDR block that contains all IP ranges of its children, and children's children.
<p align="left"><img src="http://i.imgur.com/vSKTEBb.png" width="600" /></p>

Visualization of trie storing same CIDR blocks with path compression, improving both lookup speed and memory footprint.
<p align="left"><img src="http://i.imgur.com/JtaDlD4.png" width="600" /></p>

## Getting Started
Configure imports.
```go
import (
  "net"
  "net/netip"

  "github.com/phemmer/go-iptrie"
)
```
Create a new ranger implemented using Path-Compressed prefix trie.
```go
ipt := iptrie.NewTrie()
```

Inserts CIDR blocks.
```go
ipt.Insert(netip.MustParsePrefix("10.0.0.0/8"), "foo")
ipt.Insert(netip.MustParsePrefix("10.1.0.0/16"), "bar")
ipt.Insert(netip.MustParsePrefix("192.168.0.0/24"), nil)
ipt.Insert(netip.MustParsePrefix("192.168.1.1/32"), nil)
```

The prefix trie can be visualized as:
```go
ipt.String()
```
```
::/0
├ ::ffff:0.0.0.0/96
├ ├ ::ffff:10.0.0.0/104 • foo
├ ├ ├ ::ffff:10.1.0.0/112 • bar
├ ├ ::ffff:192.168.0.0/119
├ ├ ├ ::ffff:192.168.0.0/120 • <nil>
├ ├ ├ ::ffff:192.168.1.1/128 • <nil>
```
<sup>^ Note that addresses are normalized to IPv6</sup>

To test if given IP is contained in the trie:
```go
ipt.Contains(netip.MustParseAddr("10.0.0.1")) // returns true
ipt.Contains(netip.MustParseAddr("11.0.0.1")) // returns false
```

To get all the networks containing the given IP:
```go
ipt.ContainingNetworks(netip.MustParseAddr("10.1.0.0"))
```

## Bulk inserts

For insertion of a large number (millions) of addresses, it will likely be much faster to use TrieLoader.

```go
ipt := iptrie.NewTrie()
loader := iptrie.NewTrieLoader(ipt)

for network in []string{
    "10.0.0.0/8",
    "10.1.0.0/16",
    "192.168.0.0/24",
    "192.168.1.1/32",
} {
    loader.Insert(netip.MustParsePrefix(network), "net=" + network))
}
```
