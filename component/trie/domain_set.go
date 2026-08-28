package trie

// Package succinct provides several succinct data types.
// Modify from https://github.com/openacid/succinct/blob/d4684c35d123f7528b14e03c24327231723db704/sskv.go

import (
	"math/bits"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/Miku0139oao/aster-core/common/utils"

	"github.com/openacid/low/bitmap"
)

const (
	complexWildcardByte = byte('+')
	wildcardByte        = byte('*')
	domainStepByte      = byte('.')
)

type DomainSet struct {
	leaves, labelBitmap []uint64
	labels              []byte
	ranks, selects      []int32
	// childStart[i] is the index in labels of node i's first child.
	// childStart[i+1] is exclusive. Built in init(); not serialized.
	childStart []int32
	rankOnce   sync.Once
}

// asciiLower maps a byte to its ASCII lowercase counterpart. Non-letters are
// unchanged, so UTF-8 payload bytes pass through after the unicode path has
// already lowercased the string.
var asciiLower = func() (table [256]byte) {
	for i := 0; i < 256; i++ {
		c := byte(i)
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		table[i] = c
	}
	return table
}()

type qElt struct{ s, e, col int }

// NewDomainSet creates a new *DomainSet struct, from a DomainTrie.
func (t *DomainTrie[T]) NewDomainSet() *DomainSet {
	reserveDomains := make([]string, 0)
	t.Foreach(func(domain string, data T) bool {
		reserveDomains = append(reserveDomains, utils.Reverse(domain))
		return true
	})
	// ensure that the same prefix is continuous
	// and according to the ascending sequence of length
	sort.Strings(reserveDomains)
	keys := reserveDomains
	if len(keys) == 0 {
		return nil
	}
	ss := &DomainSet{}
	lIdx := 0

	queue := []qElt{{0, len(keys), 0}}
	for i := 0; i < len(queue); i++ {
		elt := queue[i]
		if elt.col == len(keys[elt.s]) {
			elt.s++
			// a leaf node
			setBit(&ss.leaves, i, 1)
		}

		for j := elt.s; j < elt.e; {

			frm := j

			for ; j < elt.e && keys[j][elt.col] == keys[frm][elt.col]; j++ {
			}
			queue = append(queue, qElt{frm, j, elt.col + 1})
			ss.labels = append(ss.labels, keys[frm][elt.col])
			setBit(&ss.labelBitmap, lIdx, 0)
			lIdx++
		}
		setBit(&ss.labelBitmap, lIdx, 1)
		lIdx++
	}

	ss.init()
	return ss
}

// Has query for a key and return whether it presents in the DomainSet.
func (ss *DomainSet) Has(key string) bool {
	if ss == nil || len(ss.childStart) < 2 {
		return false
	}
	for i := 0; i < len(key); i++ {
		if key[i] >= utf8.RuneSelf {
			// The set is built with rune-wise reversal, which only matches
			// byte-wise reversal for ASCII. Normalize the same way and
			// byte-reverse so revLowerAt below observes it unchanged.
			key = byteReverse(strings.ToLower(utils.Reverse(key)))
			break
		}
	}

	type wildcardCursor struct {
		nodeId, index int
	}
	var stackBuf [8]wildcardCursor
	stack := stackBuf[:0]

	nodeId := 0
	e := int(ss.childStart[0])
	i := 0
	for {
		for ; i < len(key); i++ {
			c := revLowerAt(key, i)
			end := int(ss.childStart[nodeId+1])
			found := false
			for ; e < end; e++ {
				lab := ss.labels[e]
				if lab == complexWildcardByte {
					return true
				}
				if lab == wildcardByte {
					stack = append(stack, wildcardCursor{nodeId: e + 1, index: i})
				} else if lab == c {
					found = true
					break
				}
			}
			if !found {
				goto backtrack
			}
			nodeId = e + 1
			e = int(ss.childStart[nodeId])
		}
		if getBit(ss.leaves, nodeId) != 0 {
			return true
		}
	backtrack:
		ok := false
		for len(stack) > 0 {
			cursor := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			nodeId = cursor.nodeId
			i = cursor.index
			for ; i < len(key) && revLowerAt(key, i) != domainStepByte; i++ {
			}
			if i == len(key) {
				if getBit(ss.leaves, nodeId) != 0 {
					return true
				}
				continue
			}
			start := int(ss.childStart[nodeId])
			end := int(ss.childStart[nodeId+1])
			for e2 := start; e2 < end; e2++ {
				if ss.labels[e2] == domainStepByte {
					e = e2
					ok = true
					break
				}
			}
			if ok {
				break
			}
		}
		if !ok {
			return false
		}
	}
}

// revLowerAt returns the i-th byte of key read back to front, lowercased for
// ASCII. It lets Has walk the reversed key without materializing it.
func revLowerAt(key string, i int) byte {
	return asciiLower[key[len(key)-1-i]]
}

func byteReverse(s string) string {
	buf := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		buf[i] = s[len(s)-1-i]
	}
	return string(buf)
}

func (ss *DomainSet) keys(f func(key string) bool) {
	ss.ensureRankSelect()
	var currentKey []byte
	var traverse func(int, int) bool
	traverse = func(nodeId, bmIdx int) bool {
		if getBit(ss.leaves, nodeId) != 0 {
			if !f(string(currentKey)) {
				return false
			}
		}

		for ; ; bmIdx++ {
			if getBit(ss.labelBitmap, bmIdx) != 0 {
				return true
			}
			nextLabel := ss.labels[bmIdx-nodeId]
			currentKey = append(currentKey, nextLabel)
			nextNodeId := countZeros(ss.labelBitmap, ss.ranks, bmIdx+1)
			nextBmIdx := selectIthOne(ss.labelBitmap, ss.ranks, ss.selects, nextNodeId-1) + 1
			if !traverse(nextNodeId, nextBmIdx) {
				return false
			}
			currentKey = currentKey[:len(currentKey)-1]
		}
	}

	traverse(0, 0)
	return
}

func (ss *DomainSet) Foreach(f func(key string) bool) {
	ss.keys(func(key string) bool {
		return f(utils.Reverse(key))
	})
}

// MatchDomain implements C.DomainMatcher
func (ss *DomainSet) MatchDomain(domain string) bool {
	return ss.Has(domain)
}

func setBit(bm *[]uint64, i int, v int) {
	for i>>6 >= len(*bm) {
		*bm = append(*bm, 0)
	}
	(*bm)[i>>6] |= uint64(v) << uint(i&63)
}

func getBit(bm []uint64, i int) uint64 {
	return bm[i>>6] & (1 << uint(i&63))
}

// init builds the child index used by Has. Rank/select indexes used by
// Foreach are built lazily; the lookup path never needs them.
func (ss *DomainSet) init() {
	ss.buildChildIndex()
}

func (ss *DomainSet) ensureRankSelect() {
	ss.rankOnce.Do(func() {
		ss.selects, ss.ranks = bitmap.IndexSelect32R64(ss.labelBitmap)
	})
}

// buildChildIndex flattens LOUDS edges into per-node label ranges so Has can
// walk children without rank/select on every character.
func (ss *DomainSet) buildChildIndex() {
	nNodes := 0
	for _, word := range ss.labelBitmap {
		nNodes += bits.OnesCount64(word)
	}
	if nNodes == 0 {
		ss.childStart = []int32{0, 0}
		return
	}
	childStart := make([]int32, nNodes+1)
	node := 0
	var edge int32
	for _, word := range ss.labelBitmap {
		for b := 0; b < 64; b++ {
			if word&(uint64(1)<<uint(b)) == 0 {
				edge++
				continue
			}
			node++
			childStart[node] = edge
			if node == nNodes {
				ss.childStart = childStart
				return
			}
		}
	}
	ss.childStart = childStart
}

// countZeros counts the number of "0" in a bitmap before the i-th bit(excluding
// the i-th bit) on behalf of rank index.
// E.g.:
//
//	countZeros("010010", 4) == 3
//	//          012345
func countZeros(bm []uint64, ranks []int32, i int) int {
	a, _ := bitmap.Rank64(bm, ranks, int32(i))
	return i - int(a)
}

// selectIthOne returns the index of the i-th "1" in a bitmap, on behalf of rank
// and select indexes.
// E.g.:
//
//	selectIthOne("010010", 1) == 4
//	//            012345
func selectIthOne(bm []uint64, ranks, selects []int32, i int) int {
	a, _ := bitmap.Select32R64(bm, selects, ranks, int32(i))
	return int(a)
}
