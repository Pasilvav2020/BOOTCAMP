package infrastructure

import (
	"context"
	"fmt"
	"time"

	"microservice/internal/health/domain/entity"
	"microservice/internal/health/domain/repository"
	"microservice/pkg/database"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresOrderRepository struct {
	pool *pgxpool.Pool
}

var _ repository.IOrderRepository = (*PostgresOrderRepository)(nil)

//--------------------------------------------

func NewPostgresOrderRepository(db database.Database) (*PostgresOrderRepository, error) {
	pool, ok := db.Native().(*pgxpool.Pool)
	if !ok {
		return nil, fmt.Errorf("la conexión de base de datos no es un pool de PostgreSQL")
	}

	return &PostgresOrderRepository{pool: pool}, nil
}

// --------------------------------------------
func (r *PostgresOrderRepository) FindAll(ctx context.Context) ([]entity.PedidoListado, error) {
	const query = `
		SELECT p.id, p.rut_cliente, p.estado, p.fecha_creacion,
			COALESCE(d.id_articulo, ''), COALESCE(a.nombre, ''),
			COALESCE(d.cantidad, 0),
			COALESCE(d.precio_unitario, 0)
		FROM public.pedido p
		LEFT JOIN public.detalle_pedido d ON d.id_pedido = p.id
		LEFT JOIN public.articulo a ON a.id = d.id_articulo
		ORDER BY p.id, d.id
	`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("listar pedidos: %w", err)
	}
	defer rows.Close()

	orders := make([]entity.PedidoListado, 0)
	for rows.Next() {
		var pedido entity.PedidoListado
		var articulo entity.ArticuloPedido
		var fechaCreacion time.Time
		if err := rows.Scan(
			&pedido.ID,
			&pedido.RUTCliente,
			&pedido.Estado,
			&fechaCreacion,
			&articulo.ID,
			&articulo.Nombre,
			&articulo.Cantidad,
			&articulo.PrecioUnitario,
		); err != nil {
			return nil, fmt.Errorf("leer pedido: %w", err)
		}
		pedido.FechaCreacion = fechaCreacion.Format("2006-01-02T15:04:05")
		if len(orders) == 0 || orders[len(orders)-1].ID != pedido.ID {
			pedido.Articulos = make([]entity.ArticuloPedido, 0)
			orders = append(orders, pedido)
		}
		if articulo.ID != "" {
			orders[len(orders)-1].Articulos = append(orders[len(orders)-1].Articulos, articulo)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterar pedidos: %w", err)
	}

	return orders, nil
}

// --------------------------------------------
func (r *PostgresOrderRepository) FindByID(ctx context.Context, id string) (*entity.Pedido, error) {
	const query = `
		SELECT id, rut_cliente, estado, fecha_creacion
		FROM public.pedido
		WHERE id = $1
	`

	var pedido entity.Pedido
	if err := r.pool.QueryRow(ctx, query, id).
		Scan(&pedido.ID, &pedido.RUTCliente, &pedido.Estado, &pedido.FechaCreacion); err != nil {
		return nil, fmt.Errorf("buscar pedido por ID: %w", err)
	}

	pedido.Detalles = make([]entity.DetallePedido, 0)
	const itemsQuery = `
		SELECT id, id_pedido, id_articulo, cantidad, precio_unitario
		FROM public.detalle_pedido
		WHERE id_pedido = $1
		ORDER BY id
	`
	rows, err := r.pool.Query(ctx, itemsQuery, id)
	if err != nil {
		return nil, fmt.Errorf("listar ítems del pedido: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var detalle entity.DetallePedido
		if err := rows.Scan(&detalle.ID, &detalle.IDPedido, &detalle.IDArticulo, &detalle.Cantidad, &detalle.PrecioUnitario); err != nil {
			return nil, fmt.Errorf("leer ítem del pedido: %w", err)
		}
		pedido.Detalles = append(pedido.Detalles, detalle)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterar ítems del pedido: %w", err)
	}

	return &pedido, nil
}

// --------------------------------------------
func (r *PostgresOrderRepository) Create(ctx context.Context, pedido entity.Pedido, detalles []entity.DetallePedido) (entity.Pedido, []entity.DetallePedido, error) {
	return r.create(ctx, "", "", pedido, detalles)
}

func (r *PostgresOrderRepository) CreateIdempotent(ctx context.Context, key, requestHash string, pedido entity.Pedido, detalles []entity.DetallePedido) (entity.Pedido, []entity.DetallePedido, error) {
	return r.create(ctx, key, requestHash, pedido, detalles)
}

func (r *PostgresOrderRepository) create(ctx context.Context, idempotencyKey, requestHash string, pedido entity.Pedido, detalles []entity.DetallePedido) (entity.Pedido, []entity.DetallePedido, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return entity.Pedido{}, nil, fmt.Errorf("iniciar transacción para crear pedido: %w", err)
	}
	defer tx.Rollback(ctx)

	if idempotencyKey != "" {
		const reserveKey = `
			INSERT INTO public.idempotency_keys (idempotency_key, request_hash)
			VALUES ($1, $2)
			ON CONFLICT (idempotency_key) DO NOTHING
		`
		result, err := tx.Exec(ctx, reserveKey, idempotencyKey, requestHash)
		if err != nil {
			return entity.Pedido{}, nil, fmt.Errorf("reservar clave de idempotencia: %w", err)
		}
		if result.RowsAffected() == 0 {
			var storedHash, orderID, status string
			const findKey = `
				SELECT request_hash, COALESCE(order_id, ''), COALESCE(response_status, '')
				FROM public.idempotency_keys
				WHERE idempotency_key = $1
			`
			if err := tx.QueryRow(ctx, findKey, idempotencyKey).Scan(&storedHash, &orderID, &status); err != nil {
				return entity.Pedido{}, nil, fmt.Errorf("consultar clave de idempotencia: %w", err)
			}
			if storedHash != requestHash {
				return entity.Pedido{}, nil, repository.ErrIdempotencyKeyReuse
			}
			if orderID == "" || status == "" {
				return entity.Pedido{}, nil, fmt.Errorf("clave de idempotencia %q sin pedido asociado", idempotencyKey)
			}
			if err := tx.Commit(ctx); err != nil {
				return entity.Pedido{}, nil, fmt.Errorf("confirmar lectura idempotente: %w", err)
			}
			return entity.Pedido{ID: orderID, Estado: entity.EstadoPedido(status)}, nil, nil
		}
	}

	pedido.ID = uuid.NewString()
	const insertOrder = `
		INSERT INTO public.pedido (id, rut_cliente, estado, fecha_creacion)
		VALUES ($1, $2, $3, NOW())
		RETURNING id, rut_cliente, estado, fecha_creacion
	`
	if err := tx.QueryRow(ctx, insertOrder, pedido.ID, pedido.RUTCliente, pedido.Estado).
		Scan(&pedido.ID, &pedido.RUTCliente, &pedido.Estado, &pedido.FechaCreacion); err != nil {
		return entity.Pedido{}, nil, fmt.Errorf("crear pedido: %w", err)
	}

	const insertItem = `
		INSERT INTO public.detalle_pedido (id, id_pedido, id_articulo, cantidad, precio_unitario)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, id_pedido, id_articulo, cantidad, precio_unitario
	`
	for i := range detalles {
		detalles[i].ID = uuid.NewString()
		if err := tx.QueryRow(ctx, insertItem,
			detalles[i].ID,
			pedido.ID,
			detalles[i].IDArticulo,
			detalles[i].Cantidad,
			detalles[i].PrecioUnitario,
		).Scan(
			&detalles[i].ID,
			&detalles[i].IDPedido,
			&detalles[i].IDArticulo,
			&detalles[i].Cantidad,
			&detalles[i].PrecioUnitario,
		); err != nil {
			return entity.Pedido{}, nil, fmt.Errorf("crear detalle %d del pedido: %w", i, err)
		}
	}

	if idempotencyKey != "" {
		const saveResult = `
			UPDATE public.idempotency_keys
			SET order_id = $2, response_status = $3
			WHERE idempotency_key = $1
		`
		if _, err := tx.Exec(ctx, saveResult, idempotencyKey, pedido.ID, pedido.Estado); err != nil {
			return entity.Pedido{}, nil, fmt.Errorf("guardar resultado idempotente: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return entity.Pedido{}, nil, fmt.Errorf("confirmar creación del pedido: %w", err)
	}

	pedido.Detalles = detalles
	return pedido, detalles, nil
}

func (r *PostgresOrderRepository) UpdateOrderItem(ctx context.Context, orderID, itemID string, quantity int, unitPrice float64) (entity.DetallePedido, error) {
	const query = `
		UPDATE public.detalle_pedido
		SET cantidad = $3, precio_unitario = $4
		WHERE id_pedido = $1 AND id = $2
		RETURNING id, id_pedido, id_articulo, cantidad, precio_unitario
	`

	var detalle entity.DetallePedido
	if err := r.pool.QueryRow(ctx, query, orderID, itemID, quantity, unitPrice).
		Scan(&detalle.ID, &detalle.IDPedido, &detalle.IDArticulo, &detalle.Cantidad, &detalle.PrecioUnitario); err != nil {
		return entity.DetallePedido{}, fmt.Errorf("actualizar ítem del pedido: %w", err)
	}

	return detalle, nil
}

func (r *PostgresOrderRepository) ArticuloExists(ctx context.Context, id string) (bool, error) {
	const query = `SELECT EXISTS (SELECT 1 FROM public.articulo WHERE id::text = $1)`
	var exists bool
	if err := r.pool.QueryRow(ctx, query, id).Scan(&exists); err != nil {
		return false, fmt.Errorf("verificar existencia del artículo %q: %w", id, err)
	}
	return exists, nil
}

// --------------------------------------------
func (r *PostgresOrderRepository) Delete(ctx context.Context, id string) (*entity.Pedido, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("iniciar transacción para eliminar pedido: %w", err)
	}
	defer tx.Rollback(ctx)

	const deleteItemsQuery = `
		DELETE FROM public.detalle_pedido
		WHERE id_pedido = $1
	`
	if _, err := tx.Exec(ctx, deleteItemsQuery, id); err != nil {
		return nil, fmt.Errorf("eliminar ítems del pedido: %w", err)
	}

	const query = `
		DELETE FROM public.pedido
		WHERE id = $1
		RETURNING id, rut_cliente, estado, fecha_creacion
	`

	var pedido entity.Pedido
	if err := tx.QueryRow(ctx, query, id).
		Scan(&pedido.ID, &pedido.RUTCliente, &pedido.Estado, &pedido.FechaCreacion); err != nil {
		return nil, fmt.Errorf("eliminar pedido: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("confirmar eliminación del pedido: %w", err)
	}

	return &pedido, nil
}
