package entity

import (
	"time"
)

// Pedido representa la cabecera de la transacción.
type Pedido struct {
	ID            string
	RUTCliente    string
	Estado        EstadoPedido
	Detalles      []DetallePedido
	FechaCreacion time.Time
}

// DetallePedido representa cada línea de producto dentro del pedido.
type DetallePedido struct {
	ID             string
	IDPedido       string
	IDArticulo     string
	Cantidad       int
	PrecioUnitario float64
}

// Articulo representa el producto en el catálogo maestro.
type Articulo struct {
	ID          string
	Nombre      string
	Descripcion string
}

// EstadoPedido define los posibles estados de un pedido mediante un tipo personalizado.
type EstadoPedido string

const (
	EstadoEnPreparacion     EstadoPedido = "en_preparacion"
	EstadoListoParaDespacho EstadoPedido = "listo_para_despacho"
	EstadoEnTransito        EstadoPedido = "en_transito"
	EstadoEntregado         EstadoPedido = "entregado"
	EstadoNoEntregado       EstadoPedido = "no_entregado"
	EstadoDevuelto          EstadoPedido = "devuelto"
)
