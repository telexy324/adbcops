package toolregistry

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestBuiltinRegistryFindsToolByName(t *testing.T) {
	registry := NewBuiltinRegistry()

	definition, err := registry.Get(" Kubernetes ")
	if err != nil {
		t.Fatalf("get kubernetes: %v", err)
	}
	if definition.Name != "kubernetes" {
		t.Fatalf("name = %q, want kubernetes", definition.Name)
	}
	if !definition.ReadOnly || !definition.Enabled {
		t.Fatalf("unexpected definition: %+v", definition)
	}
}

func TestDisabledToolBlocksSkillExecutionAndInvocation(t *testing.T) {
	registry := NewBuiltinRegistry()

	if _, err := registry.Disable("prometheus"); err != nil {
		t.Fatalf("disable prometheus: %v", err)
	}
	if err := registry.SkillCanExecute([]string{"prometheus"}); !errors.Is(err, ErrToolDisabled) {
		t.Fatalf("SkillCanExecute error = %v, want ErrToolDisabled", err)
	}
	if err := registry.Test(context.Background(), "prometheus"); !errors.Is(err, ErrToolDisabled) {
		t.Fatalf("Test error = %v, want ErrToolDisabled", err)
	}
	if _, err := registry.Invoke(context.Background(), "prometheus", "query", json.RawMessage(`{}`)); !errors.Is(err, ErrToolDisabled) {
		t.Fatalf("Invoke error = %v, want ErrToolDisabled", err)
	}
}

func TestBuiltinToolsAreReadOnly(t *testing.T) {
	registry := NewBuiltinRegistry()

	for _, definition := range registry.List() {
		if !definition.ReadOnly {
			t.Fatalf("tool %s is not read-only", definition.Name)
		}
	}
}

func TestBuiltinRegistryIncludesComponentTools(t *testing.T) {
	registry := NewBuiltinRegistry()
	for _, name := range []string{"nacos", "redis", "tidb", "nginx"} {
		definition, err := registry.Get(name)
		if err != nil {
			t.Fatalf("get %s: %v", name, err)
		}
		if !definition.ReadOnly || len(definition.Capabilities) == 0 {
			t.Fatalf("component tool %s definition = %+v", name, definition)
		}
	}
}

func TestBuiltinRegistryIncludesLinuxServerTool(t *testing.T) {
	definition, err := NewBuiltinRegistry().Get("linux_server")
	if err != nil {
		t.Fatalf("get linux_server: %v", err)
	}
	if !definition.ReadOnly || definition.Type != "linux_server" || len(definition.Capabilities) != 14 {
		t.Fatalf("linux server definition = %+v", definition)
	}
}

func TestReadOnlyToolDoesNotExposeGenericInvoke(t *testing.T) {
	registry := NewBuiltinRegistry()

	_, err := registry.Invoke(context.Background(), "kubernetes", "delete_pod", json.RawMessage(`{}`))
	if !errors.Is(err, ErrInvokeBlocked) {
		t.Fatalf("Invoke error = %v, want ErrInvokeBlocked", err)
	}
}
