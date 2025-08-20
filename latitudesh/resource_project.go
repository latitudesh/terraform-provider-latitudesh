package latitudesh

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
	"github.com/latitudesh/latitudesh-go-sdk/models/operations"
	iprovider "github.com/latitudesh/terraform-provider-latitudesh/internal/provider"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &ProjectResource{}
var _ resource.ResourceWithImportState = &ProjectResource{}

func NewProjectResource() resource.Resource {
	return &ProjectResource{}
}

// ProjectResource defines the resource implementation.
type ProjectResource struct {
	client *latitudeshgosdk.Latitudesh
}

// ProjectResourceModel describes the resource data model.
type ProjectResourceModel struct {
	ID               types.String `tfsdk:"id"`
	Name             types.String `tfsdk:"name"`
	Description      types.String `tfsdk:"description"`
	Environment      types.String `tfsdk:"environment"`
	ProvisioningType types.String `tfsdk:"provisioning_type"`
	Slug             types.String `tfsdk:"slug"`
	Tags             types.List   `tfsdk:"tags"`
}

func (r *ProjectResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project"
}

func (r *ProjectResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Project resource",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Project identifier",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The project name",
				Required:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "The project description",
				Optional:            true,
			},
			"environment": schema.StringAttribute{
				MarkdownDescription: "The project environment (Production, Development, Staging)",
				Optional:            true,
			},
			"provisioning_type": schema.StringAttribute{
				MarkdownDescription: "The provisioning type (on_demand, reserved)",
				Optional:            true,
				Computed:            true,
			},
			"slug": schema.StringAttribute{
				MarkdownDescription: "Project slug",
				Computed:            true,
			},
			"tags": schema.ListAttribute{
				MarkdownDescription: "Project tags",
				ElementType:         types.StringType,
				Optional:            true,
			},
		},
	}
}

func (r *ProjectResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	deps := iprovider.ConfigureFromProviderData(req.ProviderData, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	r.client = deps.Client
}

func (r *ProjectResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data ProjectResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	name := data.Name.ValueString()

	// Validate that provisioning_type is provided during creation
	if data.ProvisioningType.IsNull() || data.ProvisioningType.ValueString() == "" {
		resp.Diagnostics.AddError(
			"Missing Required Field",
			"The provisioning_type field is required when creating a project.",
		)
		return
	}

	provisioningType := data.ProvisioningType.ValueString()

	// Convert provisioning type string to enum
	var provisioningEnum operations.CreateProjectProvisioningType
	switch provisioningType {
	case "reserved":
		provisioningEnum = operations.CreateProjectProvisioningTypeReserved
	case "on_demand":
		provisioningEnum = operations.CreateProjectProvisioningTypeOnDemand
	default:
		provisioningEnum = operations.CreateProjectProvisioningTypeOnDemand // default
	}

	attrs := &operations.CreateProjectProjectsAttributes{
		Name:             name,
		ProvisioningType: provisioningEnum,
	}

	// Add optional description
	if !data.Description.IsNull() {
		desc := data.Description.ValueString()
		attrs.Description = &desc
	}

	// Add optional environment
	if !data.Environment.IsNull() {
		env := data.Environment.ValueString()
		var envEnum operations.CreateProjectEnvironment
		switch env {
		case "Production":
			envEnum = operations.CreateProjectEnvironmentProduction
		case "Development":
			envEnum = operations.CreateProjectEnvironmentDevelopment
		case "Staging":
			envEnum = operations.CreateProjectEnvironmentStaging
		default:
			envEnum = operations.CreateProjectEnvironmentDevelopment // default
		}
		attrs.Environment = &envEnum
	}

	createRequest := operations.CreateProjectProjectsRequestBody{
		Data: &operations.CreateProjectProjectsData{
			Type:       operations.CreateProjectProjectsTypeProjects,
			Attributes: attrs,
		},
	}

	result, err := r.client.Projects.Create(ctx, createRequest)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to create project, got error: "+err.Error())
		return
	}

	if result.Object == nil || result.Object.Data == nil || result.Object.Data.ID == nil {
		resp.Diagnostics.AddError("API Error", "Failed to get project ID from response")
		return
	}

	data.ID = types.StringValue(*result.Object.Data.ID)

	// Read the resource to populate all attributes
	r.readProject(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ProjectResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data ProjectResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	r.readProject(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ProjectResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data ProjectResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	projectID := data.ID.ValueString()
	name := data.Name.ValueString()

	attrs := &operations.UpdateProjectProjectsAttributes{
		Name: &name,
	}

	// Add optional description
	if !data.Description.IsNull() {
		desc := data.Description.ValueString()
		attrs.Description = &desc
	}

	// Add optional environment
	if !data.Environment.IsNull() {
		env := data.Environment.ValueString()
		var envEnum operations.UpdateProjectProjectsEnvironment
		switch env {
		case "Production":
			envEnum = operations.UpdateProjectProjectsEnvironmentProduction
		case "Development":
			envEnum = operations.UpdateProjectProjectsEnvironmentDevelopment
		case "Staging":
			envEnum = operations.UpdateProjectProjectsEnvironmentStaging
		default:
			envEnum = operations.UpdateProjectProjectsEnvironmentDevelopment // default
		}
		attrs.Environment = &envEnum
	}

	updateRequest := operations.UpdateProjectProjectsRequestBody{
		Data: operations.UpdateProjectProjectsData{
			ID:         &projectID,
			Type:       operations.UpdateProjectProjectsTypeProjects,
			Attributes: attrs,
		},
	}

	_, err := r.client.Projects.Update(ctx, projectID, &updateRequest)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to update project, got error: "+err.Error())
		return
	}

	// Read the resource to populate all attributes
	r.readProject(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ProjectResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data ProjectResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	projectID := data.ID.ValueString()

	_, err := r.client.Projects.Delete(ctx, projectID)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to delete project, got error: "+err.Error())
		return
	}
}

func (r *ProjectResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	var data ProjectResourceModel
	data.ID = types.StringValue(req.ID)

	r.readProject(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ProjectResource) readProject(ctx context.Context, data *ProjectResourceModel, diags *diag.Diagnostics) {
	projectID := data.ID.ValueString()

	// Use List with filter to find the project since there's no Get method
	response, err := r.client.Projects.List(ctx, operations.GetProjectsRequest{})
	if err != nil {
		diags.AddError("Client Error", "Unable to read projects, got error: "+err.Error())
		return
	}

	if response.Projects == nil || response.Projects.Data == nil {
		data.ID = types.StringNull()
		return
	}

	var project *components.Project
	for _, p := range response.Projects.Data {
		if p.ID != nil && *p.ID == projectID {
			project = &p
			break
		}
	}

	if project == nil {
		data.ID = types.StringNull()
		return
	}

	if project.Attributes != nil {
		if project.Attributes.Name != nil {
			data.Name = types.StringValue(*project.Attributes.Name)
		}

		if project.Attributes.Description != nil {
			data.Description = types.StringValue(*project.Attributes.Description)
		}

		if project.Attributes.Environment != nil {
			data.Environment = types.StringValue(string(*project.Attributes.Environment))
		}

		if project.Attributes.Slug != nil {
			data.Slug = types.StringValue(*project.Attributes.Slug)
		}

		if data.ProvisioningType.IsNull() {
			data.ProvisioningType = types.StringValue("on_demand")
		}

		data.Tags = types.ListNull(types.StringType)
	}
}
