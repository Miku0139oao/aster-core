package strmatcher

import (
	list "github.com/bahlo/generic-list-go"
)

const validCharCount = 53

type MatchType struct {
	matchType Type
	exist     bool
}

const (
	TrieEdge bool = true
	FailEdge bool = false
)

type Edge struct {
	edgeType bool
	nextNode int
}

type ACAutomaton struct {
	trie   [][validCharCount]Edge
	fail   []int
	exists []MatchType
	count  int
}

func newNode() [validCharCount]Edge {
	// Zero value is FailEdge (false) with nextNode 0.
	return [validCharCount]Edge{}
}

const acInvalid int8 = -1

// acIndex maps a byte to the compact 53-slot alphabet, or acInvalid.
// A 256-entry table avoids a bounds check and the A/a-vs-zero special case
// on every character of Match.
var acIndex = func() [256]int8 {
	var t [256]int8
	for i := range t {
		t[i] = acInvalid
	}
	for i := int8(0); i < 26; i++ {
		t['A'+byte(i)] = i
		t['a'+byte(i)] = i
	}
	t['!'] = 26
	t['$'] = 27
	t['&'] = 28
	t['\''] = 29
	t['('] = 30
	t[')'] = 31
	t['*'] = 32
	t['+'] = 33
	t[','] = 34
	t[';'] = 35
	t['='] = 36
	t[':'] = 37
	t['%'] = 38
	t['-'] = 39
	t['.'] = 40
	t['_'] = 41
	t['~'] = 42
	for i := int8(0); i < 10; i++ {
		t['0'+byte(i)] = 43 + i
	}
	return t
}()

func NewACAutomaton() *ACAutomaton {
	ac := new(ACAutomaton)
	ac.trie = append(ac.trie, newNode())
	ac.fail = append(ac.fail, 0)
	ac.exists = append(ac.exists, MatchType{
		matchType: Full,
		exist:     false,
	})
	return ac
}

func acCharIndex(char byte) (int, bool) {
	idx := acIndex[char]
	if idx < 0 {
		return 0, false
	}
	return int(idx), true
}

func acPatternSupported(pattern string) bool {
	for i := range pattern {
		if _, ok := acCharIndex(pattern[i]); !ok {
			return false
		}
	}
	return true
}

func (ac *ACAutomaton) Add(domain string, t Type) bool {
	// Validate before mutating so callers can fall back to a byte-capable
	// matcher without leaving a partial path in the automaton.
	if !acPatternSupported(domain) {
		return false
	}
	node := 0
	for i := len(domain) - 1; i >= 0; i-- {
		idx, _ := acCharIndex(domain[i])
		if ac.trie[node][idx].nextNode == 0 {
			ac.count++
			if len(ac.trie) < ac.count+1 {
				ac.trie = append(ac.trie, newNode())
				ac.fail = append(ac.fail, 0)
				ac.exists = append(ac.exists, MatchType{
					matchType: Full,
					exist:     false,
				})
			}
			ac.trie[node][idx] = Edge{
				edgeType: TrieEdge,
				nextNode: ac.count,
			}
		}
		node = ac.trie[node][idx].nextNode
	}
	ac.exists[node] = MatchType{
		matchType: t,
		exist:     true,
	}
	switch t {
	case Domain:
		ac.exists[node] = MatchType{
			matchType: Full,
			exist:     true,
		}
		idx := int(acIndex['.'])
		if ac.trie[node][idx].nextNode == 0 {
			ac.count++
			if len(ac.trie) < ac.count+1 {
				ac.trie = append(ac.trie, newNode())
				ac.fail = append(ac.fail, 0)
				ac.exists = append(ac.exists, MatchType{
					matchType: Full,
					exist:     false,
				})
			}
			ac.trie[node][idx] = Edge{
				edgeType: TrieEdge,
				nextNode: ac.count,
			}
		}
		node = ac.trie[node][idx].nextNode
		ac.exists[node] = MatchType{
			matchType: t,
			exist:     true,
		}
	default:
		break
	}
	return true
}

func (ac *ACAutomaton) Build() {
	queue := list.New[Edge]()
	for i := 0; i < validCharCount; i++ {
		if ac.trie[0][i].nextNode != 0 {
			queue.PushBack(ac.trie[0][i])
		}
	}
	for {
		front := queue.Front()
		if front == nil {
			break
		} else {
			node := front.Value.nextNode
			queue.Remove(front)
			for i := 0; i < validCharCount; i++ {
				if ac.trie[node][i].nextNode != 0 {
					ac.fail[ac.trie[node][i].nextNode] = ac.trie[ac.fail[node]][i].nextNode
					queue.PushBack(ac.trie[node][i])
				} else {
					ac.trie[node][i] = Edge{
						edgeType: FailEdge,
						nextNode: ac.trie[ac.fail[node]][i].nextNode,
					}
				}
			}
		}
	}
}

func (ac *ACAutomaton) Match(s string) bool {
	node := 0
	fullMatch := true
	// 1. the match string is all through trie edge. FULL MATCH or DOMAIN
	// 2. the match string is through a fail edge. NOT FULL MATCH
	// 2.1 Through a fail edge, but there exists a valid node. SUBSTR
	for i := len(s) - 1; i >= 0; i-- {
		idx := acIndex[s[i]]
		if idx < 0 {
			// An unsupported byte is a hard boundary for compact-alphabet
			// patterns. Resetting preserves substring matches on either side.
			node = 0
			fullMatch = false
			continue
		}
		e := ac.trie[node][idx]
		fullMatch = fullMatch && e.edgeType
		node = e.nextNode
		switch ac.exists[node].matchType {
		case Substr:
			return true
		case Domain:
			if fullMatch {
				return true
			}
		}
	}
	return fullMatch && ac.exists[node].exist
}
