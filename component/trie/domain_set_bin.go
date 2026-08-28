package trie

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/bits"
)

const maxBinaryDomainLabels = 1 << 24

func (ss *DomainSet) WriteBin(w io.Writer) (err error) {
	// version
	_, err = w.Write([]byte{1})
	if err != nil {
		return err
	}

	if err = writeUint64Slice(w, ss.leaves); err != nil {
		return err
	}
	if err = writeUint64Slice(w, ss.labelBitmap); err != nil {
		return err
	}

	var hdr [8]byte
	binary.BigEndian.PutUint64(hdr[:], uint64(len(ss.labels)))
	if _, err = w.Write(hdr[:]); err != nil {
		return err
	}
	_, err = w.Write(ss.labels)
	return err
}

func ReadDomainSetBin(r io.Reader) (ds *DomainSet, err error) {
	// version
	version := make([]byte, 1)
	_, err = io.ReadFull(r, version)
	if err != nil {
		return nil, err
	}
	if version[0] != 1 {
		return nil, errors.New("version is invalid")
	}

	ds = &DomainSet{}

	ds.leaves, err = readUint64Slice(r, 1, (maxBinaryDomainLabels+64)/64, "leaf bitmap")
	if err != nil {
		return nil, err
	}
	ds.labelBitmap, err = readUint64Slice(r, 1, (2*maxBinaryDomainLabels+64)/64, "label bitmap")
	if err != nil {
		return nil, err
	}

	var hdr [8]byte
	if _, err = io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	length := int64(binary.BigEndian.Uint64(hdr[:]))
	if length < 1 || length > maxBinaryDomainLabels {
		return nil, fmt.Errorf("label length %d is invalid", length)
	}
	ds.labels = make([]byte, int(length))
	_, err = io.ReadFull(r, ds.labels)
	if err != nil {
		return nil, err
	}

	if err := ds.validateBinary(); err != nil {
		return nil, err
	}
	ds.init()
	return ds, nil
}

// validateBinary checks the LOUDS-style tree invariants assumed by Has and
// Foreach before the structure becomes reachable from packet-routing code.
func (ss *DomainSet) validateBinary() error {
	nodes := 0
	for _, word := range ss.labelBitmap {
		nodes += bits.OnesCount64(word)
	}
	if nodes < 2 || nodes != len(ss.labels)+1 {
		return fmt.Errorf("domain set node/label mismatch: nodes=%d labels=%d", nodes, len(ss.labels))
	}
	labelBits := 2*nodes - 1 // one terminator per node plus one zero per edge
	if len(ss.labelBitmap) != (labelBits+63)/64 {
		return fmt.Errorf("domain set label bitmap has %d words, want %d", len(ss.labelBitmap), (labelBits+63)/64)
	}
	if rem := labelBits & 63; rem != 0 && ss.labelBitmap[len(ss.labelBitmap)-1]>>rem != 0 {
		return errors.New("domain set label bitmap has non-zero padding")
	}
	if len(ss.leaves) != (nodes+63)/64 {
		return fmt.Errorf("domain set leaf bitmap has %d words, want %d", len(ss.leaves), (nodes+63)/64)
	}
	if rem := nodes & 63; rem != 0 && ss.leaves[len(ss.leaves)-1]>>rem != 0 {
		return errors.New("domain set leaf bitmap has non-zero padding")
	}
	leafCount := 0
	for _, word := range ss.leaves {
		leafCount += bits.OnesCount64(word)
	}
	if leafCount == 0 {
		return errors.New("domain set has no terminal nodes")
	}

	zeros, ones := 0, 0
	for i := 0; i < labelBits; i++ {
		if getBit(ss.labelBitmap, i) == 0 {
			zeros++
		} else {
			ones++
		}
		// Before the final root-balanced bit there must still be a discovered,
		// unprocessed node. This excludes disconnected/truncated encodings.
		if i < labelBits-1 && ones > zeros {
			return fmt.Errorf("domain set tree is disconnected at bit %d", i)
		}
	}
	if zeros != nodes-1 || ones != nodes || getBit(ss.labelBitmap, labelBits-1) == 0 {
		return errors.New("domain set tree bitmap is not balanced")
	}
	return nil
}

func writeUint64Slice(w io.Writer, values []uint64) error {
	var hdr [8]byte
	binary.BigEndian.PutUint64(hdr[:], uint64(len(values)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	if len(values) == 0 {
		return nil
	}
	buf := make([]byte, len(values)*8)
	for i, v := range values {
		binary.BigEndian.PutUint64(buf[i*8:], v)
	}
	_, err := w.Write(buf)
	return err
}

func readUint64Slice(r io.Reader, minLen, maxLen int64, what string) ([]uint64, error) {
	var hdr [8]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	length := int64(binary.BigEndian.Uint64(hdr[:]))
	if length < minLen || length > maxLen {
		return nil, fmt.Errorf("%s length %d is invalid", what, length)
	}
	n := int(length)
	buf := make([]byte, n*8)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	out := make([]uint64, n)
	for i := 0; i < n; i++ {
		out[i] = binary.BigEndian.Uint64(buf[i*8:])
	}
	return out, nil
}
