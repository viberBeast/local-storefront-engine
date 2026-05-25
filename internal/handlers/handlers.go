package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"local-storefront-engine/internal/storage"
)

type ClientOrderItem struct {
	ProductID string `json:"product_id"`
	Quantity  int    `json:"quantity"`
}

type checkoutPayload struct {
	UserEmail string            `json:"user_email"`
	Items     []ClientOrderItem `json:"items"`
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

	dbWrapper, ok := h.DB.(*storage.DB)
	if !ok {
		writeError(w, http.StatusInternalServerError, "invalid database connection driver type")
		return
	}

	products, err := dbWrapper.GetAllProducts()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to retrieve products")
		return
	}
	if products == nil {
		products = []storage.Product{}
	}

	for i := range products {
		if products[i].ImageURL == "" {
			cleanName := strings.ToLower(products[i].Name)
			cleanName = strings.ReplaceAll(cleanName, " ", "-")
			cleanName = strings.ReplaceAll(cleanName, "'", "-")
			cleanName = strings.ReplaceAll(cleanName, "\"", "-")
			cleanName = strings.ReplaceAll(cleanName, "/", "-")
			cleanName = strings.ReplaceAll(cleanName, "(", "")
			cleanName = strings.ReplaceAll(cleanName, ")", "")
			cleanName = strings.ReplaceAll(cleanName, "--", "-")
			cleanName = strings.Trim(cleanName, "-")
			
			products[i].ImageURL = "/images/" + cleanName + ".png"
			
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
	if len(payload.Items) == 0 {
		writeError(w, http.StatusUnprocessableEntity, "empty items list context validation")
		return
	}

	dbWrapper, ok := h.DB.(*storage.DB)
	if !ok {
		writeError(w, http.StatusInternalServerError, "invalid database connection driver type")
		return
	}

	var orderItems []storage.OrderItem
	var totalAmount int

	for _, item := range payload.Items {
		prod, err := dbWrapper.GetProductByID(item.ProductID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "product validation failed: "+item.ProductID)
			return
		}

		if prod.Stock < item.Quantity {
			writeError(w, http.StatusBadRequest, "insufficient inventory context for product: "+item.ProductID)
			return
		}

		totalAmount += prod.Price * item.Quantity

		orderItems = append(orderItems, storage.OrderItem{
			ProductID: item.ProductID,
			Quantity:  item.Quantity,
			Price:     prod.Price,
		})
	}

	orderId := "ord_" + string(time.Now().UnixNano())
	order := storage.Order{
		ID:        orderId,
		CreatedAt: time.Now().UTC(),
		Items:     orderItems,
		Total:     totalAmount,
		Status:    "Pending",
	}

	err := dbWrapper.CreateOrder(order)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed saving order timeline profile")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": orderId, "status": "Pending"})
}

func (h *Handler) GetUserOrdersHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	dbWrapper, ok := h.DB.(*storage.DB)
	if !ok {
		writeError(w, http.StatusInternalServerError, "invalid database connection driver type")
		return
	}

	orders, err := dbWrapper.GetAllOrders()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load store orders")
		return
	}
	if orders == nil {
		orders = []storage.Order{}
	}
	writeJSON(w, http.StatusOK, orders)
}

func (h *Handler) AdminUpdateInventoryHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		writeError(w, http.StatusMethodNotAllowed, "method notAllowed")
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

	dbWrapper, ok := h.DB.(*storage.DB)
	if !ok {
		writeError(w, http.StatusInternalServerError, "invalid database connection driver type")
		return
	}

	prod, err := dbWrapper.GetProductByID(mutation.ProductID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "target variant item entry missing")
		return
	}

	prod.Stock = mutation.Stock
	err = dbWrapper.UpdateProduct(prod)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed applying production scale threshold modifications")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "inventory update applied successfully"})
}
