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
	records   map[uint32][]recordSize
	RawScheme []byte
	Stop      uint32
	Md5       string
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
	p := &PaddingFactory{
		RawScheme: rawScheme,
		Md5:       fmt.Sprintf("%x", md5.Sum(rawScheme)),
	}
	scheme := util.StringMapFromBytes(rawScheme)
	if len(scheme) == 0 {
		return nil
	}
	if stop, err := strconv.Atoi(scheme["stop"]); err == nil {
		p.Stop = uint32(stop)
	} else {
		return nil
	}
	p.records = make(map[uint32][]recordSize, len(scheme)-1)
	for key, value := range scheme {
		if key == "stop" {
			continue
		}
		pkt, err := strconv.ParseUint(key, 10, 32)
		if err != nil {
			continue
		}
		if records := parseRecordSizes(value); len(records) > 0 {
			p.records[uint32(pkt)] = records
		}
	}
	return p
}

func parseRecordSizes(value string) []recordSize {
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
			record := recordSize{min: min}
			if min != max {
				record.randomMax = uint64(max - min)
			}
			records = append(records, record)
		} else if valueRange == "c" {
			records = append(records, recordSize{checkMark: true})
		}
	}
	return records
}

func (p *PaddingFactory) GenerateRecordPayloadSizes(pkt uint32) []int {
	records := p.records[pkt]
	if len(records) == 0 {
		return nil
	}

	pktSizes := make([]int, 0, len(records))
	for _, record := range records {
		switch {
		case record.checkMark:
			pktSizes = append(pktSizes, CheckMark)
		case record.randomMax == 0:
			pktSizes = append(pktSizes, int(record.min))
		default:
			random, err := utils.CryptoRandomUint64n(record.randomMax)
			if err == nil {
				pktSizes = append(pktSizes, int(int64(random)+record.min))
			}
		}
	}
	return pktSizes
}
