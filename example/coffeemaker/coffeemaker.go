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

// Load existing coffee maker from store
func LoadStoredCoffeeMaker(dataStore *store.Store, id string) (*StoredCoffeeMaker, error) {
	obj, err := dataStore.Get("", id)
	if err != nil {
		return nil, fmt.Errorf("failed to load coffee maker %s: %w", id, err)
	}

	// Convert the stored object back to our model
	model, ok := obj.(*CoffeeMakerModel)
	if !ok {
		return nil, fmt.Errorf("invalid coffee maker data type")
	}

	return &StoredCoffeeMaker{
		dataStore: dataStore,
		model:     model,
	}, nil
}

// Helper method to save current state to store
func (cm *StoredCoffeeMaker) save() error {
	cm.model.UpdatedAt = time.Now()

	setArgs := types.SetArgs{
		Key:     cm.model.ID,
		Object:  cm.model,
		JobID:   util.Ulid(),
		Check:   true,
		Schema:  "CoffeeMaker",
		Persist: true,
	}

	_, err := cm.dataStore.Set(setArgs)
	return err
}

// Implement Appliance interface
func (cm *StoredCoffeeMaker) TurnOn() error {
	cm.model.IsOn = true
	cm.model.Status = "idle"
	cm.model.Temperature = 20 // Room temperature

	if err := cm.save(); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}

	fmt.Printf("%s %s is now ON\n", cm.model.Brand, cm.model.Model)
	return nil
}

func (cm *StoredCoffeeMaker) TurnOff() error {
	cm.model.IsOn = false
	cm.model.Status = "off"
	cm.model.Temperature = 20

	if err := cm.save(); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}

	fmt.Printf("%s %s is now OFF\n", cm.model.Brand, cm.model.Model)
	return nil
}

func (cm *StoredCoffeeMaker) IsRunning() bool {
	return cm.model.IsOn
}

func (cm *StoredCoffeeMaker) GetStatus() string {
	if !cm.model.IsOn {
		return "OFF"
	}
	return fmt.Sprintf("ON - Status: %s, Water: %d/%d ml, Coffee: %d/%d g, Temp: %d°C",
		cm.model.Status, cm.model.WaterLevel, cm.model.WaterCapacity,
		cm.model.CoffeeLevel, cm.model.CoffeeCapacity, cm.model.Temperature)
}

// Implement WaterReservoir interface
func (cm *StoredCoffeeMaker) AddWater(amount int) error {
	if cm.model.WaterLevel+amount > cm.model.WaterCapacity {
		return fmt.Errorf("water overflow: capacity is %d ml, trying to add %d ml to %d ml",
			cm.model.WaterCapacity, amount, cm.model.WaterLevel)
	}

	cm.model.WaterLevel += amount

	if err := cm.save(); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}

	fmt.Printf("Added %d ml of water. Current level: %d ml\n", amount, cm.model.WaterLevel)
	return nil
}

func (cm *StoredCoffeeMaker) GetWaterLevel() int {
	return cm.model.WaterLevel
}

func (cm *StoredCoffeeMaker) GetWaterCapacity() int {
	return cm.model.WaterCapacity
}

// Implement CoffeeContainer interface
func (cm *StoredCoffeeMaker) AddCoffee(amount int) error {
	if cm.model.CoffeeLevel+amount > cm.model.CoffeeCapacity {
		return fmt.Errorf("coffee overflow: capacity is %d g, trying to add %d g to %d g",
			cm.model.CoffeeCapacity, amount, cm.model.CoffeeLevel)
	}

	cm.model.CoffeeLevel += amount

	if err := cm.save(); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}

	fmt.Printf("Added %d g of coffee. Current level: %d g\n", amount, cm.model.CoffeeLevel)
	return nil
}

func (cm *StoredCoffeeMaker) GetCoffeeLevel() int {
	return cm.model.CoffeeLevel
}

func (cm *StoredCoffeeMaker) GetCoffeeCapacity() int {
	return cm.model.CoffeeCapacity
}

// Implement Brewer interface
func (cm *StoredCoffeeMaker) Brew(cups int) (<-chan BrewStatus, error) {
	if !cm.model.IsOn {
		return nil, fmt.Errorf("coffee maker is off")
	}

	waterNeeded := cups * cm.model.Settings.WaterPerCup
	coffeeNeeded := cups * cm.model.Settings.CoffeePerCup

	if cm.model.WaterLevel < waterNeeded {
		return nil, fmt.Errorf("not enough water: need %d ml, have %d ml",
			waterNeeded, cm.model.WaterLevel)
	}

	if cm.model.CoffeeLevel < coffeeNeeded {
		return nil, fmt.Errorf("not enough coffee: need %d g, have %d g",
			coffeeNeeded, cm.model.CoffeeLevel)
	}

	statusChan := make(chan BrewStatus, 10)

	go func() {
		defer close(statusChan)

		// Update status to brewing
		cm.model.Status = "heating"
		cm.save()

		// Heating phase
		statusChan <- BrewStatus{
			Stage:       "heating",
			Progress:    0,
			Temperature: cm.model.Temperature,
			Message:     "Heating water...",
			Timestamp:   time.Now(),
		}

		// Simulate heating
		for temp := cm.model.Temperature; temp < cm.model.Settings.Temperature; temp += 5 {
			time.Sleep(200 * time.Millisecond)
			cm.model.Temperature = temp
			cm.save()

			progress := int(float64(temp-20) / float64(cm.model.Settings.Temperature-20) * 30)
			statusChan <- BrewStatus{
				Stage:       "heating",
				Progress:    progress,
				Temperature: temp,
				Message:     fmt.Sprintf("Heating... %d°C", temp),
				Timestamp:   time.Now(),
			}
		}

		// Brewing phase
		cm.model.Status = "brewing"
		cm.save()

		statusChan <- BrewStatus{
			Stage:       "brewing",
			Progress:    30,
			Temperature: cm.model.Settings.Temperature,
			Message:     "Brewing coffee...",
			Timestamp:   time.Now(),
		}

		// Simulate brewing process
		brewSteps := 7
		for i := 0; i < brewSteps; i++ {
			time.Sleep(time.Duration(cm.model.Settings.BrewTime) / time.Duration(brewSteps))
			progress := 30 + int(float64(i+1)/float64(brewSteps)*60)

			statusChan <- BrewStatus{
				Stage:       "brewing",
				Progress:    progress,
				Temperature: cm.model.Settings.Temperature,
				Message:     fmt.Sprintf("Brewing step %d/%d", i+1, brewSteps),
				Timestamp:   time.Now(),
			}
		}

		// Finishing
		cm.model.Status = "ready"
		cm.model.WaterLevel -= waterNeeded
		cm.model.CoffeeLevel -= coffeeNeeded
		cm.model.LastBrew = time.Now()
		cm.model.BrewCount++
		cm.model.MaintenanceHours++
		cm.save()

		statusChan <- BrewStatus{
			Stage:       "complete",
			Progress:    100,
			Temperature: cm.model.Settings.Temperature,
			Message:     fmt.Sprintf("Coffee ready! %d cups brewed ☕", cups),
			Timestamp:   time.Now(),
		}

		// Return to idle after 30 seconds
		time.Sleep(30 * time.Second)
		cm.model.Status = "idle"
		cm.model.Temperature = 25 // Cooling down
		cm.save()
	}()

	return statusChan, nil
}

func (cm *StoredCoffeeMaker) GetBrewingSettings() BrewSettings {
	return cm.model.Settings
}

func (cm *StoredCoffeeMaker) SetBrewingSettings(settings BrewSettings) {
	cm.model.Settings = settings
	cm.save()
}

// Additional methods for the stored coffee maker
func (cm *StoredCoffeeMaker) GetModel() *CoffeeMakerModel {
	return cm.model
}

func (cm *StoredCoffeeMaker) NeedsMaintenance() bool {
	return cm.model.MaintenanceHours > 100 || cm.model.BrewCount > 500
}

func (cm *StoredCoffeeMaker) PerformMaintenance() error {
	cm.model.MaintenanceHours = 0
	cm.model.Status = "maintenance_completed"

	if err := cm.save(); err != nil {
		return fmt.Errorf("failed to save maintenance state: %w", err)
	}

	fmt.Printf("Maintenance completed for %s %s\n", cm.model.Brand, cm.model.Model)
	return nil
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

// Example usage function
func DemonstrateCoffeeMaker(dataStore *store.Store) error {
	// Create a new coffee maker
	maker, err := NewStoredCoffeeMaker(dataStore, "cm-001", "Breville", "Barista Express")
	if err != nil {
		return fmt.Errorf("failed to create coffee maker: %w", err)
	}

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

// Main function to run the coffee maker demo
func main() {
	util.SetEnv() // Load default environment variables

	fmt.Println("Initializing store...")
	dataStore := store.NewStore(true) // reset = true

	fmt.Println("Running coffee maker demonstration...")
	if err := DemonstrateCoffeeMaker(dataStore); err != nil {
		fmt.Printf("Demo failed: %v\n", err)
		return
	}

	fmt.Println("Demo completed successfully!")
}
