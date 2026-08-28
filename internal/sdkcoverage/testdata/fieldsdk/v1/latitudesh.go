// Package fieldsdk is a frozen mini-SDK for exercising the field-level parser.
// It is parsed, never compiled, so unresolved identifiers are fine. Its sibling
// v2 differs by exactly one instance of each drift kind — the pair is the
// fixture for drift_test.go, so a change here ripples there.
package fieldsdk

type Latitudesh struct {
	Widgets *Widgets
	Gadgets *Gadgets

	SDKVersion string
}

type Widgets struct{}

// List returns every widget.
func (s *Widgets) List(ctx context.Context, request operations.ListWidgetsRequest, opts ...operations.Option) (*operations.ListWidgetsResponse, error) {
	return nil, nil
}

// Create makes a widget.
func (s *Widgets) Create(ctx context.Context, request operations.CreateWidgetWidgetsRequestBody, opts ...operations.Option) (*operations.CreateWidgetResponse, error) {
	return nil, nil
}

// Get fetches one widget.
func (s *Widgets) Get(ctx context.Context, widgetID string, opts ...operations.Option) (*operations.GetWidgetResponse, error) {
	return nil, nil
}

// Delete removes a widget.
func (s *Widgets) Delete(ctx context.Context, widgetID string, opts ...operations.Option) (*operations.DeleteWidgetResponse, error) {
	return nil, nil
}

// Legacy is an old endpoint.
func (s *Widgets) Legacy(ctx context.Context, opts ...operations.Option) (*operations.LegacyWidgetResponse, error) {
	return nil, nil
}

// Gadgets exists to prove the walk is covered-groups-only: v2 mutates its
// models too, and none of that may drift.
type Gadgets struct{}

// Get fetches one gadget.
func (s *Gadgets) Get(ctx context.Context, gadgetID string, opts ...operations.Option) (*operations.GetGadgetResponse, error) {
	return nil, nil
}
