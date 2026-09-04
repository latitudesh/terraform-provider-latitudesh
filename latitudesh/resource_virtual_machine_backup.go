package latitudesh

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
	iprovider "github.com/latitudesh/terraform-provider-latitudesh/v2/internal/provider"
)

var _ resource.Resource = &VirtualMachineBackupResource{}
var _ resource.ResourceWithImportState = &VirtualMachineBackupResource{}

// Poll intervals for the async create/delete waits. Declared as vars (not
// consts) so the lifecycle tests can shorten them to keep the mock-backed
// suite fast; production keeps the defaults.
var (
	vmBackupReadyPollInterval  = 10 * time.Second
	vmBackupDeletePollInterval = 5 * time.Second
)

func NewVirtualMachineBackupResource() resource.Resource {
	return &VirtualMachineBackupResource{}
}

// VirtualMachineBackupResource defines the resource implementation.
type VirtualMachineBackupResource struct {
	client *latitudeshgosdk.Latitudesh
}

// VirtualMachineBackupResourceModel describes the resource data model.
type VirtualMachineBackupResourceModel struct {
	ID             types.String   `tfsdk:"id"`
	VirtualMachine types.String   `tfsdk:"virtual_machine"`
	Status         types.String   `tfsdk:"status"`
	SizeBytes      types.Int64    `tfsdk:"size_bytes"`
	ExpiresAt      types.String   `tfsdk:"expires_at"`
	FailureReason  types.String   `tfsdk:"failure_reason"`
	CreatedAt      types.String   `tfsdk:"created_at"`
	Timeouts       timeouts.Value `tfsdk:"timeouts"`
}

func (r *VirtualMachineBackupResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_virtual_machine_backup"
}

func (r *VirtualMachineBackupResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Virtual Machine Backup resource. Triggers a backup of a [Latitude.sh](https://latitude.sh/) virtual machine. Backups have no update path: any change to `virtual_machine` destroys and recreates the resource, which triggers a brand new backup rather than modifying one in place.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Virtual machine backup identifier",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"virtual_machine": schema.StringAttribute{
				MarkdownDescription: "The ID of the virtual machine to back up. Changing this forces a new resource.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "Backup status (`Creating`, `Ready`, `Failed`, or `Archived`).",
				Computed:            true,
			},
			"size_bytes": schema.Int64Attribute{
				MarkdownDescription: "Backup size in bytes.",
				Computed:            true,
			},
			"expires_at": schema.StringAttribute{
				MarkdownDescription: "Timestamp when the backup expires.",
				Computed:            true,
			},
			"failure_reason": schema.StringAttribute{
				MarkdownDescription: "Reason the backup failed, when `status` is `Failed`.",
				Computed:            true,
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "The timestamp for when the backup was created.",
				Computed:            true,
			},
			"timeouts": timeouts.Attributes(ctx, timeouts.Opts{
				Create:            true,
				Delete:            true,
				CreateDescription: `Timeout for the backup to reach a terminal status (Ready or Failed). Default: 30 minutes. Example: "45m", "1h"`,
				DeleteDescription: `Timeout for the backup to be fully deleted. Default: 10 minutes. Example: "20m"`,
			}),
		},
	}
}

func (r *VirtualMachineBackupResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	deps := iprovider.ConfigureFromProviderData(req.ProviderData, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	r.client = deps.Client
}

func (r *VirtualMachineBackupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data VirtualMachineBackupResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	payload := components.VirtualMachineBackupPayload{
		Data: &components.VirtualMachineBackupPayloadData{
			Type: components.VirtualMachineBackupPayloadTypeVirtualMachineBackups.ToPointer(),
			Attributes: &components.VirtualMachineBackupPayloadAttributes{
				VirtualMachine: data.VirtualMachine.ValueString(),
			},
		},
	}

	result, err := r.client.VirtualMachineBackups.Create(ctx, payload)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to create virtual machine backup, got error: "+err.Error())
		return
	}

	if result.VirtualMachineBackup == nil || result.VirtualMachineBackup.Data == nil || result.VirtualMachineBackup.Data.ID == nil {
		resp.Diagnostics.AddError("API Error", "Failed to get virtual machine backup ID from response")
		return
	}

	id := *result.VirtualMachineBackup.Data.ID
	data.ID = types.StringValue(id)

	// Persist the ID before the (potentially long) wait so the backup is
	// tracked in state even if polling times out; otherwise it leaks as an
	// orphan. Only known values are written here: the remaining computed
	// attributes are still unknown at this point, and writing unknown values
	// to state makes Terraform reject the apply with "Provider returned
	// invalid result object after apply".
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createTimeout, diags := data.Timeouts.Create(ctx, 30*time.Minute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.waitForBackupReady(ctx, id, createTimeout, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	r.readVirtualMachineBackup(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *VirtualMachineBackupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data VirtualMachineBackupResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.readVirtualMachineBackup(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	// If the backup no longer exists, drop it from state.
	if data.ID.IsNull() {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update is unreachable: virtual_machine is the only configurable attribute
// and it carries RequiresReplace, so the framework replaces the resource
// instead of calling Update. The method still must exist to satisfy
// resource.Resource.
func (r *VirtualMachineBackupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Update Not Supported", "Virtual machine backups cannot be updated, they must be replaced.")
}

func (r *VirtualMachineBackupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data VirtualMachineBackupResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := data.ID.ValueString()

	_, err := r.client.VirtualMachineBackups.Delete(ctx, id)
	if err != nil {
		var apiErr *components.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			return
		}
		resp.Diagnostics.AddError("Client Error", "Unable to delete virtual machine backup, got error: "+err.Error())
		return
	}

	deleteTimeout, diags := data.Timeouts.Delete(ctx, 10*time.Minute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Deletion is asynchronous server-side (202 Accepted): the API archives
	// and removes the backup afterwards. Wait until it is actually gone so a
	// dependent delete (e.g. the virtual machine it backs up) doesn't race it.
	r.waitForBackupDeleted(ctx, id, deleteTimeout, &resp.Diagnostics)
}

func (r *VirtualMachineBackupResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *VirtualMachineBackupResource) waitForBackupReady(ctx context.Context, id string, timeout time.Duration, diags *diag.Diagnostics) {
	const maxConsecutiveErrors = 5
	pollInterval := vmBackupReadyPollInterval

	deadline := time.Now().Add(timeout)
	lastStatus := ""
	consecutiveErrors := 0

	for time.Now().Before(deadline) {
		result, err := r.client.VirtualMachineBackups.Get(ctx, id)
		if err != nil {
			// A 404 right after create (backup not queryable yet) and 5xx
			// responses are transient: keep polling. Other API errors
			// (401/403/422/...) will not resolve by waiting, so fail
			// immediately instead of burning the full timeout budget.
			var apiErr *components.APIError
			if errors.As(err, &apiErr) && apiErr.StatusCode != http.StatusNotFound && apiErr.StatusCode < 500 {
				diags.AddError("Client Error", "Unable to check virtual machine backup status: "+err.Error())
				return
			}
			consecutiveErrors++
			if consecutiveErrors >= maxConsecutiveErrors {
				diags.AddError("Client Error", fmt.Sprintf("Unable to check virtual machine backup status after %d consecutive attempts, last error: %s", consecutiveErrors, err.Error()))
				return
			}
			select {
			case <-ctx.Done():
				diags.AddError("Client Error", "Context cancelled while waiting for virtual machine backup to be ready: "+ctx.Err().Error())
				return
			case <-time.After(pollInterval):
				continue
			}
		}
		consecutiveErrors = 0

		if result.VirtualMachineBackup != nil && result.VirtualMachineBackup.Data != nil && result.VirtualMachineBackup.Data.Attributes != nil {
			attrs := result.VirtualMachineBackup.Data.Attributes
			if attrs.Status != nil {
				lastStatus = string(*attrs.Status)
				switch *attrs.Status {
				case components.VirtualMachineBackupAttributesStatusReady:
					return
				case components.VirtualMachineBackupAttributesStatusFailed:
					reason := "unknown reason"
					if attrs.FailureReason != nil && *attrs.FailureReason != "" {
						reason = *attrs.FailureReason
					}
					diags.AddError("Virtual Machine Backup Failed", fmt.Sprintf("Backup %q failed: %s", id, reason))
					return
				}
			}
		}

		select {
		case <-ctx.Done():
			diags.AddError("Client Error", "Context cancelled while waiting for virtual machine backup to be ready: "+ctx.Err().Error())
			return
		case <-time.After(pollInterval):
		}
	}

	diags.AddError(
		"Timeout waiting for virtual machine backup",
		fmt.Sprintf("Virtual machine backup %q did not reach a terminal status within %s (last status: %q).", id, timeout, lastStatus),
	)
}

func (r *VirtualMachineBackupResource) waitForBackupDeleted(ctx context.Context, id string, timeout time.Duration, diags *diag.Diagnostics) {
	const maxConsecutiveErrors = 5
	pollInterval := vmBackupDeletePollInterval

	deadline := time.Now().Add(timeout)
	consecutiveErrors := 0

	for time.Now().Before(deadline) {
		_, err := r.client.VirtualMachineBackups.Get(ctx, id)
		if err != nil {
			var apiErr *components.APIError
			if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
				return
			}
			if errors.As(err, &apiErr) && apiErr.StatusCode < 500 {
				diags.AddError("Client Error", "Unable to check virtual machine backup deletion: "+err.Error())
				return
			}
			consecutiveErrors++
			if consecutiveErrors >= maxConsecutiveErrors {
				diags.AddError("Client Error", fmt.Sprintf("Unable to check virtual machine backup deletion after %d consecutive attempts, last error: %s", consecutiveErrors, err.Error()))
				return
			}
		} else {
			consecutiveErrors = 0
		}

		select {
		case <-ctx.Done():
			diags.AddError("Client Error", "Context cancelled while waiting for virtual machine backup deletion: "+ctx.Err().Error())
			return
		case <-time.After(pollInterval):
		}
	}

	diags.AddError(
		"Timeout waiting for virtual machine backup deletion",
		fmt.Sprintf("Virtual machine backup %q was still present after %s.", id, timeout),
	)
}

func (r *VirtualMachineBackupResource) readVirtualMachineBackup(ctx context.Context, data *VirtualMachineBackupResourceModel, diags *diag.Diagnostics) {
	id := data.ID.ValueString()

	result, err := r.client.VirtualMachineBackups.Get(ctx, id)
	if err != nil {
		var apiErr *components.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			data.ID = types.StringNull()
			return
		}
		diags.AddError("Client Error", "Unable to read virtual machine backup, got error: "+err.Error())
		return
	}

	if result.VirtualMachineBackup == nil || result.VirtualMachineBackup.Data == nil {
		data.ID = types.StringNull()
		return
	}

	obj := result.VirtualMachineBackup.Data
	if obj.ID != nil {
		data.ID = types.StringValue(*obj.ID)
	}

	fields := mapVirtualMachineBackupAttrs(obj.Attributes)

	// virtual_machine is Required + RequiresReplace, not Computed: only
	// backfill it when it isn't already known (e.g. on import), otherwise a
	// config-provided slug-vs-ID mismatch against the API's echoed value would
	// read as drift on every plan.
	if data.VirtualMachine.IsNull() || data.VirtualMachine.IsUnknown() {
		data.VirtualMachine = fields.VirtualMachine
	}
	data.Status = fields.Status
	data.SizeBytes = fields.SizeBytes
	data.ExpiresAt = fields.ExpiresAt
	data.FailureReason = fields.FailureReason
	data.CreatedAt = fields.CreatedAt
}

// virtualMachineBackupAttrs holds the framework-typed form of
// components.VirtualMachineBackupAttributesAttributes, shared by the resource
// and data source so the nil-check-and-convert logic is written, and tested,
// once.
type virtualMachineBackupAttrs struct {
	VirtualMachine types.String
	Status         types.String
	SizeBytes      types.Int64
	ExpiresAt      types.String
	FailureReason  types.String
	CreatedAt      types.String
}

func mapVirtualMachineBackupAttrs(a *components.VirtualMachineBackupAttributesAttributes) virtualMachineBackupAttrs {
	out := virtualMachineBackupAttrs{
		VirtualMachine: types.StringNull(),
		Status:         types.StringNull(),
		SizeBytes:      types.Int64Null(),
		ExpiresAt:      types.StringNull(),
		FailureReason:  types.StringNull(),
		CreatedAt:      types.StringNull(),
	}
	if a == nil {
		return out
	}

	if a.VirtualMachine != nil && a.VirtualMachine.ID != nil {
		out.VirtualMachine = types.StringValue(*a.VirtualMachine.ID)
	}
	if a.Status != nil {
		out.Status = types.StringValue(string(*a.Status))
	}
	if a.SizeBytes != nil {
		out.SizeBytes = types.Int64Value(*a.SizeBytes)
	}
	if a.ExpiresAt != nil {
		out.ExpiresAt = types.StringValue(*a.ExpiresAt)
	}
	if a.FailureReason != nil {
		out.FailureReason = types.StringValue(*a.FailureReason)
	}
	if a.CreatedAt != nil {
		out.CreatedAt = types.StringValue(*a.CreatedAt)
	}
	return out
}
