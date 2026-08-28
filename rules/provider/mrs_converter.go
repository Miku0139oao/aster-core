package provider

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	P "github.com/Miku0139oao/aster-core/constant/provider"

	"github.com/klauspost/compress/zstd"
)

var mrsEncoderPool = sync.Pool{
	New: func() any {
		enc, err := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1))
		if err != nil {
			return nil
		}
		return enc
	},
}

func acquireMRSEncoder(w io.Writer) (*zstd.Encoder, error) {
	enc, _ := mrsEncoderPool.Get().(*zstd.Encoder)
	if enc == nil {
		return zstd.NewWriter(w, zstd.WithEncoderConcurrency(1))
	}
	enc.Reset(w)
	return enc, nil
}

func releaseMRSEncoder(encoder *zstd.Encoder, errp *error) {
	if encoder == nil {
		return
	}
	if closeErr := encoder.Close(); *errp == nil {
		*errp = closeErr
	}
	encoder.Reset(nil)
	mrsEncoderPool.Put(encoder)
}

func ConvertToMrs(buf []byte, behavior P.RuleBehavior, format P.RuleFormat, w io.Writer) (err error) {
	strategy := newStrategy(behavior, nil)
	strategy, err = rulesParse(buf, strategy, format)
	if err != nil {
		return err
	}
	if strategy.Count() == 0 {
		return errors.New("empty rule")
	}
	if _strategy, ok := strategy.(mrsRuleStrategy); ok {
		if format == P.MrsRule { // export to TextRule
			_strategy.DumpMrs(func(key string) bool {
				_, err = fmt.Fprintln(w, key)
				return err == nil
			})
			return err
		}

		var encoder *zstd.Encoder
		encoder, err = acquireMRSEncoder(w)
		if err != nil {
			return err
		}
		defer releaseMRSEncoder(encoder, &err)

		var hdr [mrsHeaderSize]byte
		copy(hdr[:4], MrsMagicBytes[:])
		hdr[4] = behavior.Byte()
		binary.BigEndian.PutUint64(hdr[5:13], uint64(_strategy.Count()))
		if _, err = encoder.Write(hdr[:]); err != nil {
			return err
		}
		return _strategy.WriteMrs(encoder)
	} else {
		return ErrInvalidFormat
	}
}

func ConvertMain(args []string) {
	if len(args) > 3 {
		behavior, err := P.ParseBehavior(args[0])
		if err != nil {
			panic(err)
		}
		format, err := P.ParseRuleFormat(args[1])
		if err != nil {
			panic(err)
		}
		source := args[2]
		target := args[3]

		sourceFile, err := os.ReadFile(source)
		if err != nil {
			panic(err)
		}

		targetFile, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if err != nil {
			panic(err)
		}

		err = ConvertToMrs(sourceFile, behavior, format, targetFile)
		if err != nil {
			panic(err)
		}

		err = targetFile.Close()
		if err != nil {
			panic(err)
		}
	} else {
		panic("Usage: convert-ruleset <behavior> <format> <source file> <target file>")
	}
}
