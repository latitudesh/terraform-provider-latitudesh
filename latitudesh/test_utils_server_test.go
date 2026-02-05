package latitudesh

import (
	"fmt"
	"testing"
)

func TestIsServersOutOfStockError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "out of stock 422",
			err:      fmt.Errorf("Status 422: SERVERS_OUT_OF_STOCK"),
			expected: true,
		},
		{
			name:     "out of stock unprocessable",
			err:      fmt.Errorf("unprocessable_entity: No stock availability"),
			expected: true,
		},
		{
			name:     "lowercase out of stock",
			err:      fmt.Errorf("422 servers_out_of_stock"),
			expected: true,
		},
		{
			name:     "mixed case error",
			err:      fmt.Errorf("Status 422: Servers are out of stock in this region"),
			expected: true,
		},
		{
			name:     "different 422 error",
			err:      fmt.Errorf("Status 422: INVALID_PARAMETER"),
			expected: false,
		},
		{
			name:     "500 error",
			err:      fmt.Errorf("Status 500: Internal Server Error"),
			expected: false,
		},
		{
			name:     "404 not found",
			err:      fmt.Errorf("Status 404: Not Found"),
			expected: false,
		},
		{
			name:     "no stock without 422",
			err:      fmt.Errorf("no stock available"),
			expected: false,
		},
		{
			name:     "unprocessable without stock keywords",
			err:      fmt.Errorf("Status 422: unprocessable entity - validation failed"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isServersOutOfStockError(tt.err)
			if result != tt.expected {
				t.Errorf("expected %v, got %v for error: %v",
					tt.expected, result, tt.err)
			}
		})
	}
}
