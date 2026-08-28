// Package fieldsdk (v2) is v1 after one instance of each drift kind:
//
//	method_removed            Widgets.Delete is gone
//	method_signature_changed  Widgets.List gained a filterTag parameter
//	method_added              Widgets.Update is new
//	deprecated                Widgets.Legacy gained a Deprecated: marker
//	enum_value_removed/added  WidgetColor renamed "blue" to "green"
//	default_changed           ListWidgetsRequest.PageSize default 20 -> 50
//	field_removed             CreateWidgetWidgetsRequestBody.Size is gone
//	field_added               WidgetData gained bgp_ready and badge
//	model_added               components.Badge and operations.UpdateWidgetResponse are new
//	model_removed             operations.DeleteWidgetResponse is gone
//	field_type_changed        WidgetData.Weight *int64 -> *string
//	field_required_changed    WidgetData.Status string -> *string omitempty
//	doc_changed               WidgetData.Label's doc comment changed
//
// Gadgets mutates too (Gadget.Knob is gone), and being uncovered must produce
// no drift at all.
package fieldsdk

type Latitudesh struct {
	Widgets *Widgets
	Gadgets *Gadgets

	SDKVersion string
}

type Widgets struct{}

// List returns every widget.
func (s *Widgets) List(ctx context.Context, request operations.ListWidgetsRequest, filterTag *string, opts ...operations.Option) (*operations.ListWidgetsResponse, error) {
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

// Update changes a widget.
func (s *Widgets) Update(ctx context.Context, widgetID string, requestBody operations.CreateWidgetWidgetsRequestBody, opts ...operations.Option) (*operations.UpdateWidgetResponse, error) {
	return nil, nil
}

// Legacy is an old endpoint.
//
// Deprecated: use List instead.
func (s *Widgets) Legacy(ctx context.Context, opts ...operations.Option) (*operations.LegacyWidgetResponse, error) {
	return nil, nil
}

// Gadgets exists to prove the walk is covered-groups-only: its models mutated
// here in v2, and none of that may drift.
type Gadgets struct{}

// Get fetches one gadget.
func (s *Gadgets) Get(ctx context.Context, gadgetID string, opts ...operations.Option) (*operations.GetGadgetResponse, error) {
	return nil, nil
}
