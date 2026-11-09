package api

import (
	"reflect"
	"testing"
)

func TestUpdateSetReturnsNextPlaceholder(t *testing.T) {
	tests := []struct {
		name      string
		values    map[string]any
		order     []string
		wantSet   []string
		wantArgs  []any
		wantIndex int
	}{
		{
			name:      "semua kolom",
			values:    map[string]any{"a": 1, "b": 2},
			order:     []string{"a", "b"},
			wantSet:   []string{"a = $1", "b = $2"},
			wantArgs:  []any{1, 2},
			wantIndex: 3,
		},
		{
			name:      "sebagian kolom",
			values:    map[string]any{"b": 2},
			order:     []string{"a", "b"},
			wantSet:   []string{"b = $1"},
			wantArgs:  []any{2},
			wantIndex: 2,
		},
		{
			name:      "tanpa kolom",
			values:    map[string]any{},
			order:     []string{"a"},
			wantSet:   []string{},
			wantArgs:  []any{},
			wantIndex: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			set, args, index := updateSet(test.values, test.order)
			if !reflect.DeepEqual(set, test.wantSet) {
				t.Fatalf("set = %v, want %v", set, test.wantSet)
			}
			if !reflect.DeepEqual(args, test.wantArgs) {
				t.Fatalf("args = %v, want %v", args, test.wantArgs)
			}
			if index != test.wantIndex {
				t.Fatalf("index = %d, want %d (placeholder untuk id)", index, test.wantIndex)
			}
		})
	}
}
