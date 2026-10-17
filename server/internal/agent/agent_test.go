package agent

import (
	"encoding/json"
	"testing"
)

// The panel compares the hash of what it rendered with the hash the node
// reports for what it received. The request re-encodes the configuration
// on the way, so the hash must not depend on formatting.
func TestHashConfigSurvivesTransport(t *testing.T) {
	rendered := []byte("{\n  \"log\": {\"level\": \"warn\"},\n  \"note\": \"a <b> & c\",\n  \"n\": [1, 2]\n}")
	wire, err := json.Marshal(ConfigRequest{Config: rendered})
	if err != nil {
		t.Fatal(err)
	}
	var got ConfigRequest
	if err := json.Unmarshal(wire, &got); err != nil {
		t.Fatal(err)
	}
	if string(got.Config) == string(rendered) {
		t.Fatal("expected the transport to reformat the configuration")
	}
	if HashConfig(got.Config) != HashConfig(rendered) {
		t.Fatal("hash changed in transport: the panel would resend the configuration forever")
	}
	if HashConfig(nil) != "" || HashConfig(rendered) == HashConfig([]byte(`{"log":{"level":"info"}}`)) {
		t.Fatal("hash does not distinguish configurations")
	}
}
