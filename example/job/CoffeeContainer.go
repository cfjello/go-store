package main

type CoffeeContainer struct {
	CoffeeLevel    int
	CoffeeCapacity int
}

func (c *CoffeeContainer) Fill() {
	c.CoffeeLevel = c.CoffeeCapacity
}

func (c *CoffeeContainer) IsEmpty() bool {
	return c.CoffeeLevel == 0
}

func (c *CoffeeContainer) Consume(amount int) {
	if c.CoffeeLevel >= amount {
		c.CoffeeLevel -= amount
	} else {
		c.CoffeeLevel = 0
	}
}

func (c *CoffeeContainer) GetLevel() int {
	return c.CoffeeLevel
}

func (c *CoffeeContainer) GetCapacity() int {
	return c.CoffeeCapacity
}

func NewCoffeeContainer(capacity int) *CoffeeContainer {
	return &CoffeeContainer{
		CoffeeLevel:    0,
		CoffeeCapacity: capacity,
	}
}
