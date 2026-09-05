package cidr

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/netip"

	"go4.org/netipx"
)

func (ss *IpCidrSet) WriteBin(w io.Writer) (err error) {
	// version
	_, err = w.Write([]byte{1})
	if err != nil {
		return err
	}

	n := ss.rangeCount()
	var hdr [8]byte
	binary.BigEndian.PutUint64(hdr[:], uint64(n))
	if _, err = w.Write(hdr[:]); err != nil {
		return err
	}
	if n == 0 {
		return nil
	}
	buf := make([]byte, n*32)
	i := 0
	ss.forEachRange(func(r netipx.IPRange) bool {
		from := r.From().As16()
		to := r.To().As16()
		copy(buf[i*32:], from[:])
		copy(buf[i*32+16:], to[:])
		i++
		return true
	})
	_, err = w.Write(buf)
	return err
}

const maxBinaryIPRanges = 1 << 20

func ReadIpCidrSet(r io.Reader) (ss *IpCidrSet, err error) {
	// version
	version := make([]byte, 1)
	_, err = io.ReadFull(r, version)
	if err != nil {
		return nil, err
	}
	if version[0] != 1 {
		return nil, errors.New("version is invalid")
	}

	ss = NewIpCidrSet()

	var hdr [8]byte
	if _, err = io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	length := int64(binary.BigEndian.Uint64(hdr[:]))
	if length < 1 {
		return nil, errors.New("length is invalid")
	}
	if length > maxBinaryIPRanges {
		return nil, fmt.Errorf("IP range count %d exceeds maximum %d", length, maxBinaryIPRanges)
	}
	n := int(length)
	buf := make([]byte, n*32)
	if _, err = io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	ss.rr = make([]netipx.IPRange, n)
	for i := 0; i < n; i++ {
		var a16 [16]byte
		off := i * 32
		copy(a16[:], buf[off:off+16])
		from := netip.AddrFrom16(a16).Unmap()
		copy(a16[:], buf[off+16:off+32])
		to := netip.AddrFrom16(a16).Unmap()
		rangeValue := netipx.IPRangeFrom(from, to)
		if !rangeValue.IsValid() {
			return nil, fmt.Errorf("IP range %d is invalid", i)
		}
		ss.rr[i] = rangeValue
	}
	if err := ss.Merge(); err != nil {
		return nil, fmt.Errorf("normalize IP ranges: %w", err)
	}
	return ss, nil
}
