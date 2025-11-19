package main

import (
	"fmt"

	// "github.com/cfjello/hierarkey-go/pkg/hierarkey"

	_ "github.com/cfjello/hierarkey-go/pkg/hierarkey"
	"github.com/dominikbraun/graph"
)

func main() {
	// Create a new directed graph with string vertices
	g := graph.New(graph.StringHash, graph.Directed(), graph.Acyclic(), graph.PreventCycles())

	_ = g.AddVertex("A")
	_ = g.AddVertex("B")
	_ = g.AddVertex("C")
	_ = g.AddVertex("C1")
	_ = g.AddVertex("C2")
	_ = g.AddVertex("C3")
	_ = g.AddVertex("C4")
	_ = g.AddVertex("D")
	_ = g.AddVertex("E")
	_ = g.AddVertex("E1")
	_ = g.AddVertex("E2")
	_ = g.AddVertex("E3")
	_ = g.AddVertex("E4")
	_ = g.AddVertex("F")
	_ = g.AddVertex("G")
	_ = g.AddVertex("H")

	_ = g.AddEdge("A", "B")
	_ = g.AddEdge("A", "E")
	_ = g.AddEdge("B", "D")
	_ = g.AddEdge("B", "E1")
	_ = g.AddEdge("B", "E2")
	_ = g.AddEdge("B", "E3")
	_ = g.AddEdge("B", "C")
	_ = g.AddEdge("C", "C1")
	_ = g.AddEdge("C", "C2")
	_ = g.AddEdge("C", "C3")
	_ = g.AddEdge("C", "F")
	_ = g.AddEdge("F", "G")
	_ = g.AddEdge("G", "H")
	_ = g.AddEdge("A", "C")
	_ = g.AddEdge("B", "F")

	// level := 1
	// hieKey := hierarkey.NewHierarKey(1, 4)
	/*
		_ = graph.BFSWithDepth(g, "A", func(value string, depth int) bool {
			graphStr := fmt.Sprintf("Value: %s, Depth: %d\n", value, depth)

			leaf := ""
			if depth == level {
				leaf = hieKey.NextLeaf()
						level = depth
			} else if depth > level {
				leaf = hieKey.NextLevel()
			} else if depth < level {
				leaf = hieKey.PrevLevel()
						level = depth
			} else {
				fmt.Printf("Going down in depth is not implemented in this example: %d\n", depth)
			}

			fmt.Printf("HierarKey: %s, Leaf: %s\n", strings.Trim(graphStr, " \n"), strings.Trim(leaf, " \n"))
			return false
		})
	*/
	trans, err := graph.TransitiveReduction(g)

	if err != nil {
		fmt.Printf("Error during transitive reduction: %v\n", err)
		return
	}

	vertices, err := graph.TopologicalSort(trans)
	if err != nil {
		fmt.Printf("Error getting vertices: %v\n", err)
		return
	}

	for _, v := range vertices {
		fmt.Printf("Transitive Reduction Vertex: %s\n", v)
	}

}
