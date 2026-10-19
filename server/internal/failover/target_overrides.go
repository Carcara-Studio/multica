package failover

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
)

var ErrTargetDocument = errors.New("invalid fallback execution configuration")

// ExecutionOverrides is a complete, target-local document. Empty values never
// inherit primary credentials, arguments, MCP or runtime-specific options.
// Version describes serialization, not readiness or an adapter capability.
type ExecutionOverrides struct {
	Version       int               `json:"version"`
	CustomEnv     map[string]string `json:"custom_env"`
	CustomArgs    []string          `json:"custom_args"`
	RuntimeConfig json.RawMessage   `json:"runtime_config"`
	MCPConfig     json.RawMessage   `json:"mcp_config"`
}

func decodeExecutionOverrides(raw []byte) (ExecutionOverrides, error) {
	var out ExecutionOverrides
	if len(raw) == 0 || len(raw) > 1<<20 {
		return out, ErrTargetDocument
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&out) != nil {
		return ExecutionOverrides{}, ErrTargetDocument
	}
	if d.Decode(new(any)) != io.EOF {
		return ExecutionOverrides{}, ErrTargetDocument
	}
	return normalizeExecutionOverrides(out)
}

func normalizeExecutionOverrides(in ExecutionOverrides) (ExecutionOverrides, error) {
	if in.Version != 1 {
		return ExecutionOverrides{}, ErrTargetDocument
	}
	out := in
	out.CustomEnv = make(map[string]string, len(in.CustomEnv))
	for key, value := range in.CustomEnv {
		if key == "" || strings.ContainsAny(key, "=\x00") || strings.ContainsRune(value, 0) {
			return ExecutionOverrides{}, ErrTargetDocument
		}
		out.CustomEnv[key] = value
	}
	out.CustomArgs = slices.Clone(in.CustomArgs)
	if out.CustomArgs == nil {
		out.CustomArgs = []string{}
	}
	for _, arg := range out.CustomArgs {
		if strings.ContainsRune(arg, 0) {
			return ExecutionOverrides{}, ErrTargetDocument
		}
	}
	if len(out.RuntimeConfig) == 0 {
		out.RuntimeConfig = json.RawMessage(`{}`)
	}
	for i, raw := range []json.RawMessage{out.RuntimeConfig, out.MCPConfig} {
		if len(raw) == 0 {
			continue
		}
		var value map[string]json.RawMessage
		if json.Unmarshal(raw, &value) != nil || (i == 0 && value == nil) {
			return ExecutionOverrides{}, ErrTargetDocument
		}
	}
	out.RuntimeConfig = bytes.Clone(out.RuntimeConfig)
	out.MCPConfig = bytes.Clone(out.MCPConfig)
	raw, err := json.Marshal(out)
	if err != nil || len(raw) > 1<<20 {
		return ExecutionOverrides{}, ErrTargetDocument
	}
	return out, nil
}
