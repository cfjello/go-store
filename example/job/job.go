package main

import (
	"fmt"
	"time"

	"github.com/cfjello/go-store/pkg/store"
	"github.com/cfjello/go-store/pkg/types"
	"github.com/cfjello/go-store/pkg/util"
)

// Core interfaces from Approach 2
type Appliance interface {
	TurnOn() error
	TurnOff() error
	IsRunning() bool
	GetStatus() string
}

type WaterReservoir interface {
	AddWater(amount int) error
	GetWaterLevel() int
	GetWaterCapacity() int
}

type CoffeeContainer interface {
	AddCoffee(amount int) error
	GetCoffeeLevel() int
	GetCoffeeCapacity() int
}

type Brewer interface {
	Brew(cups int) (<-chan BrewStatus, error)
	GetBrewingSettings() BrewSettings
	SetBrewingSettings(settings BrewSettings)
}

// Data structures
type BrewStatus struct {
	Stage       string    `json:"stage"`
	Progress    int       `json:"progress"`
	Temperature int       `json:"temperature"`
	Message     string    `json:"message"`
	Timestamp   time.Time `json:"timestamp"`
}

type BrewSettings struct {
	Temperature  int           `json:"temperature"`
	BrewTime     time.Duration `json:"brew_time"`
	CoffeePerCup int           `json:"coffee_per_cup"` // grams
	WaterPerCup  int           `json:"water_per_cup"`  // ml
}

// Store model - this gets persisted to your store
type CoffeeMakerModel struct {
	ID               string       `json:"id"`
	Brand            string       `json:"brand"`
	Model            string       `json:"model"`
	Status           string       `json:"status"`
	IsOn             bool         `json:"is_on"`
	WaterLevel       int          `json:"water_level"`
	WaterCapacity    int          `json:"water_capacity"`
	CoffeeLevel      int          `json:"coffee_level"`
	CoffeeCapacity   int          `json:"coffee_capacity"`
	Temperature      int          `json:"temperature"`
	Settings         BrewSettings `json:"settings"`
	LastBrew         time.Time    `json:"last_brew"`
	BrewCount        int          `json:"brew_count"`
	MaintenanceHours int          `json:"maintenance_hours"`
	CreatedAt        time.Time    `json:"created_at"`
	UpdatedAt        time.Time    `json:"updated_at"`
}

// Main coffee maker implementation
type StoredCoffeeMaker struct {
	dataStore *store.Store
	model     *CoffeeMakerModel
}

// Factory function
func NewStoredCoffeeMaker(dataStore *store.Store, id, brand, model string) (*StoredCoffeeMaker, error) {
	coffeeMaker := &CoffeeMakerModel{
		ID:             id,
		Brand:          brand,
		Model:          model,
		Status:         "off",
		IsOn:           false,
		WaterLevel:     0,
		WaterCapacity:  1000,
		CoffeeLevel:    0,
		CoffeeCapacity: 200,
		Temperature:    20,
		Settings: BrewSettings{
			Temperature:  85,
			BrewTime:     4 * time.Second,
			CoffeePerCup: 10,
			WaterPerCup:  150,
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// Register the schema if not already registered
	if !dataStore.IsRegistered("CoffeeMaker") {
		if err := registerCoffeeMakerSchema(dataStore); err != nil {
			return nil, fmt.Errorf("failed to register schema: %w", err)
		}
	}

	// Store the initial model
	setArgs := types.SetArgs{
		Key:     id,
		Object:  coffeeMaker,
		JobID:   util.Ulid(),
		Check:   true,
		Schema:  "CoffeeMaker",
		Persist: true,
	}

	_, err := dataStore.Set(setArgs)
	if err != nil {
		return nil, fmt.Errorf("failed to store coffee maker: %w", err)
	}

	return &StoredCoffeeMaker{
		dataStore: dataStore,
		model:     coffeeMaker,
	}, nil
}

// Schema registration function
func registerCoffeeMakerSchema(dataStore *store.Store) error {
	coffeeMakerSchema := map[string]interface{}{
		"@context": "https://schema.org/",
		"@type":    "Product",
		"name":     "CoffeeMaker",
		"properties": map[string]interface{}{
			"id":                map[string]string{"type": "string", "description": "Unique identifier"},
			"brand":             map[string]string{"type": "string", "description": "Coffee maker brand"},
			"model":             map[string]string{"type": "string", "description": "Coffee maker model"},
			"status":            map[string]string{"type": "string", "description": "Current status"},
			"is_on":             map[string]string{"type": "boolean", "description": "Power state"},
			"water_level":       map[string]string{"type": "integer", "description": "Current water level in ml"},
			"water_capacity":    map[string]string{"type": "integer", "description": "Water reservoir capacity in ml"},
			"coffee_level":      map[string]string{"type": "integer", "description": "Current coffee level in grams"},
			"coffee_capacity":   map[string]string{"type": "integer", "description": "Coffee container capacity in grams"},
			"temperature":       map[string]string{"type": "integer", "description": "Current temperature in Celsius"},
			"settings":          map[string]string{"type": "object", "description": "Brewing settings"},
			"last_brew":         map[string]string{"type": "string", "description": "Last brew timestamp"},
			"brew_count":        map[string]string{"type": "integer", "description": "Total number of brews"},
			"maintenance_hours": map[string]string{"type": "integer", "description": "Hours since last maintenance"},
			"created_at":        map[string]string{"type": "string", "description": "Creation timestamp"},
			"updated_at":        map[string]string{"type": "string", "description": "Last update timestamp"},
		},
	}

	regArgs := types.RegisterArgs{
		Key:       "CoffeeMaker",
		Object:    coffeeMakerSchema,
		Init:      true,
		Check:     true,
		SchemaKey: "CoffeeMaker",
	}

	_, err := dataStore.Register(regArgs)
	return err
}
