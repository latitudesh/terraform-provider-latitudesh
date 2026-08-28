package components

type Widget struct {
	Data *WidgetData `json:"data,omitempty"`
}

type WidgetData struct {
	ID *string `json:"id,omitempty"`
	// Weight in grams.
	Weight *int64 `json:"weight,omitempty"`
	// The label shown in the dashboard.
	Label  *string `json:"label,omitempty"`
	Status string  `json:"status"`
	// Parent is a self-reference: the walk's cycle guard is what keeps this from
	// recursing forever.
	Parent *WidgetData `json:"parent,omitempty"`
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
	ID   *string `json:"id,omitempty"`
	Knob *string `json:"knob,omitempty"`
}

type HTTPMetadata struct {
	Response *http.Response `json:"-"`
}
