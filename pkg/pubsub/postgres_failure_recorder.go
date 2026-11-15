package pubsub

import (
	"context"
	"encoding/json"
	"fmt"

	"microservice/pkg/database"

	"github.com/jackc/pgx/v5/pgxpool"
)

type postgresFailureRecorder struct {
	pool *pgxpool.Pool
}

func NewPostgresFailureRecorder(db database.Database) (FailureRecorder, error) {
	pool, ok := db.Native().(*pgxpool.Pool)
	if !ok {
		return nil, fmt.Errorf("la conexión de base de datos no es un pool de PostgreSQL")
	}
	return &postgresFailureRecorder{pool: pool}, nil
}

func (r *postgresFailureRecorder) RecordFailure(ctx context.Context, message FailedMessage) error {
	attributes, err := json.Marshal(message.Attributes)
	if err != nil {
		return fmt.Errorf("serializar atributos del mensaje fallido: %w", err)
	}

	const query = `
		INSERT INTO public.failed_pubsub_messages
			(subscription, message_id, data, attributes, attempts, last_error, failed_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW())
		ON CONFLICT (subscription, message_id) DO UPDATE
		SET data = EXCLUDED.data,
			attributes = EXCLUDED.attributes,
			attempts = public.failed_pubsub_messages.attempts + EXCLUDED.attempts,
			last_error = EXCLUDED.last_error,
			failed_at = EXCLUDED.failed_at
	`
	if _, err := r.pool.Exec(
		ctx,
		query,
		message.Subscription,
		message.MessageID,
		message.Data,
		attributes,
		message.Attempts,
		message.LastError,
	); err != nil {
		return fmt.Errorf("persistir mensaje Pub/Sub fallido: %w", err)
	}
	return nil
}
