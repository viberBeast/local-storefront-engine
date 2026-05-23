// Package storage provides database access functions for products and orders
// using a pure-Go SQLite driver (modernc.org/sqlite) with no CGO dependency.
package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // Pure-Go SQLite driver; registers as "sqlite"
)

// ─────────────────────────────────────────────
// Domain structs
// ─────────────────────────────────────────────

// Product represents a sellable item in the catalogue.
type Product struct {
	ID          string
	Name        string
	Description string
	Price       int // stored in smallest currency unit (e.g. paise / cents)
	Stock       int
}

// Order represents a customer purchase header.
type Order struct {
	ID          string
	UserEmail   string
	TotalAmount int // smallest currency unit
	Status      string
	CreatedAt   time.Time
}

// OrderItem is a single line inside an Order.
type OrderItem struct {
	ID        int
	OrderID   string
	ProductID string
	Quantity  int
	Price     int // unit price at time of purchase
}

// ─────────────────────────────────────────────
// Initialisation
// ─────────────────────────────────────────────

// InitDB opens (or creates) the SQLite database at dbPath, applies the
// recommended WAL pragmas for high-concurrency workloads, and creates the
// required tables if they do not yet exist.
//
// Connection-string pragmas used:
//   - journal_mode=WAL  – enables concurrent readers + one writer
//   - busy_timeout=5000 – wait up to 5 s before returning SQLITE_BUSY
//
// MaxOpenConns is set to 1 to serialise all writes through a single
// connection and avoid "database is locked" errors that would otherwise
// occur with multiple concurrent write connections to SQLite.
func InitDB(dbPath string) (*sql.DB, error) {
	dsn := fmt.Sprintf(
		"file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)",
		dbPath,
	)

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("storage.InitDB: open: %w", err)
	}

	// Serialise writes; SQLite supports only one concurrent writer.
	db.SetMaxOpenConns(1)

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("storage.InitDB: ping: %w", err)
	}

	if err := createSchema(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("storage.InitDB: schema: %w", err)
	}

	return db, nil
}

// createSchema runs the DDL statements that set up the three core tables.
// All statements use IF NOT EXISTS so they are safe to run on every start-up.
func createSchema(db *sql.DB) error {
	const ddl = `
	CREATE TABLE IF NOT EXISTS products (
		id          TEXT PRIMARY KEY,
		name        TEXT    NOT NULL,
		description TEXT    NOT NULL DEFAULT '',
		price       INTEGER NOT NULL CHECK (price >= 0),
		stock       INTEGER NOT NULL CHECK (stock >= 0)
	);

	CREATE TABLE IF NOT EXISTS orders (
		id           TEXT    PRIMARY KEY,
		user_email   TEXT    NOT NULL,
		total_amount INTEGER NOT NULL CHECK (total_amount >= 0),
		status       TEXT    NOT NULL DEFAULT 'pending',
		created_at   TEXT    NOT NULL  -- stored as RFC3339 string
	);

	CREATE TABLE IF NOT EXISTS order_items (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		order_id   TEXT    NOT NULL REFERENCES orders(id)   ON DELETE CASCADE,
		product_id TEXT    NOT NULL REFERENCES products(id) ON DELETE RESTRICT,
		quantity   INTEGER NOT NULL CHECK (quantity > 0),
		price      INTEGER NOT NULL CHECK (price >= 0)
	);

	CREATE INDEX IF NOT EXISTS idx_orders_user_email   ON orders(user_email);
	CREATE INDEX IF NOT EXISTS idx_order_items_order   ON order_items(order_id);
	CREATE INDEX IF NOT EXISTS idx_order_items_product ON order_items(product_id);
	`

	if _, err := db.Exec(ddl); err != nil {
		return err
	}
	return nil
}

// ─────────────────────────────────────────────
// Query functions
// ─────────────────────────────────────────────

// GetAllProducts returns every product row from the database, ordered by name.
// The caller supplies a context so the query can be cancelled or time-boxed.
func GetAllProducts(ctx context.Context, db *sql.DB) ([]Product, error) {
	const q = `
		SELECT id, name, description, price, stock
		FROM   products
		ORDER  BY name ASC`

	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("storage.GetAllProducts: query: %w", err)
	}
	defer rows.Close()

	var products []Product
	for rows.Next() {
		var p Product
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.Price, &p.Stock); err != nil {
			return nil, fmt.Errorf("storage.GetAllProducts: scan: %w", err)
		}
		products = append(products, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage.GetAllProducts: rows: %w", err)
	}

	return products, nil
}

// CreateOrder inserts an Order header and all its OrderItems inside a single
// ACID-compliant transaction.  The transaction is automatically rolled back if
// any step fails; otherwise it is committed before returning.
//
// Note: order.CreatedAt is serialised to RFC3339 because SQLite has no native
// DATETIME type; it is deserialised back to time.Time in GetOrdersByUser.
func CreateOrder(ctx context.Context, db *sql.DB, order Order, items []OrderItem) error {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("storage.CreateOrder: begin tx: %w", err)
	}
	// Ensure the transaction is always resolved – rolled back on any error path.
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	// 1. Insert the order header.
	const insertOrder = `
		INSERT INTO orders (id, user_email, total_amount, status, created_at)
		VALUES             (?,  ?,          ?,            ?,      ?         )`

	if _, err = tx.ExecContext(ctx, insertOrder,
		order.ID,
		order.UserEmail,
		order.TotalAmount,
		order.Status,
		order.CreatedAt.UTC().Format(time.RFC3339),
	); err != nil {
		return fmt.Errorf("storage.CreateOrder: insert order: %w", err)
	}

	// 2. Insert each line item.
	const insertItem = `
		INSERT INTO order_items (order_id, product_id, quantity, price)
		VALUES                  (?,        ?,          ?,        ?    )`

	for i, item := range items {
		if _, err = tx.ExecContext(ctx, insertItem,
			order.ID,
			item.ProductID,
			item.Quantity,
			item.Price,
		); err != nil {
			return fmt.Errorf("storage.CreateOrder: insert item[%d]: %w", i, err)
		}
	}

	// 3. Commit; any error here is surfaced to the caller.
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("storage.CreateOrder: commit: %w", err)
	}
	return nil
}

// GetOrdersByUser returns all orders placed by the given email address,
// sorted newest-first.  created_at is parsed from its RFC3339 storage form
// back into a time.Time value.
func GetOrdersByUser(ctx context.Context, db *sql.DB, email string) ([]Order, error) {
	const q = `
		SELECT id, user_email, total_amount, status, created_at
		FROM   orders
		WHERE  user_email = ?
		ORDER  BY created_at DESC`

	rows, err := db.QueryContext(ctx, q, email)
	if err != nil {
		return nil, fmt.Errorf("storage.GetOrdersByUser: query: %w", err)
	}
	defer rows.Close()

	var orders []Order
	for rows.Next() {
		var o Order
		var createdAtStr string

		if err := rows.Scan(
			&o.ID, &o.UserEmail, &o.TotalAmount, &o.Status, &createdAtStr,
		); err != nil {
			return nil, fmt.Errorf("storage.GetOrdersByUser: scan: %w", err)
		}

		// Parse the RFC3339 string back to time.Time.
		o.CreatedAt, err = time.Parse(time.RFC3339, createdAtStr)
		if err != nil {
			return nil, fmt.Errorf("storage.GetOrdersByUser: parse time %q: %w", createdAtStr, err)
		}

		orders = append(orders, o)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage.GetOrdersByUser: rows: %w", err)
	}

	return orders, nil
}

// AdminUpdateStock sets the stock level for a specific product to newStock.
// newStock must be ≥ 0; the function returns an error for negative values
// before touching the database.
func AdminUpdateStock(ctx context.Context, db *sql.DB, productID string, newStock int) error {
	if newStock < 0 {
		return fmt.Errorf("storage.AdminUpdateStock: newStock must be >= 0, got %d", newStock)
	}

	const q = `UPDATE products SET stock = ? WHERE id = ?`

	result, err := db.ExecContext(ctx, q, newStock, productID)
	if err != nil {
		return fmt.Errorf("storage.AdminUpdateStock: exec: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("storage.AdminUpdateStock: rows affected: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("storage.AdminUpdateStock: product %q not found", productID)
	}

	return nil
}
