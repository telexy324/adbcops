package toolregistry

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
)

var (
	ErrInvalidInput  = errors.New("invalid input")
	ErrToolNotFound  = errors.New("tool not found")
	ErrToolDisabled  = errors.New("tool disabled")
	ErrInvokeBlocked = errors.New("generic invoke is not exposed")
)

type ToolDefinition struct {
	Name         string   `json:"name"`
	Type         string   `json:"type"`
	Description  string   `json:"description"`
	ReadOnly     bool     `json:"readOnly"`
	Enabled      bool     `json:"enabled"`
	Capabilities []string `json:"capabilities"`
}

type Tool interface {
	Definition() ToolDefinition
	Test(ctx context.Context) error
	Invoke(ctx context.Context, operation string, input json.RawMessage) (json.RawMessage, error)
}

type Registry struct {
	mu      sync.RWMutex
	entries map[string]*entry
}

type entry struct {
	tool    Tool
	enabled bool
}

func NewRegistry(tools ...Tool) (*Registry, error) {
	registry := &Registry{entries: map[string]*entry{}}
	for _, tool := range tools {
		if err := registry.Register(tool); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func NewBuiltinRegistry() *Registry {
	registry, err := NewRegistry(BuiltinTools()...)
	if err != nil {
		panic(err)
	}
	return registry
}

func BuiltinTools() []Tool {
	return []Tool{
		NewReadOnlyTool(ToolDefinition{
			Name:         "elasticsearch",
			Type:         "log",
			Description:  "Query Elasticsearch/OpenSearch logs with time, keyword and field filters.",
			ReadOnly:     true,
			Capabilities: []string{"test", "query_logs", "time_range", "keyword", "query_string", "field_mapping"},
		}),
		NewReadOnlyTool(ToolDefinition{
			Name:         "ssh_sftp",
			Type:         "file",
			Description:  "Read allowlisted remote files through SFTP without shell execution.",
			ReadOnly:     true,
			Capabilities: []string{"test", "read_file", "tail", "path_allowlist"},
		}),
		NewReadOnlyTool(ToolDefinition{
			Name:         "kubernetes",
			Type:         "kubernetes",
			Description:  "Read Kubernetes resources, pod logs and diagnosis context inside allowed namespaces.",
			ReadOnly:     true,
			Capabilities: []string{"test", "resources", "pod_diagnose", "events", "logs", "rules"},
		}),
		NewReadOnlyTool(ToolDefinition{
			Name:         "prometheus",
			Type:         "metrics",
			Description:  "Run Prometheus instant and range queries with series and point limits.",
			ReadOnly:     true,
			Capabilities: []string{"test", "instant_query", "range_query", "series_limit", "points_limit"},
		}),
		NewReadOnlyTool(ToolDefinition{
			Name:         "alertmanager",
			Type:         "event",
			Description:  "Receive and normalize Alertmanager webhook alerts into ops events.",
			ReadOnly:     true,
			Capabilities: []string{"webhook", "parse_labels", "fingerprint", "deduplicate", "resolved"},
		}),
		NewReadOnlyTool(ToolDefinition{
			Name:         "generic_http",
			Type:         "http",
			Description:  "Read release, configuration and Git change records from configured HTTP APIs.",
			ReadOnly:     true,
			Capabilities: []string{"get", "recent_release", "config_change", "git_change", "time_range"},
		}),
	}
}

func (r *Registry) Register(tool Tool) error {
	if tool == nil {
		return ErrInvalidInput
	}
	definition := tool.Definition()
	name := normalizeName(definition.Name)
	if name == "" {
		return ErrInvalidInput
	}
	definition.Name = name
	if !definition.ReadOnly {
		return ErrInvalidInput
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.entries[name]; exists {
		return ErrInvalidInput
	}
	r.entries[name] = &entry{tool: tool, enabled: true}
	return nil
}

func (r *Registry) List() []ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	definitions := make([]ToolDefinition, 0, len(r.entries))
	for name, entry := range r.entries {
		definition := entry.tool.Definition()
		definition.Name = name
		definition.Enabled = entry.enabled
		definitions = append(definitions, definition)
	}
	sort.Slice(definitions, func(i, j int) bool {
		return definitions[i].Name < definitions[j].Name
	})
	return definitions
}

func (r *Registry) Get(name string) (ToolDefinition, error) {
	entry, normalized, err := r.lookup(name)
	if err != nil {
		return ToolDefinition{}, err
	}
	definition := entry.tool.Definition()
	definition.Name = normalized
	definition.Enabled = entry.enabled
	return definition, nil
}

func (r *Registry) Enable(name string) (ToolDefinition, error) {
	return r.setEnabled(name, true)
}

func (r *Registry) Disable(name string) (ToolDefinition, error) {
	return r.setEnabled(name, false)
}

func (r *Registry) Test(ctx context.Context, name string) error {
	entry, _, err := r.lookup(name)
	if err != nil {
		return err
	}
	if !entry.enabled {
		return ErrToolDisabled
	}
	return entry.tool.Test(ctx)
}

func (r *Registry) Invoke(ctx context.Context, name string, operation string, input json.RawMessage) (json.RawMessage, error) {
	entry, _, err := r.lookup(name)
	if err != nil {
		return nil, err
	}
	if !entry.enabled {
		return nil, ErrToolDisabled
	}
	return entry.tool.Invoke(ctx, operation, input)
}

func (r *Registry) SkillCanExecute(requiredTools []string) error {
	for _, name := range requiredTools {
		entry, _, err := r.lookup(name)
		if err != nil {
			return err
		}
		if !entry.enabled {
			return ErrToolDisabled
		}
	}
	return nil
}

func (r *Registry) setEnabled(name string, enabled bool) (ToolDefinition, error) {
	normalized := normalizeName(name)
	if normalized == "" {
		return ToolDefinition{}, ErrInvalidInput
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.entries[normalized]
	if !ok {
		return ToolDefinition{}, ErrToolNotFound
	}
	entry.enabled = enabled
	definition := entry.tool.Definition()
	definition.Name = normalized
	definition.Enabled = entry.enabled
	return definition, nil
}

func (r *Registry) lookup(name string) (*entry, string, error) {
	normalized := normalizeName(name)
	if normalized == "" {
		return nil, "", ErrInvalidInput
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, ok := r.entries[normalized]
	if !ok {
		return nil, "", ErrToolNotFound
	}
	return entry, normalized, nil
}

func normalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

type ReadOnlyTool struct {
	definition ToolDefinition
}

func NewReadOnlyTool(definition ToolDefinition) *ReadOnlyTool {
	definition.Name = normalizeName(definition.Name)
	definition.ReadOnly = true
	return &ReadOnlyTool{definition: definition}
}

func (t *ReadOnlyTool) Definition() ToolDefinition {
	return t.definition
}

func (t *ReadOnlyTool) Test(context.Context) error {
	return nil
}

func (t *ReadOnlyTool) Invoke(context.Context, string, json.RawMessage) (json.RawMessage, error) {
	return nil, ErrInvokeBlocked
}
