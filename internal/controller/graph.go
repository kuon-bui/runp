package controller

import (
	"fmt"
	"slices"

	"runp/internal/config"
)

type graph struct {
	nodes      map[string]struct{}
	requires   map[string][]string
	requiredBy map[string][]string
}

func newGraph(processes []config.Process) (*graph, error) {
	g := &graph{
		nodes:      make(map[string]struct{}, len(processes)),
		requires:   make(map[string][]string, len(processes)),
		requiredBy: make(map[string][]string, len(processes)),
	}
	for _, process := range processes {
		if _, exists := g.nodes[process.Name]; exists {
			return nil, fmt.Errorf("duplicate process %q", process.Name)
		}
		g.nodes[process.Name] = struct{}{}
		g.requires[process.Name] = append([]string(nil), process.DependsOn...)
	}
	for name, dependencies := range g.requires {
		for _, dependency := range dependencies {
			if _, exists := g.nodes[dependency]; !exists {
				return nil, fmt.Errorf("process %q depends on missing process %q", name, dependency)
			}
			g.requiredBy[dependency] = append(g.requiredBy[dependency], name)
		}
	}
	for name := range g.nodes {
		slices.Sort(g.requires[name])
		slices.Sort(g.requiredBy[name])
	}
	levels := g.levels()
	count := 0
	for _, level := range levels {
		count += len(level)
	}
	if count != len(g.nodes) {
		return nil, fmt.Errorf("process dependency cycle")
	}
	return g, nil
}

func (g *graph) levels() [][]string {
	indegree := make(map[string]int, len(g.nodes))
	current := make([]string, 0)
	for name := range g.nodes {
		indegree[name] = len(g.requires[name])
		if indegree[name] == 0 {
			current = append(current, name)
		}
	}
	slices.Sort(current)
	result := make([][]string, 0)
	for len(current) > 0 {
		result = append(result, current)
		next := make([]string, 0)
		for _, name := range current {
			for _, dependent := range g.requiredBy[name] {
				indegree[dependent]--
				if indegree[dependent] == 0 {
					next = append(next, dependent)
				}
			}
		}
		slices.Sort(next)
		current = next
	}
	return result
}

func (g *graph) reverseLevels() [][]string {
	levels := g.levels()
	slices.Reverse(levels)
	return levels
}

func (g *graph) dependencies(name string) []string {
	return g.related(name, g.requires)
}

func (g *graph) dependents(name string) []string {
	return g.related(name, g.requiredBy)
}

func (g *graph) related(name string, edges map[string][]string) []string {
	selected := make(map[string]struct{})
	var visit func(string)
	visit = func(current string) {
		for _, next := range edges[current] {
			if _, seen := selected[next]; seen {
				continue
			}
			selected[next] = struct{}{}
			visit(next)
		}
	}
	visit(name)
	result := make([]string, 0, len(selected))
	for _, level := range g.levels() {
		for _, process := range level {
			if _, ok := selected[process]; ok {
				result = append(result, process)
			}
		}
	}
	return result
}
