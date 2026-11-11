package latitudesh

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
)

// apiNotFoundMarkers are the values a JSON:API error uses to say "gone".
//
// JSON:API specifies `status` as the HTTP status code as a string, and the
// SDK's own examples use "404" — but the live API renders it in snake_case
// ("not_found"), with the code alongside it ("NOT_FOUND"). Matching only "404"
// meant a destroy polled an LKS cluster that was already deleted, got the
// not-found it was waiting for, and reported it as an unexpected client error
// (observed live 2026-09-18). Both spellings count, on either field, compared
// lower-cased and trimmed.
var apiNotFoundMarkers = map[string]bool{
	"404":       true,
	"not_found": true,
	"notfound":  true,
}

// apiNotFound reports whether err says the object is gone: an
// *components.APIError with status 404, or a *components.ErrorObject carrying
// a not-found marker. A spelling it fails to recognize turns "already deleted"
// into a failed apply.
//
// Only not-found counts: a 403 or a 409 is a real error and must never be
// read as "gone".
func apiNotFound(err error) bool {
	var apiErr *components.APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == http.StatusNotFound
	}

	var errObj *components.ErrorObject
	if errors.As(err, &errObj) {
		for _, e := range errObj.Errors {
			if e.Status != nil && apiNotFoundMarkers[strings.ToLower(strings.TrimSpace(*e.Status))] {
				return true
			}
			if e.Code != nil && apiNotFoundMarkers[strings.ToLower(strings.TrimSpace(*e.Code))] {
				return true
			}
		}
	}

	return false
}

// resolveProjectID turns a `project` selector (an ID or a slug) into the
// project ID that create payloads' project_id takes. Some create endpoints
// accept only an ID: POST /public_networks answers a slug with 404 "Specified
// Record Not Found" (verified live 2026-09-11). So every create resolves
// conservatively. A selector that
// already carries the `proj_` prefix is returned unchanged; anything else is
// looked up with GetProject, which accepts either. Callers keep the configured
// selector in state.
func resolveProjectID(ctx context.Context, client *latitudeshgosdk.Latitudesh, selector string) (string, error) {
	if strings.HasPrefix(selector, "proj_") {
		return selector, nil
	}

	res, err := client.Projects.GetProject(ctx, selector)
	if err != nil {
		return "", err
	}
	if res == nil || res.Object == nil || res.Object.Data == nil || res.Object.Data.ID == nil || *res.Object.Data.ID == "" {
		return "", fmt.Errorf("project %q not found", selector)
	}
	return *res.Object.Data.ID, nil
}

// nullCRUDTimeouts is a typed null for a `timeouts` attribute with create,
// update and delete. Import sets no timeouts block, and the zero value of
// timeouts.Value is a null object with no attribute types, which fails state
// conversion ("Object[]" vs "Object[create:String, update:String,
// delete:String]").
func nullCRUDTimeouts() timeouts.Value {
	return timeouts.Value{
		Object: types.ObjectNull(map[string]attr.Type{
			"create": types.StringType,
			"update": types.StringType,
			"delete": types.StringType,
		}),
	}
}
