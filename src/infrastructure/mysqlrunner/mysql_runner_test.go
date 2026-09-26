package mysqlrunner

import (
	"testing"

	"github.com/samuelloza/isolate-wrapper/src/domain"
)

func cell(value string) *string { return &value }

func TestEqualResultSetsHonorsRowOrder(t *testing.T) {
	expected := domain.SQLResultSet{Columns: []string{"id"}, Rows: [][]*string{{cell("1")}, {cell("2")}}}
	actual := domain.SQLResultSet{Columns: []string{"id"}, Rows: [][]*string{{cell("2")}, {cell("1")}}}

	if EqualResultSets(expected, actual, true) {
		t.Fatal("ordered results must not accept a different row order")
	}
	if !EqualResultSets(expected, actual, false) {
		t.Fatal("unordered results must accept the same row set")
	}
}

func TestEqualResultSetsPreservesNull(t *testing.T) {
	expected := domain.SQLResultSet{Columns: []string{"value"}, Rows: [][]*string{{nil}}}
	actualNull := domain.SQLResultSet{Columns: []string{"VALUE"}, Rows: [][]*string{{nil}}}
	actualEmpty := domain.SQLResultSet{Columns: []string{"value"}, Rows: [][]*string{{cell("")}}}

	if !EqualResultSets(expected, actualNull, true) {
		t.Fatal("NULL values and case-insensitive column names should match")
	}
	if EqualResultSets(expected, actualEmpty, true) {
		t.Fatal("NULL and an empty string must be different")
	}
}

func TestNormalizeMaxRowsUsesSafeBounds(t *testing.T) {
	if normalizeMaxRows(0) != defaultMaxRows {
		t.Fatal("zero must use the default")
	}
	if normalizeMaxRows(maxAllowedRows+1) != maxAllowedRows {
		t.Fatal("maximum row count must be capped")
	}
}
