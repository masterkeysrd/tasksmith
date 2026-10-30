package metrics

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Query describes a generic aggregation over records and measurements.
type Query struct {
	Kind       Kind // optional filter
	Aggregates []Aggregate
	GroupBy    []Dimension
	Bucket     Bucket // optional time bucket
	Filter     Filter
	Since      time.Time
	Until      time.Time
}

// AggFunc is an aggregate function over a metric.
type AggFunc string

// Aggregate functions.
const (
	AggSum AggFunc = "sum"
	AggMin AggFunc = "min"
	AggMax AggFunc = "max"
)

// Aggregate computes Func over Metric.
type Aggregate struct {
	Metric Metric
	Func   AggFunc
}

// Bucket is a time bucket size.
type Bucket string

// Time buckets.
const (
	BucketNone  Bucket = ""
	BucketHour  Bucket = "hour"
	BucketDay   Bucket = "day"
	BucketWeek  Bucket = "week"
	BucketMonth Bucket = "month"
)

// Dimension is a column name (workspace_root, session_id, parent_session_id,
// turn_id, message_id, agent, status, kind) or an attr key prefixed with
// "attr." (e.g. "attr.model").
type Dimension string

// AttrPrefix prefixes attr keys used as a [Dimension].
const AttrPrefix = "attr."

// Filter restricts the records a query considers.
type Filter struct {
	WorkspaceRoot   string
	SessionID       string
	TurnIDs         []string // match any of these turns
	IncludeChildren bool     // also match parent_session_id = SessionID
	Agent           string
	Status          []Status
	Attrs           map[string]string // exact-match attr filters
}

// Result holds the rows of an executed query.
type Result struct {
	Rows []Row
}

// Row is one group of a [Result].
type Row struct {
	Bucket time.Time         // zero when no bucket was requested
	Group  map[string]string // dimension -> value
	Count  int64             // number of records in the group
	Values map[Metric]int64  // aggregated metric values
}

// ErrInvalidQuery is returned for malformed queries.
var ErrInvalidQuery = errors.New("metrics: invalid query")

var (
	identPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	metricRe     = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$`)

	columnDims = map[string]bool{
		"workspace_root": true, "session_id": true, "parent_session_id": true,
		"turn_id": true, "message_id": true, "agent": true, "status": true, "kind": true,
	}
)

func dimensionExpr(d Dimension) (string, error) {
	name := string(d)
	if columnDims[name] {
		return "r." + name, nil
	}
	if key, ok := strings.CutPrefix(name, AttrPrefix); ok {
		if !identPattern.MatchString(key) {
			return "", fmt.Errorf("%w: invalid attr key %q", ErrInvalidQuery, key)
		}
		return fmt.Sprintf("COALESCE(json_extract(r.attrs, '$.%s'), '')", key), nil
	}
	return "", fmt.Errorf("%w: unknown dimension %q", ErrInvalidQuery, name)
}

func bucketExpr(b Bucket) (string, error) {
	switch b {
	case BucketHour:
		return "strftime('%Y-%m-%dT%H:00:00Z', r.created_at)", nil
	case BucketDay:
		return "strftime('%Y-%m-%dT00:00:00Z', r.created_at)", nil
	case BucketWeek:
		return "strftime('%Y-%m-%dT00:00:00Z', date(r.created_at, 'weekday 0', '-6 days'))", nil
	case BucketMonth:
		return "strftime('%Y-%m-01T00:00:00Z', r.created_at)", nil
	}
	return "", fmt.Errorf("%w: unknown bucket %q", ErrInvalidQuery, b)
}

// BuildSQL returns the parameterized SQL for q and the group dimensions in
// select order. It is exported for testing.
func BuildSQL(q Query) (string, []any, error) {
	var (
		selects []string
		args    []any
		groups  []string
	)
	if q.Bucket != BucketNone {
		expr, err := bucketExpr(q.Bucket)
		if err != nil {
			return "", nil, err
		}
		selects = append(selects, expr+" AS bucket")
		groups = append(groups, "bucket")
	}
	for i, d := range q.GroupBy {
		expr, err := dimensionExpr(d)
		if err != nil {
			return "", nil, err
		}
		selects = append(selects, fmt.Sprintf("%s AS g%d", expr, i))
		groups = append(groups, fmt.Sprintf("g%d", i))
	}
	selects = append(selects, "COUNT(DISTINCT r.id) AS cnt")
	for _, a := range q.Aggregates {
		if !metricRe.MatchString(string(a.Metric)) {
			return "", nil, fmt.Errorf("%w: invalid metric %q", ErrInvalidQuery, a.Metric)
		}
		var fn string
		switch a.Func {
		case AggSum, "":
			fn = "SUM"
		case AggMin:
			fn = "MIN"
		case AggMax:
			fn = "MAX"
		default:
			return "", nil, fmt.Errorf("%w: unknown aggregate %q", ErrInvalidQuery, a.Func)
		}
		selects = append(selects, fmt.Sprintf("COALESCE(%s(CASE WHEN m.metric = ? THEN m.value END), 0)", fn))
		args = append(args, string(a.Metric))
	}

	var where []string
	if q.Kind != "" {
		where = append(where, "r.kind = ?")
		args = append(args, string(q.Kind))
	}
	f := q.Filter
	if f.WorkspaceRoot != "" {
		where = append(where, "r.workspace_root = ?")
		args = append(args, f.WorkspaceRoot)
	}
	if f.SessionID != "" {
		if f.IncludeChildren {
			where = append(where, "(r.session_id = ? OR r.parent_session_id = ?)")
			args = append(args, f.SessionID, f.SessionID)
		} else {
			where = append(where, "r.session_id = ?")
			args = append(args, f.SessionID)
		}
	}
	if len(f.TurnIDs) > 0 {
		ph := make([]string, len(f.TurnIDs))
		for i, id := range f.TurnIDs {
			ph[i] = "?"
			args = append(args, id)
		}
		where = append(where, "r.turn_id IN ("+strings.Join(ph, ",")+")")
	}
	if f.Agent != "" {
		where = append(where, "r.agent = ?")
		args = append(args, f.Agent)
	}
	if len(f.Status) > 0 {
		ph := make([]string, len(f.Status))
		for i, s := range f.Status {
			ph[i] = "?"
			args = append(args, string(s))
		}
		where = append(where, "r.status IN ("+strings.Join(ph, ",")+")")
	}
	for _, kv := range sortedAttrPairs(f.Attrs) {
		if !identPattern.MatchString(kv[0]) {
			return "", nil, fmt.Errorf("%w: invalid attr key %q", ErrInvalidQuery, kv[0])
		}
		where = append(where, fmt.Sprintf("json_extract(r.attrs, '$.%s') = ?", kv[0]))
		args = append(args, kv[1])
	}
	if !q.Since.IsZero() {
		where = append(where, "r.created_at >= ?")
		args = append(args, q.Since.UTC().Format(TimeLayout))
	}
	if !q.Until.IsZero() {
		where = append(where, "r.created_at < ?")
		args = append(args, q.Until.UTC().Format(TimeLayout))
	}

	var sb strings.Builder
	sb.WriteString("SELECT " + strings.Join(selects, ", "))
	sb.WriteString(" FROM metrics_records r LEFT JOIN metrics_measurements m ON m.record_id = r.id")
	if len(where) > 0 {
		sb.WriteString(" WHERE " + strings.Join(where, " AND "))
	}
	if len(groups) > 0 {
		sb.WriteString(" GROUP BY " + strings.Join(groups, ", ") + " ORDER BY " + strings.Join(groups, ", "))
	}
	return sb.String(), args, nil
}

// Query runs q against the database.
func (s *Store) Query(ctx context.Context, q Query) (Result, error) {
	return RunQuery(ctx, s.db, q)
}

// RunQuery runs q against db.
func RunQuery(ctx context.Context, db *sql.DB, q Query) (Result, error) {
	query, args, err := BuildSQL(q)
	if err != nil {
		return Result{}, err
	}
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return Result{}, fmt.Errorf("metrics: query: %w", err)
	}
	defer rows.Close()

	var res Result
	for rows.Next() {
		var (
			bucket sql.NullString
			groups = make([]sql.NullString, len(q.GroupBy))
			count  int64
			values = make([]int64, len(q.Aggregates))
			dest   []any
		)
		if q.Bucket != BucketNone {
			dest = append(dest, &bucket)
		}
		for i := range groups {
			dest = append(dest, &groups[i])
		}
		dest = append(dest, &count)
		for i := range values {
			dest = append(dest, &values[i])
		}
		if err := rows.Scan(dest...); err != nil {
			return Result{}, fmt.Errorf("metrics: scan: %w", err)
		}
		row := Row{Group: make(map[string]string, len(groups)), Count: count, Values: make(map[Metric]int64, len(values))}
		if bucket.Valid {
			t, err := time.Parse(time.RFC3339, bucket.String)
			if err != nil {
				return Result{}, fmt.Errorf("metrics: parse bucket %q: %w", bucket.String, err)
			}
			row.Bucket = t.UTC()
		}
		for i, d := range q.GroupBy {
			row.Group[string(d)] = groups[i].String
		}
		for i, a := range q.Aggregates {
			row.Values[a.Metric] = values[i]
		}
		res.Rows = append(res.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return Result{}, err
	}
	return res, nil
}

func sortedAttrPairs(m map[string]string) [][2]string {
	out := make([][2]string, 0, len(m))
	for k, v := range m {
		out = append(out, [2]string{k, v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	return out
}
