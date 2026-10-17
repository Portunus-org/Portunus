package singbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// srsMagic starts every binary rule-set file.
var srsMagic = []byte("SRS")

// IsBinaryRuleSet reports whether data looks like a compiled rule-set (.srs).
func IsBinaryRuleSet(data []byte) bool {
	return len(data) > 3 && string(data[:3]) == string(srsMagic)
}

// RuleSetRules turns a rule-set file, binary (.srs) or source (JSON), into
// the JSON array of its rules, checked by sing-box itself: whatever comes
// back is safe to inline into a configuration.
func (m *Manager) RuleSetRules(ctx context.Context, data []byte) (json.RawMessage, error) {
	dir, err := os.MkdirTemp(m.dir, "ruleset-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	srs, src := filepath.Join(dir, "set.srs"), filepath.Join(dir, "set.json")

	run := func(args ...string) error {
		out, err := exec.CommandContext(ctx, m.bin, args...).CombinedOutput() // nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command -- m.bin is the operator-configured binary resolved to an absolute path in Init; arguments are fixed paths in a private temp dir
		if err != nil {
			return fmt.Errorf("%s", strings.TrimSpace(string(out)))
		}
		return nil
	}
	if IsBinaryRuleSet(data) {
		if err := os.WriteFile(srs, data, 0o600); err != nil {
			return nil, err
		}
		if err := run("rule-set", "decompile", srs, "-o", src); err != nil {
			return nil, err
		}
		if data, err = os.ReadFile(src); err != nil {
			return nil, err
		}
	} else {
		// Source format: compiling is the validation.
		if err := os.WriteFile(src, data, 0o600); err != nil {
			return nil, err
		}
		if err := run("rule-set", "compile", src, "-o", srs); err != nil {
			return nil, err
		}
	}
	var parsed struct {
		Rules json.RawMessage `json:"rules"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, err
	}
	var rules []any
	if err := json.Unmarshal(parsed.Rules, &rules); err != nil || len(rules) == 0 {
		return nil, errors.New("rule-set has no rules")
	}
	return parsed.Rules, nil
}
