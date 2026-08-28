package latitudesh

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
	"github.com/latitudesh/latitudesh-go-sdk/models/operations"
	iprovider "github.com/latitudesh/terraform-provider-latitudesh/v2/internal/provider"
)

// BaselinesPreview is a preview SDK group: available only to teams with the
// `baselines_api` feature flag, and its shape may change before general
// availability (see the SDK's group-level doc comment).
//
// The group exposes no update endpoint (Create, Get, GetAll, Destroy only), so
// every schema attribute below other than id/created_at requires replacement.

var _ resource.Resource = &BaselinesPreviewResource{}
var _ resource.ResourceWithImportState = &BaselinesPreviewResource{}
var _ resource.ResourceWithValidateConfig = &BaselinesPreviewResource{}

func NewBaselinesPreviewResource() resource.Resource {
	return &BaselinesPreviewResource{}
}

type BaselinesPreviewResource struct {
	client *latitudeshgosdk.Latitudesh
}

// BaselineDiskLayoutModel describes a single disk group in a baseline's
// expected disk layout. It mirrors components.BaselineDiskLayoutGroup.
type BaselineDiskLayoutModel struct {
	Role       types.String `tfsdk:"role"`
	Count      types.Int64  `tfsdk:"count"`
	RaidLevel  types.String `tfsdk:"raid_level"`
	Filesystem types.String `tfsdk:"filesystem"`
	MountPoint types.String `tfsdk:"mount_point"`
}

type BaselinesPreviewResourceModel struct {
	ID              types.String              `tfsdk:"id"`
	Name            types.String              `tfsdk:"name"`
	Description     types.String              `tfsdk:"description"`
	TargetType      types.String              `tfsdk:"target_type"`
	OperatingSystem types.String              `tfsdk:"operating_system"`
	Platforms       types.List                `tfsdk:"platforms"`
	SSHKeyIds       types.List                `tfsdk:"ssh_key_ids"`
	UserDataID      types.String              `tfsdk:"user_data_id"`
	DiskLayout      []BaselineDiskLayoutModel `tfsdk:"disk_layout"`
	CreatedAt       types.String              `tfsdk:"created_at"`
}

func (r *BaselinesPreviewResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_baselines_preview"
}

func baselineDiskLayoutNestedObject() schema.NestedAttributeObject {
	return schema.NestedAttributeObject{
		Attributes: map[string]schema.Attribute{
			"role": schema.StringAttribute{
				MarkdownDescription: "Purpose of the disk group: `os`, `storage`, or `raw`.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("os", "storage", "raw"),
				},
			},
			"count": schema.Int64Attribute{
				MarkdownDescription: "Number of disks in the group.",
				Required:            true,
				Validators: []validator.Int64{
					int64validator.AtLeast(1),
				},
			},
			"raid_level": schema.StringAttribute{
				MarkdownDescription: "RAID level for the group: `raid-0` or `raid-1`. Only valid for the `os` and `storage` roles.",
				Optional:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("raid-0", "raid-1"),
				},
			},
			"filesystem": schema.StringAttribute{
				MarkdownDescription: "Filesystem to format the group with. Only valid for the `storage` role. Only `ext4` is currently accepted by the SDK.",
				Optional:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("ext4"),
				},
			},
			"mount_point": schema.StringAttribute{
				MarkdownDescription: "Where the group is mounted, e.g. `/data`. Required for the `storage` role.",
				Optional:            true,
			},
		},
	}
}

func (r *BaselinesPreviewResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Baseline resource. **Preview.** Available to teams with the `baselines_api` feature flag; the shape of this resource may change before general availability.\n\n" +
			"A baseline records the configuration you expect your servers to be delivered with: plan/platform target, operating system, SSH keys, user data, disk layout, and BIOS settings. There is no update endpoint for baselines, so any change to this resource replaces it.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Baseline identifier.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the baseline.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Description of the baseline.",
				Optional:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"target_type": schema.StringAttribute{
				MarkdownDescription: "Baseline target: `all_servers`, `custom` (a set of servers, when the plan is not yet known), or `platforms` (one or more specific platforms). When `platforms`, set `platforms` to the plan slugs it applies to.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("all_servers", "custom", "platforms"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"operating_system": schema.StringAttribute{
				MarkdownDescription: "Slug of the operating system the baseline expects the server to run.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"platforms": schema.ListAttribute{
				MarkdownDescription: "Slugs of the plans this baseline applies to. Required when `target_type` is `\"platforms\"`.",
				ElementType:         types.StringType,
				Optional:            true,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.RequiresReplace(),
				},
			},
			"ssh_key_ids": schema.ListAttribute{
				MarkdownDescription: "SSH key IDs the baseline expects on the server.",
				ElementType:         types.StringType,
				Optional:            true,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.RequiresReplace(),
				},
			},
			"user_data_id": schema.StringAttribute{
				MarkdownDescription: "ID of the user data the baseline expects to run on first boot.",
				Optional:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"disk_layout": schema.ListNestedAttribute{
				MarkdownDescription: "Expected disk layout. When the baseline targets specific platforms, this is validated server-side against the smallest of the selected platforms.",
				Optional:            true,
				NestedObject:        baselineDiskLayoutNestedObject(),
				PlanModifiers: []planmodifier.List{
					listplanmodifier.RequiresReplace(),
				},
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "Date the baseline was created.",
				Computed:            true,
			},
		},
	}
}

// ValidateConfig enforces the two cross-field rules the SDK's field docs call
// out (CreateBaselineAttributes.Platforms: "Required when target_type is
// \"platforms\"") and mirrors the disk_layout field placement rules the API
// documents (BaselineDiskLayoutGroup.RaidLevel/Filesystem/MountPoint). The
// deeper "smallest selected platform" disk validation is server-side only and
// is not replicated here (see handoff).
func (r *BaselinesPreviewResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data BaselinesPreviewResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(validateBaselinesPreviewConfig(data)...)
}

// validateBaselinesPreviewConfig holds the cross-field rules ValidateConfig
// enforces, factored out as a plain function of the model so it can be unit
// tested without constructing a framework request.
func validateBaselinesPreviewConfig(data BaselinesPreviewResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	if !data.TargetType.IsNull() && !data.TargetType.IsUnknown() && data.TargetType.ValueString() == "platforms" {
		if data.Platforms.IsNull() || (len(data.Platforms.Elements()) == 0 && !data.Platforms.IsUnknown()) {
			diags.AddAttributeError(
				path.Root("platforms"),
				"Missing platforms",
				`"platforms" is required and must be non-empty when target_type is "platforms".`,
			)
		}
	}

	for i, d := range data.DiskLayout {
		if d.Role.IsUnknown() {
			continue
		}
		role := d.Role.ValueString()
		hasRaid := !d.RaidLevel.IsNull() && !d.RaidLevel.IsUnknown()
		hasFilesystem := !d.Filesystem.IsNull() && !d.Filesystem.IsUnknown()
		hasMount := !d.MountPoint.IsNull() && !d.MountPoint.IsUnknown() && d.MountPoint.ValueString() != ""

		switch role {
		case "storage":
			if !hasMount {
				diags.AddAttributeError(
					path.Root("disk_layout"),
					"Invalid disk_layout",
					fmt.Sprintf("disk_layout[%d]: mount_point is required on role \"storage\".", i),
				)
			}
		case "os":
			if hasFilesystem {
				diags.AddAttributeError(
					path.Root("disk_layout"),
					"Invalid disk_layout",
					fmt.Sprintf("disk_layout[%d]: filesystem is not allowed on role \"os\" (only \"storage\").", i),
				)
			}
			if hasMount {
				diags.AddAttributeError(
					path.Root("disk_layout"),
					"Invalid disk_layout",
					fmt.Sprintf("disk_layout[%d]: mount_point is not allowed on role \"os\".", i),
				)
			}
		case "raw":
			if hasRaid {
				diags.AddAttributeError(
					path.Root("disk_layout"),
					"Invalid disk_layout",
					fmt.Sprintf("disk_layout[%d]: raid_level is not allowed on role \"raw\".", i),
				)
			}
			if hasFilesystem {
				diags.AddAttributeError(
					path.Root("disk_layout"),
					"Invalid disk_layout",
					fmt.Sprintf("disk_layout[%d]: filesystem is not allowed on role \"raw\".", i),
				)
			}
			if hasMount {
				diags.AddAttributeError(
					path.Root("disk_layout"),
					"Invalid disk_layout",
					fmt.Sprintf("disk_layout[%d]: mount_point is not allowed on role \"raw\".", i),
				)
			}
		}
	}

	return diags
}

func (r *BaselinesPreviewResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	deps := iprovider.ConfigureFromProviderData(req.ProviderData, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	r.client = deps.Client
}

func baselineDiskLayoutFromModel(in []BaselineDiskLayoutModel) []components.BaselineDiskLayoutGroup {
	out := make([]components.BaselineDiskLayoutGroup, 0, len(in))
	for _, d := range in {
		role := components.BaselineDiskLayoutGroupRole(d.Role.ValueString())
		entry := components.BaselineDiskLayoutGroup{Role: &role}

		if !d.Count.IsNull() && !d.Count.IsUnknown() {
			c := d.Count.ValueInt64()
			entry.Count = &c
		}
		if !d.RaidLevel.IsNull() && !d.RaidLevel.IsUnknown() {
			rl := components.RaidLevel(d.RaidLevel.ValueString())
			entry.RaidLevel = &rl
		}
		if !d.Filesystem.IsNull() && !d.Filesystem.IsUnknown() {
			fs := components.Filesystem(d.Filesystem.ValueString())
			entry.Filesystem = &fs
		}
		if !d.MountPoint.IsNull() && !d.MountPoint.IsUnknown() && d.MountPoint.ValueString() != "" {
			mp := d.MountPoint.ValueString()
			entry.MountPoint = &mp
		}
		out = append(out, entry)
	}
	return out
}

// baselineDiskLayoutToModel is the inverse of baselineDiskLayoutFromModel,
// shared by the resource read path and the data source.
func baselineDiskLayoutToModel(in []components.BaselineDiskLayoutGroup) []BaselineDiskLayoutModel {
	if len(in) == 0 {
		return nil
	}
	out := make([]BaselineDiskLayoutModel, 0, len(in))
	for _, d := range in {
		m := BaselineDiskLayoutModel{
			Role:       types.StringNull(),
			Count:      types.Int64Null(),
			RaidLevel:  types.StringNull(),
			Filesystem: types.StringNull(),
			MountPoint: types.StringNull(),
		}
		if d.Role != nil {
			m.Role = types.StringValue(string(*d.Role))
		}
		if d.Count != nil {
			m.Count = types.Int64Value(*d.Count)
		}
		if d.RaidLevel != nil {
			m.RaidLevel = types.StringValue(string(*d.RaidLevel))
		}
		if d.Filesystem != nil {
			m.Filesystem = types.StringValue(string(*d.Filesystem))
		}
		if d.MountPoint != nil {
			m.MountPoint = types.StringValue(*d.MountPoint)
		}
		out = append(out, m)
	}
	return out
}

// baselinePlatformSlugs extracts the plan slugs from the API's platform
// objects, matching the flat list of slugs the create request accepts.
func baselinePlatformSlugs(in []components.Platforms) []string {
	out := make([]string, 0, len(in))
	for _, p := range in {
		if p.Slug != nil {
			out = append(out, *p.Slug)
		}
	}
	return out
}

// baselineSSHKeyIDs extracts the SSH key IDs from the API's SSH key objects,
// matching the flat list of IDs the create request accepts.
func baselineSSHKeyIDs(in []components.BaselineDataSSHKeys) []string {
	out := make([]string, 0, len(in))
	for _, k := range in {
		if k.ID != nil {
			out = append(out, *k.ID)
		}
	}
	return out
}

func (r *BaselinesPreviewResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data BaselinesPreviewResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := data.Name.ValueString()
	targetType := operations.CreateBaselineTargetType(data.TargetType.ValueString())
	operatingSystem := data.OperatingSystem.ValueString()

	attrs := &operations.CreateBaselineAttributes{
		Name:            name,
		TargetType:      targetType,
		OperatingSystem: operatingSystem,
	}

	if !data.Description.IsNull() && !data.Description.IsUnknown() {
		desc := data.Description.ValueString()
		attrs.Description = &desc
	}

	if !data.Platforms.IsNull() && !data.Platforms.IsUnknown() {
		var platforms []string
		resp.Diagnostics.Append(data.Platforms.ElementsAs(ctx, &platforms, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
		attrs.Platforms = platforms
	}

	if !data.SSHKeyIds.IsNull() && !data.SSHKeyIds.IsUnknown() {
		var sshKeyIDs []string
		resp.Diagnostics.Append(data.SSHKeyIds.ElementsAs(ctx, &sshKeyIDs, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
		attrs.SSHKeyIds = sshKeyIDs
	}

	if !data.UserDataID.IsNull() && !data.UserDataID.IsUnknown() {
		userDataID := data.UserDataID.ValueString()
		attrs.UserDataID = &userDataID
	}

	if len(data.DiskLayout) > 0 {
		attrs.DiskLayout = baselineDiskLayoutFromModel(data.DiskLayout)
	}

	createType := operations.CreateBaselineTypeBaselines
	createRequest := operations.CreateBaselineBaselinesPreviewRequestBody{
		Data: &operations.CreateBaselineData{
			Type:       &createType,
			Attributes: attrs,
		},
	}

	result, err := r.client.BaselinesPreview.CreateBaseline(ctx, createRequest)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to create baseline, got error: "+err.Error())
		return
	}

	if result.Baseline == nil || result.Baseline.Data == nil || result.Baseline.Data.ID == nil {
		resp.Diagnostics.AddError("API Error", "Failed to get baseline ID from response")
		return
	}

	data.ID = types.StringValue(*result.Baseline.Data.ID)

	r.readBaselineInto(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *BaselinesPreviewResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data BaselinesPreviewResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.readBaselineInto(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	if data.ID.IsNull() {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update is only reached for the timeouts-free, no-op case: every attribute
// other than id/created_at requires replacement because the SDK exposes no
// baseline update endpoint (CR-D only), so a plan that reaches Update has no
// remote change to make. Persist the plan as-is.
func (r *BaselinesPreviewResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data BaselinesPreviewResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *BaselinesPreviewResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data BaselinesPreviewResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := data.ID.ValueString()

	_, err := r.client.BaselinesPreview.DestroyBaseline(ctx, id)
	if err != nil {
		var apiErr *components.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			return
		}
		resp.Diagnostics.AddError("Client Error", "Unable to delete baseline, got error: "+err.Error())
		return
	}
}

func (r *BaselinesPreviewResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *BaselinesPreviewResource) readBaselineInto(ctx context.Context, data *BaselinesPreviewResourceModel, diags *diag.Diagnostics) {
	id := data.ID.ValueString()

	result, err := r.client.BaselinesPreview.GetBaseline(ctx, id)
	if err != nil {
		var apiErr *components.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			data.ID = types.StringNull()
			return
		}
		diags.AddError("Client Error", "Unable to read baseline, got error: "+err.Error())
		return
	}

	if result.Baseline == nil || result.Baseline.Data == nil {
		data.ID = types.StringNull()
		return
	}

	bd := result.Baseline.Data
	if bd.ID != nil {
		data.ID = types.StringValue(*bd.ID)
	}

	a := bd.Attributes
	if a == nil {
		return
	}

	if a.Name != nil {
		data.Name = types.StringValue(*a.Name)
	}

	if a.Description != nil && *a.Description != "" {
		data.Description = types.StringValue(*a.Description)
	} else {
		data.Description = types.StringNull()
	}

	if a.TargetType != nil {
		data.TargetType = types.StringValue(string(*a.TargetType))
	} else {
		data.TargetType = types.StringNull()
	}

	if a.OperatingSystem != nil {
		data.OperatingSystem = types.StringValue(*a.OperatingSystem)
	} else {
		data.OperatingSystem = types.StringNull()
	}

	if len(a.Platforms) > 0 {
		listVal, d := types.ListValueFrom(ctx, types.StringType, baselinePlatformSlugs(a.Platforms))
		diags.Append(d...)
		data.Platforms = listVal
	} else {
		data.Platforms = types.ListNull(types.StringType)
	}

	if len(a.SSHKeys) > 0 {
		listVal, d := types.ListValueFrom(ctx, types.StringType, baselineSSHKeyIDs(a.SSHKeys))
		diags.Append(d...)
		data.SSHKeyIds = listVal
	} else {
		data.SSHKeyIds = types.ListNull(types.StringType)
	}

	if a.UserData != nil && a.UserData.ID != nil {
		data.UserDataID = types.StringValue(*a.UserData.ID)
	} else {
		data.UserDataID = types.StringNull()
	}

	data.DiskLayout = baselineDiskLayoutToModel(a.DiskLayout)

	if a.CreatedAt != nil {
		data.CreatedAt = types.StringValue(*a.CreatedAt)
	}
}
