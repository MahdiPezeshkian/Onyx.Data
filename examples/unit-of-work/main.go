// Command unit-of-work is a self-contained, runnable example of integrating the
// Onyx.Data library into a Go application.
//
// It shows how to:
//   - define typed read and write repository collections,
//   - build them with the library's generic repository constructors,
//   - create a Unit of Work with repository factory functions,
//   - wrap the Unit of Work to expose convenient repository access,
//   - create, update, soft-delete, and hard-delete entities transactionally,
//   - run a raw, parameterized UPDATE through the command repository,
//   - and roll a transaction back when the application logic returns an error.
//
// The example uses an in-memory SQLite database, so it needs no configuration and
// leaves no files behind. Run it with:
//
//	cd examples/unit-of-work
//	go run .
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/MahdiPezeshkian/Onyx.Data/db/sql/implementation"
	"github.com/MahdiPezeshkian/Onyx.Data/entity"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func main() {
	ctx := context.Background()

	gormDB, err := openDatabase()
	if err != nil {
		log.Fatalf("open database: %v", err)
	}

	// The base Unit of Work is not transactional. Reads work immediately; writes
	// require Begin, as demonstrated below.
	uow := NewUnitOfWork(gormDB)

	if err := createProducts(ctx, uow); err != nil {
		log.Fatalf("create products: %v", err)
	}
	if err := listProducts(ctx, uow); err != nil {
		log.Fatalf("list products: %v", err)
	}
	if err := renameProduct(ctx, uow, 1, "Mechanical Keyboard"); err != nil {
		log.Fatalf("rename product: %v", err)
	}
	if err := applyPriceUpdate(ctx, uow, 1, 12900); err != nil {
		log.Fatalf("apply price update: %v", err)
	}
	if err := queryWithSpecification(ctx, uow); err != nil {
		log.Fatalf("query with specification: %v", err)
	}
	if err := createOrderAndPreload(ctx, uow); err != nil {
		log.Fatalf("create order and preload: %v", err)
	}
	if err := softDeleteProduct(ctx, uow, 2); err != nil {
		log.Fatalf("soft delete product: %v", err)
	}
	if err := hardDeleteProduct(ctx, uow, 2); err != nil {
		log.Fatalf("hard delete product: %v", err)
	}
	if err := rollbackOnError(ctx, uow); err != nil {
		log.Fatalf("rollback demo: %v", err)
	}

	fmt.Println("\nexample finished successfully")
}

// openDatabase opens an in-memory SQLite database and creates the schema. The DSN
// uses a shared cache so every pooled connection sees the same in-memory database;
// a plain ":memory:" DSN would give each connection its own empty database.
func openDatabase() (*gorm.DB, error) {
	gormDB, err := gorm.Open(sqlite.Open("file:onyx_example?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, err
	}
	if err := gormDB.AutoMigrate(&Product{}, &Customer{}, &Order{}); err != nil {
		return nil, err
	}
	return gormDB, nil
}

// createProducts inserts two products in a single transaction. Commit receives the
// entities that were added to the graph; because they were never loaded through a
// write repository, the change tracker reports them as new and inserts them.
func createProducts(ctx context.Context, uow *UnitOfWork) error {
	fmt.Println("== create products ==")

	// entity.Initial records the id and the current UTC creation/update timestamps.
	keyboard := &Product{BaseEntity: entity.Initial(1), Name: "Keyboard", Price: 9900}
	mouse := &Product{BaseEntity: entity.Initial(2), Name: "Mouse", Price: 4500}

	if err := uow.InTransaction(ctx, func(tx *UnitOfWork) ([]any, error) {
		return []any{keyboard, mouse}, nil
	}); err != nil {
		return err
	}

	fmt.Printf("created %q (id=%d) and %q (id=%d)\n", keyboard.Name, keyboard.Id, mouse.Name, mouse.Id)
	return nil
}

// listProducts reads through the read repositories, which run against the root
// database handle and do not participate in change tracking.
func listProducts(ctx context.Context, uow *UnitOfWork) error {
	fmt.Println("\n== list products (read repositories) ==")

	products, err := uow.Reads().ProductRepo.GetAll(ctx)
	if err != nil {
		return err
	}

	for _, p := range products {
		fmt.Printf("  id=%d name=%q price=%d deleted=%t\n", p.Id, p.Name, p.Price, p.IsDeleted)
	}
	return nil
}

// renameProduct loads a product through a write repository, mutates it, and commits
// the change. Loaded entities are snapshotted, so Commit detects that only the name
// changed and issues an UPDATE.
func renameProduct(ctx context.Context, uow *UnitOfWork, id int, name string) error {
	fmt.Printf("\n== rename product %d (write repository + change tracking) ==\n", id)

	return uow.InTransaction(ctx, func(tx *UnitOfWork) ([]any, error) {
		product, err := tx.Writes().ProductRepo.GetByID(ctx, id)
		if err != nil {
			return nil, err
		}

		product.Name = name
		fmt.Printf("  loaded and renamed product %d to %q\n", product.Id, product.Name)

		return []any{product}, nil
	})
}

// applyPriceUpdate runs a raw, parameterized UPDATE through the command repository.
// The command repository executes the statement on the Unit of Work transaction, so
// the update participates in the same commit or rollback.
func applyPriceUpdate(ctx context.Context, uow *UnitOfWork, id int, price int64) error {
	fmt.Printf("\n== raw command update on product %d ==\n", id)

	return uow.InTransaction(ctx, func(tx *UnitOfWork) ([]any, error) {
		rows, err := tx.Writes().CommandRepository.ApplyUpdateCommand(ctx, updatePriceCommand{id: id, price: price})
		if err != nil {
			return nil, err
		}
		fmt.Printf("  update affected %d row(s)\n", rows)
		return nil, nil
	})
}

// queryWithSpecification builds a filtered, sorted, and paginated query with the
// library's specification API. The default RequestConfig allow-lists the id and
// createdAt fields, so the filter below is accepted.
func queryWithSpecification(ctx context.Context, uow *UnitOfWork) error {
	fmt.Println("\n== query with specification ==")

	req := implementation.Request{
		Pagination: implementation.Pagination{Page: 1, PageSize: 10},
		Filter: implementation.FilterGroup{
			Operator: "AND",
			Filters: []implementation.Filter{
				{Field: "id", Op: ">", Value: 0},
			},
		},
	}
	spec := implementation.NewSpec[Product](req, implementation.RequestRequestConfig)

	products, err := uow.Reads().ProductRepo.GetListBySpec(ctx, spec)
	if err != nil {
		return err
	}

	fmt.Printf("  specification matched %d product(s)\n", len(products))
	return nil
}

// createOrderAndPreload creates a customer and an order that references a product,
// then reads the order back with its associations preloaded. It shows that several
// repository types can share one transaction, and that Commit accepts several change
// roots at once (here the customer and the order).
func createOrderAndPreload(ctx context.Context, uow *UnitOfWork) error {
	fmt.Println("\n== create customer + order, then preload associations ==")

	err := uow.InTransaction(ctx, func(tx *UnitOfWork) ([]any, error) {
		customer := &Customer{BaseEntity: entity.Initial(1), Name: "Ada Lovelace", Email: "ada@example.com"}
		order := &Order{
			BaseEntity: entity.Initial(1),
			CustomerID: customer.Id,
			ProductID:  1,
			Quantity:   2,
			PlacedAt:   time.Now().UTC(),
		}
		return []any{customer, order}, nil
	})
	if err != nil {
		return err
	}

	order, err := uow.Reads().OrderRepo.GetByIDWithPreload(ctx, 1, "Customer", "Product")
	if err != nil {
		return err
	}

	customerName, productName := "<nil>", "<nil>"
	if order.Customer != nil {
		customerName = order.Customer.Name
	}
	if order.Product != nil {
		productName = order.Product.Name
	}
	fmt.Printf("  order %d for customer %q, product %q, qty %d\n",
		order.Id, customerName, productName, order.Quantity)
	return nil
}

// softDeleteProduct marks a product as deleted. BaseEntity.Delete sets IsDeleted and
// DeletedAt, and Commit persists the change as an UPDATE. The row is kept, because the
// model's DeletedAt is a plain time.Time rather than gorm.DeletedAt.
func softDeleteProduct(ctx context.Context, uow *UnitOfWork, id int) error {
	fmt.Printf("\n== soft delete product %d ==\n", id)

	return uow.InTransaction(ctx, func(tx *UnitOfWork) ([]any, error) {
		product, err := tx.Writes().ProductRepo.GetByID(ctx, id)
		if err != nil {
			return nil, err
		}

		product.Delete()
		fmt.Printf("  product %d flagged is_deleted=%t\n", product.Id, product.IsDeleted)

		return []any{product}, nil
	})
}

// hardDeleteProduct removes a product physically. BaseEntity.HardDelete sets the
// HardDeleted flag, and Commit turns the tracked change into a real DELETE.
func hardDeleteProduct(ctx context.Context, uow *UnitOfWork, id int) error {
	fmt.Printf("\n== hard delete product %d ==\n", id)

	err := uow.InTransaction(ctx, func(tx *UnitOfWork) ([]any, error) {
		product, err := tx.Writes().ProductRepo.GetByID(ctx, id)
		if err != nil {
			return nil, err
		}

		product.HardDelete()
		return []any{product}, nil
	})
	if err != nil {
		return err
	}

	// Confirm the row is gone: the read repository returns gorm.ErrRecordNotFound.
	if _, err := uow.Reads().ProductRepo.GetByID(ctx, id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			fmt.Printf("  product %d is gone (ErrRecordNotFound)\n", id)
			return nil
		}
		return err
	}

	return fmt.Errorf("expected product %d to be deleted", id)
}

// rollbackOnError shows that an application error aborts the transaction. The rename
// performed inside fn is never committed, so the stored value is unchanged.
func rollbackOnError(ctx context.Context, uow *UnitOfWork) error {
	fmt.Println("\n== rollback on application error ==")

	doomed := errors.New("business rule failed")
	err := uow.InTransaction(ctx, func(tx *UnitOfWork) ([]any, error) {
		product, err := tx.Writes().ProductRepo.GetByID(ctx, 1)
		if err != nil {
			return nil, err
		}
		product.Name = "Should Not Persist"
		return nil, doomed
	})
	if !errors.Is(err, doomed) {
		return fmt.Errorf("expected rollback error %v, got %v", doomed, err)
	}

	product, err := uow.Reads().ProductRepo.GetByID(ctx, 1)
	if err != nil {
		return err
	}
	if product.Name != "Mechanical Keyboard" {
		return fmt.Errorf("rename unexpectedly persisted: name is %q", product.Name)
	}
	fmt.Printf("  transaction rolled back; product 1 is still %q\n", product.Name)

	return nil
}

// updatePriceCommand is an application-defined contract.Command that changes a single
// product price. Statements run by the command repository must be a single UPDATE
// with a WHERE clause and must use bound parameters for values.
type updatePriceCommand struct {
	id    int
	price int64
}

// RawUpdateQuery implements contract.Command.
func (c updatePriceCommand) RawUpdateQuery() (string, []any) {
	return "UPDATE products SET price = ? WHERE id = ?", []any{c.price, c.id}
}
