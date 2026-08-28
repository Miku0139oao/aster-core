package padding

import (
	"crypto/md5"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/Miku0139oao/aster-core/common/utils"
	"github.com/Miku0139oao/aster-core/transport/anytls/util"
)

const CheckMark = -1

const maxPaddingPayloadSize = 1<<16 - 1

var DefaultPaddingScheme = []byte(`stop=8
0=30-30
1=100-400
2=400-500,c,500-1000,c,500-1000,c,500-1000,c,500-1000
3=9-9,500-1000
4=500-1000
5=500-1000
6=500-1000
7=500-1000`)

type PaddingFactory struct {
	records    map[uint32][]recordSize
	staticSize map[uint32][]int
	RawScheme  []byte
	Stop       uint32
	Md5        string
}

type recordSize struct {
	min       int64
	randomMax uint64
	checkMark bool
}

func UpdatePaddingScheme(rawScheme []byte, to *atomic.Pointer[PaddingFactory]) bool {
	if p := NewPaddingFactory(rawScheme); p != nil {
		to.Store(p)
		return true
	}
	return false
}

func NewPaddingFactory(rawScheme []byte) *PaddingFactory {
	rawScheme = append([]byte(nil), rawScheme...)
	p := &PaddingFactory{
		RawScheme: rawScheme,
		Md5:       fmt.Sprintf("%x", md5.Sum(rawScheme)),
	}
	scheme := util.StringMapFromBytes(rawScheme)
	if len(scheme) == 0 {
		return nil
	}
	stop, err := strconv.ParseUint(scheme["stop"], 10, 32)
	if err != nil {
		return nil
	}
	p.Stop = uint32(stop)
	p.records = make(map[uint32][]recordSize, len(scheme)-1)
	for key, value := range scheme {
		if key == "stop" {
			continue
		}
		pkt, err := strconv.ParseUint(key, 10, 32)
		if err != nil {
			continue
		}
		if records, valid := parseRecordSizes(value); !valid {
			return nil
		} else if len(records) > 0 {
			p.records[uint32(pkt)] = records
		}
	}
	p.staticSize = make(map[uint32][]int, len(p.records))
	for pkt, records := range p.records {
		if sizes, ok := staticRecordSizes(records); ok {
			p.staticSize[pkt] = sizes
		}
	}
	return p
}

func parseRecordSizes(value string) ([]recordSize, bool) {
	records := make([]recordSize, 0, strings.Count(value, ",")+1)
	for _, valueRange := range strings.Split(value, ",") {
		minMax := strings.Split(valueRange, "-")
		if len(minMax) == 2 {
			min, err := strconv.ParseInt(minMax[0], 10, 64)
			if err != nil {
				continue
			}
			max, err := strconv.ParseInt(minMax[1], 10, 64)
			if err != nil {
				continue
			}
			if min > max {
				min, max = max, min
			}
			if min <= 0 || max <= 0 {
				continue
			}
			if max > maxPaddingPayloadSize {
				return nil, false
			}
			record := recordSize{min: min}
			if min != max {
				record.randomMax = uint64(max - min)
			}
			records = append(records, record)
		} else if valueRange == "c" {
			records = append(records, recordSize{checkMark: true})
		}
	}
	return records, true
}

func staticRecordSizes(records []recordSize) ([]int, bool) {
	sizes := make([]int, 0, len(records))
	for _, record := range records {
		switch {
		case record.checkMark:
			sizes = append(sizes, CheckMark)
		case record.randomMax == 0:
			sizes = append(sizes, int(record.min))
		default:
			return nil, false
		}
	}
	return sizes, true
}

// GenerateRecordPayloadSizes returns the record sizes for pkt.
// The returned slice is uniquely owned by the caller.
func (p *PaddingFactory) GenerateRecordPayloadSizes(pkt uint32) []int {
	if sizes, ok := p.staticSize[pkt]; ok {
		out := make([]int, len(sizes))
		copy(out, sizes)
		return out
	}
	return p.AppendRecordPayloadSizes(pkt, nil)
}

// AppendRecordPayloadSizes appends pkt's generated record sizes to dst.
// Callers may reuse dst across calls to avoid allocating on the random path.
// Static interned tables are copied into dst and must not be mutated in place.
func (p *PaddingFactory) AppendRecordPayloadSizes(pkt uint32, dst []int) []int {
	if sizes, ok := p.staticSize[pkt]; ok {
		return append(dst[:0], sizes...)
	}
	records := p.records[pkt]
	if len(records) == 0 {
		if dst == nil {
			return nil
		}
		return dst[:0]
	}
	dst = dst[:0]
	if cap(dst) < len(records) {
		dst = make([]int, 0, len(records))
	}
	for _, record := range records {
		switch {
		case record.checkMark:
			dst = append(dst, CheckMark)
		case record.randomMax == 0:
			dst = append(dst, int(record.min))
		default:
			random, err := utils.CryptoRandomUint64n(record.randomMax)
			if err == nil {
				dst = append(dst, int(int64(random)+record.min))
			}
		}
	}
	return dst
}
