package main

import (
	"time"

	"github.com/MahdiPezeshkian/Onyx.Data/entity"
)

// Product is a small fictional domain model. Embedding entity.BaseEntity[int]
// supplies the Id, timestamp, soft-delete, hard-delete, and event fields the
// library expects.
type Product struct {
	entity.BaseEntity[int]

	Name  string
	Price int64
}

// TableName pins the table name so the example is independent of GORM's
// pluralization rules.
func (Product) TableName() string { return "products" }

// Customer is the second fictional model, used to show that a single Unit of
// Work can coordinate several repository types.
type Customer struct {
	entity.BaseEntity[int]

	Name  string
	Email string
}

// TableName pins the table name.
func (Customer) TableName() string { return "customers" }

// Order links a customer to a product, demonstrating associations that can be
// preloaded by the repository.
type Order struct {
	entity.BaseEntity[int]

	CustomerID int
	ProductID  int
	Quantity   int
	PlacedAt   time.Time

	// Customer and Product are belongs-to associations, preloaded with
	// GetByIDWithPreload("Customer", "Product").
	Customer *Customer `gorm:"foreignKey:CustomerID"`
	Product  *Product  `gorm:"foreignKey:ProductID"`
}

// TableName pins the table name.
func (Order) TableName() string { return "orders" }
