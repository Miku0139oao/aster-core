package yaml_test

import (
	"testing"

	"github.com/Miku0139oao/aster-core/common/yaml"
)

// A YAML merge key whose merged map is unhashable used to panic the whole
// process under gopkg.in/yaml.v3 v3.0.1 ("hash of unhashable type"), because
// the library only converts its own error type and lets runtime panics escape.
// go.yaml.in/yaml/v3 returns a normal decode error instead.
func TestUnmarshalMergeKeyWithUnhashableKeyReturnsError(t *testing.T) {
	var out map[string]any
	err := yaml.Unmarshal([]byte("proxies:\n  - <<: {c: 1}\n    ? [1,2]: 3\n"), &out)
	if err == nil {
		t.Fatal("expected an error for an unhashable merge key, got nil")
	}
	t.Logf("returned error: %v", err)
}

func TestUnmarshalRoundTrips(t *testing.T) {
	var out struct {
		Name string `yaml:"name"`
		Port int    `yaml:"port"`
	}
	if err := yaml.Unmarshal([]byte("name: node\nport: 443\n"), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Name != "node" || out.Port != 443 {
		t.Fatalf("unexpected value: %+v", out)
	}
}
