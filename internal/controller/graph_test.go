package controller

import (
	"reflect"
	"testing"

	"runp/internal/config"
)

func testGraph(t *testing.T) *graph {
	t.Helper()
	g, err := newGraph([]config.Process{
		{Name: "db"},
		{Name: "api", DependsOn: []string{"db"}},
		{Name: "web", DependsOn: []string{"api"}},
		{Name: "worker"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestGraphLevels(t *testing.T) {
	g := testGraph(t)
	want := [][]string{{"db", "worker"}, {"api"}, {"web"}}
	if got := g.levels(); !reflect.DeepEqual(got, want) {
		t.Fatalf("levels = %#v", got)
	}
	want = [][]string{{"web"}, {"api"}, {"db", "worker"}}
	if got := g.reverseLevels(); !reflect.DeepEqual(got, want) {
		t.Fatalf("reverse levels = %#v", got)
	}
}

func TestGraphTransitiveRelations(t *testing.T) {
	g := testGraph(t)
	if got, want := g.dependencies("web"), []string{"db", "api"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("dependencies = %#v", got)
	}
	if got, want := g.dependents("db"), []string{"api", "web"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("dependents = %#v", got)
	}
}

func TestGraphRejectsMissingNodeAndCycle(t *testing.T) {
	if _, err := newGraph([]config.Process{{Name: "api", DependsOn: []string{"db"}}}); err == nil {
		t.Fatal("missing dependency accepted")
	}
	if _, err := newGraph([]config.Process{{Name: "api", DependsOn: []string{"web"}}, {Name: "web", DependsOn: []string{"api"}}}); err == nil {
		t.Fatal("cycle accepted")
	}
}
