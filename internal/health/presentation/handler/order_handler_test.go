package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"microservice/internal/health/application/usecase"
	"microservice/internal/health/domain/entity"
	"microservice/internal/health/domain/repository"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type createOrderRepository struct {
	order            entity.Pedido
	items            []entity.DetallePedido
	updatedOrderID   string
	updatedItemID    string
	updatedQuantity  int
	updatedUnitPrice float64
	articleExists    bool
	articleErr       error
	orderByID        *entity.Pedido
	deletedOrderID   string
	createCalls      int
	idempotency      map[string]idempotencyResult
}

type idempotencyResult struct {
	hash  string
	order entity.Pedido
}

func (r *createOrderRepository) FindAll(context.Context) ([]entity.PedidoListado, error) {
	return []entity.PedidoListado{{
		ID:            "pedido-42",
		RUTCliente:    "12345678-5",
		Estado:        string(entity.EstadoEnPreparacion),
		FechaCreacion: "2026-11-05T16:53:23",
		Articulos: []entity.ArticuloPedido{{
			ID:             "1",
			Nombre:         "Artículo de prueba",
			Cantidad:       2,
			PrecioUnitario: 1500.50,
		}},
	}}, nil
}

func (r *createOrderRepository) FindByID(context.Context, string) (*entity.Pedido, error) {
	return r.orderByID, nil
}

func (r *createOrderRepository) Delete(_ context.Context, id string) (*entity.Pedido, error) {
	r.deletedOrderID = id
	return &entity.Pedido{ID: id}, nil
}

func (r *createOrderRepository) ArticuloExists(context.Context, string) (bool, error) {
	return r.articleExists, r.articleErr
}

func (r *createOrderRepository) UpdateOrderItem(_ context.Context, orderID, itemID string, quantity int, unitPrice float64) (entity.DetallePedido, error) {
	r.updatedOrderID = orderID
	r.updatedItemID = itemID
	r.updatedQuantity = quantity
	r.updatedUnitPrice = unitPrice
	return entity.DetallePedido{
		ID:             itemID,
		IDPedido:       orderID,
		IDArticulo:     "2",
		Cantidad:       quantity,
		PrecioUnitario: unitPrice,
	}, nil
}

func (r *createOrderRepository) Create(_ context.Context, order entity.Pedido, items []entity.DetallePedido) (entity.Pedido, []entity.DetallePedido, error) {
	r.createCalls++
	r.order = order
	for i := range items {
		items[i].ID = "detalle-1"
		items[i].IDPedido = "42"
	}
	r.items = items
	order.ID = "42"
	order.Detalles = items
	return order, items, nil
}

func (r *createOrderRepository) CreateIdempotent(ctx context.Context, key, requestHash string, order entity.Pedido, items []entity.DetallePedido) (entity.Pedido, []entity.DetallePedido, error) {
	if r.idempotency == nil {
		r.idempotency = make(map[string]idempotencyResult)
	}
	if existing, ok := r.idempotency[key]; ok {
		if existing.hash != requestHash {
			return entity.Pedido{}, nil, repository.ErrIdempotencyKeyReuse
		}
		return existing.order, nil, nil
	}

	created, createdItems, err := r.Create(ctx, order, items)
	if err != nil {
		return entity.Pedido{}, nil, err
	}
	r.idempotency[key] = idempotencyResult{hash: requestHash, order: created}
	return created, createdItems, nil
}

func TestCreateOrderGeneratesOrderID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &createOrderRepository{articleExists: true}
	h := NewOrderHandler(usecase.NewOrderUseCase(repo), zap.NewNop())

	request := httptest.NewRequest(http.MethodPost, "/api/v1/orders", strings.NewReader(`{
		"rut_cliente": "12.345.678-5",
		"articulos": [{"id": "1", "cantidad": 2, "precio_unitario": 1550.50}]
	}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router := gin.New()
	router.POST("/api/v1/orders", h.Create)

	router.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("Create() status = %d, want %d; body: %s", response.Code, http.StatusCreated, response.Body)
	}

	var body map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body) != 2 {
		t.Fatalf("response has %d fields, want only order_id and estado: %s", len(body), response.Body)
	}
	var orderID string
	if err := json.Unmarshal(body["order_id"], &orderID); err != nil {
		t.Fatalf("decode response order_id: %v", err)
	}
	if orderID != "42" {
		t.Errorf("response order_id = %q, want %q", orderID, "42")
	}
	var status entity.EstadoPedido
	if err := json.Unmarshal(body["estado"], &status); err != nil {
		t.Fatalf("decode response estado: %v", err)
	}
	if status != entity.EstadoEnPreparacion {
		t.Errorf("response estado = %q, want %q", status, entity.EstadoEnPreparacion)
	}
	if repo.order.ID != "" {
		t.Errorf("repository received order ID %q, want it to be generated", repo.order.ID)
	}
	if repo.order.RUTCliente != "12345678-5" {
		t.Errorf("repository received customer ID %q, want %q", repo.order.RUTCliente, "12345678-5")
	}
	if repo.order.Estado != entity.EstadoEnPreparacion {
		t.Errorf("repository received status %q, want %q", repo.order.Estado, entity.EstadoEnPreparacion)
	}
	if len(repo.items) != 1 {
		t.Fatalf("repository received %d items, want 1", len(repo.items))
	}
	if repo.items[0].ID != "detalle-1" || repo.items[0].IDPedido != "42" || repo.items[0].IDArticulo != "1" ||
		repo.items[0].Cantidad != 2 || repo.items[0].PrecioUnitario != 1550.50 {
		t.Errorf("repository received item %+v, want generated ID and id=1, cantidad=2, precio_unitario=1550.50", repo.items[0])
	}
}

func TestCreateOrderIdempotencyKeyReturnsOriginalOrder(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &createOrderRepository{articleExists: true}
	h := NewOrderHandler(usecase.NewOrderUseCase(repo), zap.NewNop())
	router := gin.New()
	router.POST("/api/v1/orders", h.Create)

	for i := 0; i < 2; i++ {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/orders", strings.NewReader(`{
			"rut_cliente": "12.345.678-5",
			"articulos": [{"id": "1", "cantidad": 2, "precio_unitario": 1550.50}]
		}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", "order-request-1")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)

		if response.Code != http.StatusCreated {
			t.Fatalf("request %d status = %d, want %d; body: %s", i+1, response.Code, http.StatusCreated, response.Body)
		}
		var body createOrderResponse
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode response %d: %v", i+1, err)
		}
		if body.OrderID != "42" || body.Estado != entity.EstadoEnPreparacion {
			t.Errorf("request %d response = %+v, want original order 42", i+1, body)
		}
	}

	if repo.createCalls != 1 {
		t.Errorf("repository created %d orders, want 1", repo.createCalls)
	}
}

func TestCreateOrderRejectsIdempotencyKeyWithDifferentRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &createOrderRepository{articleExists: true}
	h := NewOrderHandler(usecase.NewOrderUseCase(repo), zap.NewNop())
	router := gin.New()
	router.POST("/api/v1/orders", h.Create)

	bodies := []string{
		`{"rut_cliente":"12.345.678-5","articulos":[{"id":"1","cantidad":2,"precio_unitario":1550.50}]}`,
		`{"rut_cliente":"12.345.678-5","articulos":[{"id":"1","cantidad":3,"precio_unitario":1550.50}]}`,
	}
	for i, body := range bodies {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/orders", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", "order-request-2")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)

		wantStatus := http.StatusCreated
		if i == 1 {
			wantStatus = http.StatusConflict
		}
		if response.Code != wantStatus {
			t.Errorf("request %d status = %d, want %d; body: %s", i+1, response.Code, wantStatus, response.Body)
		}
	}
	if repo.createCalls != 1 {
		t.Errorf("repository created %d orders, want 1", repo.createCalls)
	}
}

func TestCreateOrderRejectsMissingOrInvalidFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name string
		body string
	}{
		{name: "missing RUT", body: `{"articulos":[{"id":"1","cantidad":1,"precio_unitario":100}]}`},
		{name: "missing articles", body: `{"rut_cliente":"12.345.678-5"}`},
		{name: "empty articles", body: `{"rut_cliente":"12.345.678-5","articulos":[]}`},
		{name: "missing article ID", body: `{"rut_cliente":"12.345.678-5","articulos":[{"cantidad":1,"precio_unitario":100}]}`},
		{name: "missing quantity", body: `{"rut_cliente":"12.345.678-5","articulos":[{"id":"1","precio_unitario":100}]}`},
		{name: "zero quantity", body: `{"rut_cliente":"12.345.678-5","articulos":[{"id":"1","cantidad":0,"precio_unitario":100}]}`},
		{name: "missing unit price", body: `{"rut_cliente":"12.345.678-5","articulos":[{"id":"1","cantidad":1}]}`},
		{name: "zero unit price", body: `{"rut_cliente":"12.345.678-5","articulos":[{"id":"1","cantidad":1,"precio_unitario":0}]}`},
		{name: "invalid RUT", body: `{"rut_cliente":"12.345.678-9","articulos":[{"id":"1","cantidad":1,"precio_unitario":100}]}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &createOrderRepository{articleExists: true}
			h := NewOrderHandler(usecase.NewOrderUseCase(repo), zap.NewNop())
			request := httptest.NewRequest(http.MethodPost, "/api/v1/orders", strings.NewReader(tt.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router := gin.New()
			router.POST("/api/v1/orders", h.Create)

			router.ServeHTTP(response, request)

			if response.Code != http.StatusBadRequest {
				t.Errorf("Create() status = %d, want %d; body: %s", response.Code, http.StatusBadRequest, response.Body)
			}
		})
	}
}

func TestCreateOrderRejectsUnknownArticle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &createOrderRepository{}
	h := NewOrderHandler(usecase.NewOrderUseCase(repo), zap.NewNop())
	request := httptest.NewRequest(http.MethodPost, "/api/v1/orders", strings.NewReader(`{
		"rut_cliente": "12.345.678-5",
		"articulos": [{"id": "999", "cantidad": 1, "precio_unitario": 100}]
	}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router := gin.New()
	router.POST("/api/v1/orders", h.Create)

	router.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("Create() status = %d, want %d; body: %s", response.Code, http.StatusBadRequest, response.Body)
	}
}

func TestFindAllIncludesArticleDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewOrderHandler(usecase.NewOrderUseCase(&createOrderRepository{}), zap.NewNop())
	request := httptest.NewRequest(http.MethodGet, "/api/v1/orders", nil)
	response := httptest.NewRecorder()
	router := gin.New()
	router.GET("/api/v1/orders", h.FindAll)

	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("FindAll() status = %d, want %d; body: %s", response.Code, http.StatusOK, response.Body)
	}

	var body []entity.PedidoListado
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body) != 1 || len(body[0].Articulos) != 1 {
		t.Fatalf("FindAll() response = %+v, want one order with one article", body)
	}
	if body[0].FechaCreacion != "2026-11-05T16:53:23" {
		t.Errorf("fecha_creacion = %q, want %q", body[0].FechaCreacion, "2026-11-05T16:53:23")
	}
	articulo := body[0].Articulos[0]
	if articulo.Nombre != "Artículo de prueba" || articulo.Cantidad != 2 || articulo.PrecioUnitario != 1500.50 {
		t.Errorf("article details = %+v, want name, quantity=2, unit_price=1500.50", articulo)
	}
}

func TestFindByIDFormatsCreationDate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &createOrderRepository{
		orderByID: &entity.Pedido{
			ID:            "pedido-42",
			RUTCliente:    "12345678-5",
			Estado:        entity.EstadoEnPreparacion,
			FechaCreacion: time.Date(2026, time.November, 5, 17, 0, 55, 0, time.UTC),
		},
	}
	h := NewOrderHandler(usecase.NewOrderUseCase(repo), zap.NewNop())
	request := httptest.NewRequest(http.MethodGet, "/api/v1/orders/pedido-42", nil)
	response := httptest.NewRecorder()
	router := gin.New()
	router.GET("/api/v1/orders/:id", h.FindByID)

	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("FindByID() status = %d, want %d; body: %s", response.Code, http.StatusOK, response.Body)
	}

	var body struct {
		FechaCreacion string
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.FechaCreacion != "2026-11-05T17:00:55" {
		t.Errorf("FechaCreacion = %q, want %q", body.FechaCreacion, "2026-11-05T17:00:55")
	}
}

func TestDeleteOrderAcceptsTextID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &createOrderRepository{}
	h := NewOrderHandler(usecase.NewOrderUseCase(repo), zap.NewNop())
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/orders/pedido-42", nil)
	response := httptest.NewRecorder()
	router := gin.New()
	router.DELETE("/api/v1/orders/:id", h.DeleteOrder)

	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("DeleteOrder() status = %d, want %d; body: %s", response.Code, http.StatusOK, response.Body)
	}
	if repo.deletedOrderID != "pedido-42" {
		t.Errorf("repository received ID %q, want %q", repo.deletedOrderID, "pedido-42")
	}
}

func TestNormalizeChileanRUT(t *testing.T) {
	tests := []struct {
		name string
		rut  string
		want string
		ok   bool
	}{
		{name: "formatted", rut: "12.345.678-5", want: "12345678-5", ok: true},
		{name: "without separators", rut: "123456785", want: "12345678-5", ok: true},
		{name: "uppercase K", rut: "11.111.112-k", want: "11111112-K", ok: true},
		{name: "invalid verifier", rut: "12.345.678-9"},
		{name: "malformed grouping", rut: "123.45.678-5"},
		{name: "non-numeric body", rut: "12.345.67A-5"},
		{name: "zero RUT", rut: "0-0"},
		{name: "empty", rut: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeChileanRUT(tt.rut)
			if (err == nil) != tt.ok {
				t.Fatalf("normalizeChileanRUT(%q) error = %v, want success %t", tt.rut, err, tt.ok)
			}
			if got != tt.want {
				t.Errorf("normalizeChileanRUT(%q) = %q, want %q", tt.rut, got, tt.want)
			}
		})
	}
}

func TestUpdateOrderItemUpdatesOnlyAllowedValues(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &createOrderRepository{}
	h := NewOrderHandler(usecase.NewOrderUseCase(repo), zap.NewNop())

	request := httptest.NewRequest(http.MethodPut, "/api/v1/orders/pedido-42/items/detalle-7", strings.NewReader(
		`{"quantity":3,"unit_price":1999.50}`,
	))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router := gin.New()
	router.PUT("/api/v1/orders/:order_id/items/:item_id", h.UpdateOrderItem)

	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("UpdateOrderItem() status = %d, want %d; body: %s", response.Code, http.StatusOK, response.Body)
	}
	if repo.updatedOrderID != "pedido-42" || repo.updatedItemID != "detalle-7" {
		t.Errorf("repository received order/item IDs %s/%s, want pedido-42/detalle-7", repo.updatedOrderID, repo.updatedItemID)
	}
	if repo.updatedQuantity != 3 || repo.updatedUnitPrice != 1999.50 {
		t.Errorf("repository received quantity/unit_price %d/%g, want 3/1999.5", repo.updatedQuantity, repo.updatedUnitPrice)
	}

	var body updateOrderItemResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.ID != "detalle-7" || body.ProductID != "2" || body.Quantity != 3 || body.UnitPrice != 1999.50 {
		t.Errorf("response item = %+v, want ID=detalle-7, id_articulo=2, cantidad=3, precio_unitario=1999.5", body)
	}
}

func TestUpdateOrderItemRejectsInvalidValues(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name string
		body string
	}{
		{name: "missing quantity", body: `{"unit_price":100}`},
		{name: "missing unit_price", body: `{"quantity":1}`},
		{name: "zero quantity", body: `{"quantity":0,"unit_price":100}`},
		{name: "negative unit_price", body: `{"quantity":1,"unit_price":-1}`},
		{name: "null quantity", body: `{"quantity":null,"unit_price":100}`},
		{name: "null unit_price", body: `{"quantity":1,"unit_price":null}`},
		{name: "non-numeric quantity", body: `{"quantity":"2","unit_price":100}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &createOrderRepository{}
			h := NewOrderHandler(usecase.NewOrderUseCase(repo), zap.NewNop())
			request := httptest.NewRequest(http.MethodPut, "/api/v1/orders/pedido-42/items/detalle-7", strings.NewReader(tt.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router := gin.New()
			router.PUT("/api/v1/orders/:order_id/items/:item_id", h.UpdateOrderItem)

			router.ServeHTTP(response, request)

			if response.Code != http.StatusBadRequest {
				t.Errorf("UpdateOrderItem() status = %d, want %d; body: %s", response.Code, http.StatusBadRequest, response.Body)
			}
		})
	}
}
