package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/cfjello/go-store/pkg/store"
	"github.com/cfjello/go-store/pkg/types"
	"github.com/cfjello/go-store/pkg/util"
)

func main() {
	util.SetEnv() // Load default environment variables
	log.Println("Connecting to SQLite database...")
	reset := true // Set to true if you want to reset the database
	DataStore := store.NewStoreService(reset)

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
		graphItemMap, ok := graphItem.(map[string]any)
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
		// Remove "schema:" prefix if present
		if len(id) > 7 && id[:7] == "schema:" {
			id = id[7:]
		}

		// Create RegisterArgs and register the schema entry
		regArgs := types.RegisterArgs{
			Key:         id,
			ObjectGroup: "jsonld",
			DataObject:  graphItem,
			Init:        true,
			Check:       false,
		}
		_, err := DataStore.Register(regArgs)
		if err != nil {
			log.Printf("Error registering schema entry %s: %v", id, err)
			break
		}

		counter += 1
		// Log progress every 100 items processed
		if counter > 0 && counter%100 == 0 {
			elapsed := time.Since(startTime).Milliseconds()
			log.Printf("Processed %d items in %d ms", counter, elapsed)
			//
			// storeId := DataStore.Get()
			res := DataStore.Publish(types.SetArgs{
				Key:       regArgs.Key,
				Object:    regArgs.Object.DataObject,
				JobId:     "",
				Check:     regArgs.Check,
				ParentKey: regArgs.ParentKey,
			})
			elapsed2 := time.Since(startTime).Milliseconds()
			log.Printf("Persisted 1 item in %d ms", elapsed2-elapsed)
			if !res {
				log.Printf("Failed to persist schema entry %s", regArgs.Key)
			}
			// startTime = time.Now() // Reset timer for next batch
		}

	}
	log.Println("Successfully loaded schema.org schema")
}
