package main

import (
	"fmt"
)

func extractMap(m map[string]any, key string) (map[string]any, error) {
	val, found := m[key]
	if !found {
		return nil, fmt.Errorf("key %s not found", key)
	}
	switch val := val.(type) {
	case map[string]any:
		return val, nil
	default:
		return nil, fmt.Errorf("Type mismatch: %T vs map[string]any", val)
	}
}

func extractArray(m map[string]any, key string) ([]any, error) {
	val, found := m[key]
	if !found {
		return nil, fmt.Errorf("key %s not found", key)
	}
	switch val := val.(type) {
	case []any:
		return val, nil
	default:
		return nil, fmt.Errorf("Type mismatch: %T vs []any", val)
	}
}

func extractString(m map[string]any, key string) (string, error) {
	val, found := m[key]
	if !found {
		return "", fmt.Errorf("key %s not found", key)
	}
	switch val := val.(type) {
	case string:
		return val, nil
	default:
		return "", fmt.Errorf("Type mismatch: %T vs string", val)
	}
}
