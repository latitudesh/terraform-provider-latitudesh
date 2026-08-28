package operations

type WidgetColor string

const (
	WidgetColorRed  WidgetColor = "red"
	WidgetColorBlue WidgetColor = "blue"
)

type ListWidgetsRequest struct {
	// The widget name to filter by
	FilterName *string `queryParam:"style=form,explode=true,name=filter[name]"`
	// Number of items to return per page
	PageSize *int64 `default:"20" queryParam:"style=form,explode=true,name=page[size]"`
}

type ListWidgetsResponse struct {
	HTTPMeta components.HTTPMetadata `json:"-"`
	// OK
	WidgetList *components.WidgetList
	Next       func() (*ListWidgetsResponse, error)
}

type CreateWidgetWidgetsRequestBody struct {
	Name  string       `json:"name"`
	Color *WidgetColor `json:"color,omitempty"`
	Size  *string      `json:"size,omitempty"`
}

type CreateWidgetResponse struct {
	HTTPMeta components.HTTPMetadata `json:"-"`
	// Created
	Widget *components.Widget
}

type GetWidgetResponse struct {
	HTTPMeta components.HTTPMetadata `json:"-"`
	// OK
	Widget *components.Widget
}

type DeleteWidgetResponse struct {
	HTTPMeta components.HTTPMetadata `json:"-"`
}

type LegacyWidgetResponse struct {
	HTTPMeta components.HTTPMetadata `json:"-"`
	// OK
	Widget *components.Widget
}
