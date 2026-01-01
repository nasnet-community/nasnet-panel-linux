package database

import (
	"fmt"
	"strings"
)

// ILike returns a cross-DB case-insensitive LIKE clause.
// PostgreSQL uses ILIKE; SQLite's LIKE is already case-insensitive for ASCII.
func ILike(column, placeholder string) string {
	if IsPostgres() {
		return fmt.Sprintf("%s ILIKE %s", column, placeholder)
	}
	return fmt.Sprintf("%s LIKE %s", column, placeholder)
}

// NullsLast returns a cross-DB ORDER BY clause that sorts NULLs last.
// PostgreSQL supports "NULLS LAST" natively; for SQLite we use a CASE expression.
// expr may include a direction suffix like "column DESC".
func NullsLast(expr string) string {
	if IsPostgres() {
		return expr + " NULLS LAST"
	}
	// Split off trailing ASC/DESC so the CASE WHEN only uses the column expression.
	col, dir := splitDirection(expr)
	return fmt.Sprintf("CASE WHEN %s IS NULL THEN 1 ELSE 0 END, %s %s", col, col, dir)
}

// splitDirection splits "expr ASC" or "expr DESC" into the column expression and direction.
func splitDirection(expr string) (column, direction string) {
	upper := strings.ToUpper(strings.TrimSpace(expr))
	if strings.HasSuffix(upper, " ASC") {
		return strings.TrimSpace(expr[:len(expr)-4]), "ASC"
	}
	if strings.HasSuffix(upper, " DESC") {
		return strings.TrimSpace(expr[:len(expr)-5]), "DESC"
	}
	return expr, "ASC"
}

// CastFloat returns a cross-DB expression to cast a column to a float.
// PostgreSQL uses ::float; SQLite uses CAST(x AS REAL).
func CastFloat(expr string) string {
	if IsPostgres() {
		return expr + "::float"
	}
	return fmt.Sprintf("CAST(%s AS REAL)", expr)
}

// DateTruncMonth returns an expression for the first day of the current month.
// PostgreSQL: DATE_TRUNC('month', CURRENT_DATE)
// SQLite: DATE(CURRENT_DATE, 'start of month')
func DateTruncMonth() string {
	if IsPostgres() {
		return "DATE_TRUNC('month', CURRENT_DATE)"
	}
	return "DATE(CURRENT_DATE, 'start of month')"
}

// JSONArrayElements returns a SQL fragment to extract text elements from a JSON array column.
// PostgreSQL: jsonb_array_elements_text(column)
// SQLite: json_each(column) with value
func JSONArrayElements(column string) (fromClause, valueExpr string) {
	if IsPostgres() {
		return fmt.Sprintf("jsonb_array_elements_text(%s) as elem", column), "elem"
	}
	return fmt.Sprintf("json_each(%s) as elem", column), "elem.value"
}

// JSONContains returns a cross-DB expression for checking if a JSON array contains a value.
// Uses a placeholder (?) for safe parameterized queries — caller must pass the value as a bind arg.
// PostgreSQL: column @> ? (caller passes JSON value, e.g. `'"somevalue"'`)
// SQLite: EXISTS (SELECT 1 FROM json_each(column) WHERE json_each.value = ?)
func JSONContains(column, placeholder string) string {
	if IsPostgres() {
		return fmt.Sprintf("%s @> %s", column, placeholder)
	}
	return fmt.Sprintf("EXISTS (SELECT 1 FROM json_each(%s) WHERE json_each.value = %s)", column, placeholder)
}

// Interval returns a cross-DB interval expression.
// PostgreSQL: INTERVAL '1 day' * n
// SQLite: (n * 86400) seconds, expressed for datetime functions
func IntervalDays(n string) string {
	if IsPostgres() {
		return fmt.Sprintf("INTERVAL '1 day' * %s", n)
	}
	// SQLite: use the || operator with datetime modifiers
	return fmt.Sprintf("(%s || ' days')", n)
}

// AddInterval returns a cross-DB expression for adding days to a date column.
// PostgreSQL: column + INTERVAL '1 day' * n
// SQLite: datetime(column, '+' || n || ' days')
func AddInterval(column, n string) string {
	if IsPostgres() {
		return fmt.Sprintf("%s + INTERVAL '1 day' * %s", column, n)
	}
	return fmt.Sprintf("datetime(%s, '+' || %s || ' days')", column, n)
}

// AddIntervalConditional returns a CASE WHEN expression that adds days only when condition is true.
// PostgreSQL: CASE WHEN cond THEN col + INTERVAL '1 day' * n ELSE fallback END
// SQLite: CASE WHEN cond THEN datetime(col, '+' || n || ' days') ELSE fallback END
func AddIntervalConditional(condition, column, n, fallback string) string {
	return fmt.Sprintf("CASE WHEN %s THEN %s ELSE %s END", condition, AddInterval(column, n), fallback)
}

// NowInterval returns a cross-DB expression for "NOW() - INTERVAL '...'".
// PostgreSQL: NOW() - INTERVAL 'N unit'
// SQLite: datetime('now', '-N unit')
func NowMinusInterval(value int, unit string) string {
	if IsPostgres() {
		return fmt.Sprintf("NOW() - INTERVAL '%d %s'", value, unit)
	}
	return fmt.Sprintf("datetime('now', '-%d %s')", value, unit)
}

// Now returns the cross-DB expression for the current timestamp.
func Now() string {
	if IsPostgres() {
		return "NOW()"
	}
	return "datetime('now')"
}

// CountFilter returns a cross-DB conditional count.
// PostgreSQL: COUNT(*) FILTER (WHERE condition)
// SQLite: SUM(CASE WHEN condition THEN 1 ELSE 0 END)
func CountFilter(condition string) string {
	if IsPostgres() {
		return fmt.Sprintf("COUNT(*) FILTER (WHERE %s)", condition)
	}
	return fmt.Sprintf("SUM(CASE WHEN %s THEN 1 ELSE 0 END)", condition)
}

// ExtractHour returns a cross-DB expression to extract the hour (0-23) from a timestamp column.
// PostgreSQL: CAST(EXTRACT(HOUR FROM column) AS INTEGER)
// SQLite: CAST(strftime('%H', column) AS INTEGER)
func ExtractHour(column string) string {
	if IsPostgres() {
		return fmt.Sprintf("CAST(EXTRACT(HOUR FROM %s) AS INTEGER)", column)
	}
	return fmt.Sprintf("CAST(strftime('%%H', %s) AS INTEGER)", column)
}

// TruncateTime: cross-dialect hour/day bucket for time-series GROUP BY.
// Unknown granularity falls back to "hour".
func TruncateTime(column, granularity string) string {
	if granularity == "day" {
		if IsPostgres() {
			return fmt.Sprintf("DATE_TRUNC('day', %s)", column)
		}
		return fmt.Sprintf("date(%s)", column)
	}
	if IsPostgres() {
		return fmt.Sprintf("DATE_TRUNC('hour', %s)", column)
	}
	return fmt.Sprintf("strftime('%%Y-%%m-%%d %%H:00:00', %s)", column)
}
