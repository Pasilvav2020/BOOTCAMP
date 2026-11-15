package infrastructure

import (
	"context"
	"microservice/pkg/database"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresProductRepository struct {
	db database.Database
}

func NewPostgresProductRepository(db database.Database) *PostgresProductRepository {
	return &PostgresProductRepository{db: db}
}

func (r *PostgresProductRepository) Ping(ctx context.Context) error {
	pool := r.db.Native().(*pgxpool.Pool)
	return pool.Ping(ctx)
}
