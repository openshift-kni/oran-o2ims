package repo

import "github.com/jackc/pgx/v5/pgxpool"

// ResourceRepository defines the database repository for the resource server tables
type ResourceRepository struct {
	Db *pgxpool.Pool
}
