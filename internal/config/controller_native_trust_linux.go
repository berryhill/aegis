//go:build linux

package config

import (
	"bytes"
	"io"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Only a newly appended, unambiguous block mapping is supported. No YAML is
// re-encoded: an exact prefix must reproduce the predecessor commitment.
func nativeTrustAddition(raw []byte, predecessor string) bool {
	var doc yaml.Node
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	if dec.Decode(&doc) != nil || dec.Decode(new(yaml.Node)) != io.EOF || len(doc.Content) != 1 || !plainNativeYAML(&doc) {
		return false
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode || len(root.Content) < 2 {
		return false
	}
	key, value := root.Content[len(root.Content)-2], root.Content[len(root.Content)-1]
	if key.Value != "implementation" || key.Column != 1 || key.Style != 0 || value.Kind != yaml.MappingNode || value.Style != 0 || len(value.Content) != 4 {
		return false
	}
	for i := 0; i < len(value.Content); i += 2 {
		k, v := value.Content[i], value.Content[i+1]
		switch k.Value {
		case "go_binary":
			if v.Kind != yaml.ScalarNode || v.Tag != "!!str" || v.Value == "" {
				return false
			}
		case "authorized_contracts":
			if v.Kind != yaml.SequenceNode || len(v.Content) == 0 {
				return false
			}
			for _, item := range v.Content {
				if item.Kind != yaml.ScalarNode || item.Tag != "!!str" {
					return false
				}
			}
		default:
			return false
		}
	}
	lines := bytes.SplitAfter(raw, []byte("\n"))
	offset := 0
	for i := 0; i < key.Line-1; i++ {
		offset += len(lines[i])
	}
	if !bytes.HasPrefix(raw[offset:], []byte("implementation:")) {
		return false
	}
	// The safe append helper may insert blank separator lines. Only remove
	// those exact lines together with the one new block, never other content.
	for {
		if connectionDigest(raw[:offset]) == predecessor {
			return true
		}
		if offset == 0 {
			return false
		}
		start := bytes.LastIndexByte(raw[:offset-1], '\n') + 1
		if strings.TrimSpace(string(raw[start:offset])) != "" {
			return false
		}
		offset = start
	}
}

func plainNativeYAML(n *yaml.Node) bool {
	if n.Kind == yaml.AliasNode || n.Anchor != "" {
		return false
	}
	if n.Kind == yaml.MappingNode {
		seen := map[string]bool{}
		for i := 0; i < len(n.Content); i += 2 {
			key := n.Content[i]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || key.Value == "<<" || seen[strings.ToLower(key.Value)] {
				return false
			}
			seen[strings.ToLower(key.Value)] = true
		}
	}
	for _, child := range n.Content {
		if !plainNativeYAML(child) {
			return false
		}
	}
	return true
}

func ReadControllerCodexForReview(path string) (*ControllerCodexLogin, ControllerCodexConfigReview, error) {
	var state ControllerCodexConfigReview
	binding, err := readControllerCodexBinding(path, &state)
	return binding, state, err
}
