//go:build linux

package config

import (
	"bytes"
	"encoding/hex"
	"io"
	"strings"

	"go.yaml.in/yaml/v3"
)

func nativeTrustAddition(raw []byte, predecessor string) bool {
	_, ok := nativeTrustPredecessor(raw, predecessor)
	return ok
}

// Reconstruct exactly one supported addition. The hash covers every prior byte;
// parsed semantic equality is never a substitute for the configuration commitment.
// Returned bytes stay in memory, never in connection facts or review output.
func nativeTrustPredecessor(raw []byte, predecessor string) ([]byte, bool) {
	var doc yaml.Node
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	if dec.Decode(&doc) != nil || dec.Decode(new(yaml.Node)) != io.EOF || len(doc.Content) != 1 || !plainNativeYAML(&doc) {
		return nil, false
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, false
	}
	lines := bytes.SplitAfter(raw, []byte("\n"))
	offsetAt := func(line int) int {
		offset := 0
		for _, b := range lines[:line-1] {
			offset += len(b)
		}
		return offset
	}
	// Keep the original initial-block transition and its separator-line semantics.
	if nativeTrustBlockAddition(raw, predecessor) {
		key := root.Content[len(root.Content)-2]
		offset := offsetAt(key.Line)
		for {
			if connectionDigest(raw[:offset]) == predecessor {
				return raw[:offset], true
			}
			if offset == 0 {
				return nil, false
			}
			offset = bytes.LastIndexByte(raw[:offset-1], '\n') + 1
		}
	}
	for i := 0; i < len(root.Content); i += 2 {
		if root.Content[i].Value != "implementation" {
			continue
		}
		block := root.Content[i+1]
		if block.Kind != yaml.MappingNode || block.Style != 0 {
			return nil, false
		}
		for j := 0; j < len(block.Content); j += 2 {
			if block.Content[j].Value != "authorized_contracts" {
				continue
			}
			seq := block.Content[j+1]
			if seq.Kind != yaml.SequenceNode || seq.Style != 0 || len(seq.Content) < 2 {
				return nil, false
			}
			seen := map[string]bool{}
			for _, item := range seq.Content {
				if item.Kind != yaml.ScalarNode || item.Tag != "!!str" || !exactNativeDigest(item.Value) || seen[item.Value] {
					return nil, false
				}
				seen[item.Value] = true
			}
			last := seq.Content[len(seq.Content)-1]
			if last.Line < 1 || last.Line > len(lines) {
				return nil, false
			}
			line := lines[last.Line-1]
			// One whole plain block-sequence line, not flow syntax or a second edit.
			if strings.TrimSpace(string(line)) != "- "+last.Value {
				return nil, false
			}
			offset := offsetAt(last.Line)
			prior := append(append([]byte{}, raw[:offset]...), raw[offset+len(line):]...)
			if connectionDigest(prior) == predecessor {
				return prior, true
			}
			return nil, false
		}
	}
	return nil, false
}

func exactNativeDigest(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil
}
