package iptrie

import (
	"fmt"
	"math/bits"
	"net/netip"
	"strings"
	"unsafe"
)

// Trie is a compressed IP radix trie implementation, similar to what is described at
// https://vincent.bernat.im/en/blog/2017-ipv4-route-lookup-linux
//
// Path compression merges nodes with only one child into their parent, decreasing the amount of traversals needed when
// looking up a value.
type Trie struct {
	parent   *Trie
	children [2]*Trie

	network netip.Prefix
	value   any
}

// NewTrie creates a new Trie.
func NewTrie() *Trie {
	return &Trie{
		network: netip.PrefixFrom(netip.IPv6Unspecified(), 0),
	}
}

func newSubTree(network netip.Prefix, value any) *Trie {
	return &Trie{
		network: network,
		value:   value,
	}
}

// Insert inserts an entry into the trie.
func (pt *Trie) Insert(network netip.Prefix, value any) {
	network = normalizePrefix(network)
	pt.insert(network, emptyize(value))
}

// Remove removes the entry identified by given network from trie.
func (pt *Trie) Remove(network netip.Prefix) any {
	network = normalizePrefix(network)
	return pt.remove(network)
}

// Find returns the value from the most specific network (largest prefix) containing the given address.
func (pt *Trie) Find(ip netip.Addr) any {
	ip = normalizeAddr(ip)
	return unempty(pt.find(ip))
}

// FindLargest returns the value from the largest network (smallest prefix) containing the given address.
func (pt *Trie) FindLargest(ip netip.Addr) any {
	ip = normalizeAddr(ip)
	return unempty(pt.findLargest(ip))
}

// Contains indicates whether the trie contains the given ip.
//
// This is just a shorthand for `FindLargest() != nil`.
func (pt *Trie) Contains(ip netip.Addr) bool {
	ip = normalizeAddr(ip)
	return pt.findLargest(ip) != nil
}

// ContainingNetworks returns the list of networks containing the given ip in ascending prefix order.
//
// Note: Inserted addresses are normalized to IPv6, so the returned list will be IPv6 only.
func (pt *Trie) ContainingNetworks(ip netip.Addr) []netip.Prefix {
	ip = normalizeAddr(ip)
	return pt.containingNetworks(ip)
}

// CoveredNetworks returns the list of networks contained within the given network.
//
// Note: Inserted addresses are normalized to IPv6, so the returned list will be IPv6 only.
func (pt *Trie) CoveredNetworks(network netip.Prefix) []netip.Prefix {
	network = normalizePrefix(network)
	return pt.coveredNetworks(network)
}

func (pt *Trie) Network() netip.Prefix {
	return pt.network
}

// String returns string representation of trie.
//
// The result will contain implicit nodes which exist as parents for multiple entries, but can be distinguished by the
// lack of a value.
//
// Note: Addresses are normalized to IPv6.
func (pt *Trie) String() string {
	children := []string{}
	padding := strings.Repeat("├ ", pt.level()+1)
	for _, child := range pt.children {
		if child == nil {
			continue
		}
		childStr := fmt.Sprintf("\n%s%s", padding, child.String())
		children = append(children, childStr)
	}

	var value string
	if pt.value != nil {
		value = fmt.Sprintf("%v", unempty(pt.value))
		if len(value) > 32 {
			value = value[0:31] + "…"
		}
		value = " • " + value
	}

	return fmt.Sprintf("%s%s%s", pt.network,
		value, strings.Join(children, ""))
}

func (pt *Trie) find(ip netip.Addr) any {
	if !netContains(pt.network, ip) {
		return nil
	}

	if pt.network.Bits() == 128 {
		return pt.value
	}

	bit := pt.discriminatorBitFromIP(ip)
	child := pt.children[bit]
	if child != nil {
		if v := child.find(ip); v != nil {
			return v
		}
	}

	return unempty(pt.value)
}

func (pt *Trie) findLargest(ip netip.Addr) any {
	if !netContains(pt.network, ip) {
		return nil
	}

	if pt.value != nil {
		return pt.value
	}

	if pt.network.Bits() == 128 {
		return nil
	}

	bit := pt.discriminatorBitFromIP(ip)
	child := pt.children[bit]
	if child != nil {
		return child.findLargest(ip)
	}

	return nil
}

func (pt *Trie) containingNetworks(ip netip.Addr) []netip.Prefix {
	var results []netip.Prefix
	if !pt.network.Contains(ip) {
		return results
	}
	if pt.value != nil {
		results = []netip.Prefix{pt.network}
	}
	if pt.network.Bits() == 128 {
		return results
	}
	bit := pt.discriminatorBitFromIP(ip)
	child := pt.children[bit]
	if child != nil {
		ranges := child.containingNetworks(ip)
		if len(ranges) > 0 {
			if len(results) > 0 {
				results = append(results, ranges...)
			} else {
				results = ranges
			}
		}
	}
	return results
}

func (pt *Trie) coveredNetworks(network netip.Prefix) []netip.Prefix {
	var results []netip.Prefix
	if network.Bits() <= pt.network.Bits() && network.Contains(pt.network.Addr()) {
		for entry := range pt.walkDepth() {
			results = append(results, entry)
		}
	} else if pt.network.Bits() < 128 {
		bit := pt.discriminatorBitFromIP(network.Addr())
		child := pt.children[bit]
		if child != nil {
			return child.coveredNetworks(network)
		}
	}
	return results
}

// This is an unsafe, but faster version of netip.Prefix.Contains
func netContains(pfx netip.Prefix, ip netip.Addr) bool {
	pfxAddr := addr128(pfx.Addr())
	ipAddr := addr128(ip)
	return ipAddr.xor(pfxAddr).and(mask6(pfx.Bits())).isZero()
}

// netDivergence returns the largest prefix shared by the provided 2 prefixes
func netDivergence(net1 netip.Prefix, net2 netip.Prefix) netip.Prefix {
	if net1.Bits() > net2.Bits() {
		net1, net2 = net2, net1
	}

	if netContains(net1, net2.Addr()) {
		return net1
	}

	diff := addr128(net1.Addr()).xor(addr128(net2.Addr()))
	var bit int
	if diff.hi != 0 {
		bit = bits.LeadingZeros64(diff.hi)
	} else {
		bit = bits.LeadingZeros64(diff.lo) + 64
	}
	if bit > net1.Bits() {
		bit = net1.Bits()
	}
	pfx, _ := net1.Addr().Prefix(bit)
	return pfx
}

func (pt *Trie) insert(network netip.Prefix, value any) *Trie {
	if pt.network == network {
		pt.value = value
		return pt
	}

	bit := pt.discriminatorBitFromIP(network.Addr())
	existingChild := pt.children[bit]

	// No existing child, insert new leaf trie.
	if existingChild == nil {
		pNew := newSubTree(network, value)
		pt.appendTrie(bit, pNew)
		return pNew
	}

	// Check whether it is necessary to insert additional path prefix between current trie and existing child,
	// in the case that inserted network diverges on its path to existing child.
	netdiv := netDivergence(existingChild.network, network)
	if netdiv != existingChild.network {
		pathPrefix := newSubTree(netdiv, nil)
		pt.insertPrefix(bit, pathPrefix, existingChild)
		// Update new child
		existingChild = pathPrefix
	}
	return existingChild.insert(network, value)
}

func (pt *Trie) appendTrie(bit uint8, prefix *Trie) {
	pt.children[bit] = prefix
	prefix.parent = pt
}

func (pt *Trie) insertPrefix(bit uint8, pathPrefix, child *Trie) {
	// Set parent/child relationship between current trie and inserted pathPrefix
	pt.children[bit] = pathPrefix
	pathPrefix.parent = pt

	// Set parent/child relationship between inserted pathPrefix and original child
	pathPrefixBit := pathPrefix.discriminatorBitFromIP(child.network.Addr())
	pathPrefix.children[pathPrefixBit] = child
	child.parent = pathPrefix
}

func (pt *Trie) remove(network netip.Prefix) any {
	if pt.value != nil && pt.network == network {
		entry := pt.value
		pt.value = nil

		pt.compressPathIfPossible()
		return entry
	}
	if pt.network.Bits() == 128 {
		return nil
	}
	bit := pt.discriminatorBitFromIP(network.Addr())
	child := pt.children[bit]
	if child != nil {
		return child.remove(network)
	}
	return nil
}

func (pt *Trie) qualifiesForPathCompression() bool {
	// Current prefix trie can be path compressed if it meets all following.
	//		1. records no CIDR entry
	//		2. has single or no child
	//		3. is not root trie
	return pt.value == nil && pt.childrenCount() <= 1 && pt.parent != nil
}

func (pt *Trie) compressPathIfPossible() {
	if !pt.qualifiesForPathCompression() {
		// Does not qualify to be compressed
		return
	}

	// Find lone child.
	var loneChild *Trie
	for _, child := range pt.children {
		if child != nil {
			loneChild = child
			break
		}
	}

	// Find root of currnt single child lineage.
	parent := pt.parent
	for ; parent.qualifiesForPathCompression(); parent = parent.parent {
	}
	parentBit := parent.discriminatorBitFromIP(pt.network.Addr())
	parent.children[parentBit] = loneChild

	// Attempts to furthur apply path compression at current lineage parent, in case current lineage
	// compressed into parent.
	parent.compressPathIfPossible()
}

func (pt *Trie) childrenCount() int {
	count := 0
	for _, child := range pt.children {
		if child != nil {
			count++
		}
	}
	return count
}

func (pt *Trie) discriminatorBitFromIP(addr netip.Addr) uint8 {
	// This is a safe uint boxing of int since we should never attempt to get
	// target bit at a negative position.
	pos := pt.network.Bits()
	a128 := addr128(addr)
	if pos < 64 {
		return uint8(a128.hi >> (63 - pos) & 1)
	}
	return uint8(a128.lo >> (63 - (pos - 64)) & 1)
}

func (pt *Trie) level() int {
	if pt.parent == nil {
		return 0
	}
	return pt.parent.level() + 1
}

// walkDepth walks the trie in depth order
func (pt *Trie) walkDepth() <-chan netip.Prefix {
	entries := make(chan netip.Prefix)
	go func() {
		if pt.value != nil {
			entries <- pt.network
		}
		childEntriesList := []<-chan netip.Prefix{}
		for _, trie := range pt.children {
			if trie == nil {
				continue
			}
			childEntriesList = append(childEntriesList, trie.walkDepth())
		}
		for _, childEntries := range childEntriesList {
			for entry := range childEntries {
				entries <- entry
			}
		}
		close(entries)
	}()
	return entries
}

// TrieLoader can be used to improve the performance of bulk inserts to a Trie. It caches the node of the
// last insert in the tree, using it as the starting point to start searching for the location of the next insert. This
// is highly beneficial when the addresses are pre-sorted.
type TrieLoader struct {
	trie       *Trie
	lastInsert *Trie
}

func NewTrieLoader(trie *Trie) *TrieLoader {
	return &TrieLoader{
		trie:       trie,
		lastInsert: trie,
	}
}

func (ptl *TrieLoader) Insert(pfx netip.Prefix, v any) {
	pfx = normalizePrefix(pfx)

	diff := addr128(ptl.lastInsert.network.Addr()).xor(addr128(pfx.Addr()))
	var pos int
	if diff.hi != 0 {
		pos = bits.LeadingZeros64(diff.hi)
	} else {
		pos = bits.LeadingZeros64(diff.lo) + 64
	}
	if pos > pfx.Bits() {
		pos = pfx.Bits()
	}
	if pos > ptl.lastInsert.network.Bits() {
		pos = ptl.lastInsert.network.Bits()
	}

	parent := ptl.lastInsert
	for parent.network.Bits() > pos {
		parent = parent.parent
	}
	ptl.lastInsert = parent.insert(pfx, v)
}

func normalizeAddr(addr netip.Addr) netip.Addr {
	if addr.Is4() {
		return netip.AddrFrom16(addr.As16())
	}
	return addr
}
func normalizePrefix(pfx netip.Prefix) netip.Prefix {
	if pfx.Addr().Is4() {
		pfx = netip.PrefixFrom(netip.AddrFrom16(pfx.Addr().As16()), pfx.Bits()+96)
	}
	return pfx.Masked()
}

// A lot of the code uses nil value tests to determine whether a node is explicit or implicitly created. Therefore
// inserted values cannot be nil, and so `empty` is a placeholder to represent nil.
type emptyStruct struct{}

var empty = emptyStruct{}

func emptyize(v any) any {
	if v == nil {
		return empty
	}
	return v
}
func unempty(v any) any {
	if v == empty {
		return nil
	}
	return v
}

func addr128(addr netip.Addr) uint128 {
	return *(*uint128)(unsafe.Pointer(&addr))
}
func init() {
	// Accessing the underlying data of a `netip.Addr` relies upon the data being
	// in a known format, which is not guaranteed to be stable. So this init()
	// function is to detect if it ever changes.
	ip := netip.AddrFrom16([16]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15})
	i128 := addr128(ip)
	if i128.hi != 0x0001020304050607 || i128.lo != 0x08090a0b0c0d0e0f {
		panic("netip.Addr format mismatch")
	}
}
