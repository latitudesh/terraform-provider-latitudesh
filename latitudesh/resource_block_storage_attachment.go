package latitudesh

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
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

var _ resource.Resource = &BlockStorageAttachmentResource{}
var _ resource.ResourceWithImportState = &BlockStorageAttachmentResource{}

// Mapping lifecycle states reported by the volume's `block` object. Mapping and
// unmapping are asynchronous, and `block` becomes null once a volume is fully
// unmapped.
const (
	blockStorageStatusMapping   = "mapping"
	blockStorageStatusMapped    = "mapped"
	blockStorageStatusUnmapping = "unmapping"
	blockStorageStatusFailed    = "failed"
)

// blockStorageAttachmentPollInterval is how long the waiters sleep between
// reads. A var so tests can shrink it.
var blockStorageAttachmentPollInterval = 10 * time.Second

// BlockStorageAttachmentResource attaches a high performance volume to a server
// over NVMe-TCP (map) and detaches it on destroy (unmap). A volume is mapped to
// at most one server and the API gives the mapping no identifier of its own, so
// this resource's id is the volume's id.
//
// The deprecated mount flow (adding a client NQN to an allowed list) is a
// different mechanism and is deliberately not modeled here.
type BlockStorageAttachmentResource struct {
	client *latitudeshgosdk.Latitudesh
}

type BlockStorageAttachmentResourceModel struct {
	ID             types.String   `tfsdk:"id"`
	BlockStorageID types.String   `tfsdk:"block_storage_id"`
	ServerID       types.String   `tfsdk:"server_id"`
	Status         types.String   `tfsdk:"status"`
	Nqn            types.String   `tfsdk:"nqn"`
	Nsid           types.Int64    `tfsdk:"nsid"`
	StorageNetwork types.Object   `tfsdk:"storage_network"`
	Timeouts       timeouts.Value `tfsdk:"timeouts"`
}

var blockStorageNetworkObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"vid":           types.Int64Type,
		"host_cidr":     types.StringType,
		"gateway":       types.StringType,
		"routes":        types.ListType{ElemType: types.StringType},
		"block_gateway": types.StringType,
		"block_port":    types.Int64Type,
	},
}

func NewBlockStorageAttachmentResource() resource.Resource {
	return &BlockStorageAttachmentResource{}
}

func (r *BlockStorageAttachmentResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_block_storage_attachment"
}

func (r *BlockStorageAttachmentResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Attaches a [Latitude.sh](https://latitude.sh/) block storage volume to a server over NVMe-TCP.\n\nThe server must be in the same project and site as the volume, and eligible for storage: enabled for bonding and cabled with two NICs to two different switches. Deploying the server onto a public network (`public_network_id` on `latitudesh_server`) is how to get one, because that deploy only picks servers that pass the same check. A volume can be attached to only one server, so changing either side forces a new resource.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Identifier of the attachment, which is the attached volume's id.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"block_storage_id": schema.StringAttribute{
				MarkdownDescription: "ID of the volume to attach. Changing this forces a new resource.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"server_id": schema.StringAttribute{
				MarkdownDescription: "ID of the server that will access the volume. It must be in the same project and site as the volume, and eligible for storage — deploy it with `public_network_id` to get an eligible server. Changing this forces a new resource: the volume is unmapped from the old server before being mapped to the new one.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "Mapping state reported by the API (`mapped` once the server can reach the volume).",
				Computed:            true,
			},
			"nqn": schema.StringAttribute{
				MarkdownDescription: "NVMe Qualified Name of the mapped server.",
				Computed:            true,
			},
			"nsid": schema.Int64Attribute{
				MarkdownDescription: "NVMe namespace ID of the mapping.",
				Computed:            true,
			},
			"storage_network": schema.SingleNestedAttribute{
				MarkdownDescription: "Storage network the mapped server joins to reach the volume. These are the values to configure in the server's operating system, which is only set up for the volume automatically when the server is deployed or reinstalled while the volume is attached.",
				Computed:            true,
				Attributes: map[string]schema.Attribute{
					"vid": schema.Int64Attribute{
						MarkdownDescription: "VLAN ID of the storage VLAN, trunked to the server's bond (or to its PXE interface when the server has no bond).",
						Computed:            true,
					},
					"host_cidr": schema.StringAttribute{
						MarkdownDescription: "Address, in CIDR notation, to configure on the server's storage interface.",
						Computed:            true,
					},
					"gateway": schema.StringAttribute{
						MarkdownDescription: "Gateway of the storage network.",
						Computed:            true,
					},
					"routes": schema.ListAttribute{
						MarkdownDescription: "Routes the server must install to reach the storage fabric.",
						ElementType:         types.StringType,
						Computed:            true,
					},
					"block_gateway": schema.StringAttribute{
						MarkdownDescription: "Address of the NVMe-TCP target.",
						Computed:            true,
					},
					"block_port": schema.Int64Attribute{
						MarkdownDescription: "Port of the NVMe-TCP target.",
						Computed:            true,
					},
				},
			},
			"timeouts": timeouts.Attributes(ctx, timeouts.Opts{
				Create: true,
				Delete: true,
			}),
		},
	}
}

func (r *BlockStorageAttachmentResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	deps := iprovider.ConfigureFromProviderData(req.ProviderData, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	r.client = deps.Client
}

func (r *BlockStorageAttachmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data BlockStorageAttachmentResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createTimeout, diagTO := data.Timeouts.Create(ctx, 15*time.Minute)
	resp.Diagnostics.Append(diagTO...)
	if resp.Diagnostics.HasError() {
		return
	}

	volumeID := data.BlockStorageID.ValueString()

	_, err := r.client.BlockStorage.MapVolume(ctx, volumeID, operations.MapVolumeBlockStorageRequestBody{
		Data: operations.MapVolumeBlockStorageData{
			Type: operations.MapVolumeBlockStorageTypeVolumes,
			Attributes: operations.MapVolumeBlockStorageAttributes{
				ServerID: data.ServerID.ValueString(),
			},
		},
	})
	if err != nil {
		detail := "Unable to attach block storage volume, got error: " + err.Error()
		if blockStorageServerNotEligible(err) {
			detail += "\n\nNot every server can reach block storage: it has to be cabled for a bond. " +
				"Deploy the server with `public_network_id`, since a public network deploy only picks servers that qualify, or attach the volume to another server."
		}
		resp.Diagnostics.AddError("Client Error", detail)
		return
	}

	data.ID = types.StringValue(volumeID)

	// The mapping exists from here on. Persist what is already known before the
	// wait, so a poll that errors or times out leaves the attachment in state
	// (Terraform marks it tainted and replaces it on the next apply) instead of
	// orphaning a mapped volume the next apply could not map again. Only known
	// values are written: the computed attributes are still unknown.
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), volumeID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("block_storage_id"), volumeID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("server_id"), data.ServerID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("timeouts"), data.Timeouts)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The map response echoes the volume back, but mapping is asynchronous and
	// the echo is the pre-mapping state, so the attachment's own attributes
	// only become known once the poll below sees a terminal state.
	block, failed := r.waitForBlockStorageMapped(ctx, volumeID, createTimeout, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		// The API rolls a failed mapping back, so nothing is attached and the
		// next apply can simply map again.
		if failed {
			resp.State.RemoveResource(ctx)
		}
		return
	}

	resp.Diagnostics.Append(mapBlockStorageAttachment(ctx, &data, block)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *BlockStorageAttachmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data BlockStorageAttachmentResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	volumeID := data.BlockStorageID.ValueString()
	if volumeID == "" {
		volumeID = data.ID.ValueString()
	}

	block, found, err := r.readBlock(ctx, volumeID)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to read block storage attachment, got error: "+err.Error())
		return
	}
	// The volume is gone, or nothing is mapped to it any more: either way the
	// attachment this resource tracks no longer exists.
	if !found || block == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	data.ID = types.StringValue(volumeID)
	data.BlockStorageID = types.StringValue(volumeID)
	resp.Diagnostics.Append(mapBlockStorageAttachment(ctx, &data, block)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update only ever sees a change to `timeouts`: both inputs are
// RequiresReplace, because the API offers no way to move a mapping without
// unmapping first. Nothing is sent to the API. The computed attributes are
// unknown in that plan, so they are carried over from state, which a mapping
// left untouched still describes.
func (r *BlockStorageAttachmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data, state BlockStorageAttachmentResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	data.Status = state.Status
	data.Nqn = state.Nqn
	data.Nsid = state.Nsid
	data.StorageNetwork = state.StorageNetwork

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *BlockStorageAttachmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data BlockStorageAttachmentResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	deleteTimeout, diagTO := data.Timeouts.Delete(ctx, 15*time.Minute)
	resp.Diagnostics.Append(diagTO...)
	if resp.Diagnostics.HasError() {
		return
	}

	volumeID := data.BlockStorageID.ValueString()
	if volumeID == "" {
		volumeID = data.ID.ValueString()
	}

	if _, err := r.client.BlockStorage.UnmapVolume(ctx, volumeID); err != nil {
		// The volume itself may already be gone (it is destroyed right after
		// this resource in a normal `terraform destroy`), which unmaps it.
		if blockStorageNotFound(err) {
			return
		}
		// The API answers 409 when there is no active mapping to remove. If the
		// volume shows none (or only a failed one, which the API rolls back),
		// there is nothing left to detach — the case of a tainted attachment
		// whose mapping ended up failing.
		if blockStorageConflict(err) && r.nothingMapped(ctx, volumeID) {
			return
		}
		resp.Diagnostics.AddError("Client Error", "Unable to detach block storage volume, got error: "+err.Error())
		return
	}

	r.waitForBlockStorageUnmapped(ctx, volumeID, deleteTimeout, &resp.Diagnostics)
}

// ImportState takes the volume id: the mapping has no id of its own, and a
// volume is mapped to at most one server.
func (r *BlockStorageAttachmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("block_storage_id"), req, resp)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

// readBlock returns the volume's mapping. found reports whether the volume
// itself still exists, so a missing volume and an unmapped volume stay
// distinguishable.
func (r *BlockStorageAttachmentResource) readBlock(ctx context.Context, volumeID string) (block *components.Block, found bool, err error) {
	result, err := r.client.BlockStorage.RetrieveVolume(ctx, volumeID)
	if err != nil {
		if blockStorageNotFound(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if result.Object == nil || result.Object.Data == nil {
		return nil, false, nil
	}
	if result.Object.Data.Attributes == nil {
		return nil, true, nil
	}
	return result.Object.Data.Attributes.Block, true, nil
}

// waitForBlockStorageMapped polls the volume until its mapping reaches a
// terminal state, returning the mapped block. A `failed` status, a vanished
// volume or the timeout all produce an error diagnostic; failed reports
// whether the API settled the mapping as `failed` (and so rolled it back), as
// opposed to the outcome being unknown.
func (r *BlockStorageAttachmentResource) waitForBlockStorageMapped(ctx context.Context, volumeID string, timeout time.Duration, diags *diag.Diagnostics) (block *components.Block, failed bool) {
	deadline := time.Now().Add(timeout)

	for {
		current, found, err := r.readBlock(ctx, volumeID)
		if err != nil {
			diags.AddError("Client Error", "Unable to poll block storage attachment, got error: "+err.Error())
			return nil, false
		}
		if !found {
			diags.AddError("API Error", fmt.Sprintf("Block storage volume %s disappeared while it was being attached.", volumeID))
			return nil, false
		}

		if current != nil && current.Status != nil {
			switch *current.Status {
			case blockStorageStatusMapped:
				return current, false
			case blockStorageStatusFailed:
				// Project, site and eligibility are rejected synchronously by the
				// map request, so a failure here comes from the storage side.
				diags.AddError("API Error", fmt.Sprintf(
					"Attaching block storage volume %s failed on the storage side. Run `terraform apply` again to retry, and contact support if it keeps failing.", volumeID))
				return nil, true
			}
		}

		if !time.Now().Before(deadline) {
			status := "no mapping reported"
			if current != nil && current.Status != nil {
				status = *current.Status
			}
			diags.AddError("Timeout", fmt.Sprintf(
				"Block storage volume %s was still not attached after %s (last status: %s). The attachment was saved as tainted, so the next apply detaches and re-attaches it.", volumeID, timeout, status))
			return nil, false
		}

		select {
		case <-ctx.Done():
			diags.AddError("Cancelled", "Attaching block storage volume "+volumeID+" was cancelled while polling.")
			return nil, false
		case <-time.After(blockStorageAttachmentPollInterval):
		}
	}
}

// waitForBlockStorageUnmapped polls until the volume reports no mapping at all
// (`block` is null once the unmap completes) or the volume itself is gone.
func (r *BlockStorageAttachmentResource) waitForBlockStorageUnmapped(ctx context.Context, volumeID string, timeout time.Duration, diags *diag.Diagnostics) {
	deadline := time.Now().Add(timeout)

	for {
		block, found, err := r.readBlock(ctx, volumeID)
		if err != nil {
			diags.AddError("Client Error", "Unable to poll block storage detachment, got error: "+err.Error())
			return
		}
		if !found || block == nil {
			return
		}
		// The unmap request moves the mapping to "unmapping" before it returns,
		// and a detach that fails puts it back to "mapped" rather than "failed".
		if block.Status != nil && (*block.Status == blockStorageStatusMapped || *block.Status == blockStorageStatusFailed) {
			diags.AddError("API Error", fmt.Sprintf(
				"Detaching block storage volume %s failed and the volume is still attached. Run the destroy again to retry, and contact support if it keeps failing.", volumeID))
			return
		}

		if !time.Now().Before(deadline) {
			diags.AddError("Timeout", fmt.Sprintf(
				"Block storage volume %s was still attached after %s.", volumeID, timeout))
			return
		}

		select {
		case <-ctx.Done():
			diags.AddError("Cancelled", "Detaching block storage volume "+volumeID+" was cancelled while polling.")
			return
		case <-time.After(blockStorageAttachmentPollInterval):
		}
	}
}

// mapBlockStorageAttachment copies one mapping into the model. server_id is
// taken from the API so a volume remapped outside Terraform reads as drift on
// an attribute that forces replacement.
func mapBlockStorageAttachment(ctx context.Context, data *BlockStorageAttachmentResourceModel, block *components.Block) diag.Diagnostics {
	var diags diag.Diagnostics
	if block == nil {
		return diags
	}

	if block.ServerID != nil {
		data.ServerID = types.StringValue(*block.ServerID)
	}
	if block.Status != nil {
		data.Status = types.StringValue(*block.Status)
	} else {
		data.Status = types.StringNull()
	}
	if block.Nqn != nil {
		data.Nqn = types.StringValue(*block.Nqn)
	} else {
		data.Nqn = types.StringNull()
	}
	if block.Nsid != nil {
		data.Nsid = types.Int64Value(*block.Nsid)
	} else {
		data.Nsid = types.Int64Null()
	}

	network := block.StorageNetwork
	if network == nil {
		data.StorageNetwork = types.ObjectNull(blockStorageNetworkObjectType.AttrTypes)
		return diags
	}

	routes := types.ListNull(types.StringType)
	if network.Routes != nil {
		list, listDiags := types.ListValueFrom(ctx, types.StringType, network.Routes)
		diags.Append(listDiags...)
		if diags.HasError() {
			return diags
		}
		routes = list
	}

	object, objectDiags := types.ObjectValue(blockStorageNetworkObjectType.AttrTypes, map[string]attr.Value{
		"vid":           optionalInt64(network.Vid),
		"host_cidr":     optionalString(network.HostCidr),
		"gateway":       optionalString(network.Gateway),
		"routes":        routes,
		"block_gateway": optionalString(network.BlockGateway),
		"block_port":    optionalInt64(network.BlockPort),
	})
	diags.Append(objectDiags...)
	data.StorageNetwork = object

	return diags
}

// blockStorageServerNotEligible reports whether the map request was rejected
// because the server cannot reach block storage (it is not cabled for a bond).
func blockStorageServerNotEligible(err error) bool {
	var apiErr *components.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusUnprocessableEntity {
		return false
	}
	return strings.Contains(apiErr.Body, "SERVER_NOT_ELIGIBLE_FOR_VOLUME_MAPPING")
}

// blockStorageConflict reports whether err is the API's 409, which unmap
// returns when the volume has no active mapping to remove.
func blockStorageConflict(err error) bool {
	var apiErr *components.APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusConflict
}

// nothingMapped reports whether the volume is gone or carries no active
// mapping (none at all, or one the API settled as failed and rolled back).
func (r *BlockStorageAttachmentResource) nothingMapped(ctx context.Context, volumeID string) bool {
	block, found, err := r.readBlock(ctx, volumeID)
	if err != nil {
		return false
	}
	return !found || block == nil || (block.Status != nil && *block.Status == blockStorageStatusFailed)
}

// optionalInt64 is the int64 twin of optionalString (helpers_server.go).
func optionalInt64(v *int64) types.Int64 {
	if v == nil {
		return types.Int64Null()
	}
	return types.Int64Value(*v)
}
