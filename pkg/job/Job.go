package job

import (
	"strings"

	"github.com/cfjello/go-store/pkg/store"
	"github.com/cfjello/go-store/pkg/util"
	"github.com/cfjello/hierarkey-go/pkg/hierarkey"
	"github.com/dominikbraun/graph"
	_ "github.com/dominikbraun/graph"
)

// Define a struct to hold vertex information
type JobVertex struct {
	// ID    string
	JobID    string
	Key      string
	NodeName string
	Level    int
	// Add other fields as needed
}

type JobService struct {
	Graph graph.Graph[string, JobVertex]
	Store *store.Store
}

var js *JobService

func NewJobService() *JobService {
	g := graph.New(func(jv JobVertex) string { return jv.Key + "_" + jv.JobID }, graph.Directed(), graph.Acyclic())
	if js != nil {
		return js
	} else {
		js = &JobService{
			Graph: g,
			Store: store.NewStore(false),
		}
	}
	return js
}

func (js *JobService) GetJobEntry(jobId string, currKey string) (JobVertex, error) {
	return js.Graph.Vertex(jobId + "_" + currKey)
}

// Recursive function to build the job graph
// It starts from the top node and traverses through all dependencies
func (js *JobService) BuildJobGraph(currNode string, level int, keySize int, hk *hierarkey.HierarKey) (string, error) {
	jobID := ""
	// var currNode string = topNode
	var currChildKey string

	if level == 0 {
		jobID = util.Ulid()
		keySize = 4
		hk := hierarkey.NewHierarKey(1, keySize)
		currChildKey = hk.NextLeaf()
	} else {
		jobID = strings.SplitAfter(currNode, "_")[1]
	}

	vertex := JobVertex{
		JobID:    jobID,
		Key:      currChildKey,
		NodeName: currNode,
		Level:    level,
	}

	parentHash := vertex.Key + "_" + vertex.JobID
	if _, err := js.Graph.Vertex(parentHash); err != nil {
		return "", err
	}
	// Add the very first vertex
	if level == 0 {
		err := js.Graph.AddVertex(vertex)
		if err != nil {
			return "", err
		}
	}

	nodes, err := js.Store.GetDependsOn(currNode)
	if err != nil {
		return "", err
	}
	for _, node := range nodes {
		currChildKey := hk.NextLeaf()
		childVertex := JobVertex{
			JobID:    jobID,
			Key:      currChildKey,
			NodeName: node,
			Level:    level + 1,
		}
		childHash := childVertex.Key + "_" + childVertex.JobID
		if _, err := js.Graph.Vertex(childHash); err != nil {
			err := js.Graph.AddVertex(childVertex)
			if err != nil {
				return "", err
			}
		}
		if _, err := js.Graph.Edge(parentHash, childHash); err != nil {
			if err = js.Graph.AddEdge(parentHash, childHash); err != nil {
				return "", err
			}
		}
		// Recursively add the job graph for the dependent node
		if _, err = js.BuildJobGraph(childHash, level+1, keySize, hk); err != nil {
			return "", err
		}
	}
	return jobID, nil
}

/*
func (js *JobService) GetAdjacencyMap() (map[string]map[string]graph.Edge[string], error) {
	return js.Graph.AdjacencyMap()
}
*/
