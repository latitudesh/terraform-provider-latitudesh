package components

type Widget struct {
	Data *WidgetData `json:"data,omitempty"`
}

type WidgetData struct {
	ID *string `json:"id,omitempty"`
	// Weight in grams.
	Weight *string `json:"weight,omitempty"`
	// The label shown on the invoice.
	Label  *string `json:"label,omitempty"`
	Status *string `json:"status,omitempty"`
	// Parent is a self-reference: the walk's cycle guard is what keeps this from
	// recursing forever.
	Parent   *WidgetData `json:"parent,omitempty"`
	BgpReady *bool       `json:"bgp_ready,omitempty"`
	Badge    *Badge      `json:"badge,omitempty"`
}

// Badge is a whole new model: it must surface as one model_added row, never as
// a per-field cascade of its own fields.
type Badge struct {
	Kind  *string `json:"kind,omitempty"`
	Score *int64  `json:"score,omitempty"`
}

// Pagination is embedded into WidgetList: the walk must record embedded fields
// under their promoted name and follow their type, or a change here would be
// invisible.
type Pagination struct {
	Page *int64 `json:"page,omitempty"`
}

type WidgetList struct {
	Pagination
	Data []WidgetData `json:"data,omitempty"`
}

type Gadget struct {
	ID *string `json:"id,omitempty"`
}

type HTTPMetadata struct {
	Response *http.Response `json:"-"`
}
