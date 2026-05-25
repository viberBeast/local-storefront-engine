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

// Product represents a sellable item in the catalogue.
type Product struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Price       int    `json:"price"`
	Stock       int    `json:"stock"`
	ImageURL    string `json:"image_url"`
	Category    string `json:"category"`   // GARMENTS, FOOTWEAR, ACCESSORIES
	SortOrder   int    `json:"sort_order"` // Manual feed priority
}

// Order represents a completed customer purchase.
type Order struct {
	ID        string      `json:"id"`
	CreatedAt time.Time   `json:"created_at"`
	Items     []OrderItem `json:"items"`
	Total     int         `json:"total"`
	Status    string      `json:"status"` // Pending, Shipped, Cancelled
}

// OrderItem maps individual product quantities within an order.
type OrderItem struct {
	ProductID   string `json:"product_id"`
	ProductName string `json:"product_name"`
	Quantity    int    `json:"quantity"`
	Price       int    `json:"price"`
}

// DB wraps the standard *sql.DB connection pool.
type DB struct {
	Ctx context.Context
	*sql.DB
}

// Open initializes the SQLite database engine file and builds default schemas.
func Open(ctx context.Context, dsn string) (*DB, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite pool: %w", err)
	}

	// Optimize transactional execution overhead
	db.SetMaxOpenConns(1)

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping sqlite pool: %w", err)
	}

	instance := &DB{Ctx: ctx, DB: db}
	if err := instance.createSchema(); err != nil {
		return nil, fmt.Errorf("failed to build internal schemas: %w", err)
	}

	return instance, nil
}

func (db *DB) createSchema() error {
	const ddl = `
	CREATE TABLE IF NOT EXISTS products (
		id          TEXT PRIMARY KEY,
		name        TEXT    NOT NULL,
		description TEXT    NOT NULL DEFAULT '',
		price       INTEGER NOT NULL CHECK (price >= 0),
		stock       INTEGER NOT NULL CHECK (stock >= 0),
		image_url   TEXT    NOT NULL DEFAULT '',
		category    TEXT    NOT NULL DEFAULT 'GARMENTS',
		sort_order  INTEGER NOT NULL DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS orders (
		id          TEXT PRIMARY KEY,
		created_at  TEXT NOT NULL,
		total       INTEGER NOT NULL,
		status      TEXT NOT NULL DEFAULT 'Pending'
	);

	CREATE TABLE IF NOT EXISTS order_items (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		order_id    TEXT NOT NULL,
		product_id  TEXT NOT NULL,
		quantity    INTEGER NOT NULL CHECK (quantity > 0),
		price       INTEGER NOT NULL CHECK (price >= 0),
		FOREIGN KEY(order_id) REFERENCES orders(id) ON DELETE CASCADE
	);
	`
	_, err := db.ExecContext(db.Ctx, ddl)
	return err
}

// GetAllProducts fetches store rows ordered intelligently by stock state and curation sequences.
func (db *DB) GetAllProducts() ([]Product, error) {
	const query = `
		SELECT id, name, description, price, stock, image_url, category, sort_order 
		FROM products 
		ORDER BY CASE WHEN stock > 0 THEN 0 ELSE 1 END ASC, sort_order ASC, id DESC
	`
	rows, err := db.QueryContext(db.Ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var products []Product
	for rows.Next() {
		var p Product
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.Price, &p.Stock, &p.ImageURL, &p.Category, &p.SortOrder); err != nil {
			return nil, err
		}
		products = append(products, p)
	}
	return products, nil
}

// GetProductByID extracts a single targeted identifier match from SQLite.
func (db *DB) GetProductByID(id string) (Product, error) {
	const query = `SELECT id, name, description, price, stock, image_url, category, sort_order FROM products WHERE id = ?`
	var p Product
	err := db.QueryRowContext(db.Ctx, query, id).Scan(&p.ID, &p.Name, &p.Description, &p.Price, &p.Stock, &p.ImageURL, &p.Category, &p.SortOrder)
	if err == sql.ErrNoRows {
		return p, fmt.Errorf("item mapping index target not located: %s", id)
	}
	return p, err
}

// CreateProduct writes a new product item node into the core timeline sequence.
func (db *DB) CreateProduct(p Product) error {
	const query = `INSERT INTO products (id, name, description, price, stock, image_url, category, sort_order) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := db.ExecContext(db.Ctx, query, p.ID, p.Name, p.Description, p.Price, p.Stock, p.ImageURL, p.Category, p.SortOrder)
	return err
}

// UpdateProduct mutates transactional operational parameters on an existing entry item.
func (db *DB) UpdateProduct(p Product) error {
	const query = `UPDATE products SET name = ?, description = ?, price = ?, stock = ?, image_url = ?, category = ?, sort_order = ? WHERE id = ?`
	_, err := db.ExecContext(db.Ctx, query, p.Name, p.Description, p.Price, p.Stock, p.ImageURL, p.Category, p.SortOrder, p.ID)
	return err
}

// DeleteProduct purges an item index pointer completely from the catalog store.
func (db *DB) DeleteProduct(id string) error {
	const query = `DELETE FROM products WHERE id = ?`
	_, err := db.ExecContext(db.Ctx, query, id)
	return err
}

// CreateOrder processes customer acquisition bundles securely into local records.
func (db *DB) CreateOrder(o Order) error {
	tx, err := db.BeginTx(db.Ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	const insertOrder = `INSERT INTO orders (id, created_at, total, status) VALUES (?, ?, ?, ?)`
	_, err = tx.ExecContext(db.Ctx, insertOrder, o.ID, o.CreatedAt.Format(time.RFC3339), o.Total, o.Status)
	if err != nil {
		return err
	}

	const insertItem = `INSERT INTO order_items (order_id, product_id, quantity, price) VALUES (?, ?, ?, ?)`
	const updateStock = `UPDATE products SET stock = stock - ? WHERE id = ? AND stock >= ?`

	for _, item := range o.Items {
		_, err = tx.ExecContext(db.Ctx, insertItem, o.ID, item.ProductID, item.Quantity, item.Price)
		if err != nil {
			return err
		}

		res, err := tx.ExecContext(db.Ctx, updateStock, item.Quantity, item.ProductID, item.Quantity)
		if err != nil {
			return err
		}
		rows, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if rows == 0 {
			return fmt.Errorf("insufficient items available inside pipeline for ID: %s", item.ProductID)
		}
	}
	return tx.Commit()
}

// GetAllOrders maps processing client records back into the main view monitor panel.
func (db *DB) GetAllOrders() ([]Order, error) {
	const query = `SELECT id, created_at, total, status FROM orders ORDER BY created_at DESC`
	rows, err := db.QueryContext(db.Ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orders []Order
	for rows.Next() {
		var o Order
		var timeStr string
		if err := rows.Scan(&o.ID, &timeStr, &o.Total, &o.Status); err != nil {
			return nil, err
		}
		o.CreatedAt, _ = time.Parse(time.RFC3339, timeStr)

		itemsQuery := `
			SELECT oi.product_id, p.name, oi.quantity, oi.price 
			FROM order_items oi
			LEFT JOIN products p ON oi.product_id = p.id
			WHERE oi.order_id = ?`
		itemRows, err := db.QueryContext(db.Ctx, itemsQuery, o.ID)
		if err != nil {
			return nil, err
		}

		for itemRows.Next() {
			var oi OrderItem
			var nameNull sql.NullString
			if err := itemRows.Scan(&oi.ProductID, &nameNull, &oi.Quantity, &oi.Price); err != nil {
				itemRows.Close()
				return nil, err
			}
			oi.ProductName = "Archived Item"
			if nameNull.Valid {
				oi.ProductName = nameNull.String
			}
			o.Items = append(o.Items, oi)
		}
		itemRows.Close()
		orders = append(orders, o)
	}
	return orders, nil
}

// UpdateOrderStatus mutates logistical progression state tags inside the terminal tracking system.
func (db *DB) UpdateOrderStatus(id string, status string) error {
	const query = `UPDATE orders SET status = ? WHERE id = ?`
	_, err := db.ExecContext(db.Ctx, query, status, id)
	return err
}
