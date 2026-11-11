package latitudesh

import (
	"errors"
	"fmt"
	"testing"

	"github.com/latitudesh/latitudesh-go-sdk/models/components"
)

func TestAPINotFound(t *testing.T) {
	str := func(v string) *string { return &v }

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"generic 404", &components.APIError{StatusCode: 404}, true},
		{"generic 403", &components.APIError{StatusCode: 403}, false},
		{"generic 500", &components.APIError{StatusCode: 500}, false},

		// The numeric spelling JSON:API prescribes, and what the mocks send.
		{"jsonapi 404", &components.ErrorObject{Errors: []components.Errors{{Status: str("404")}}}, true},

		// What the live API actually sent on a destroy, verbatim (2026-09-18).
		// Matching only "404" made destroy fail on a cluster it had just
		// successfully deleted.
		{"jsonapi not_found", &components.ErrorObject{Errors: []components.Errors{{
			Code: str("NOT_FOUND"), Status: str("not_found"),
			Title: str("Cluster Not Found"), Detail: str("Cluster 'lksc_87c0ff4d8d024f' not found"),
		}}}, true},

		// The code alone is enough, in case status is omitted.
		{"code only", &components.ErrorObject{Errors: []components.Errors{{Code: str("NOT_FOUND")}}}, true},
		{"mixed case", &components.ErrorObject{Errors: []components.Errors{{Status: str("Not_Found")}}}, true},

		// Anything else is a real error and must not be swallowed as "gone" —
		// especially NOT_READY, which is a 409 the kubeconfig endpoint returns
		// and which looks superficially similar.
		{"jsonapi 403", &components.ErrorObject{Errors: []components.Errors{{Status: str("403")}}}, false},
		{"jsonapi not_ready", &components.ErrorObject{Errors: []components.Errors{{
			Code: str("NOT_READY"), Status: str("conflict"),
		}}}, false},
		{"jsonapi 422", &components.ErrorObject{Errors: []components.Errors{{Status: str("unprocessable_entity")}}}, false},
		{"empty", &components.ErrorObject{}, false},
		{"generic 409", &components.APIError{StatusCode: 409}, false},

		// Never classified by message text: a plain error that merely
		// mentions 404 is not a not-found.
		{"plain error", errors.New("404 not_found"), false},

		// Wrapped errors are unwrapped.
		{"wrapped jsonapi 404", fmt.Errorf("get failed: %w", &components.ErrorObject{Errors: []components.Errors{{Status: str("404")}}}), true},
		{"wrapped generic 404", fmt.Errorf("get failed: %w", &components.APIError{StatusCode: 404}), true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := apiNotFound(tc.err); got != tc.want {
				t.Errorf("apiNotFound(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
