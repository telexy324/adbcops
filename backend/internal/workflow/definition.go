package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"aiops-platform/backend/internal/agentruntime"
	"aiops-platform/backend/internal/skillframework"
)

const (
	NodeTypeStart     = "start"
	NodeTypeEnd       = "end"
	NodeTypeAgent     = "agent"
	NodeTypeSkill     = "skill"
	NodeTypeCondition = "condition"
	NodeTypeMerge     = "merge"
)

var ErrInvalidDefinition = errors.New("invalid workflow definition")

type Definition struct {
	Name        string          `json:"name"`
	Version     string          `json:"version"`
	Description string          `json:"description,omitempty"`
	Nodes       []Node          `json:"nodes"`
	Edges       []Edge          `json:"edges"`
	Variables   json.RawMessage `json:"variables,omitempty"`
}

type Node struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	Name      string          `json:"name,omitempty"`
	AgentName string          `json:"agentName,omitempty"`
	SkillName string          `json:"skillName,omitempty"`
	Config    json.RawMessage `json:"config,omitempty"`
}

type Edge struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Condition string `json:"condition,omitempty"`
}

type ValidationResult struct {
	Valid    bool     `json:"valid"`
	Errors   []string `json:"errors"`
	Warnings []string `json:"warnings"`
}

type AgentCatalog interface {
	Get(name string) (agentruntime.AgentDefinition, error)
}

type SkillCatalog interface {
	Get(name string) (skillframework.SkillDefinition, error)
}

func Validate(definition Definition, agents AgentCatalog, skills SkillCatalog) ValidationResult {
	validator := graphValidator{
		definition: definition,
		agents:     agents,
		skills:     skills,
		nodeByID:   map[string]Node{},
		indegree:   map[string]int{},
		outdegree:  map[string]int{},
		outgoing:   map[string][]string{},
	}
	validator.validate()
	return ValidationResult{
		Valid:    len(validator.errors) == 0,
		Errors:   validator.errors,
		Warnings: validator.warnings,
	}
}

func MustValidate(definition Definition, agents AgentCatalog, skills SkillCatalog) error {
	result := Validate(definition, agents, skills)
	if result.Valid {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrInvalidDefinition, strings.Join(result.Errors, "; "))
}

type graphValidator struct {
	definition Definition
	agents     AgentCatalog
	skills     SkillCatalog
	nodeByID   map[string]Node
	indegree   map[string]int
	outdegree  map[string]int
	outgoing   map[string][]string
	errors     []string
	warnings   []string
}

func (v *graphValidator) validate() {
	if strings.TrimSpace(v.definition.Name) == "" {
		v.addError("name is required")
	}
	if strings.TrimSpace(v.definition.Version) == "" {
		v.addError("version is required")
	}
	if len(v.definition.Nodes) == 0 {
		v.addError("at least one node is required")
		return
	}
	v.validateNodes()
	v.validateEdges()
	v.validateStartEnd()
	if len(v.errors) > 0 {
		return
	}
	v.validateDAG()
	v.validateReachability()
}

func (v *graphValidator) validateNodes() {
	startCount := 0
	endCount := 0
	for _, node := range v.definition.Nodes {
		node.ID = strings.TrimSpace(node.ID)
		node.Type = strings.ToLower(strings.TrimSpace(node.Type))
		if node.ID == "" {
			v.addError("node id is required")
			continue
		}
		if _, exists := v.nodeByID[node.ID]; exists {
			v.addError("duplicate node id: " + node.ID)
			continue
		}
		if !allowedNodeType(node.Type) {
			v.addError("unsupported node type for " + node.ID + ": " + node.Type)
		}
		switch node.Type {
		case NodeTypeStart:
			startCount++
		case NodeTypeEnd:
			endCount++
		case NodeTypeAgent:
			if strings.TrimSpace(node.AgentName) == "" {
				v.addError("agent node " + node.ID + " requires agentName")
			} else if v.agents != nil {
				if _, err := v.agents.Get(node.AgentName); err != nil {
					v.addError("agent node " + node.ID + " references unknown agent: " + node.AgentName)
				}
			}
		case NodeTypeSkill:
			if strings.TrimSpace(node.SkillName) == "" {
				v.addError("skill node " + node.ID + " requires skillName")
			} else if v.skills != nil {
				if _, err := v.skills.Get(node.SkillName); err != nil {
					v.addError("skill node " + node.ID + " references unknown skill: " + node.SkillName)
				}
			}
		}
		v.nodeByID[node.ID] = node
		v.indegree[node.ID] = 0
		v.outdegree[node.ID] = 0
	}
	if startCount != 1 {
		v.addError(fmt.Sprintf("workflow must have exactly one start node, got %d", startCount))
	}
	if endCount != 1 {
		v.addError(fmt.Sprintf("workflow must have exactly one end node, got %d", endCount))
	}
}

func (v *graphValidator) validateEdges() {
	seen := map[string]struct{}{}
	for _, edge := range v.definition.Edges {
		from := strings.TrimSpace(edge.From)
		to := strings.TrimSpace(edge.To)
		if from == "" || to == "" {
			v.addError("edge from/to is required")
			continue
		}
		if from == to {
			v.addError("self edge is not allowed: " + from)
			continue
		}
		if _, ok := v.nodeByID[from]; !ok {
			v.addError("edge references unknown from node: " + from)
			continue
		}
		if _, ok := v.nodeByID[to]; !ok {
			v.addError("edge references unknown to node: " + to)
			continue
		}
		key := from + "->" + to
		if _, ok := seen[key]; ok {
			v.addWarning("duplicate edge ignored by validator: " + key)
			continue
		}
		seen[key] = struct{}{}
		v.outgoing[from] = append(v.outgoing[from], to)
		v.outdegree[from]++
		v.indegree[to]++
	}
}

func (v *graphValidator) validateStartEnd() {
	for id, node := range v.nodeByID {
		if node.Type == NodeTypeStart && v.indegree[id] != 0 {
			v.addError("start node must not have incoming edges: " + id)
		}
		if node.Type == NodeTypeEnd && v.outdegree[id] != 0 {
			v.addError("end node must not have outgoing edges: " + id)
		}
		if node.Type != NodeTypeStart && v.indegree[id] == 0 {
			v.addError("non-start node has no incoming edges: " + id)
		}
		if node.Type != NodeTypeEnd && v.outdegree[id] == 0 {
			v.addError("non-end node has no outgoing edges: " + id)
		}
	}
}

func (v *graphValidator) validateDAG() {
	indegree := map[string]int{}
	queue := []string{}
	for id, degree := range v.indegree {
		indegree[id] = degree
		if degree == 0 {
			queue = append(queue, id)
		}
	}
	sort.Strings(queue)
	visited := 0
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		visited++
		for _, next := range v.outgoing[id] {
			indegree[next]--
			if indegree[next] == 0 {
				queue = append(queue, next)
				sort.Strings(queue)
			}
		}
	}
	if visited != len(v.nodeByID) {
		v.addError("workflow graph must be a DAG")
	}
}

func (v *graphValidator) validateReachability() {
	startID := ""
	endID := ""
	for id, node := range v.nodeByID {
		if node.Type == NodeTypeStart {
			startID = id
		}
		if node.Type == NodeTypeEnd {
			endID = id
		}
	}
	if startID == "" || endID == "" {
		return
	}
	fromStart := v.walk(startID, v.outgoing)
	reverse := map[string][]string{}
	for from, tos := range v.outgoing {
		for _, to := range tos {
			reverse[to] = append(reverse[to], from)
		}
	}
	toEnd := v.walk(endID, reverse)
	for id := range v.nodeByID {
		if !fromStart[id] {
			v.addError("node is not reachable from start: " + id)
		}
		if !toEnd[id] {
			v.addError("node cannot reach end: " + id)
		}
	}
}

func (v *graphValidator) walk(start string, edges map[string][]string) map[string]bool {
	seen := map[string]bool{start: true}
	queue := []string{start}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		for _, next := range edges[id] {
			if seen[next] {
				continue
			}
			seen[next] = true
			queue = append(queue, next)
		}
	}
	return seen
}

func (v *graphValidator) addError(message string) {
	v.errors = append(v.errors, message)
}

func (v *graphValidator) addWarning(message string) {
	v.warnings = append(v.warnings, message)
}

func allowedNodeType(nodeType string) bool {
	switch nodeType {
	case NodeTypeStart, NodeTypeEnd, NodeTypeAgent, NodeTypeSkill, NodeTypeCondition, NodeTypeMerge:
		return true
	default:
		return false
	}
}
