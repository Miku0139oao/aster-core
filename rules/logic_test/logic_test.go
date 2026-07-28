package logic_test

import (
	"testing"

	C "github.com/Miku0139oao/aster-core/constant"
	"github.com/Miku0139oao/aster-core/rules"
	// https://github.com/golang/go/wiki/CodeReviewComments#import-dot
	. "github.com/Miku0139oao/aster-core/rules/logic"

	"github.com/stretchr/testify/assert"
)

var ParseRule = rules.ParseRule

func TestAND(t *testing.T) {
	and, err := NewAND("((DOMAIN,baidu.com),(NETWORK,TCP),(DST-PORT,10001-65535))", "DIRECT", ParseRule)
	assert.Equal(t, nil, err)
	assert.Equal(t, "DIRECT", and.Adapter())
	m, _ := and.Match(&C.Metadata{
		Host:    "baidu.com",
		NetWork: C.TCP,
		DstPort: 20000,
	}, C.RuleMatchHelper{})
	assert.Equal(t, true, m)

	_, err = NewAND("(DOMAIN,baidu.com),(NETWORK,TCP),(DST-PORT,10001-65535))", "DIRECT", ParseRule)
	assert.NotEqual(t, nil, err)

	_, err = NewAND("((AND,(DOMAIN,baidu.com),(NETWORK,TCP)),(NETWORK,TCP),(DST-PORT,10001-65535))", "DIRECT", ParseRule)
	assert.Equal(t, nil, err)
}

func TestNOT(t *testing.T) {
	not, err := NewNOT("((DST-PORT,6000-6500))", "REJECT", ParseRule)
	assert.Equal(t, nil, err)
	m, _ := not.Match(&C.Metadata{
		DstPort: 6100,
	}, C.RuleMatchHelper{})
	assert.Equal(t, false, m)

	_, err = NewNOT("(DST-PORT,5600-6666)", "DIRECT", ParseRule)
	assert.NotEqual(t, nil, err)

	_, err = NewNOT("DST-PORT,5600-6666", "DIRECT", ParseRule)
	assert.NotEqual(t, nil, err)

	_, err = NewNOT("((DST-PORT,5600-6666),(DOMAIN,baidu.com))", "DIRECT", ParseRule)
	assert.NotEqual(t, nil, err)

	_, err = NewNOT("(())", "DIRECT", ParseRule)
	assert.NotEqual(t, nil, err)
}

func TestOR(t *testing.T) {
	or, err := NewOR("((DOMAIN,baidu.com),(NETWORK,TCP),(DST-PORT,10001-65535))", "DIRECT", ParseRule)
	assert.Equal(t, nil, err)
	m, _ := or.Match(&C.Metadata{
		NetWork: C.TCP,
	}, C.RuleMatchHelper{})
	assert.Equal(t, true, m)
}
