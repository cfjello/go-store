package main

import "time"

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

// BrewerService implements the Brewer interface
type BrewerService struct {
	settings BrewSettings
}

func NewBrewerService() *BrewerService {
	return &BrewerService{
		settings: BrewSettings{
			Temperature:  92,
			BrewTime:     4 * time.Minute,
			CoffeePerCup: 12,
			WaterPerCup:  200,
		},
	}
}

func (b *BrewerService) Brew(cups int) (<-chan BrewStatus, error) {
	statusChan := make(chan BrewStatus)

	go func() {
		defer close(statusChan)

		stages := []struct {
			name     string
			duration time.Duration
			temp     int
		}{
			{"Heating water", 30 * time.Second, 60},
			{"Grinding beans", 10 * time.Second, b.settings.Temperature},
			{"Brewing", b.settings.BrewTime, b.settings.Temperature},
			{"Finishing", 5 * time.Second, b.settings.Temperature},
		}

		for _, stage := range stages {
			steps := 10
			stepDuration := stage.duration / time.Duration(steps)

			for i := 0; i <= steps; i++ {
				progress := (i * 100) / steps
				statusChan <- BrewStatus{
					Stage:       stage.name,
					Progress:    progress,
					Temperature: stage.temp,
					Message:     stage.name,
					Timestamp:   time.Now(),
				}
				if i < steps {
					time.Sleep(stepDuration)
				}
			}
		}
	}()

	return statusChan, nil
}

func (b *BrewerService) GetBrewingSettings() BrewSettings {
	return b.settings
}

func (b *BrewerService) SetBrewingSettings(settings BrewSettings) {
	b.settings = settings
}
