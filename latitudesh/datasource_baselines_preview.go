package latitudesh

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
	iprovider "github.com/latitudesh/terraform-provider-latitudesh/v2/internal/provider"
)

var _ datasource.DataSource = &BaselinesPreviewDataSource{}
var _ datasource.DataSourceWithConfigure = &BaselinesPreviewDataSource{}

func NewBaselinesPreviewDataSource() datasource.DataSource {
	return &BaselinesPreviewDataSource{}
}

type BaselinesPreviewDataSource struct {
	client *latitudeshgosdk.Latitudesh
}

type BaselinesPreviewDataSourceModel struct {
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

func (d *BaselinesPreviewDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_baselines_preview"
}

func (d *BaselinesPreviewDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	deps := iprovider.ConfigureFromProviderData(req.ProviderData, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	d.client = deps.Client
}

func (d *BaselinesPreviewDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Baseline data source - lookup a baseline by `id` or `name`. **Preview.** Available to teams with the `baselines_api` feature flag; the shape of this data source may change before general availability.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Baseline ID to look up. Mutually exclusive with `name`.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.ExactlyOneOf(
						path.MatchRoot("id"),
						path.MatchRoot("name"),
					),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Baseline name to look up. Mutually exclusive with `id`.",
				Optional:            true,
				Computed:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Description of the baseline.",
				Computed:            true,
			},
			"target_type": schema.StringAttribute{
				MarkdownDescription: "Baseline target: `all_servers`, `custom`, or `platforms`.",
				Computed:            true,
			},
			"operating_system": schema.StringAttribute{
				MarkdownDescription: "Slug of the operating system the baseline expects the server to run.",
				Computed:            true,
			},
			"platforms": schema.ListAttribute{
				MarkdownDescription: "Slugs of the plans this baseline applies to (only populated when `target_type` is `\"platforms\"`).",
				ElementType:         types.StringType,
				Computed:            true,
			},
			"ssh_key_ids": schema.ListAttribute{
				MarkdownDescription: "SSH key IDs the baseline expects on the server.",
				ElementType:         types.StringType,
				Computed:            true,
			},
			"user_data_id": schema.StringAttribute{
				MarkdownDescription: "ID of the user data the baseline expects to run on first boot.",
				Computed:            true,
			},
			"disk_layout": schema.ListNestedAttribute{
				MarkdownDescription: "Expected disk layout.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"role": schema.StringAttribute{
							MarkdownDescription: "Purpose of the disk group: `os`, `storage`, or `raw`.",
							Computed:            true,
						},
						"count": schema.Int64Attribute{
							MarkdownDescription: "Number of disks in the group.",
							Computed:            true,
						},
						"raid_level": schema.StringAttribute{
							MarkdownDescription: "RAID level for the group.",
							Computed:            true,
						},
						"filesystem": schema.StringAttribute{
							MarkdownDescription: "Filesystem the group is formatted with.",
							Computed:            true,
						},
						"mount_point": schema.StringAttribute{
							MarkdownDescription: "Where the group is mounted.",
							Computed:            true,
						},
					},
				},
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "Date the baseline was created.",
				Computed:            true,
			},
		},
	}
}

func (d *BaselinesPreviewDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data BaselinesPreviewDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if data.ID.IsUnknown() || data.Name.IsUnknown() {
		resp.Diagnostics.AddError(
			"Unknown selector value",
			"One of 'id' or 'name' is unknown. Please provide a concrete value.",
		)
		return
	}

	var baseline *components.BaselineData
	var err error

	switch {
	case !data.ID.IsNull():
		baseline, err = d.getByID(ctx, data.ID.ValueString())
	case !data.Name.IsNull():
		baseline, err = d.findByName(ctx, data.Name.ValueString())
	default:
		resp.Diagnostics.AddError(
			"Missing selector",
			"Exactly one of 'id' or 'name' must be provided.",
		)
		return
	}

	if err != nil {
		resp.Diagnostics.AddError("Client Error", err.Error())
		return
	}
	if baseline == nil {
		resp.Diagnostics.AddError("Baseline not found", fmt.Sprintf("No baseline exists matching the given selector (id=%q, name=%q)", data.ID.ValueString(), data.Name.ValueString()))
		return
	}

	d.mapBaselineToModel(ctx, baseline, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (d *BaselinesPreviewDataSource) getByID(ctx context.Context, id string) (*components.BaselineData, error) {
	res, err := d.client.BaselinesPreview.GetBaseline(ctx, id)
	if err != nil {
		var apiErr *components.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("unable to retrieve baseline %q: %w", id, err)
	}
	if res.Baseline == nil || res.Baseline.Data == nil {
		return nil, nil
	}
	return res.Baseline.Data, nil
}

// findByName lists all baselines and filters in memory: GetBaselines takes no
// filter parameters (unlike, e.g., virtual network assignments), and the
// group's response carries no pagination metadata to follow.
func (d *BaselinesPreviewDataSource) findByName(ctx context.Context, name string) (*components.BaselineData, error) {
	res, err := d.client.BaselinesPreview.GetBaselines(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to list baselines: %w", err)
	}
	if res == nil || res.Baselines == nil || res.Baselines.Data == nil {
		return nil, nil
	}

	nameQ := strings.TrimSpace(name)
	for i := range res.Baselines.Data {
		b := res.Baselines.Data[i]
		if b.Attributes != nil && b.Attributes.Name != nil && strings.TrimSpace(*b.Attributes.Name) == nameQ {
			return &b, nil
		}
	}
	return nil, nil
}

func (d *BaselinesPreviewDataSource) mapBaselineToModel(ctx context.Context, baseline *components.BaselineData, data *BaselinesPreviewDataSourceModel, diags *diag.Diagnostics) {
	if baseline.ID != nil {
		data.ID = types.StringValue(*baseline.ID)
	}

	a := baseline.Attributes
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
		listVal, d2 := types.ListValueFrom(ctx, types.StringType, baselinePlatformSlugs(a.Platforms))
		diags.Append(d2...)
		data.Platforms = listVal
	} else {
		data.Platforms = types.ListNull(types.StringType)
	}

	if len(a.SSHKeys) > 0 {
		listVal, d2 := types.ListValueFrom(ctx, types.StringType, baselineSSHKeyIDs(a.SSHKeys))
		diags.Append(d2...)
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
