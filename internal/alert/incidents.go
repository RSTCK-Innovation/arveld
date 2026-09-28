package alert

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// ErrInvalidIncidentQuery means a history filter or page boundary is invalid.
var ErrInvalidIncidentQuery = errors.New("invalid incident query")

// ErrIncidentClosed means a closed, unacknowledged episode cannot be acknowledged.
var ErrIncidentClosed = errors.New("incident is closed")

// Incident is a durable snapshot of an observed firing episode.
type Incident struct {
	ID              int64      `json:"id,string"`
	RuleID          string     `json:"rule_id"`
	OwnerKind       string     `json:"owner_kind"`
	OwnerID         string     `json:"owner_id"`
	OwnerName       string     `json:"owner_name"`
	Condition       string     `json:"condition"`
	Threshold       *float64   `json:"threshold,omitempty"`
	ForSeconds      int        `json:"for_seconds"`
	Severity        string     `json:"severity"`
	Status          string     `json:"status"`
	OpenedAt        time.Time  `json:"opened_at"`
	LastEvaluatedAt time.Time  `json:"last_evaluated_at"`
	ClosedAt        *time.Time `json:"closed_at,omitempty"`
	CloseReason     string     `json:"close_reason,omitempty"`
	AcknowledgedAt  *time.Time `json:"acknowledged_at,omitempty"`
	AcknowledgedBy  string     `json:"acknowledged_by,omitempty"`
}

// IncidentQuery scopes a bounded history page. Before excludes that incident ID.
type IncidentQuery struct {
	Status, Severity, OwnerKind, OwnerID string
	Before                               int64
	Limit                                int
}

// IncidentPage includes matching history and the cursor for an older page.
type IncidentPage struct {
	Incidents  []Incident `json:"incidents"`
	Total      int        `json:"total"`
	NextBefore string     `json:"next_before,omitempty"`
}

const incidentColumns = `id,rule_id,owner_kind,owner_id,owner_name,condition,threshold,for_seconds,severity,
    opened_at,last_evaluated_at,closed_at,close_reason,acknowledged_at,acknowledged_by`

type incidentScanner interface{ Scan(dest ...any) error }

func scanIncident(row incidentScanner) (Incident, error) {
	var value Incident
	var opened, evaluated int64
	var closed, acknowledged sql.NullInt64
	err := row.Scan(&value.ID, &value.RuleID, &value.OwnerKind, &value.OwnerID, &value.OwnerName, &value.Condition,
		&value.Threshold, &value.ForSeconds, &value.Severity, &opened, &evaluated, &closed, &value.CloseReason, &acknowledged, &value.AcknowledgedBy)
	if err != nil {
		return Incident{}, fmt.Errorf("scan incident: %w", err)
	}
	value.OpenedAt, value.LastEvaluatedAt = time.UnixMilli(opened).UTC(), time.UnixMilli(evaluated).UTC()
	value.Status = "open"
	if closed.Valid {
		timestamp := time.UnixMilli(closed.Int64).UTC()
		value.ClosedAt = &timestamp
		value.Status = "closed"
	}
	if acknowledged.Valid {
		timestamp := time.UnixMilli(acknowledged.Int64).UTC()
		value.AcknowledgedAt = &timestamp
	}
	return value, nil
}

// Incident reads historical details independently of the current rule/resource.
func (store *Store) Incident(ctx context.Context, id int64) (Incident, error) {
	value, err := scanIncident(store.db.QueryRowContext(ctx, `SELECT `+incidentColumns+` FROM incidents WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Incident{}, ErrNotFound
	}
	return value, err
}

// AcknowledgeIncident records the first acknowledgment without changing alert state.
func (store *Store) AcknowledgeIncident(ctx context.Context, id int64, actor string) (Incident, error) {
	if _, err := store.db.ExecContext(ctx, `UPDATE incidents SET acknowledged_at=?,acknowledged_by=?
        WHERE id=? AND acknowledged_at IS NULL AND closed_at IS NULL`, time.Now().UnixMilli(), actor, id); err != nil {
		return Incident{}, fmt.Errorf("acknowledge incident: %w", err)
	}
	value, err := store.Incident(ctx, id)
	if err != nil {
		return Incident{}, err
	}
	if value.AcknowledgedAt == nil && value.ClosedAt != nil {
		return Incident{}, ErrIncidentClosed
	}
	return value, nil
}

// Incidents returns newest episodes first, preserving stable cursor pagination.
func (store *Store) Incidents(ctx context.Context, query IncidentQuery) (page IncidentPage, returnErr error) {
	if err := validateIncidentQuery(query); err != nil {
		return page, err
	}
	const filter = `(?='' OR (?='open' AND closed_at IS NULL) OR (?='closed' AND closed_at IS NOT NULL))
        AND (?='' OR severity=?) AND (?='' OR owner_kind=?) AND (?='' OR owner_id=?)`
	args := []any{query.Status, query.Status, query.Status, query.Severity, query.Severity, query.OwnerKind, query.OwnerKind, query.OwnerID, query.OwnerID}
	rows, err := store.db.QueryContext(ctx, `SELECT `+incidentColumns+` FROM incidents WHERE `+filter+` AND (?=0 OR id<?) ORDER BY id DESC LIMIT ?`, append(args, query.Before, query.Before, query.Limit+1)...)
	if err != nil {
		return page, fmt.Errorf("read incident history: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("close incident history: %w", err))
		}
	}()
	page.Incidents = make([]Incident, 0)
	for rows.Next() {
		value, err := scanIncident(rows)
		if err != nil {
			return page, err
		}
		page.Incidents = append(page.Incidents, value)
	}
	if err := rows.Err(); err != nil {
		return page, fmt.Errorf("iterate incident history: %w", err)
	}
	if err := rows.Close(); err != nil {
		return page, fmt.Errorf("close incident history: %w", err)
	}
	if len(page.Incidents) > query.Limit {
		page.Incidents = page.Incidents[:query.Limit]
		page.NextBefore = strconv.FormatInt(page.Incidents[query.Limit-1].ID, 10)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT count(*) FROM incidents WHERE `+filter, args...).Scan(&page.Total); err != nil {
		return page, fmt.Errorf("count incident history: %w", err)
	}
	return page, nil
}

func validateIncidentQuery(query IncidentQuery) error {
	if query.Limit < 1 || query.Limit > 100 || query.Before < 0 {
		return ErrInvalidIncidentQuery
	}
	if query.Status != "" && query.Status != "open" && query.Status != "closed" {
		return ErrInvalidIncidentQuery
	}
	if query.Severity != "" && query.Severity != "info" && query.Severity != "warning" && query.Severity != "critical" {
		return ErrInvalidIncidentQuery
	}
	if query.OwnerKind != "" && query.OwnerKind != "monitor" && query.OwnerKind != "agent" {
		return ErrInvalidIncidentQuery
	}
	return nil
}
