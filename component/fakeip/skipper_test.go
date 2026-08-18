package fakeip

import (
	"testing"

	"github.com/Miku0139oao/aster-core/component/trie"
	C "github.com/Miku0139oao/aster-core/constant"

	"github.com/stretchr/testify/assert"
)

func TestSkipper_BlackList(t *testing.T) {
	tree := trie.New[struct{}]()
	assert.NoError(t, tree.Insert("example.com", struct{}{}))
	assert.False(t, tree.IsEmpty())
	skipper := &Skipper{
		Host: []C.DomainMatcher{tree.NewDomainSet()},
	}
	assert.True(t, skipper.ShouldSkipped("example.com"))
	assert.False(t, skipper.ShouldSkipped("foo.com"))
	assert.False(t, skipper.shouldSkipped("baz.com"))
}

func TestSkipper_CaseInsensitive(t *testing.T) {
	tree := trie.New[struct{}]()
	assert.NoError(t, tree.Insert("example.com", struct{}{}))
	skipper := &Skipper{
		Host: []C.DomainMatcher{tree.NewDomainSet()},
	}
	assert.True(t, skipper.ShouldSkipped("EXAMPLE.COM"))
	assert.True(t, skipper.ShouldSkipped("Example.Com"))
	assert.False(t, skipper.ShouldSkipped("FOO.COM"))
}

func TestSkipper_Nil(t *testing.T) {
	var skipper *Skipper
	assert.False(t, skipper.ShouldSkipped("example.com"))
}

func TestSkipper_WhiteList(t *testing.T) {
	tree := trie.New[struct{}]()
	assert.NoError(t, tree.Insert("example.com", struct{}{}))
	assert.False(t, tree.IsEmpty())
	skipper := &Skipper{
		Host: []C.DomainMatcher{tree.NewDomainSet()},
		Mode: C.FilterWhiteList,
	}
	assert.False(t, skipper.ShouldSkipped("example.com"))
	assert.True(t, skipper.ShouldSkipped("foo.com"))
	assert.True(t, skipper.ShouldSkipped("baz.com"))
}
