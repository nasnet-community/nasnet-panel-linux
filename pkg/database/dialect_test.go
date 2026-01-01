package database

import "testing"

// setDriver temporarily swaps the package-level DriverName so the dialect
// branches can be exercised without standing up a real DB connection.
func setDriver(t *testing.T, name string) {
	t.Helper()
	orig := DriverName
	DriverName = name
	t.Cleanup(func() { DriverName = orig })
}

func TestILike_Dialects(t *testing.T) {
	setDriver(t, "postgres")
	if got := ILike("u.name", "?"); got != "u.name ILIKE ?" {
		t.Errorf("postgres: %q", got)
	}
	setDriver(t, "sqlite")
	if got := ILike("u.name", "?"); got != "u.name LIKE ?" {
		t.Errorf("sqlite: %q", got)
	}
}

func TestNullsLast_Dialects(t *testing.T) {
	setDriver(t, "postgres")
	if got := NullsLast("created_at DESC"); got != "created_at DESC NULLS LAST" {
		t.Errorf("postgres: %q", got)
	}
	setDriver(t, "sqlite")
	got := NullsLast("created_at DESC")
	want := "CASE WHEN created_at IS NULL THEN 1 ELSE 0 END, created_at DESC"
	if got != want {
		t.Errorf("sqlite: %q, want %q", got, want)
	}
}

func TestSplitDirection(t *testing.T) {
	tests := []struct {
		in      string
		wantCol string
		wantDir string
	}{
		{"foo ASC", "foo", "ASC"},
		{"foo DESC", "foo", "DESC"},
		{"foo", "foo", "ASC"}, // default when no suffix
	}
	for _, tt := range tests {
		col, dir := splitDirection(tt.in)
		if col != tt.wantCol || dir != tt.wantDir {
			t.Errorf("splitDirection(%q) = (%q, %q), want (%q, %q)", tt.in, col, dir, tt.wantCol, tt.wantDir)
		}
	}
}

func TestCastFloat_Dialects(t *testing.T) {
	setDriver(t, "postgres")
	if got := CastFloat("x"); got != "x::float" {
		t.Errorf("postgres: %q", got)
	}
	setDriver(t, "sqlite")
	if got := CastFloat("x"); got != "CAST(x AS REAL)" {
		t.Errorf("sqlite: %q", got)
	}
}

func TestDateTruncMonth_Dialects(t *testing.T) {
	setDriver(t, "postgres")
	if got := DateTruncMonth(); got != "DATE_TRUNC('month', CURRENT_DATE)" {
		t.Errorf("postgres: %q", got)
	}
	setDriver(t, "sqlite")
	if got := DateTruncMonth(); got != "DATE(CURRENT_DATE, 'start of month')" {
		t.Errorf("sqlite: %q", got)
	}
}

func TestJSONArrayElements_Dialects(t *testing.T) {
	setDriver(t, "postgres")
	from, val := JSONArrayElements("col")
	if from != "jsonb_array_elements_text(col) as elem" || val != "elem" {
		t.Errorf("postgres: from=%q val=%q", from, val)
	}
	setDriver(t, "sqlite")
	from, val = JSONArrayElements("col")
	if from != "json_each(col) as elem" || val != "elem.value" {
		t.Errorf("sqlite: from=%q val=%q", from, val)
	}
}

func TestJSONContains_Dialects(t *testing.T) {
	setDriver(t, "postgres")
	if got := JSONContains("col", "?"); got != "col @> ?" {
		t.Errorf("postgres: %q", got)
	}
	setDriver(t, "sqlite")
	want := "EXISTS (SELECT 1 FROM json_each(col) WHERE json_each.value = ?)"
	if got := JSONContains("col", "?"); got != want {
		t.Errorf("sqlite: %q", got)
	}
}

func TestIntervalDays_Dialects(t *testing.T) {
	setDriver(t, "postgres")
	if got := IntervalDays("3"); got != "INTERVAL '1 day' * 3" {
		t.Errorf("postgres: %q", got)
	}
	setDriver(t, "sqlite")
	if got := IntervalDays("3"); got != "(3 || ' days')" {
		t.Errorf("sqlite: %q", got)
	}
}

func TestAddInterval_Dialects(t *testing.T) {
	setDriver(t, "postgres")
	if got := AddInterval("c", "5"); got != "c + INTERVAL '1 day' * 5" {
		t.Errorf("postgres: %q", got)
	}
	setDriver(t, "sqlite")
	if got := AddInterval("c", "5"); got != "datetime(c, '+' || 5 || ' days')" {
		t.Errorf("sqlite: %q", got)
	}
}

// AddIntervalConditional just wraps AddInterval in a CASE; check the structure
// stays intact across dialects.
func TestAddIntervalConditional(t *testing.T) {
	setDriver(t, "sqlite")
	got := AddIntervalConditional("cond", "c", "5", "fallback")
	want := "CASE WHEN cond THEN datetime(c, '+' || 5 || ' days') ELSE fallback END"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNowMinusInterval_Dialects(t *testing.T) {
	setDriver(t, "postgres")
	if got := NowMinusInterval(5, "minutes"); got != "NOW() - INTERVAL '5 minutes'" {
		t.Errorf("postgres: %q", got)
	}
	setDriver(t, "sqlite")
	if got := NowMinusInterval(5, "minutes"); got != "datetime('now', '-5 minutes')" {
		t.Errorf("sqlite: %q", got)
	}
}

func TestNow_Dialects(t *testing.T) {
	setDriver(t, "postgres")
	if got := Now(); got != "NOW()" {
		t.Errorf("postgres: %q", got)
	}
	setDriver(t, "sqlite")
	if got := Now(); got != "datetime('now')" {
		t.Errorf("sqlite: %q", got)
	}
}

func TestCountFilter_Dialects(t *testing.T) {
	setDriver(t, "postgres")
	if got := CountFilter("x > 0"); got != "COUNT(*) FILTER (WHERE x > 0)" {
		t.Errorf("postgres: %q", got)
	}
	setDriver(t, "sqlite")
	if got := CountFilter("x > 0"); got != "SUM(CASE WHEN x > 0 THEN 1 ELSE 0 END)" {
		t.Errorf("sqlite: %q", got)
	}
}

func TestExtractHour_Dialects(t *testing.T) {
	setDriver(t, "postgres")
	if got := ExtractHour("ts"); got != "CAST(EXTRACT(HOUR FROM ts) AS INTEGER)" {
		t.Errorf("postgres: %q", got)
	}
	setDriver(t, "sqlite")
	if got := ExtractHour("ts"); got != "CAST(strftime('%H', ts) AS INTEGER)" {
		t.Errorf("sqlite: %q", got)
	}
}

func TestTruncateTime(t *testing.T) {
	setDriver(t, "postgres")
	if got := TruncateTime("ts", "day"); got != "DATE_TRUNC('day', ts)" {
		t.Errorf("postgres day: %q", got)
	}
	if got := TruncateTime("ts", "hour"); got != "DATE_TRUNC('hour', ts)" {
		t.Errorf("postgres hour: %q", got)
	}
	// Unknown granularity falls back to "hour".
	if got := TruncateTime("ts", "garbage"); got != "DATE_TRUNC('hour', ts)" {
		t.Errorf("postgres fallback: %q", got)
	}

	setDriver(t, "sqlite")
	if got := TruncateTime("ts", "day"); got != "date(ts)" {
		t.Errorf("sqlite day: %q", got)
	}
	if got := TruncateTime("ts", "hour"); got != "strftime('%Y-%m-%d %H:00:00', ts)" {
		t.Errorf("sqlite hour: %q", got)
	}
}
