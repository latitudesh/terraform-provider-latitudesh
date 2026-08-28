package operations

type GetGadgetResponse struct {
	HTTPMeta components.HTTPMetadata `json:"-"`
	// OK
	Gadget *components.Gadget
}
