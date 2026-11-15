package entity

type PedidoListado struct {
	ID            string           `json:"id"`
	RUTCliente    string           `json:"rut_cliente"`
	Estado        string           `json:"estado"`
	FechaCreacion string           `json:"fecha_creacion"`
	Articulos     []ArticuloPedido `json:"articulos"`
}

type ArticuloPedido struct {
	ID             string  `json:"id"`
	Nombre         string  `json:"nombre"`
	Cantidad       int     `json:"cantidad"`
	PrecioUnitario float64 `json:"precio_unitario"`
}
