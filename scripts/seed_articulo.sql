BEGIN;

INSERT INTO public.articulo (id, nombre, descripcion)
VALUES
    ('1', 'Artículo de prueba 1', 'Artículo de ejemplo para probar pedidos'),
    ('2', 'Artículo de prueba 2', 'Segundo artículo de ejemplo para probar pedidos')
ON CONFLICT (id) DO NOTHING;

COMMIT;

SELECT id, nombre, descripcion
FROM public.articulo
WHERE id::text IN ('1', '2')
ORDER BY id;
