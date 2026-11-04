package atomic

import (
	"encoding/json"
	"math"
	"testing"
)

// TestInt64AppendTextMatchesMarshalJSON pins that the allocation-free integer
// encoders agree with MarshalJSON, including negative and extreme values.
func TestInt64AppendTextMatchesMarshalJSON(t *testing.T) {
	values := []int64{0, 1, -1, 127, 128, 1 << 40, -(1 << 40), math.MaxInt64, math.MinInt64}
	for _, v := range values {
		i := NewInt64(v)
		want, err := json.Marshal(i.Load())
		if err != nil {
			t.Fatal(err)
		}
		got, err := json.Marshal(&i)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Fatalf("Int64(%d): json.Marshal = %s, MarshalJSON path = %s", v, got, want)
		}
		appended, err := i.AppendText(nil)
		if err != nil {
			t.Fatal(err)
		}
		if string(appended) != string(want) {
			t.Fatalf("Int64(%d): AppendText = %s, want %s", v, appended, want)
		}
	}
}

func TestUint64AppendTextMatchesMarshalJSON(t *testing.T) {
	values := []uint64{0, 1, 255, 1 << 40, math.MaxUint64}
	for _, v := range values {
		i := NewUint64(v)
		want, err := json.Marshal(i.Load())
		if err != nil {
			t.Fatal(err)
		}
		appended, err := i.AppendText(nil)
		if err != nil {
			t.Fatal(err)
		}
		if string(appended) != string(want) {
			t.Fatalf("Uint64(%d): AppendText = %s, want %s", v, appended, want)
		}
	}
}
