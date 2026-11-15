CREATE TABLE IF NOT EXISTS public.pedido (
    id TEXT PRIMARY KEY,
    rut_cliente TEXT NOT NULL,
    estado TEXT NOT NULL CHECK (
        estado IN (
            'en_preparacion',
            'listo_para_despacho',
            'en_transito',
            'entregado',
            'no_entregado',
            'devuelto'
        )
    ),
    fecha_creacion TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS public.detalle_pedido (
    id TEXT PRIMARY KEY,
    id_pedido TEXT NOT NULL REFERENCES public.pedido(id) ON DELETE CASCADE,
    id_articulo TEXT NOT NULL REFERENCES public.articulo(id),
    cantidad INTEGER NOT NULL CHECK (cantidad > 0),
    precio_unitario DOUBLE PRECISION NOT NULL CHECK (precio_unitario > 0)
);
