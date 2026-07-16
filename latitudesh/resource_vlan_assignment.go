package latitudesh

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
	"github.com/latitudesh/latitudesh-go-sdk/models/operations"
	iprovider "github.com/latitudesh/terraform-provider-latitudesh/v2/internal/provider"
)

var _ resource.Resource = &VlanAssignmentResource{}
var _ resource.ResourceWithImportState = &VlanAssignmentResource{}

func NewVlanAssignmentResource() resource.Resource {
	return &VlanAssignmentResource{}
}

type VlanAssignmentResource struct {
	client *latitudeshgosdk.Latitudesh

	// connectTimeout and connectPollInterval bound the post-create wait for
	// the assignment to reach status "connected". Zero values fall back to the
	// operator-configured timeouts { create } block (or the production defaults
	// below); tests override them to keep runs fast.
	connectTimeout      time.Duration
	connectPollInterval time.Duration
}

// statusConnected is the terminal status a virtual network assignment reaches
// once provisioning has completed. Until then it sits at
// "connecting"; if that never completes the assignment never appears in the
// console (see issue #190).
const statusConnected = "connected"

const (
	defaultConnectTimeout      = 2 * time.Minute
	defaultConnectPollInterval = 3 * time.Second
)

type VlanAssignmentResourceModel struct {
	ID               types.String   `tfsdk:"id"`
	ServerID         types.String   `tfsdk:"server_id"`
	VirtualNetworkID types.String   `tfsdk:"virtual_network_id"`
	Vid              types.Int64    `tfsdk:"vid"`
	Description      types.String   `tfsdk:"description"`
	Status           types.String   `tfsdk:"status"`
	Timeouts         timeouts.Value `tfsdk:"timeouts"`
}

func (r *VlanAssignmentResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vlan_assignment"
}

func (r *VlanAssignmentResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "VLAN Assignment resource",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "VLAN Assignment identifier",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"server_id": schema.StringAttribute{
				MarkdownDescription: "The server ID to assign to the virtual network",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"virtual_network_id": schema.StringAttribute{
				MarkdownDescription: "The virtual network ID to assign the server to",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			// Computed attributes
			"vid": schema.Int64Attribute{
				MarkdownDescription: "VLAN ID of the virtual network",
				Computed:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Description of the virtual network",
				Computed:            true,
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "Status of the assignment",
				Computed:            true,
			},
		},
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{
				Create: true,
			}),
		},
	}
}

func (r *VlanAssignmentResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	deps := iprovider.ConfigureFromProviderData(req.ProviderData, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	r.client = deps.Client
}

func (r *VlanAssignmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data VlanAssignmentResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	serverID := data.ServerID.ValueString()
	vnetID := data.VirtualNetworkID.ValueString()

	// Create the assignment request
	attrs := &operations.AssignServerVirtualNetworkPrivateNetworksAttributes{
		ServerID:         serverID,
		VirtualNetworkID: vnetID,
	}

	createRequest := operations.AssignServerVirtualNetworkPrivateNetworksRequestBody{
		Data: &operations.AssignServerVirtualNetworkPrivateNetworksData{
			Type:       operations.AssignServerVirtualNetworkPrivateNetworksTypeVirtualNetworkAssignment,
			Attributes: attrs,
		},
	}

	result, err := r.client.PrivateNetworks.Assign(ctx, createRequest)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to create virtual network assignment, got error: "+err.Error())
		return
	}

	// Check for successful status codes
	if result != nil {
		httpStatus := result.HTTPMeta.Response.StatusCode
		if httpStatus != 200 && httpStatus != 201 {
			resp.Diagnostics.AddError("API Error", fmt.Sprintf("Create virtual network assignment returned unexpected status code: %d", httpStatus))
			return
		}
	}

	// Try to get ID from response first
	if result.VirtualNetworkAssignment != nil && result.VirtualNetworkAssignment.Data != nil && result.VirtualNetworkAssignment.Data.ID != nil {
		data.ID = types.StringValue(*result.VirtualNetworkAssignment.Data.ID)
	} else {
		// If we can't get ID from response, use List endpoint to find it

		// Wait a moment for the assignment to be processed
		time.Sleep(2 * time.Second)

		// Use List endpoint to find the assignment we just created
		r.findAssignmentByServerAndVNet(ctx, &data, &resp.Diagnostics)
		if resp.Diagnostics.HasError() {
			return
		}

		if data.ID.IsNull() || data.ID.ValueString() == "" {
			resp.Diagnostics.AddError("API Error", "Virtual network assignment was created but could not determine its ID")
			return
		}
	}

	// Resolve the connect-wait deadline. Tests pin r.connectTimeout to keep runs
	// fast; otherwise honor the operator-configured timeouts { create } block,
	// falling back to defaultConnectTimeout when it's unset.
	connectTimeout := r.connectTimeout
	if connectTimeout <= 0 {
		var timeoutDiags diag.Diagnostics
		connectTimeout, timeoutDiags = data.Timeouts.Create(ctx, defaultConnectTimeout)
		resp.Diagnostics.Append(timeoutDiags...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	// Wait for the assignment to actually reach "connected" and populate all
	// attributes from the connected assignment. A bare 201 only means the
	// request was accepted; provisioning happens asynchronously and
	// may never complete. Reporting success here without waiting is the
	// false-success gap in issue #190 (Terraform shows the VLAN assigned while
	// the console shows nothing).
	persist := r.waitForAssignmentConnected(ctx, &data, connectTimeout, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		if persist {
			// The assignment never connected and couldn't be rolled back, so it
			// still exists remotely. Save it to state so Terraform tracks it and
			// replaces it on the next apply (create-with-error taints it) rather
			// than leaking it or creating a duplicate.
			resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
		}
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *VlanAssignmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data VlanAssignmentResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.readVlanAssignment(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *VlanAssignmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// VLAN assignments cannot be updated, they must be replaced
	resp.Diagnostics.AddError("Update Not Supported", "VLAN assignments cannot be updated, they must be replaced.")
}

func (r *VlanAssignmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data VlanAssignmentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := data.ID.ValueString()

	result, err := r.client.PrivateNetworks.DeleteAssignment(ctx, id)
	if err != nil && err.Error() != "{}" {
		if strings.Contains(err.Error(), "404") {
			return
		}
		resp.Diagnostics.AddError("Client Error", "Unable to delete virtual network assignment, got error: "+err.Error())
		return
	}

	if result != nil && result.HTTPMeta.Response != nil {
		code := result.HTTPMeta.Response.StatusCode
		if code == 404 {
			return
		}
		if code >= 400 {
			resp.Diagnostics.AddError("Client Error",
				fmt.Sprintf("Unable to delete virtual network assignment, status code: %d", code))
			return
		}
	}

	r.waitForAssignmentRemoval(ctx, id, data.VirtualNetworkID.ValueString(), data.ServerID.ValueString())
}

// waitForAssignmentRemoval polls ListAssignments filtered by both the vnet and
// the server IDs — the tuple is unique in interface_tagged_vlans, so the
// response holds at most one entry and fits on the first page regardless of
// account size. Without the filters, `ListAssignments` returns page 1 of all
// assignments in the account (page size default 20) and the lookup silently
// no-ops when the target lives on page 2+.
func (r *VlanAssignmentResource) waitForAssignmentRemoval(ctx context.Context, id, vnetID, serverID string) {
	const (
		waitDeadline = 2 * time.Minute
		pollInterval = 3 * time.Second
	)
	deadline := time.Now().Add(waitDeadline)

	for time.Now().Before(deadline) {
		response, err := r.client.PrivateNetworks.ListAssignments(ctx, operations.GetVirtualNetworksAssignmentsRequest{
			FilterVirtualNetworkID: &vnetID,
			FilterServer:           &serverID,
		})
		if err != nil {
			// Transient list failures shouldn't fail the destroy; let the
			// parent vnet delete handle any remaining lag.
			return
		}
		if response.VirtualNetworkAssignments == nil || response.VirtualNetworkAssignments.Data == nil {
			return
		}

		stillPresent := false
		for _, a := range response.VirtualNetworkAssignments.Data {
			if a.ID != nil && *a.ID == id {
				stillPresent = true
				break
			}
		}
		if !stillPresent {
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(pollInterval):
		}
	}
}

// waitForAssignmentConnected polls the just-created assignment until it
// reaches status "connected", then populates data from it. If the deadline
// passes while the assignment is still "connecting" (or has not surfaced yet),
// it fails the apply with an actionable diagnostic instead of silently
// recording a phantom assignment. Reuses findAssignmentByID so the lookup is
// filtered by server and survives pagination.
// waitForAssignmentConnected returns whether the caller should persist the
// assignment to state. It returns false on success (the caller saves the
// connected assignment normally) and false when a non-connected assignment was
// successfully rolled back (nothing to track). It returns true only when the
// assignment never connected AND could not be rolled back — then it populates
// data so the caller can save it to state, keeping the still-existing remote
// assignment tracked (and tainted) for reconciliation on the next apply.
func (r *VlanAssignmentResource) waitForAssignmentConnected(ctx context.Context, data *VlanAssignmentResourceModel, timeout time.Duration, diags *diag.Diagnostics) (persist bool) {
	if timeout <= 0 {
		timeout = defaultConnectTimeout
	}
	pollInterval := r.connectPollInterval
	if pollInterval <= 0 {
		pollInterval = defaultConnectPollInterval
	}

	assignmentID := data.ID.ValueString()
	serverID := ""
	if !data.ServerID.IsNull() {
		serverID = data.ServerID.ValueString()
	}

	deadline := time.Now().Add(timeout)
	lastStatus := ""
	var lastAttrs *components.VirtualNetworkAssignmentDataAttributes

poll:
	for {
		assignment, err := r.findAssignmentByID(ctx, assignmentID, serverID)
		if err != nil {
			// A transient lookup error (429/503/network) must not abandon the
			// assignment we just created — keep polling within the deadline.
			lastStatus = fmt.Sprintf("lookup error: %s", err)
		} else if assignment != nil && assignment.Attributes != nil {
			lastAttrs = assignment.Attributes
			if lastAttrs.Status != nil {
				lastStatus = *lastAttrs.Status
			}
			if lastStatus == statusConnected {
				applyAssignmentAttributes(data, lastAttrs)
				return false
			}
		}

		if !time.Now().Add(pollInterval).Before(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			break poll
		case <-time.After(pollInterval):
		}
	}

	// Never reached "connected". Roll back the assignment the POST created so a
	// half-provisioned one isn't left unmanaged in the API.
	if r.rollbackAssignment(assignmentID) {
		diags.AddError("Assignment Not Connected",
			fmt.Sprintf("Virtual network assignment %q did not reach %q within %s (last status: %q). "+
				"The request was accepted but provisioning did not complete; the assignment "+
				"will not be visible in the console. Please retry again in a few moments.",
				assignmentID, statusConnected, timeout, statusOrUnknown(lastStatus)))
		return false
	}

	// Rollback failed — the assignment still exists remotely. Populate data from
	// the last read (or nulls) so the caller can save it to state; Terraform
	// then tracks it and replaces it on the next apply instead of leaking it.
	if lastAttrs != nil {
		applyAssignmentAttributes(data, lastAttrs)
	} else {
		data.Vid = types.Int64Null()
		data.Description = types.StringNull()
		data.Status = types.StringNull()
	}
	diags.AddError("Assignment Not Connected",
		fmt.Sprintf("Virtual network assignment %q did not reach %q within %s (last status: %q) and could "+
			"not be rolled back. It is now tracked in state and will be replaced on the next apply. "+
			"Please retry again in a few moments.",
			assignmentID, statusConnected, timeout, statusOrUnknown(lastStatus)))
	return true
}

// rollbackAssignment removes an assignment that Create could not bring to
// "connected", retrying transient failures within a bounded window. It runs on
// a fresh context (the caller's may be cancelled). Returns true if the
// assignment is gone (deleted, or already absent), false if it could not be
// removed — the caller then keeps it in state for reconciliation.
func (r *VlanAssignmentResource) rollbackAssignment(id string) bool {
	if id == "" {
		return true
	}

	interval := r.connectPollInterval
	if interval <= 0 {
		interval = time.Second
	}

	const attempts = 3
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			time.Sleep(interval)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		result, err := r.client.PrivateNetworks.DeleteAssignment(ctx, id)
		cancel()

		// A 404 means the assignment is already gone — treat as success.
		if err != nil && strings.Contains(err.Error(), "404") {
			return true
		}
		// A real transport/API error (the SDK reports an empty success body as
		// the literal "{}") — retry within the window.
		if err != nil && err.Error() != "{}" {
			continue
		}
		// No Go error: the SDK can still signal failure only in the HTTP status,
		// so validate it like the Delete path does before declaring success.
		if result != nil && result.HTTPMeta.Response != nil {
			code := result.HTTPMeta.Response.StatusCode
			if code == 404 {
				return true
			}
			if code >= 400 {
				continue
			}
		}
		return true
	}
	return false
}

func statusOrUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

// applyAssignmentAttributes copies the readable attributes of an assignment
// onto the resource model. Shared by the connected-wait and the read paths so
// the mapping stays in one place.
func applyAssignmentAttributes(data *VlanAssignmentResourceModel, attrs *components.VirtualNetworkAssignmentDataAttributes) {
	if attrs == nil {
		return
	}
	if attrs.Server != nil && attrs.Server.ID != nil {
		data.ServerID = types.StringValue(*attrs.Server.ID)
	}
	if attrs.VirtualNetworkID != nil {
		data.VirtualNetworkID = types.StringValue(*attrs.VirtualNetworkID)
	}
	if attrs.Vid != nil {
		data.Vid = types.Int64Value(*attrs.Vid)
	} else {
		data.Vid = types.Int64Null()
	}
	if attrs.Description != nil {
		data.Description = types.StringValue(*attrs.Description)
	} else {
		data.Description = types.StringNull()
	}
	if attrs.Status != nil {
		data.Status = types.StringValue(*attrs.Status)
	} else {
		data.Status = types.StringNull()
	}
}

func (r *VlanAssignmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	var data VlanAssignmentResourceModel

	// The documented import format is "<PROJECT_ID>:<VLAN_ASSIGNMENT_ID>"; a
	// bare assignment ID is accepted too. The project part is not needed for
	// the lookup, so only the assignment ID is kept.
	id := req.ID
	if idx := strings.LastIndex(id, ":"); idx >= 0 {
		id = id[idx+1:]
	}
	if id == "" {
		resp.Diagnostics.AddError("Invalid Import ID",
			fmt.Sprintf("expected \"<PROJECT_ID>:<VLAN_ASSIGNMENT_ID>\" or \"<VLAN_ASSIGNMENT_ID>\", got: %q", req.ID))
		return
	}
	data.ID = types.StringValue(id)

	// No timeouts block is set on import, so give the field an explicitly-typed
	// null. The zero value of timeouts.Value is a null object with no attribute
	// types, which fails state conversion ("Object[]" vs "Object[create:String]").
	data.Timeouts = timeouts.Value{
		Object: types.ObjectNull(map[string]attr.Type{"create": types.StringType}),
	}

	r.readVlanAssignment(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *VlanAssignmentResource) readVlanAssignment(ctx context.Context, data *VlanAssignmentResourceModel, diags *diag.Diagnostics) {
	assignmentID := data.ID.ValueString()

	// Initialize Computed attributes to Null. Plugin Framework requires every
	// Computed attribute to be Known (concrete value or explicit Null) after
	// Apply; if the API response is missing one of these fields we still want
	// a concrete Null in state rather than Unknown.
	data.Vid = types.Int64Null()
	data.Description = types.StringNull()
	data.Status = types.StringNull()

	serverID := ""
	if !data.ServerID.IsNull() {
		serverID = data.ServerID.ValueString()
	}

	// The API populates `vid` asynchronously when assignments are created in
	// parallel (NetBox `get_next_vid` allocation), and a just-created
	// assignment may take a moment to show up in the list. Retry the lookup a
	// few times so we don't surface Null and trigger a phantom diff on refresh.
	const vidRetryAttempts = 3
	found := false
	for attempt := 0; attempt < vidRetryAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(time.Second):
			case <-ctx.Done():
				return
			}
		}

		assignment, err := r.findAssignmentByID(ctx, assignmentID, serverID)
		if err != nil {
			diags.AddError("Client Error", "Unable to read virtual network assignments, got error: "+err.Error())
			return
		}

		if assignment == nil {
			// Might be eventual consistency right after create — retry.
			continue
		}
		found = true

		applyAssignmentAttributes(data, assignment.Attributes)

		// If we got the vid, we're done. Otherwise retry.
		if !data.Vid.IsNull() {
			return
		}
	}
	if !found {
		// Assignment is genuinely gone (e.g. removed out-of-band).
		data.ID = types.StringNull()
	}
	// Otherwise: exhausted retries — keep whatever Null fallbacks were
	// initialized above.
}

// findAssignmentByID locates an assignment by its ID. When serverID is known
// the list is filtered down to that server's assignments; otherwise (e.g.
// terraform import, where only the ID is available) it paginates through all
// of the team's assignments so entries beyond the first page are still found.
func (r *VlanAssignmentResource) findAssignmentByID(ctx context.Context, assignmentID, serverID string) (*components.VirtualNetworkAssignmentData, error) {
	pageSize := int64(100)

	for pageNumber := int64(1); ; pageNumber++ {
		page := pageNumber
		size := pageSize
		listRequest := operations.GetVirtualNetworksAssignmentsRequest{
			PageSize:   &size,
			PageNumber: &page,
		}
		if serverID != "" {
			listRequest.FilterServer = &serverID
		}

		response, err := r.client.PrivateNetworks.ListAssignments(ctx, listRequest)
		if err != nil {
			return nil, err
		}
		if response.VirtualNetworkAssignments == nil || response.VirtualNetworkAssignments.Data == nil ||
			len(response.VirtualNetworkAssignments.Data) == 0 {
			return nil, nil
		}

		for _, a := range response.VirtualNetworkAssignments.Data {
			if a.ID != nil && *a.ID == assignmentID {
				assignment := a
				return &assignment, nil
			}
		}

		if int64(len(response.VirtualNetworkAssignments.Data)) < pageSize {
			return nil, nil
		}
	}
}

func (r *VlanAssignmentResource) findAssignmentByServerAndVNet(ctx context.Context, data *VlanAssignmentResourceModel, diags *diag.Diagnostics) {
	serverID := data.ServerID.ValueString()
	vnetID := data.VirtualNetworkID.ValueString()

	// Initialize Computed attributes to Null so a partial API response can't
	// leave them Unknown after Apply.
	data.Vid = types.Int64Null()
	data.Description = types.StringNull()
	data.Status = types.StringNull()

	// Filter by server so the lookup works regardless of pagination.
	listRequest := operations.GetVirtualNetworksAssignmentsRequest{}
	if serverID != "" {
		listRequest.FilterServer = &serverID
	}

	response, err := r.client.PrivateNetworks.ListAssignments(ctx, listRequest)
	if err != nil {
		diags.AddError("Client Error", "Unable to read virtual network assignments, got error: "+err.Error())
		return
	}

	if response.VirtualNetworkAssignments == nil || response.VirtualNetworkAssignments.Data == nil {
		diags.AddError("API Error", "No virtual network assignments found in response")
		return
	}

	// Find our assignment
	var assignment *components.VirtualNetworkAssignmentData
	for _, a := range response.VirtualNetworkAssignments.Data {
		if a.Attributes != nil {
			attrs := a.Attributes

			if attrs.VirtualNetworkID != nil && *attrs.VirtualNetworkID == vnetID {
				var assignmentServerID string
				if attrs.Server != nil && attrs.Server.ID != nil {
					assignmentServerID = *attrs.Server.ID
				}

				if assignmentServerID == serverID {
					assignment = &a
					break
				}
			}
		}
	}

	if assignment == nil {
		// Add more detailed debugging information
		var foundAssignments []string
		for _, a := range response.VirtualNetworkAssignments.Data {
			if a.Attributes != nil && a.ID != nil {
				attrs := a.Attributes
				serverIDStr := "nil"
				vnetIDStr := "nil"

				if attrs.Server != nil && attrs.Server.ID != nil {
					serverIDStr = *attrs.Server.ID
				}

				if attrs.VirtualNetworkID != nil {
					vnetIDStr = *attrs.VirtualNetworkID
				}
				foundAssignments = append(foundAssignments, fmt.Sprintf("ID: %s, Server: %s, VNet: %s", *a.ID, serverIDStr, vnetIDStr))
			}
		}

		errorMsg := fmt.Sprintf("Could not find virtual network assignment for server '%s' and virtual network '%s'", serverID, vnetID)
		if len(foundAssignments) > 0 {
			errorMsg += fmt.Sprintf(". Found assignments: %v", foundAssignments)
		} else {
			errorMsg += ". No assignments found in response."
		}

		diags.AddError("API Error", errorMsg)
		return
	}

	// Set the ID
	if assignment.ID != nil {
		data.ID = types.StringValue(*assignment.ID)
	}

	if assignment.Attributes != nil {
		attrs := assignment.Attributes

		if attrs.Server != nil && attrs.Server.ID != nil {
			data.ServerID = types.StringValue(*attrs.Server.ID)
		}

		if attrs.VirtualNetworkID != nil {
			data.VirtualNetworkID = types.StringValue(*attrs.VirtualNetworkID)
		}

		if attrs.Vid != nil {
			data.Vid = types.Int64Value(*attrs.Vid)
		}

		if attrs.Description != nil {
			data.Description = types.StringValue(*attrs.Description)
		}

		if attrs.Status != nil {
			data.Status = types.StringValue(*attrs.Status)
		}
	}
}
