package iptrie

import (
	"encoding/binary"
	"fmt"
	"math/rand"
	"net/netip"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

func ExampleTrie() {
	ipt := NewTrie()
	ipt.Insert(netip.MustParsePrefix("10.0.0.0/8"), "foo")
	ipt.Insert(netip.MustParsePrefix("10.1.0.0/24"), "bar")

	fmt.Printf("10.2.0.1: %+v\n", ipt.Find(netip.MustParseAddr("10.2.0.1")))
	fmt.Printf("10.1.0.1: %+v\n", ipt.Find(netip.MustParseAddr("10.1.0.1")))
	fmt.Printf("11.0.0.1: %+v\n", ipt.Find(netip.MustParseAddr("11.0.0.1")))

	// Output:
	// 10.2.0.1: foo
	// 10.1.0.1: bar
	// 11.0.0.1: <nil>
}

func TestTrieInsert(t *testing.T) {
	cases := []struct {
		inserts                      []string
		expectedNetworksInDepthOrder []string
		name                         string
	}{
		{
			[]string{"192.168.0.1/24"},
			[]string{"192.168.0.1/24"},
			"basic insert",
		},
		{
			[]string{"1.2.3.4/32", "1.2.3.5/32"},
			[]string{"1.2.3.4/32", "1.2.3.5/32"},
			"single ip IPv4 network insert",
		},
		{
			[]string{"0::1/128", "0::2/128"},
			[]string{"0::1/128", "0::2/128"},
			"single ip IPv6 network insert",
		},
		{
			[]string{"192.168.0.1/16", "192.168.0.1/24"},
			[]string{"192.168.0.1/16", "192.168.0.1/24"},
			"in order insert",
		},
		{
			[]string{"192.168.0.1/32", "192.168.0.1/32"},
			[]string{"192.168.0.1/32"},
			"duplicate network insert",
		},
		{
			[]string{"192.168.0.1/24", "192.168.0.1/16"},
			[]string{"192.168.0.1/16", "192.168.0.1/24"},
			"reverse insert",
		},
		{
			[]string{"192.168.0.1/24", "192.168.1.1/24"},
			[]string{"192.168.0.1/24", "192.168.1.1/24"},
			"branch insert",
		},
		{
			[]string{"192.168.0.1/24", "192.168.1.1/24", "192.168.1.1/30"},
			[]string{"192.168.0.1/24", "192.168.1.1/24", "192.168.1.1/30"},
			"branch inserts",
		},
	}
	v := any(1)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			trie := NewTrie()
			for _, insert := range tc.inserts {
				network := netip.MustParsePrefix(insert)
				trie.Insert(network, v)
			}

			walk := trie.walkDepth()
			for _, network := range tc.expectedNetworksInDepthOrder {
				expected := normalizePrefix(netip.MustParsePrefix(network))
				actual := <-walk
				assert.Equal(t, expected, actual)
			}

			// Ensure no unexpected elements in trie.
			for network := range walk {
				assert.Nil(t, network)
			}
		})
	}
}

func ExampleTrie_String() {
	inserts := []string{"192.168.0.0/24", "192.168.1.0/24", "192.168.1.0/30"}
	trie := NewTrie()
	for _, insert := range inserts {
		network := netip.MustParsePrefix(insert)
		trie.Insert(network, "net="+insert)
	}
	fmt.Println(trie.String())

	// Output:
	// ::/0
	// ├ ::ffff:192.168.0.0/119
	// ├ ├ ::ffff:192.168.0.0/120 • net=192.168.0.0/24
	// ├ ├ ::ffff:192.168.1.0/120 • net=192.168.1.0/24
	// ├ ├ ├ ::ffff:192.168.1.0/126 • net=192.168.1.0/30
}

func TestTrieRemove(t *testing.T) {
	cases := []struct {
		inserts                      []string
		removes                      []string
		expectedRemoves              []string
		expectedNetworksInDepthOrder []string
		expectedTrieString           string
		name                         string
	}{
		{
			[]string{"192.168.0.1/24"},
			[]string{"192.168.0.1/24"},
			[]string{"192.168.0.1/24"},
			[]string{},
			"::/0",
			"basic remove",
		},
		{
			[]string{"192.168.0.1/32"},
			[]string{"192.168.0.1/24"},
			[]string{""},
			[]string{"192.168.0.1/32"},
			`::/0
├ ::ffff:192.168.0.1/128 • 192.168.0.1/32`,
			"remove from trie that contains a single network",
		},
		{
			[]string{"1.2.3.4/32", "1.2.3.5/32"},
			[]string{"1.2.3.5/32"},
			[]string{"1.2.3.5/32"},
			[]string{"1.2.3.4/32"},
			`::/0
├ ::ffff:1.2.3.4/128 • 1.2.3.4/32`,
			"single ip IPv4 network remove",
		},
		{
			[]string{"0::1/128", "0::2/128"},
			[]string{"0::2/128"},
			[]string{"0::2/128"},
			[]string{"0::1/128"},
			`::/0
├ ::1/128 • 0::1/128`,
			"single ip IPv6 network remove",
		},
		{
			[]string{"192.168.0.1/24", "192.168.0.1/25", "192.168.0.1/26"},
			[]string{"192.168.0.1/25"},
			[]string{"192.168.0.1/25"},
			[]string{"192.168.0.1/24", "192.168.0.1/26"},
			`::/0
├ ::ffff:192.168.0.0/120 • 192.168.0.1/24
├ ├ ::ffff:192.168.0.0/122 • 192.168.0.1/26`,
			"remove path prefix",
		},
		{
			[]string{"192.168.0.1/24", "192.168.0.1/25", "192.168.0.64/26", "192.168.0.1/26"},
			[]string{"192.168.0.1/25"},
			[]string{"192.168.0.1/25"},
			[]string{"192.168.0.1/24", "192.168.0.1/26", "192.168.0.64/26"},
			`::/0
├ ::ffff:192.168.0.0/120 • 192.168.0.1/24
├ ├ ::ffff:192.168.0.0/121
├ ├ ├ ::ffff:192.168.0.0/122 • 192.168.0.1/26
├ ├ ├ ::ffff:192.168.0.64/122 • 192.168.0.64/26`,
			"remove path prefix with more than 1 children",
		},
		{
			[]string{"192.168.0.1/24", "192.168.0.1/25"},
			[]string{"192.168.0.1/26"},
			[]string{""},
			[]string{"192.168.0.1/24", "192.168.0.1/25"},
			`::/0
├ ::ffff:192.168.0.0/120 • 192.168.0.1/24
├ ├ ::ffff:192.168.0.0/121 • 192.168.0.1/25`,
			"remove non existent",
		},
		{
			[]string{"10.0.0.0/8", "10.1.0.0/16"},
			[]string{"10.1.0.0/16", "10.0.0.0/8"},
			[]string{"10.1.0.0/16", "10.0.0.0/8"},
			[]string{},
			`::/0`,
			"remove all",
		},
	}

	for tci, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			trie := NewTrie()
			for _, insert := range tc.inserts {
				network := netip.MustParsePrefix(insert)
				trie.Insert(network, insert)
			}
			for i, remove := range tc.removes {
				network := netip.MustParsePrefix(remove)
				removed := trie.Remove(network)
				if str := tc.expectedRemoves[i]; str != "" {
					assert.Equal(t, str, removed, "tc=%d", tci)
				} else {
					assert.Nil(t, removed, "tc=%d", tci)
				}
			}

			walk := trie.walkDepth()
			for _, network := range tc.expectedNetworksInDepthOrder {
				expected := normalizePrefix(netip.MustParsePrefix(network))
				actual := <-walk
				assert.Equal(t, expected, actual, "tc=%d", tci)
			}

			// Ensure no unexpected elements in trie.
			for network := range walk {
				assert.Nil(t, network)
			}

			assert.Equal(t, tc.expectedTrieString, trie.String(), "tc=%d", tci)
		})
	}
}

func TestTrieContains(t *testing.T) {
	pt := NewTrie()

	assert.False(t, pt.Contains(netip.MustParseAddr("10.0.0.1")))

	pt.Insert(netip.MustParsePrefix("10.0.0.0/8"), nil)
	assert.True(t, pt.Contains(netip.MustParseAddr("10.0.0.1")))
	assert.True(t, pt.Contains(netip.MustParseAddr("10.0.0.0")))
}

func TestTrieNilValue(t *testing.T) {
	pt := NewTrie()
	pt.Insert(netip.MustParsePrefix("10.0.0.0/8"), nil)
	pt.Insert(netip.MustParsePrefix("10.1.0.0/16"), nil)
	pt.Remove(netip.MustParsePrefix("10.1.0.0/16"))
	pt.Remove(netip.MustParsePrefix("10.0.0.0/8"))
	assert.False(t, pt.Contains(netip.MustParseAddr("10.0.0.1")))
	t.Logf("%s", pt.String())
}

// Check that we can match an IPv6 /128 or an IPv4 /32 address in the tree.
func TestFindFull128(t *testing.T) {
	cases := []struct {
		inserts  []string
		ip       netip.Addr
		networks []string
		name     string
	}{
		{
			[]string{"192.168.0.1/32"},
			netip.MustParseAddr("192.168.0.1"),
			[]string{"192.168.0.1/32"},
			"basic containing network for /32 mask",
		},
		{
			[]string{"a::1/128"},
			netip.MustParseAddr("a::1"),
			[]string{"a::1/128"},
			"basic containing network for /128 mask",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			trie := NewTrie()
			for _, insert := range tc.inserts {
				network := netip.MustParsePrefix(insert)
				trie.Insert(network, insert)
			}
			expectedEntries := []netip.Prefix{}
			for _, network := range tc.networks {
				expected := normalizePrefix(netip.MustParsePrefix(network))
				expectedEntries = append(expectedEntries, expected)
			}
			contains := trie.Find(tc.ip)
			assert.NotNil(t, contains)
			networks := trie.ContainingNetworks(tc.ip)
			assert.Equal(t, expectedEntries, networks)
		})
	}
}

type expectedIPRange struct {
	start netip.Addr
	end   netip.Addr
}

func TestTrieFind(t *testing.T) {
	cases := []struct {
		inserts     []string
		expectedIPs []expectedIPRange
		name        string
	}{
		{
			[]string{"192.168.0.0/24"},
			[]expectedIPRange{
				{netip.MustParseAddr("192.168.0.0"), netip.MustParseAddr("192.168.1.0")},
			},
			"basic contains",
		},
		{
			[]string{"192.168.0.0/24", "128.168.0.0/24"},
			[]expectedIPRange{
				{netip.MustParseAddr("192.168.0.0"), netip.MustParseAddr("192.168.1.0")},
				{netip.MustParseAddr("128.168.0.0"), netip.MustParseAddr("128.168.1.0")},
			},
			"multiple ranges contains",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			trie := NewTrie()
			v := any(1)
			for _, insert := range tc.inserts {
				network := netip.MustParsePrefix(insert)
				trie.Insert(network, v)
			}
			for _, expectedIPRange := range tc.expectedIPs {
				var contains any
				start := expectedIPRange.start
				for ; expectedIPRange.end != start; start = start.Next() {
					contains = trie.Find(start)
					assert.NotNil(t, contains)
				}

				// Check out of bounds ips on both ends
				contains = trie.Find(expectedIPRange.start.Prev())
				assert.Nil(t, contains)
				contains = trie.Find(expectedIPRange.end.Next())
				assert.Nil(t, contains)
			}
		})
	}
}

func TestTrieFindOverlap(t *testing.T) {
	trie := NewTrie()

	v1 := any(1)
	trie.Insert(netip.MustParsePrefix("192.168.0.0/24"), v1)

	v2 := any(2)
	trie.Insert(netip.MustParsePrefix("192.168.0.0/25"), v2)

	v := trie.Find(netip.MustParseAddr("192.168.0.1"))
	assert.Equal(t, v2, v)
}

func TestTrieContainingNetworks(t *testing.T) {
	cases := []struct {
		inserts  []string
		ip       netip.Addr
		networks []string
		name     string
	}{
		{
			[]string{"192.168.0.0/24"},
			netip.MustParseAddr("192.168.0.1"),
			[]string{"192.168.0.0/24"},
			"basic containing networks",
		},
		{
			[]string{"192.168.0.0/24", "192.168.0.0/25"},
			netip.MustParseAddr("192.168.0.1"),
			[]string{"192.168.0.0/24", "192.168.0.0/25"},
			"inclusive networks",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			trie := NewTrie()
			v := any(1)
			for _, insert := range tc.inserts {
				network := netip.MustParsePrefix(insert)
				trie.Insert(network, v)
			}
			expectedEntries := []netip.Prefix{}
			for _, network := range tc.networks {
				expected := normalizePrefix(netip.MustParsePrefix(network))
				expectedEntries = append(expectedEntries, expected)
			}
			networks := trie.ContainingNetworks(tc.ip)
			assert.Equal(t, expectedEntries, networks)
		})
	}
}

type coveredNetworkTest struct {
	inserts  []string
	search   string
	networks []string
	name     string
}

var coveredNetworkTests = []coveredNetworkTest{
	{
		[]string{"192.168.0.0/24"},
		"192.168.0.0/16",
		[]string{"192.168.0.0/24"},
		"basic covered networks",
	},
	{
		[]string{"192.168.0.0/24"},
		"10.1.0.0/16",
		nil,
		"nothing",
	},
	{
		[]string{"192.168.0.0/24", "192.168.0.0/25"},
		"192.168.0.0/16",
		[]string{"192.168.0.0/24", "192.168.0.0/25"},
		"multiple networks",
	},
	{
		[]string{"192.168.0.0/24", "192.168.0.0/25", "192.168.0.1/32"},
		"192.168.0.0/16",
		[]string{"192.168.0.0/24", "192.168.0.0/25", "192.168.0.1/32"},
		"multiple networks 2",
	},
	{
		[]string{"192.168.1.1/32"},
		"192.168.0.0/16",
		[]string{"192.168.1.1/32"},
		"leaf",
	},
	{
		[]string{"0.0.0.0/0", "192.168.1.1/32"},
		"192.168.0.0/16",
		[]string{"192.168.1.1/32"},
		"leaf with root",
	},
	{
		[]string{
			"0.0.0.0/0", "192.168.0.0/24", "192.168.1.1/32",
			"10.1.0.0/16", "10.1.1.0/24",
		},
		"192.168.0.0/16",
		[]string{"192.168.0.0/24", "192.168.1.1/32"},
		"path not taken",
	},
	{
		[]string{
			"192.168.0.0/15",
		},
		"192.168.0.0/16",
		nil,
		"only masks different",
	},
}

func TestTrieCoveredNetworks(t *testing.T) {
	for _, tc := range coveredNetworkTests {
		t.Run(tc.name, func(t *testing.T) {
			trie := NewTrie()
			v := any(1)
			for _, insert := range tc.inserts {
				network := netip.MustParsePrefix(insert)
				trie.Insert(network, v)
			}
			var expectedEntries []netip.Prefix
			for _, network := range tc.networks {
				expected := normalizePrefix(netip.MustParsePrefix(network))
				expectedEntries = append(expectedEntries, expected)
			}
			snet := netip.MustParsePrefix(tc.search)
			networks := trie.CoveredNetworks(snet)
			assert.Equal(t, expectedEntries, networks)
		})
	}
}

func TestTrieMemUsage(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory test in `-short` mode")
	}
	numIPs := 100000
	runs := 10

	// Avg heap allocation over all runs should not be more than the heap allocation of first run multiplied
	// by threshold, picking 1% as sane number for detecting memory leak.
	thresh := 1.01

	trie := NewTrie()

	var baseLineHeap, totalHeapAllocOverRuns uint64
	for i := 0; i < runs; i++ {
		t.Logf("Executing Run %d of %d", i+1, runs)

		v := any(1)
		// Insert networks.
		for n := 0; n < numIPs; n++ {
			trie.Insert(GenLeafIPNet(GenIPV4()), v)
		}

		// Remove networks.
		all := netip.PrefixFrom(netip.IPv4Unspecified(), 0)
		ll := trie.CoveredNetworks(all)
		for i := 0; i < len(ll); i++ {
			trie.Remove(ll[i])
		}
		t.Logf("Removed All (%d networks)", len(ll))

		// Perform GC
		runtime.GC()

		// Get HeapAlloc stats.
		heapAlloc := GetHeapAllocation()
		totalHeapAllocOverRuns += heapAlloc
		if i == 0 {
			baseLineHeap = heapAlloc
		}
	}

	// Assert that heap allocation from first loop is within set threshold of avg over all runs.
	assert.Less(t, uint64(0), baseLineHeap)
	assert.LessOrEqual(t, float64(baseLineHeap), float64(totalHeapAllocOverRuns/uint64(runs))*thresh)
}

func GenLeafIPNet(ip netip.Addr) netip.Prefix {
	return netip.PrefixFrom(ip, 32)
}

var rng = rand.New(rand.NewSource(0))

// GenIPV4 generates an IPV4 address
func GenIPV4() netip.Addr {
	nn := rng.Uint32()
	if nn < 4294967295 {
		nn++
	}
	var ip [4]byte
	binary.BigEndian.PutUint32(ip[:], nn)
	return netip.AddrFrom4(ip)
}

func GetHeapAllocation() uint64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

func ExampleTrieLoader() {
	pt := NewTrie()
	ptl := NewTrieLoader(pt)

	networks := []string{
		"10.0.0.0/8",
		"10.1.0.0/16",
		"192.168.0.0/24",
		"192.168.1.1/32",
	}
	for _, n := range networks {
		ptl.Insert(netip.MustParsePrefix(n), "net="+n)
	}

	fmt.Printf("%s\n", pt.String())

	// Output:
	// ::/0
	// ├ ::ffff:0.0.0.0/96
	// ├ ├ ::ffff:10.0.0.0/104 • net=10.0.0.0/8
	// ├ ├ ├ ::ffff:10.1.0.0/112 • net=10.1.0.0/16
	// ├ ├ ::ffff:192.168.0.0/119
	// ├ ├ ├ ::ffff:192.168.0.0/120 • net=192.168.0.0/24
	// ├ ├ ├ ::ffff:192.168.1.1/128 • net=192.168.1.1/32
}
