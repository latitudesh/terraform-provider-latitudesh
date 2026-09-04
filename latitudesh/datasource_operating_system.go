package latitudesh

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
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

var (
	_ datasource.DataSource              = &OperatingSystemDataSource{}
	_ datasource.DataSourceWithConfigure = &OperatingSystemDataSource{}
)

func NewOperatingSystemDataSource() datasource.DataSource {
	return &OperatingSystemDataSource{}
}

type OperatingSystemDataSource struct {
	client *latitudeshgosdk.Latitudesh
}

type OperatingSystemDataSourceModel struct {
	// Selectors (exactly one)
	ID   types.String `tfsdk:"id"`
	Slug types.String `tfsdk:"slug"`
	Name types.String `tfsdk:"name"`

	// Attributes. Presentation-only catalog metadata is not part of this SDK
	// model at all (OperatingSystemDataAttributes carries no description,
	// logo, URL, or timestamp fields), so nothing had to be excluded here.
	Distro          types.String `tfsdk:"distro"`
	User            types.String `tfsdk:"user"`
	Version         types.String `tfsdk:"version"`
	ProvisionableOn types.List   `tfsdk:"provisionable_on"`
	Features        types.Object `tfsdk:"features"`
}

// OperatingSystemFeaturesModel describes which deployment features an OS
// build supports, as reported by the API.
type OperatingSystemFeaturesModel struct {
	Raid       types.Bool `tfsdk:"raid"`
	SSHKeys    types.Bool `tfsdk:"ssh_keys"`
	UserData   types.Bool `tfsdk:"user_data"`
	Accelerate types.Bool `tfsdk:"accelerate"`
	Rescue     types.Bool `tfsdk:"rescue"`
	Workflow   types.Bool `tfsdk:"workflow"`
}

var operatingSystemFeaturesObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"raid":       types.BoolType,
		"ssh_keys":   types.BoolType,
		"user_data":  types.BoolType,
		"accelerate": types.BoolType,
		"rescue":     types.BoolType,
		"workflow":   types.BoolType,
	},
}

func operatingSystemFeaturesValue(ctx context.Context, f *components.Features) (types.Object, diag.Diagnostics) {
	if f == nil {
		return types.ObjectNull(operatingSystemFeaturesObjectType.AttrTypes), nil
	}
	model := OperatingSystemFeaturesModel{
		Raid:       types.BoolPointerValue(f.Raid),
		SSHKeys:    types.BoolPointerValue(f.SSHKeys),
		UserData:   types.BoolPointerValue(f.UserData),
		Accelerate: types.BoolPointerValue(f.Accelerate),
		Rescue:     types.BoolPointerValue(f.Rescue),
		Workflow:   types.BoolPointerValue(f.Workflow),
	}
	return types.ObjectValueFrom(ctx, operatingSystemFeaturesObjectType.AttrTypes, model)
}

func (d *OperatingSystemDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_operating_system"
}

func (d *OperatingSystemDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	deps := iprovider.ConfigureFromProviderData(req.ProviderData, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	d.client = deps.Client
}

func (d *OperatingSystemDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Operating system data source - lookup an operating system available to deploy and reinstall, by id, slug, or name.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Operating system ID to look up.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.ExactlyOneOf(
						path.MatchRoot("id"),
						path.MatchRoot("slug"),
						path.MatchRoot("name"),
					),
				},
			},
			"slug": schema.StringAttribute{
				MarkdownDescription: "Operating system slug to look up (e.g. \"ubuntu_24_04_x64_lts\"). This is the value expected by `latitudesh_server.operating_system`.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.ExactlyOneOf(
						path.MatchRoot("id"),
						path.MatchRoot("slug"),
						path.MatchRoot("name"),
					),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Operating system name to look up.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.ExactlyOneOf(
						path.MatchRoot("id"),
						path.MatchRoot("slug"),
						path.MatchRoot("name"),
					),
				},
			},
			"distro": schema.StringAttribute{
				MarkdownDescription: "Distribution family (e.g. \"ubuntu\").",
				Computed:            true,
			},
			"user": schema.StringAttribute{
				MarkdownDescription: "Default login user for this operating system.",
				Computed:            true,
			},
			"version": schema.StringAttribute{
				MarkdownDescription: "Distribution version.",
				Computed:            true,
			},
			"provisionable_on": schema.ListAttribute{
				MarkdownDescription: "Server plan names this operating system can be deployed on (e.g. \"c3.small.x86\", as reported by `latitudesh_plan.name`).",
				ElementType:         types.StringType,
				Computed:            true,
			},
			"features": schema.SingleNestedAttribute{
				MarkdownDescription: "Deployment features supported by this operating system build.",
				Computed:            true,
				Attributes: map[string]schema.Attribute{
					"raid": schema.BoolAttribute{
						MarkdownDescription: "Whether RAID configuration is supported.",
						Computed:            true,
					},
					"ssh_keys": schema.BoolAttribute{
						MarkdownDescription: "Whether SSH key injection is supported.",
						Computed:            true,
					},
					"user_data": schema.BoolAttribute{
						MarkdownDescription: "Whether user data / cloud-init is supported.",
						Computed:            true,
					},
					"accelerate": schema.BoolAttribute{
						MarkdownDescription: "Whether accelerated provisioning is supported.",
						Computed:            true,
					},
					"rescue": schema.BoolAttribute{
						MarkdownDescription: "Whether rescue mode is supported.",
						Computed:            true,
					},
					"workflow": schema.BoolAttribute{
						MarkdownDescription: "Whether workflow-based provisioning is supported.",
						Computed:            true,
					},
				},
			},
		},
	}
}

func (d *OperatingSystemDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data OperatingSystemDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if d.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured.")
		return
	}

	// Avoid unknown selectors (e.g., from unresolved variables)
	if data.ID.IsUnknown() || data.Slug.IsUnknown() || data.Name.IsUnknown() {
		resp.Diagnostics.AddError(
			"Unknown selector value",
			"One of 'id', 'slug', or 'name' is unknown. Please provide a concrete value.",
		)
		return
	}

	var (
		args     findOperatingSystemArgs
		selector path.Path
		value    string
	)
	switch {
	case !data.ID.IsNull():
		selector, value = path.Root("id"), data.ID.ValueString()
		args.ID = value
	case !data.Slug.IsNull():
		selector, value = path.Root("slug"), data.Slug.ValueString()
		args.Slug = value
	case !data.Name.IsNull():
		selector, value = path.Root("name"), data.Name.ValueString()
		args.Name = value
	default:
		resp.Diagnostics.AddError(
			"Missing selector",
			"Exactly one of 'id', 'slug', or 'name' must be provided.",
		)
		return
	}

	// ExactlyOneOf only checks that a selector is set, not that it has content.
	// Reject blank values here rather than walking the whole catalog for a
	// guaranteed miss and reporting it as "not found".
	if strings.TrimSpace(value) == "" {
		resp.Diagnostics.AddAttributeError(
			selector,
			"Blank selector",
			fmt.Sprintf("%q must not be empty or whitespace-only.", selector.String()),
		)
		return
	}

	os, err := d.findOne(ctx, args)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", err.Error())
		return
	}
	if os == nil {
		resp.Diagnostics.AddError("Operating System Not Found", "No operating system matches the given selector.")
		return
	}

	if os.ID != nil {
		data.ID = types.StringValue(*os.ID)
	}

	data.ProvisionableOn = types.ListNull(types.StringType)
	data.Features = types.ObjectNull(operatingSystemFeaturesObjectType.AttrTypes)

	if os.Attributes != nil {
		attrs := os.Attributes

		data.Name = types.StringPointerValue(attrs.Name)
		data.Slug = types.StringPointerValue(attrs.Slug)
		data.Distro = types.StringPointerValue(attrs.Distro)
		data.User = types.StringPointerValue(attrs.User)
		data.Version = types.StringPointerValue(attrs.Version)

		if attrs.ProvisionableOn != nil {
			list, diags := types.ListValueFrom(ctx, types.StringType, attrs.ProvisionableOn)
			resp.Diagnostics.Append(diags...)
			if resp.Diagnostics.HasError() {
				return
			}
			data.ProvisionableOn = list
		}

		features, diags := operatingSystemFeaturesValue(ctx, attrs.Features)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		data.Features = features
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

type findOperatingSystemArgs struct {
	ID   string
	Slug string
	Name string
}

// findOne pages through ListPlans and returns the first operating system
// matching the given selector. OperatingSystems exposes no single-item read
// (ListPlans is its only method), so every lookup — including by ID — walks
// the list.
//
// pageSize is deliberately nil. The SDK still sends its default page[size]=20
// on the wire, but Next() derives its stop condition from the argument we
// passed: with nil it keeps going until the API returns an empty page (which
// the live endpoint does past the last one); with an explicit size it stops on
// the first short page. The explicit form saves one request per miss but
// silently truncates the walk if the API ever caps page[size] below the value
// requested, so the robust form is used here.
func (d *OperatingSystemDataSource) findOne(ctx context.Context, args findOperatingSystemArgs) (*components.OperatingSystemData, error) {
	idQ := strings.TrimSpace(args.ID)
	slugQ := strings.TrimSpace(args.Slug)
	nameQ := strings.TrimSpace(args.Name)

	result, err := d.client.OperatingSystems.ListPlans(ctx, nil, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("unable to list operating systems: %w", err)
	}

	for result != nil {
		if result.OperatingSystems != nil {
			for i := range result.OperatingSystems.Data {
				os := result.OperatingSystems.Data[i]
				if idQ != "" && os.ID != nil && strings.TrimSpace(*os.ID) == idQ {
					return &os, nil
				}
				if os.Attributes == nil {
					continue
				}
				if slugQ != "" && os.Attributes.Slug != nil && strings.TrimSpace(*os.Attributes.Slug) == slugQ {
					return &os, nil
				}
				if nameQ != "" && os.Attributes.Name != nil && strings.TrimSpace(*os.Attributes.Name) == nameQ {
					return &os, nil
				}
			}
		}

		if result.Next == nil {
			break
		}
		result, err = result.Next()
		if err != nil {
			return nil, fmt.Errorf("unable to fetch next page of operating systems: %w", err)
		}
	}

	return nil, nil
}
