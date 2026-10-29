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

/*
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
*/
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
	Store       *store.Store
	CoffeeMaker *CoffeeMakerModel
	JobID       string
}

// GetCurrMaker fetches the latest persisted state of the coffee maker.
// The store unmarshals the stored JSON directly into the model.
func (cm *StoredCoffeeMaker) GetCurrMaker() CoffeeMakerModel {
	var maker CoffeeMakerModel
	if _, err := cm.Store.Get("", cm.CoffeeMaker.ID, &maker); err != nil {
		return *cm.CoffeeMaker
	}
	return maker
}

// save persists the given model state and updates the in-memory copy.
func (cm *StoredCoffeeMaker) save(maker CoffeeMakerModel) error {
	maker.UpdatedAt = time.Now()
	_, err := cm.Store.Set(types.SetArgs{
		Key:       maker.ID,
		Object:    maker,
		JobId:     cm.JobID,
		Check:     "check",
		ParentKey: "CoffeeMaker",
		Publish:   true,
	})
	if err != nil {
		return err
	}
	*cm.CoffeeMaker = maker
	return nil
}

// Implement Appliance interface
func (cm *StoredCoffeeMaker) TurnOn() error {
	maker := cm.GetCurrMaker()
	maker.IsOn = true
	maker.Status = "idle"
	maker.Temperature = 20 // Room temperature

	if err := cm.save(maker); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}

	fmt.Printf("%s %s is now ON\n", maker.Brand, maker.Model)
	return nil
}

func (cm *StoredCoffeeMaker) TurnOff() error {
	maker := cm.GetCurrMaker()
	maker.IsOn = false
	maker.Status = "off"
	maker.Temperature = 20

	if err := cm.save(maker); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}

	fmt.Printf("%s %s is now OFF\n", maker.Brand, maker.Model)
	return nil
}

func (cm *StoredCoffeeMaker) IsRunning() bool {
	return cm.GetCurrMaker().IsOn
}

// AddWater fills the reservoir, rejecting amounts that exceed capacity.
func (cm *StoredCoffeeMaker) AddWater(amount int) error {
	maker := cm.GetCurrMaker()
	if maker.WaterLevel+amount > maker.WaterCapacity {
		return fmt.Errorf("cannot add %d ml: exceeds capacity of %d ml", amount, maker.WaterCapacity)
	}
	maker.WaterLevel += amount
	return cm.save(maker)
}

// AddCoffee fills the container, rejecting amounts that exceed capacity.
func (cm *StoredCoffeeMaker) AddCoffee(amount int) error {
	maker := cm.GetCurrMaker()
	if maker.CoffeeLevel+amount > maker.CoffeeCapacity {
		return fmt.Errorf("cannot add %d g: exceeds capacity of %d g", amount, maker.CoffeeCapacity)
	}
	maker.CoffeeLevel += amount
	return cm.save(maker)
}

// Brew simulates brewing the requested number of cups, consuming water and
// coffee, and reports progress on the returned channel.
func (cm *StoredCoffeeMaker) Brew(cups int) (<-chan BrewStatus, error) {
	maker := cm.GetCurrMaker()
	if !maker.IsOn {
		return nil, fmt.Errorf("coffee maker is off")
	}
	waterNeeded := maker.Settings.WaterPerCup * cups
	coffeeNeeded := maker.Settings.CoffeePerCup * cups
	if maker.WaterLevel < waterNeeded {
		return nil, fmt.Errorf("not enough water: need %d ml, have %d ml", waterNeeded, maker.WaterLevel)
	}
	if maker.CoffeeLevel < coffeeNeeded {
		return nil, fmt.Errorf("not enough coffee: need %d g, have %d g", coffeeNeeded, maker.CoffeeLevel)
	}

	statusChan := make(chan BrewStatus)

	go func() {
		defer close(statusChan)

		maker.Status = "brewing"
		stages := []struct {
			name     string
			progress int
		}{
			{"Heating water", 25},
			{"Grinding beans", 50},
			{"Brewing", 90},
			{"Finishing", 100},
		}
		for _, stage := range stages {
			statusChan <- BrewStatus{
				Stage:       stage.name,
				Progress:    stage.progress,
				Temperature: maker.Settings.Temperature,
				Message:     stage.name,
				Timestamp:   time.Now(),
			}
			time.Sleep(100 * time.Millisecond)
		}

		maker.Status = "idle"
		maker.WaterLevel -= waterNeeded
		maker.CoffeeLevel -= coffeeNeeded
		maker.BrewCount += cups
		maker.MaintenanceHours += cups
		maker.LastBrew = time.Now()
		if err := cm.save(maker); err != nil {
			statusChan <- BrewStatus{Stage: "Error", Message: err.Error(), Timestamp: time.Now()}
		}
	}()

	return statusChan, nil
}

// NeedsMaintenance reports whether the machine is due for maintenance.
func (cm *StoredCoffeeMaker) NeedsMaintenance() bool {
	return cm.GetCurrMaker().MaintenanceHours >= 100
}

// PerformMaintenance resets the maintenance counter.
func (cm *StoredCoffeeMaker) PerformMaintenance() {
	maker := cm.GetCurrMaker()
	maker.MaintenanceHours = 0
	if err := cm.save(maker); err != nil {
		fmt.Println("failed to save maintenance state:", err)
		return
	}
	fmt.Println("Maintenance performed.")
}

func (cm *StoredCoffeeMaker) GetStatus() string {
	maker := cm.GetCurrMaker()
	if !maker.IsOn {
		return "OFF"
	}
	return fmt.Sprintf("ON - Status: %s, Water: %d/%d ml, Coffee: %d/%d g, Temp: %d°C",
		maker.Status, maker.WaterLevel, maker.WaterCapacity,
		maker.CoffeeLevel, maker.CoffeeCapacity, maker.Temperature)
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

	// Register the maker under its own key (Init stores the initial model)
	jobID := util.Ulid()
	if !dataStore.IsRegistered(id) {
		regArgs := types.RegisterArgs{
			Key:       id,
			Object:    coffeeMaker,
			Init:      true,
			Check:     true,
			ParentKey: "CoffeeMaker",
		}
		if _, err := dataStore.Register(regArgs); err != nil {
			return nil, fmt.Errorf("failed to store coffee maker: %w", err)
		}
	} else {
		setArgs := types.SetArgs{
			Key:       id,
			Object:    coffeeMaker,
			JobId:     jobID,
			Check:     "check",
			ParentKey: "CoffeeMaker",
			Publish:   true,
		}
		if _, err := dataStore.Set(setArgs); err != nil {
			return nil, fmt.Errorf("failed to store coffee maker: %w", err)
		}
	}

	return &StoredCoffeeMaker{
		Store:       dataStore,
		CoffeeMaker: coffeeMaker,
		JobID:       jobID,
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
		ParentKey: "CoffeeMaker",
	}

	_, err := dataStore.Register(regArgs)
	return err
}

func DemonstrateCoffeeMaker(dataStore *store.Store, maker *StoredCoffeeMaker) error {
	// Create a new coffee maker
	// maker, err := NewStoredCoffeeMaker(dataStore, "cm-001", "Breville", "Barista Express")
	// if err != nil {
	// 	return fmt.Errorf("failed to create coffee maker: %w", err)
	// }

	fmt.Println("=== Coffee Maker Demo ===")
	fmt.Println("Initial status:", maker.GetStatus())

	// Turn on the coffee maker
	if err := maker.TurnOn(); err != nil {
		return err
	}

	// Add water and coffee
	if err := maker.AddWater(500); err != nil {
		return err
	}

	if err := maker.AddCoffee(100); err != nil {
		return err
	}

	fmt.Println("After setup:", maker.GetStatus())

	// Brew coffee
	statusChan, err := maker.Brew(2)
	if err != nil {
		return err
	}

	// Monitor brewing progress
	for status := range statusChan {
		fmt.Printf("[%s] %s - %s (Progress: %d%%, Temp: %d°C)\n",
			status.Timestamp.Format("15:04:05"),
			status.Stage,
			status.Message,
			status.Progress,
			status.Temperature)
	}

	fmt.Println("Final status:", maker.GetStatus())

	// Check if maintenance is needed
	if maker.NeedsMaintenance() {
		fmt.Println("⚠️  Maintenance required!")
		maker.PerformMaintenance()
	}

	return nil
}

func main() {
	storeId := util.Ulid()
	dataStore := store.NewStoreService(false)

	maker, err := NewStoredCoffeeMaker(dataStore, storeId, "BrandX", "ModelY")
	if err != nil {
		fmt.Println("Error initializing Coffee Maker:", err)
		return
	} else {
		fmt.Println("Coffee Maker initialized successfully with: " + maker.CoffeeMaker.ID)
	}

	err = DemonstrateCoffeeMaker(dataStore, maker)
	if err != nil {
		fmt.Println("Error demonstrating Coffee Maker:", err)
		return
	}
	fmt.Println("Coffee Maker initialized with ID:", maker.CoffeeMaker.ID)
}
