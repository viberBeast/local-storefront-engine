package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"local-storefront-engine/internal/storage"
)

type OrderItem struct {
	ProductID string `json:"product_id"`
	Quantity  int    `json:"quantity"`
}

type checkoutPayload struct {
	UserEmail string      `json:"user_email"`
	Items     []OrderItem `json:"items"`
}

type Handler struct {
	DB         any
	adminToken string
}

func NewHandler(db any, adminToken string) (*Handler, error) {
	if db == nil {
		return nil, errors.New("handlers: database connection must not be nil")
	}
	if strings.TrimSpace(adminToken) == "" {
		return nil, errors.New("handlers: adminToken must not be empty")
	}
	return &Handler{DB: db, adminToken: adminToken}, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid payload structure")
		return false
	}
	return true
}

func (h *Handler) GetProductsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	dbConn := h.DB.(*sql.DB)
	products, err := storage.GetAllProducts(r.Context(), dbConn)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to retrieve products")
		return
	}
	if products == nil {
		products = []storage.Product{}
	}

	// ── DYNAMIC IMAGE ASSET MAPPING ────────────────────────────────────
	// Computes clean asset path slugs based on lowercase product names
	for i := range products {
		if products[i].ImageURL == "" {
			// Convert "IWC BIG PILOT'S WATCH" -> "iwc-big-pilot-s-watch"
			cleanName := strings.ToLower(products[i].Name)
			cleanName = strings.ReplaceAll(cleanName, " ", "-")
			cleanName = strings.ReplaceAll(cleanName, "'", "-")
			cleanName = strings.ReplaceAll(cleanName, "\"", "-")
			cleanName = strings.ReplaceAll(cleanName, "/", "-")
			cleanName = strings.ReplaceAll(cleanName, "(", "")
			cleanName = strings.ReplaceAll(cleanName, ")", "")
			cleanName = strings.ReplaceAll(cleanName, "--", "-")
			
			// Trim trailing or double dashes from formatting sanitization
			cleanName = strings.Trim(cleanName, "-")
			
			// Map to your mounted dynamic disk folder route
			products[i].ImageURL = "/images/" + cleanName + ".png"
			
			// Hardcode standard static fallback items if IDs match
			if products[i].ID == "p1" {
				products[i].ImageURL = "/images/brutalist-steel-chair.png"
			} else if products[i].ID == "p2" {
				products[i].ImageURL = "/images/concrete-desk-lamp.png"
			}
		}
	}

	writeJSON(w, http.StatusOK, products)
}

func (h *Handler) PlaceOrderHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var payload checkoutPayload
	if !decodeJSON(w, r, &payload) {
		return
	}
	if strings.TrimSpace(payload.UserEmail) == "" || len(payload.Items) == 0 {
		writeError(w, http.StatusUnprocessableEntity, "invalid email or empty items list")
		return
	}

	var dbItems []storage.OrderItem
	var totalAmount int
	for _, item := range payload.Items {
		dbItems = append(dbItems, storage.OrderItem{
			ProductID: item.ProductID,
			Quantity:  item.Quantity,
			Price:     0,
		})
	}

	orderId := "ord_" + string(time.Now().UnixNano())
	order := storage.Order{
		ID:          orderId,
		UserEmail:   payload.UserEmail,
		TotalAmount: totalAmount,
		Status:      "pending",
		CreatedAt:   time.Now().UTC(),
	}

	dbConn := h.DB.(*sql.DB)
	err := storage.CreateOrder(r.Context(), dbConn, order, dbItems)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build order record")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": orderId, "status": "pending"})
}

func (h *Handler) GetUserOrdersHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	email := r.URL.Query().Get("q")
	if email == "" {
		cookie, err := r.Cookie("auth_token")
		if err == nil {
			email = cookie.Value
		}
	}
	if strings.TrimSpace(email) == "" {
		writeError(w, http.StatusUnauthorized, "missing email context parameter")
		return
	}

	dbConn := h.DB.(*sql.DB)
	orders, err := storage.GetOrdersByUser(r.Context(), dbConn, email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load customer records")
		return
	}
	if orders == nil {
		orders = []storage.Order{}
	}
	writeJSON(w, http.StatusOK, orders)
}

func (h *Handler) AdminUpdateInventoryHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	token := strings.TrimSpace(r.Header.Get("X-Admin-Token"))
	if token != h.adminToken {
		writeError(w, http.StatusForbidden, "unauthorized secret key context")
		return
	}
	var mutation struct {
		ProductID string `json:"product_id"`
		Stock     int    `json:"stock"`
	}
	if !decodeJSON(w, r, &mutation) {
		return
	}

	dbConn := h.DB.(*sql.DB)
	err := storage.AdminUpdateStock(r.Context(), dbConn, mutation.ProductID, mutation.Stock)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed updating inventory limits")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "inventory update applied successfully"})
}
