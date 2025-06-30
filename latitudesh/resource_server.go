package latitudesh

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/operations"
)

var _ resource.Resource = &ServerResource{}
var _ resource.ResourceWithImportState = &ServerResource{}

func NewServerResource() resource.Resource {
	return &ServerResource{}
}

type ServerResource struct {
	client *latitudeshgosdk.Latitudesh
}

type ServerResourceModel struct {
	ID              types.String `tfsdk:"id"`
	Project         types.String `tfsdk:"project"`
	Site            types.String `tfsdk:"site"`
	Plan            types.String `tfsdk:"plan"`
	OperatingSystem types.String `tfsdk:"operating_system"`
	Hostname        types.String `tfsdk:"hostname"`
	SSHKeys         types.List   `tfsdk:"ssh_keys"`
	UserData        types.String `tfsdk:"user_data"`
	Raid            types.String `tfsdk:"raid"`
	Ipxe            types.String `tfsdk:"ipxe"`
	Billing         types.String `tfsdk:"billing"`
	Tags            types.List   `tfsdk:"tags"`
	PrimaryIpv4     types.String `tfsdk:"primary_ipv4"`
	PrimaryIpv6     types.String `tfsdk:"primary_ipv6"`
	Status          types.String `tfsdk:"status"`
	Locked          types.Bool   `tfsdk:"locked"`
	CreatedAt       types.String `tfsdk:"created_at"`
	Region          types.String `tfsdk:"region"`
}

func (r *ServerResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_server"
}

func (r *ServerResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Server resource",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Server identifier",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"project": schema.StringAttribute{
				MarkdownDescription: "The project (ID or Slug) to deploy the server",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"site": schema.StringAttribute{
				MarkdownDescription: "The site to deploy the server",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"plan": schema.StringAttribute{
				MarkdownDescription: "The plan to choose server from",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"operating_system": schema.StringAttribute{
				MarkdownDescription: "The operating system for the new server",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"hostname": schema.StringAttribute{
				MarkdownDescription: "The server hostname",
				Optional:            true,
				Computed:            true,
			},
			"ssh_keys": schema.ListAttribute{
				MarkdownDescription: "SSH Keys to set on the server",
				ElementType:         types.StringType,
				Optional:            true,
			},
			"user_data": schema.StringAttribute{
				MarkdownDescription: "User data to set on the server",
				Optional:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"raid": schema.StringAttribute{
				MarkdownDescription: "RAID mode for the server (raid-0, raid-1)",
				Optional:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"ipxe": schema.StringAttribute{
				MarkdownDescription: "URL where iPXE script is stored on, OR the iPXE script encoded in base64",
				Optional:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"billing": schema.StringAttribute{
				MarkdownDescription: "The server billing type (hourly, monthly, yearly)",
				Optional:            true,
				Computed:            true,
			},
			"tags": schema.ListAttribute{
				MarkdownDescription: "List of server tag IDs",
				ElementType:         types.StringType,
				Optional:            true,
			},
			"primary_ipv4": schema.StringAttribute{
				MarkdownDescription: "Primary IPv4 address of the server",
				Computed:            true,
			},
			"primary_ipv6": schema.StringAttribute{
				MarkdownDescription: "Primary IPv6 address of the server",
				Computed:            true,
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "Server power status",
				Computed:            true,
			},
			"locked": schema.BoolAttribute{
				MarkdownDescription: "Whether the server is locked",
				Computed:            true,
				Optional:            true,
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "The timestamp for when the server was created",
				Computed:            true,
			},
			"region": schema.StringAttribute{
				MarkdownDescription: "The region where the server is deployed",
				Computed:            true,
			},
		},
	}
}

func (r *ServerResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*latitudeshgosdk.Latitudesh)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			"Expected *latitudeshgosdk.Latitudesh, got: %T. Please report this issue to the provider developers.",
		)
		return
	}

	r.client = client
}

func (r *ServerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data ServerResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	attrs := &operations.CreateServerServersAttributes{}

	if !data.Project.IsNull() {
		project := data.Project.ValueString()
		attrs.Project = &project
	}

	if !data.Plan.IsNull() {
		planValue := data.Plan.ValueString()
		plan := operations.CreateServerPlan(planValue)
		attrs.Plan = &plan
	}

	if !data.Site.IsNull() {
		siteValue := data.Site.ValueString()
		site := operations.CreateServerSite(siteValue)
		attrs.Site = &site
	}

	if !data.OperatingSystem.IsNull() {
		osValue := data.OperatingSystem.ValueString()
		os := operations.CreateServerOperatingSystem(osValue)
		attrs.OperatingSystem = &os
	}

	if !data.Hostname.IsNull() {
		hostname := data.Hostname.ValueString()
		attrs.Hostname = &hostname
	}

	if !data.SSHKeys.IsNull() && !data.SSHKeys.IsUnknown() {
		var sshKeys []string
		resp.Diagnostics.Append(data.SSHKeys.ElementsAs(ctx, &sshKeys, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
		attrs.SSHKeys = sshKeys
	}

	if !data.UserData.IsNull() {
		userData := data.UserData.ValueString()
		attrs.UserData = &userData
	}

	if !data.Raid.IsNull() {
		raidValue := data.Raid.ValueString()
		raid := operations.CreateServerRaid(raidValue)
		attrs.Raid = &raid
	}

	if !data.Ipxe.IsNull() {
		ipxe := data.Ipxe.ValueString()
		attrs.Ipxe = &ipxe
	}

	if !data.Billing.IsNull() {
		billingValue := data.Billing.ValueString()
		billing := operations.CreateServerBilling(billingValue)
		attrs.Billing = &billing
	}

	createRequest := operations.CreateServerServersRequestBody{
		Data: &operations.CreateServerServersData{
			Type:       operations.CreateServerServersTypeServers,
			Attributes: attrs,
		},
	}

	result, err := r.client.Servers.Create(ctx, createRequest)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to create server, got error: "+err.Error())
		return
	}

	if result.Server == nil || result.Server.Data == nil || result.Server.Data.ID == nil {
		resp.Diagnostics.AddError("API Error", "Failed to get server ID from response")
		return
	}

	data.ID = types.StringValue(*result.Server.Data.ID)

	// Apply tags if specified (server creation doesn't support tags)
	if !data.Tags.IsNull() && !data.Tags.IsUnknown() {
		var tagIDs []string
		resp.Diagnostics.Append(data.Tags.ElementsAs(ctx, &tagIDs, false)...)
		if resp.Diagnostics.HasError() {
			return
		}

		if len(tagIDs) > 0 {
			err := r.validateTagIDs(ctx, tagIDs)
			if err != nil {
				resp.Diagnostics.AddError("Tag Validation Error", "Unable to validate tag IDs: "+err.Error())
				return
			}

			// Get current hostname to preserve it during tag update
			var hostnamePtr *string
			if !data.Hostname.IsNull() {
				hostname := data.Hostname.ValueString()
				hostnamePtr = &hostname
			}

			err = r.updateServerTags(ctx, data.ID.ValueString(), tagIDs, hostnamePtr)
			if err != nil {
				resp.Diagnostics.AddError("Tag Update Error", "Unable to update server with tags: "+err.Error())
				return
			}
		}
	}

	// Store the planned values that we want to preserve
	plannedHostname := data.Hostname
	plannedProject := data.Project
	plannedSite := data.Site
	plannedPlan := data.Plan
	plannedOperatingSystem := data.OperatingSystem
	plannedBilling := data.Billing
	plannedTags := data.Tags

	// Read server to get computed values
	r.readServer(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	// Restore the planned values for fields we set during creation
	// This ensures consistency between plan and apply
	if !plannedHostname.IsNull() {
		data.Hostname = plannedHostname
	}
	if !plannedProject.IsNull() {
		data.Project = plannedProject
	}
	if !plannedSite.IsNull() {
		data.Site = plannedSite
	}
	if !plannedPlan.IsNull() {
		data.Plan = plannedPlan
	}
	if !plannedOperatingSystem.IsNull() {
		data.OperatingSystem = plannedOperatingSystem
	}
	if !plannedBilling.IsNull() {
		data.Billing = plannedBilling
	}
	if !plannedTags.IsNull() {
		data.Tags = plannedTags
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ServerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data ServerResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.readServer(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ServerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data ServerResourceModel
	var currentData ServerResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Get current state to preserve existing values
	resp.Diagnostics.Append(req.State.Get(ctx, &currentData)...)
	if resp.Diagnostics.HasError() {
		return
	}

	serverID := data.ID.ValueString()
	attrs := &operations.UpdateServerServersRequestApplicationJSONAttributes{}

	// Always include hostname (from plan if set, otherwise from current state)
	if !data.Hostname.IsNull() {
		hostname := data.Hostname.ValueString()
		attrs.Hostname = &hostname
	} else if !currentData.Hostname.IsNull() {
		hostname := currentData.Hostname.ValueString()
		attrs.Hostname = &hostname
	}

	// Always include billing (from plan if set, otherwise from current state)
	if !data.Billing.IsNull() {
		billingValue := data.Billing.ValueString()
		billing := operations.UpdateServerServersRequestApplicationJSONBilling(billingValue)
		attrs.Billing = &billing
	} else if !currentData.Billing.IsNull() {
		billingValue := currentData.Billing.ValueString()
		billing := operations.UpdateServerServersRequestApplicationJSONBilling(billingValue)
		attrs.Billing = &billing
	}

	// Handle tags
	if !data.Tags.IsNull() && !data.Tags.IsUnknown() {
		var tagIDs []string
		resp.Diagnostics.Append(data.Tags.ElementsAs(ctx, &tagIDs, false)...)
		if resp.Diagnostics.HasError() {
			return
		}

		err := r.validateTagIDs(ctx, tagIDs)
		if err != nil {
			resp.Diagnostics.AddError("Tag Validation Error", "Unable to validate tag IDs: "+err.Error())
			return
		}
		attrs.Tags = tagIDs
	} else if !currentData.Tags.IsNull() && !currentData.Tags.IsUnknown() {
		var tagIDs []string
		resp.Diagnostics.Append(currentData.Tags.ElementsAs(ctx, &tagIDs, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
		attrs.Tags = tagIDs
	}

	// Always include project (from plan if set, otherwise from current state)
	if !data.Project.IsNull() {
		project := data.Project.ValueString()
		attrs.Project = &project
	} else if !currentData.Project.IsNull() {
		project := currentData.Project.ValueString()
		attrs.Project = &project
	}

	updateType := operations.UpdateServerServersRequestApplicationJSONTypeServers
	updateRequest := operations.UpdateServerServersRequestBody{
		Data: &operations.UpdateServerServersData{
			ID:         &serverID,
			Type:       &updateType,
			Attributes: attrs,
		},
	}

	result, err := r.client.Servers.Update(ctx, serverID, updateRequest)
	if err != nil {
		if err.Error() != "{}" {
			resp.Diagnostics.AddError("Client Error", "Unable to update server, got error: "+err.Error())
			return
		}
	}

	if result != nil && result.HTTPMeta.Response != nil {
		statusCode := result.HTTPMeta.Response.StatusCode
		if statusCode >= 400 {
			resp.Diagnostics.AddError("API Error", "Server update failed with status code: "+string(rune(statusCode)))
			return
		}
	}

	// Store the planned values that we want to preserve
	plannedHostname := data.Hostname
	plannedBilling := data.Billing
	plannedTags := data.Tags
	plannedProject := data.Project

	// Read server to get updated computed values
	r.readServer(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	// Restore the planned values for fields we updated
	// This ensures consistency between plan and apply
	if !plannedHostname.IsNull() {
		data.Hostname = plannedHostname
	}
	if !plannedBilling.IsNull() {
		data.Billing = plannedBilling
	}
	if !plannedTags.IsNull() {
		data.Tags = plannedTags
	}
	if !plannedProject.IsNull() {
		data.Project = plannedProject
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ServerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data ServerResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	serverID := data.ID.ValueString()

	_, err := r.client.Servers.Delete(ctx, serverID, nil)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to delete server, got error: "+err.Error())
		return
	}
}

func (r *ServerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	var data ServerResourceModel
	data.ID = types.StringValue(req.ID)

	r.readServer(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ServerResource) readServer(ctx context.Context, data *ServerResourceModel, diags *diag.Diagnostics) {
	serverID := data.ID.ValueString()

	response, err := r.client.Servers.Get(ctx, serverID, nil)
	if err != nil {
		diags.AddError("Client Error", "Unable to read server, got error: "+err.Error())
		return
	}

	if response.Server == nil || response.Server.Data == nil {
		data.ID = types.StringNull()
		return
	}

	server := response.Server.Data
	if server.Attributes != nil {
		attrs := server.Attributes

		if attrs.Hostname != nil {
			data.Hostname = types.StringValue(*attrs.Hostname)
		}

		if attrs.Status != nil {
			data.Status = types.StringValue(string(*attrs.Status))
		}

		if attrs.PrimaryIpv4 != nil {
			data.PrimaryIpv4 = types.StringValue(*attrs.PrimaryIpv4)
		}

		if attrs.PrimaryIpv6 != nil {
			data.PrimaryIpv6 = types.StringValue(*attrs.PrimaryIpv6)
		}

		if attrs.Locked != nil {
			data.Locked = types.BoolValue(*attrs.Locked)
		}

		if attrs.CreatedAt != nil {
			data.CreatedAt = types.StringValue(*attrs.CreatedAt)
		}

		if attrs.Site != nil {
			data.Site = types.StringValue(*attrs.Site)
		}

		if attrs.Plan != nil {
			if attrs.Plan.Slug != nil {
				data.Plan = types.StringValue(*attrs.Plan.Slug)
			} else if attrs.Plan.ID != nil {
				data.Plan = types.StringValue(*attrs.Plan.ID)
			} else if attrs.Plan.Name != nil {
				data.Plan = types.StringValue(*attrs.Plan.Name)
			}
		}

		if attrs.OperatingSystem != nil && attrs.OperatingSystem.Slug != nil {
			data.OperatingSystem = types.StringValue(*attrs.OperatingSystem.Slug)
		}

		if attrs.Project != nil && attrs.Project.ID != nil {
			data.Project = types.StringValue(*attrs.Project.ID)
		}

		if attrs.Region != nil && attrs.Region.Site != nil && attrs.Region.Site.Slug != nil {
			data.Region = types.StringValue(*attrs.Region.Site.Slug)
		}
	}
}

func (r *ServerResource) validateTagIDs(ctx context.Context, tagIDs []string) error {
	if len(tagIDs) == 0 {
		return nil
	}

	response, err := r.client.Tags.List(ctx)
	if err != nil {
		return err
	}

	if response.CustomTags == nil || response.CustomTags.Data == nil {
		return fmt.Errorf("no tags found")
	}

	validTagIDs := make(map[string]bool)
	for _, tag := range response.CustomTags.Data {
		if tag.ID != nil {
			validTagIDs[*tag.ID] = true
		}
	}

	for _, tagID := range tagIDs {
		if !validTagIDs[tagID] {
			return fmt.Errorf("tag ID '%s' not found", tagID)
		}
	}

	return nil
}

func (r *ServerResource) updateServerTags(ctx context.Context, serverID string, tagIDs []string, hostname *string) error {
	attrs := &operations.UpdateServerServersRequestApplicationJSONAttributes{
		Tags: tagIDs,
	}

	// Preserve hostname if provided
	if hostname != nil {
		attrs.Hostname = hostname
	}

	updateType := operations.UpdateServerServersRequestApplicationJSONTypeServers
	updateRequest := operations.UpdateServerServersRequestBody{
		Data: &operations.UpdateServerServersData{
			ID:         &serverID,
			Type:       &updateType,
			Attributes: attrs,
		},
	}

	result, err := r.client.Servers.Update(ctx, serverID, updateRequest)
	if err != nil {
		if err.Error() != "{}" {
			return fmt.Errorf("unable to update server with tags: %w", err)
		}
	}

	if result != nil && result.HTTPMeta.Response != nil {
		statusCode := result.HTTPMeta.Response.StatusCode
		if statusCode >= 400 {
			return fmt.Errorf("server tag update failed with status code: %d", statusCode)
		}
	}

	return nil
}
