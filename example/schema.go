package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/cfjello/go-store/internal/database"
	"github.com/cfjello/go-store/pkg/store"
	"github.com/cfjello/go-store/pkg/types"
	"github.com/cfjello/go-store/pkg/util"
)

func main() {
	util.SetEnv() // Load default environment variables
	// Initialize the SQLite database
	dbService := database.New()
	var err error
	if dbService == nil {
		log.Fatalf("Failed to initialize database")
	}
	defer dbService.Close()

	DataStore := store.New(dbService)

	log.Println("Successfully connected to SQLite database")

	// Fetch the schema.org JSON-LD schema using standard http and json packages
	resp, err := http.Get("https://schema.org/version/latest/schemaorg-current-https.jsonld")
	if err != nil {
		log.Fatalf("Error fetching schema.org document: %v", err)
	}
	defer resp.Body.Close()

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("Error reading response body: %v", err)
	}

	// Unmarshal the JSON-LD document
	var jsonLdDoc map[string]interface{}

	if err := json.Unmarshal(body, &jsonLdDoc); err != nil {
		log.Fatalf("Error unmarshalling JSON-LD: %v", err)
	}

	// Extract the @graph array from the JSON-LD document
	graphArray, ok := jsonLdDoc["@graph"].([]interface{})
	if !ok {
		log.Fatalf("JSON document does not contain a @graph array")
	}

	// Use graphArray as our document for processing
	doc := graphArray

	// First, check if the document is correctly loaded and contains a graph
	if len(doc) == 0 {
		log.Println("Document is empty")
		return
	}
	// Process each entry in the @graph array

	// Initialize a timer to track processing time
	startTime := time.Now()
	counter := 0
	log.Println("Starting to load schema.org schema...")
	for _, graphItem := range doc {
		graphItemMap, ok := graphItem.(map[string]interface{})
		if !ok {
			log.Printf("Skipping non-map item in graph array")
			continue
		}

		idInterface := graphItemMap["@id"]
		id, ok := idInterface.(string)
		if !ok {
			log.Printf("Skipping item with non-string @id")
			continue
		}

		// Create RegisterArgs and register the schema entry
		setArgs := types.SetArgs{
			Key:    id,
			Object: graphItem,
		}

		_, err := DataStore.Set(setArgs)
		if err != nil {
			log.Printf("Error registering schema entry %s: %v", id, err)
			continue
		}
		counter += 1
		if counter > 0 && counter%100 == 0 {
			elapsed := time.Since(startTime).Milliseconds()
			log.Printf("Processed %d items in %d ms", counter, elapsed)
		}
	}
	log.Println("Successfully loaded schema.org schema")
}
