package latitudesh

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
	"github.com/latitudesh/latitudesh-go-sdk/models/operations"
	iprovider "github.com/latitudesh/terraform-provider-latitudesh/v2/internal/provider"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ datasource.DataSource = &PlanDataSource{}

func NewPlanDataSource() datasource.DataSource {
	return &PlanDataSource{}
}

type PlanDataSource struct {
	client *latitudeshgosdk.Latitudesh
}

type PlanDataSourceModel struct {
	ID       types.String `tfsdk:"id"`
	Slug     types.String `tfsdk:"slug"`
	Name     types.String `tfsdk:"name"`
	Features types.List   `tfsdk:"features"`
	// Computed nested objects
	CPUType  types.String  `tfsdk:"cpu_type"`
	CPUCores types.Float64 `tfsdk:"cpu_cores"`
	CPUClock types.Float64 `tfsdk:"cpu_clock"`
	CPUCount types.Float64 `tfsdk:"cpu_count"`
	Memory   types.Float64 `tfsdk:"memory"`
	HasGPU   types.Bool    `tfsdk:"has_gpu"`
	GPUType  types.String  `tfsdk:"gpu_type"`
	GPUCount types.Float64 `tfsdk:"gpu_count"`
	Drives   types.List    `tfsdk:"drives"`
}

// PlanDriveModel is one disk group from a plan's specs.drives array. count is an
// Int64 to align with latitudesh_server.disk_layout.count, where the value is destined.
type PlanDriveModel struct {
	Count types.Int64  `tfsdk:"count"`
	Size  types.String `tfsdk:"size"`
	Type  types.String `tfsdk:"type"`
}

// planDriveObjectType mirrors PlanDriveModel and is used to build the drives list value.
var planDriveObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"count": types.Int64Type,
		"size":  types.StringType,
		"type":  types.StringType,
	},
}

// planHasGPU reports whether a plan's specs describe a real GPU. The Plans API
// returns "gpu":{} (an empty object, not null) for non-GPU plans, so a non-nil
// *Gpu alone is not enough — at least one payload field must be present.
func planHasGPU(gpu *components.Gpu) bool {
	return gpu != nil && (gpu.Type != nil || gpu.Count != nil)
}

// planDrivesValue maps a plan's specs.drives into a Terraform list value. It
// always returns a known (possibly empty) list, never null, so downstream
// `for`/`sum` expressions never iterate over a null value. Count is converted
// float64 -> int64 to align with latitudesh_server.disk_layout.count, and the
// strict PlanDataAttributesType enum is rendered back to its string.
func planDrivesValue(ctx context.Context, drives []components.Drives) (types.List, diag.Diagnostics) {
	driveModels := make([]PlanDriveModel, 0, len(drives))
	for _, d := range drives {
		drive := PlanDriveModel{
			Count: types.Int64Null(),
			Size:  types.StringNull(),
			Type:  types.StringNull(),
		}
		if d.Count != nil {
			drive.Count = types.Int64Value(int64(*d.Count))
		}
		if d.Size != nil {
			drive.Size = types.StringValue(*d.Size)
		}
		if d.Type != nil {
			drive.Type = types.StringValue(string(*d.Type))
		}
		driveModels = append(driveModels, drive)
	}
	return types.ListValueFrom(ctx, planDriveObjectType, driveModels)
}

func (d *PlanDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_plan"
}

func (d *PlanDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Plan data source - retrieve server plan information",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Plan ID to look up",
				Optional:            true,
			},
			"slug": schema.StringAttribute{
				MarkdownDescription: "Plan slug to look up",
				Optional:            true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Plan name",
				Computed:            true,
				Optional:            true,
			},
			"features": schema.ListAttribute{
				MarkdownDescription: "List of plan features",
				ElementType:         types.StringType,
				Computed:            true,
			},
			"cpu_type": schema.StringAttribute{
				MarkdownDescription: "CPU type",
				Computed:            true,
			},
			"cpu_cores": schema.Float64Attribute{
				MarkdownDescription: "Number of CPU cores",
				Computed:            true,
			},
			"cpu_clock": schema.Float64Attribute{
				MarkdownDescription: "CPU clock speed",
				Computed:            true,
			},
			"cpu_count": schema.Float64Attribute{
				MarkdownDescription: "Number of CPUs",
				Computed:            true,
			},
			"memory": schema.Float64Attribute{
				MarkdownDescription: "Total memory",
				Computed:            true,
			},
			"has_gpu": schema.BoolAttribute{
				MarkdownDescription: "Whether the plan includes GPU",
				Computed:            true,
			},
			"gpu_type": schema.StringAttribute{
				MarkdownDescription: "GPU type if available",
				Computed:            true,
			},
			"gpu_count": schema.Float64Attribute{
				MarkdownDescription: "Number of GPUs if available",
				Computed:            true,
			},
			"drives": schema.ListNestedAttribute{
				MarkdownDescription: "Disk groups included in the plan. Sum each group's `count` to derive the total disk count for `latitudesh_server.disk_layout`.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"count": schema.Int64Attribute{
							MarkdownDescription: "Number of disks in this group",
							Computed:            true,
						},
						"size": schema.StringAttribute{
							MarkdownDescription: "Size of each disk in this group, as free-form text returned by the API (e.g. \"1.9 TB\", \"480GB\")",
							Computed:            true,
						},
						"type": schema.StringAttribute{
							MarkdownDescription: "Disk type for this group (e.g. SSD, HDD, NVME)",
							Computed:            true,
						},
					},
				},
			},
		},
	}
}

func (d *PlanDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	deps := iprovider.ConfigureFromProviderData(req.ProviderData, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	d.client = deps.Client
}

func (d *PlanDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data PlanDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Check that either ID/slug/name is provided
	if data.ID.IsNull() && data.Slug.IsNull() && data.Name.IsNull() {
		resp.Diagnostics.AddError(
			"Missing Required Attribute",
			"Either 'id', 'slug' or 'name' must be provided to look up a plan.",
		)
		return
	}

	var plan *components.PlanData

	if !data.ID.IsNull() {
		// Look up by ID
		planID := data.ID.ValueString()
		result, err := d.client.Plans.Get(ctx, planID)
		if err != nil {
			resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read plan %s, got error: %s", planID, err.Error()))
			return
		}
		if result.Plan != nil && result.Plan.Data != nil {
			plan = result.Plan.Data
		}
	} else {
		// Look up by slug or name
		slug := data.Slug.ValueString()
		if data.Slug.IsNull() {
			slug = data.Name.ValueString()
		}

		req := operations.GetPlansRequest{FilterSlug: &slug}
		result, err := d.client.Plans.List(ctx, req)
		if err != nil {
			resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to search for plan with slug or name %q: %s", slug, err))
			return
		}
		if result.Object != nil && result.Object.Data != nil && len(result.Object.Data) > 0 {
			plan = &result.Object.Data[0]
		}
	}

	if plan == nil {
		resp.Diagnostics.AddError("Plan Not Found", "The specified plan was not found.")
		return
	}

	// Populate the data model
	if plan.ID != nil {
		data.ID = types.StringValue(*plan.ID)
	}

	// drives defaults to an empty list so `for`/`sum` expressions over it never
	// iterate a null value, even for plans the API returns without a drives group.
	data.Drives = types.ListValueMust(planDriveObjectType, []attr.Value{})

	if plan.Attributes != nil {
		attrs := plan.Attributes

		if attrs.Slug != nil {
			data.Slug = types.StringValue(*attrs.Slug)
			data.Name = types.StringValue(*attrs.Slug) // Use slug as name since name is not in the API
		}

		if attrs.Features != nil {
			features := make([]types.String, len(attrs.Features))
			for i, feature := range attrs.Features {
				features[i] = types.StringValue(string(feature))
			}
			featuresList, diags := types.ListValueFrom(ctx, types.StringType, features)
			resp.Diagnostics.Append(diags...)
			if resp.Diagnostics.HasError() {
				return
			}
			data.Features = featuresList
		}

		if attrs.Specs != nil {
			specs := attrs.Specs

			// CPU information
			if specs.CPU != nil {
				cpu := specs.CPU
				if cpu.Type != nil {
					data.CPUType = types.StringValue(*cpu.Type)
				}
				if cpu.Cores != nil {
					data.CPUCores = types.Float64Value(*cpu.Cores)
				}
				if cpu.Clock != nil {
					data.CPUClock = types.Float64Value(*cpu.Clock)
				}
				if cpu.Count != nil {
					data.CPUCount = types.Float64Value(*cpu.Count)
				}
			}

			// Memory information
			if specs.Memory != nil && specs.Memory.Total != nil {
				data.Memory = types.Float64Value(*specs.Memory.Total)
			}

			// GPU information. gate on planHasGPU because the API returns "gpu":{}
			// (an empty object, not null) for plans without a GPU.
			if planHasGPU(specs.Gpu) {
				data.HasGPU = types.BoolValue(true)
				if specs.Gpu.Type != nil {
					data.GPUType = types.StringValue(*specs.Gpu.Type)
				}
				if specs.Gpu.Count != nil {
					data.GPUCount = types.Float64Value(*specs.Gpu.Count)
				}
			} else {
				data.HasGPU = types.BoolValue(false)
			}

			// Drives (disk groups). Both lookup paths converge on `plan` before this
			// block, so this single mapping covers lookup-by-id and lookup-by-slug.
			drivesList, diags := planDrivesValue(ctx, specs.Drives)
			resp.Diagnostics.Append(diags...)
			if resp.Diagnostics.HasError() {
				return
			}
			data.Drives = drivesList
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
