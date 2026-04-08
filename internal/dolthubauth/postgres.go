package dolthubauth

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ReadinessChecker reports whether a dependency is ready to serve traffic.
type ReadinessChecker interface {
	Check(context.Context) error
}

// SchemaStore is the persistence dependency required by the auth-service
// skeleton command.
type SchemaStore interface {
	ReadinessChecker
	ApplySchema(context.Context) error
	Close()
}

// PostgresStore provides startup schema bootstrap and readiness checks.
type PostgresStore struct {
	pool        *pgxpool.Pool
	tenantID    string
	environment string
}

// OpenPostgres connects to Postgres and verifies the pool can serve requests.
func OpenPostgres(ctx context.Context, databaseURL, tenantID, environment string) (*PostgresStore, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open postgres pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return &PostgresStore{
		pool:        pool,
		tenantID:    tenantID,
		environment: environment,
	}, nil
}

// ApplySchema installs the phase-1 bootstrap schema if it does not already
// exist.
func (s *PostgresStore) ApplySchema(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, bootstrapSchema); err != nil {
		return fmt.Errorf("apply bootstrap schema: %w", err)
	}
	return nil
}

// Check verifies the service can still reach Postgres.
func (s *PostgresStore) Check(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// Close releases the backing connection pool.
func (s *PostgresStore) Close() {
	s.pool.Close()
}
