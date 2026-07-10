package main

import (
	"errors"
	"fmt"
)

func extractMap(m map[string]any, key string) (map[string]any, error) {
	val, found := m[key]
	if !found {
		return nil, errors.New(fmt.Sprintf("key %s not found", key))
	}
	switch val.(type) {
	case map[string]any:
		return val.(map[string]any), nil
	default:
		return nil, errors.New(fmt.Sprintf("Type mismatch: %T vs map[string]any", val))
	}
}

func extractArray(m map[string]any, key string) ([]any, error) {
	val, found := m[key]
	if !found {
		return nil, errors.New(fmt.Sprintf("key %s not found", key))
	}
	switch val.(type) {
	case []any:
		return val.([]any), nil
	default:
		return nil, errors.New(fmt.Sprintf("Type mismatch: %T vs []any", val))
	}
}

func extractString(m map[string]any, key string) (string, error) {
	val, found := m[key]
	if !found {
		return "", errors.New(fmt.Sprintf("key %s not found", key))
	}
	switch val.(type) {
	case string:
		return val.(string), nil
	default:
		return "", errors.New(fmt.Sprintf("Type mismatch: %T vs string", val))
	}
}
