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

	// leaves
	err = binary.Write(w, binary.BigEndian, int64(len(ss.leaves)))
	if err != nil {
		return err
	}
	for _, d := range ss.leaves {
		err = binary.Write(w, binary.BigEndian, d)
		if err != nil {
			return err
		}
	}

	// labelBitmap
	err = binary.Write(w, binary.BigEndian, int64(len(ss.labelBitmap)))
	if err != nil {
		return err
	}
	for _, d := range ss.labelBitmap {
		err = binary.Write(w, binary.BigEndian, d)
		if err != nil {
			return err
		}
	}

	// labels
	err = binary.Write(w, binary.BigEndian, int64(len(ss.labels)))
	if err != nil {
		return err
	}
	_, err = w.Write(ss.labels)
	if err != nil {
		return err
	}

	return nil
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
	var length int64

	// leaves
	err = binary.Read(r, binary.BigEndian, &length)
	if err != nil {
		return nil, err
	}
	if length < 1 || length > (maxBinaryDomainLabels+64)/64 {
		return nil, fmt.Errorf("leaf bitmap length %d is invalid", length)
	}
	ds.leaves = make([]uint64, int(length))
	for i := int64(0); i < length; i++ {
		err = binary.Read(r, binary.BigEndian, &ds.leaves[i])
		if err != nil {
			return nil, err
		}
	}

	// labelBitmap
	err = binary.Read(r, binary.BigEndian, &length)
	if err != nil {
		return nil, err
	}
	if length < 1 || length > (2*maxBinaryDomainLabels+64)/64 {
		return nil, fmt.Errorf("label bitmap length %d is invalid", length)
	}
	ds.labelBitmap = make([]uint64, int(length))
	for i := int64(0); i < length; i++ {
		err = binary.Read(r, binary.BigEndian, &ds.labelBitmap[i])
		if err != nil {
			return nil, err
		}
	}

	// labels
	err = binary.Read(r, binary.BigEndian, &length)
	if err != nil {
		return nil, err
	}
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
