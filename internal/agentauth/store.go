// Package agentauth authenticates machine requests from agents and collectors.
package agentauth

import "database/sql"

// Store owns the agent keys lifecycle
// The caller supplies a migrated database opened by database.OpenFile or
// OpenExistingFile and remains responsible for closing it.
type Store struct {
	db *sql.DB
}

// NewStore uses the caller's configured database.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}
