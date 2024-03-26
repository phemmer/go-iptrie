package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc64"
	"math/rand"
	"net"
	"net/netip"
	"sort"
	"strconv"
	"testing"

	"github.com/asergeyev/nradix"
	infoblox "github.com/infobloxopen/go-trees/iptree"
	"github.com/phemmer/go-iptrie"
	"github.com/yl2chen/cidranger"
)

var rng = rand.New(rand.NewSource(0))

func randIP(bits int) net.IP {
	var ipa [4]byte
	ip := net.IP(ipa[:])
	binary.BigEndian.PutUint32(ip[:], uint32(rng.Intn(1<<bits)<<(32-bits)))
	return ip
}

var LoadNets []string
var LoadNetsSorted []string
var LookupIPs []string
var LookupResults []any

type netSorter []string

func (ns netSorter) Len() int {
	return len(ns)
}
func (ns netSorter) Less(i, j int) bool {
	return netip.MustParsePrefix(ns[i]).Addr().Compare(netip.MustParsePrefix(ns[j]).Addr()) < 0
}
func (ns netSorter) Swap(i, j int) {
	ns[i], ns[j] = ns[j], ns[i]
}

func init() {
	for len(LoadNets) < 100000 {
		ip := randIP(24)
		mask := strconv.Itoa(rand.Intn(25) + 8)
		LoadNets = append(LoadNets, ip.String()+"/"+mask)
	}

	LoadNetsSorted = make([]string, len(LoadNets))
	copy(LoadNetsSorted, LoadNets)
	sort.Sort(netSorter(LoadNetsSorted))

	// Construct LookupIPs with 10% guaranteed match from LoadNets, and the remaining random.
	LookupIPs = make([]string, 10000)
	take := len(LookupIPs) / 10
	for i := 0; i < take; i++ {
		pfx := netip.MustParsePrefix(LoadNets[i])
		// Since we populated the list with IPv4 addresses, hostSize is guaranteed to be < 32
		hostSize := 32 - pfx.Bits()
		host := rng.Intn(1 << hostSize)

		pfxBytes := pfx.Masked().Addr().As4()
		pfxInt := binary.BigEndian.Uint32(pfxBytes[:])
		hostBytes := binary.BigEndian.AppendUint32(nil, pfxInt|uint32(host))
		LookupIPs[i] = netip.AddrFrom4([4]byte(hostBytes)).String()
	}
	for i := take; i < len(LookupIPs); i++ {
		ip := randIP(24)
		LookupIPs[i] = ip.String()
	}

	LookupResults = make([]any, len(LookupIPs))
}

type pkg interface {
	Name() string
	Init()

	LoadNets([]string)

	ConvertIPNative(string) any
	Lookup([]any, *[]any)
	Check([]any, *[]bool)
}

type IPTrie struct {
	trie *iptrie.Trie
}

func (ipt *IPTrie) Name() string {
	return "IPTrie"
}
func (ipt *IPTrie) Init() {
	ipt.trie = iptrie.NewTrie()
}
func (ipt *IPTrie) LoadNets(nets []string) {
	loader := iptrie.NewTrieLoader(ipt.trie)
	for _, ipStr := range nets {
		loader.Insert(netip.MustParsePrefix(ipStr), ipStr)
	}
}
func (ipt *IPTrie) ConvertIPNative(ipStr string) any {
	return netip.MustParseAddr(ipStr)
}
func (ipt *IPTrie) Lookup(lookup []any, results *[]any) {
	for i, ip := range lookup {
		(*results)[i] = ipt.trie.Find(ip.(netip.Addr))
	}
}
func (ipt *IPTrie) Check(lookup []any, results *[]bool) {
	for i, ip := range lookup {
		(*results)[i] = ipt.trie.Contains(ip.(netip.Addr))
	}
}

type RangerEntry struct {
	net.IPNet
	data any
}

func (re RangerEntry) Network() net.IPNet {
	return re.IPNet
}

type Ranger struct {
	ranger cidranger.Ranger
}

func (r *Ranger) Name() string {
	return "Ranger"
}
func (r *Ranger) Init() {
	r.ranger = cidranger.NewPCTrieRanger()
}
func (r *Ranger) LoadNets(nets []string) {
	for _, ipStr := range nets {
		_, ipnet, _ := net.ParseCIDR(ipStr)
		r.ranger.Insert(RangerEntry{*ipnet, ipStr})
	}
}
func (r *Ranger) ConvertIPNative(ipStr string) any {
	return net.ParseIP(ipStr)
}
func (r *Ranger) Lookup(lookup []any, results *[]any) {
	for i, ip := range lookup {
		nets, _ := r.ranger.ContainingNetworks(ip.(net.IP))
		(*results)[i] = nets[len(nets)-1].(RangerEntry).data
	}
}
func (r *Ranger) Check(lookup []any, results *[]bool) {
	for i, ip := range lookup {
		(*results)[i], _ = r.ranger.Contains(ip.(net.IP))
	}
}

type Infoblox struct {
	ipt *infoblox.Tree
}

func (ib *Infoblox) Name() string {
	return "Infoblox"
}
func (ib *Infoblox) Init() {
	ib.ipt = infoblox.NewTree()
}
func (ib *Infoblox) LoadNets(nets []string) {
	for _, ipStr := range nets {
		_, ipnet, _ := net.ParseCIDR(ipStr)
		ib.ipt.InplaceInsertNet(ipnet, ipStr)
	}
}
func (ib *Infoblox) ConvertIPNative(ipStr string) any {
	return net.ParseIP(ipStr)
}
func (ib *Infoblox) Lookup(lookup []any, results *[]any) {
	for i, ip := range lookup {
		(*results)[i], _ = ib.ipt.GetByIP(ip.(net.IP))
	}
}
func (ib *Infoblox) Check(lookup []any, results *[]bool) {
	for i, ip := range lookup {
		_, (*results)[i] = ib.ipt.GetByIP(ip.(net.IP))
	}
}

type NRadix struct {
	tree *nradix.Tree
}

func (nr *NRadix) Name() string {
	return "NRadix"
}
func (nr *NRadix) Init() {
	nr.tree = nradix.NewTree(0)
}
func (nr *NRadix) LoadNets(nets []string) {
	for _, ipStr := range nets {
		nr.tree.AddCIDR(ipStr, ipStr)
	}
}
func (nr *NRadix) ConvertIPNative(ipStr string) any {
	return []byte(ipStr)
}
func (nr *NRadix) Lookup(lookup []any, results *[]any) {
	for i, ip := range lookup {
		(*results)[i], _ = nr.tree.FindCIDRb(ip.([]byte))
	}
}
func (nr *NRadix) Check(lookup []any, results *[]bool) {
	for i, ip := range lookup {
		v, _ := nr.tree.FindCIDRb(ip.([]byte))
		(*results)[i] = v != nil
	}
}

var pkgs = []pkg{
	&IPTrie{},
	&Ranger{},
	&Infoblox{},
	&NRadix{},
}

func BenchmarkLoadNets_Random(b *testing.B) {
	for _, pkg := range pkgs {
		b.Run(pkg.Name(), func(b *testing.B) {
			b.StopTimer()
			b.ReportMetric(float64(len(LoadNets)), "batch_size")
			pkg.Init()
			b.StartTimer()
			for n := 0; n < b.N; n++ {
				pkg.LoadNets(LoadNets)
			}
		})
	}
}

func BenchmarkLoadNets_Sorted(b *testing.B) {
	for _, pkg := range pkgs {
		b.Run(pkg.Name(), func(b *testing.B) {
			b.StopTimer()
			b.ReportMetric(float64(len(LoadNetsSorted)), "batch_size")
			pkg.Init()
			b.StartTimer()
			for n := 0; n < b.N; n++ {
				pkg.LoadNets(LoadNetsSorted)
			}
		})
	}
}

func BenchmarkRead_Check(b *testing.B) {
	var checksum uint64
	for _, pkg := range pkgs {
		b.Run(pkg.Name(), func(b *testing.B) {
			b.StopTimer()
			b.ReportMetric(float64(len(LookupIPs)), "batch_size")
			pkg.Init()
			pkg.LoadNets(LoadNets)
			lookup := make([]any, len(LookupIPs))
			for i, ipStr := range LookupIPs {
				lookup[i] = pkg.ConvertIPNative(ipStr)
			}
			results := make([]bool, len(LookupIPs))
			b.StartTimer()

			for n := 0; n < b.N; n++ {
				pkg.Check(lookup, &results)
			}

			b.StopTimer()

			buf := bytes.NewBuffer(nil)
			fmt.Fprintf(buf, "%+v", results)
			cksum := crc64.Checksum(buf.Bytes(), crc64.MakeTable(crc64.ISO))
			if cksum != checksum {
				if checksum == 0 {
					checksum = cksum
				} else {
					b.Errorf("output mismatch")
				}
			}
		})
	}
}

func BenchmarkRead_Lookup(b *testing.B) {
	var checksum uint64
	for _, pkg := range pkgs {
		b.Run(pkg.Name(), func(b *testing.B) {
			b.StopTimer()

			b.ReportMetric(float64(len(LookupIPs)), "batch_size")
			pkg.Init()
			pkg.LoadNets(LoadNets)
			lookup := make([]any, len(LookupIPs))
			for i, ipStr := range LookupIPs {
				lookup[i] = pkg.ConvertIPNative(ipStr)
			}
			results := make([]any, len(LookupIPs))

			b.StartTimer()

			for n := 0; n < b.N; n++ {
				pkg.Lookup(lookup, &results)
			}

			b.StopTimer()

			buf := bytes.NewBuffer(nil)
			fmt.Fprintf(buf, "%+v", results)
			cksum := crc64.Checksum(buf.Bytes(), crc64.MakeTable(crc64.ISO))
			if cksum != checksum {
				if checksum == 0 {
					checksum = cksum
				} else {
					b.Errorf("output mismatch")
				}
			}
		})
	}
}
