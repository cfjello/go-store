package main

import "fmt"

type WaterReservoir struct {
	WaterLevel    int
	WaterCapacity int
}

func (wr *WaterReservoir) AddWater(amount int) error {
	if (wr.WaterLevel + amount) > wr.WaterCapacity {
		return fmt.Errorf("cannot add %d ml: exceeds capacity of %d ml", amount, wr.WaterCapacity)
	}
	wr.WaterLevel += amount
	return nil
}
func (wr *WaterReservoir) GetWaterLevel() int {
	return wr.WaterLevel
}
func (wr *WaterReservoir) GetWaterCapacity() int {
	return wr.WaterCapacity
}
func (wr *WaterReservoir) NeedsRefill() bool {
	return wr.WaterLevel < (wr.WaterCapacity / 10) // less than 10% capacity
}
func (wr *WaterReservoir) Refill() {
	wr.WaterLevel = wr.WaterCapacity
}
func (wr *WaterReservoir) Consume(amount int) error {
	if amount > wr.WaterLevel {
		return fmt.Errorf("not enough water: requested %d ml, available %d ml", amount, wr.WaterLevel)
	}
	wr.WaterLevel -= amount
	return nil
}

func NewWaterReservoir(capacity int) *WaterReservoir {
	return &WaterReservoir{
		WaterLevel:    0,
		WaterCapacity: capacity,
	}
}
