package internal

import (
	"github.com/jackc/pgx/v5/pgxpool"
)

// All DB interaction code goes here

type AlarmsRepository struct {
	Db *pgxpool.Pool
}
