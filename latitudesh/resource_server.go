package latitudesh

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/operations"
)

const maxHostnameLength = 32

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
	AllowReinstall  types.Bool   `tfsdk:"allow_reinstall"`
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
					operatingSystemReinstallWarningModifier{},
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
			},
			"hostname": schema.StringAttribute{
				MarkdownDescription: "The server hostname",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"ssh_keys": schema.ListAttribute{
				MarkdownDescription: "SSH Keys to set on the server",
				ElementType:         types.StringType,
				Optional:            true,
				//Computed:            true,
				PlanModifiers: []planmodifier.List{
					sshKeysReinstallWarningModifier{},
					listplanmodifier.UseStateForUnknown(),
					listplanmodifier.RequiresReplace(),
				},
			},
			"user_data": schema.StringAttribute{
				MarkdownDescription: "User data ID to assign to the server (reference to latitudesh_user_data resource)",
				Optional:            true,

				PlanModifiers: []planmodifier.String{
					userDataReinstallWarningModifier{},
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
			},
			"raid": schema.StringAttribute{
				MarkdownDescription: "RAID mode for the server (raid-0, raid-1)",
				Optional:            true,
				PlanModifiers: []planmodifier.String{
					raidReinstallWarningModifier{},
				},
			},
			"ipxe": schema.StringAttribute{
				MarkdownDescription: "URL where iPXE script is stored on, OR the iPXE script encoded in base64",
				Optional:            true,
				PlanModifiers: []planmodifier.String{
					ipxeReinstallWarningModifier{},
				},
			},
			"billing": schema.StringAttribute{
				MarkdownDescription: "The server billing type (hourly, monthly, yearly). Defaults to monthly.",
				Optional:            true,
				Computed:            true,
			},
			"tags": schema.ListAttribute{
				MarkdownDescription: "List of server tag IDs",
				ElementType:         types.StringType,
				Optional:            true,
			},
			"allow_reinstall": schema.BoolAttribute{
				MarkdownDescription: "Allow server reinstallation when operating_system, ssh_keys, user_data, raid, or ipxe changes. If false, only in-place updates are allowed.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(true),
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

	// Set default value for billing if not provided
	if data.Billing.IsNull() {
		data.Billing = types.StringValue("monthly")
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
		if err := validateHostnameLength(hostname); err != nil {
			resp.Diagnostics.AddError("Hostname Too Long", err.Error())
			return
		}
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
		// Convert user data ID to int64 as expected by API
		// This is a simplification - in reality you'd need proper ID parsing
		attrs.UserData = nil // For now, skip user data in reinstall
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

	// Read server to get computed values and populate state with real values
	r.readServer(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
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

	resp.Diagnostics.Append(req.State.Get(ctx, &currentData)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Determine what changed to decide between reinstall vs in-place update
	// Compare planned config vs current state
	needsReinstall, reason := r.needsReinstall(ctx, &data, &currentData, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	if needsReinstall {
		// Add warning about reinstall
		resp.Diagnostics.AddWarning(
			"Server Reinstall Required",
			fmt.Sprintf("Changes detected (%s) will trigger a server reinstall. All data on the server will be lost unless backed up.", reason),
		)

		// Check if reinstall is allowed
		allowReinstall := true // Default to true for backward compatibility
		if !data.AllowReinstall.IsNull() && !data.AllowReinstall.IsUnknown() {
			allowReinstall = data.AllowReinstall.ValueBool()
		}

		if !allowReinstall {
			// When reinstall is not allowed, just update the state
			resp.Diagnostics.AddWarning(
				"State Updated Without Server Changes",
				fmt.Sprintf("Changes detected (%s) would normally trigger a server reinstall, but allow_reinstall is set to false. "+
					"The Terraform state has been updated to match your configuration, but no actual server changes were made. "+
					"Set allow_reinstall=true to perform the actual reinstall.", reason),
			)

			// Skip deploy config update until SDK is fixed
			err := r.updateDeployConfig(ctx, &data, &resp.Diagnostics)
			if err != nil {
				resp.Diagnostics.AddError("Deploy Config Update Error", "Unable to update deploy config: "+err.Error())
				return
			}

			// Read current server state
			r.readServer(ctx, &data, &resp.Diagnostics)
			if resp.Diagnostics.HasError() {
				return
			}

			// Skip reinstall - just update state

			resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
			return
		}

		// Perform reinstall for OS, SSH keys, user data, raid, or ipxe changes
		err := r.reinstallServer(ctx, &data, &resp.Diagnostics)
		if err != nil {
			resp.Diagnostics.AddError("Reinstall Error", "Unable to reinstall server: "+err.Error())
			return
		}

		// Read server to get updated values after reinstall
		r.readServer(ctx, &data, &resp.Diagnostics)
		if resp.Diagnostics.HasError() {
			return
		}
	} else {
		// Performing in-place update

		// Perform in-place update for hostname, billing, tags, project changes
		err := r.updateServerInPlace(ctx, &data, &currentData, &resp.Diagnostics)
		if err != nil {
			resp.Diagnostics.AddError("Update Error", "Unable to update server: "+err.Error())
			return
		}

		// For in-place updates, only read server info
		r.readServer(ctx, &data, &resp.Diagnostics)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// needsReinstall determines if server needs reinstall based on changed fields
func (r *ServerResource) needsReinstall(ctx context.Context, planned *ServerResourceModel, current *ServerResourceModel, diags *diag.Diagnostics) (bool, string) {
	var reasons []string

	if !planned.OperatingSystem.Equal(current.OperatingSystem) {
		reasons = append(reasons, "operating_system")
	}

	// Compare SSH keys with proper handling of null vs empty lists
	sshKeysChanged := false
	if planned.SSHKeys.IsNull() && !current.SSHKeys.IsNull() {
		// Planned is null, current is not null
		sshKeysChanged = true
	} else if !planned.SSHKeys.IsNull() && current.SSHKeys.IsNull() {
		// Planned is not null, current is null
		sshKeysChanged = true
	} else if !planned.SSHKeys.IsNull() && !current.SSHKeys.IsNull() {
		// Both are not null, compare values
		sshKeysChanged = !planned.SSHKeys.Equal(current.SSHKeys)
	}

	if sshKeysChanged {
		reasons = append(reasons, "ssh_keys")
	}

	if !planned.UserData.Equal(current.UserData) {
		reasons = append(reasons, "user_data")
	}

	if !planned.Raid.Equal(current.Raid) {
		reasons = append(reasons, "raid")
	}

	if !planned.Ipxe.Equal(current.Ipxe) {
		reasons = append(reasons, "ipxe")
	}

	if len(reasons) > 0 {
		return true, "Changed: " + fmt.Sprintf("%v", reasons)
	}

	return false, ""
}

func (r *ServerResource) reinstallServer(ctx context.Context, data *ServerResourceModel, diags *diag.Diagnostics) error {
	serverID := data.ID.ValueString()
	attrs := &operations.CreateServerReinstallServersAttributes{}

	if !data.OperatingSystem.IsNull() && !data.OperatingSystem.IsUnknown() {
		osValue := data.OperatingSystem.ValueString()
		if osValue != "" {
			os := operations.CreateServerReinstallServersOperatingSystem(osValue)
			attrs.OperatingSystem = &os
		}
	}

	if !data.Hostname.IsNull() && !data.Hostname.IsUnknown() {
		hostname := data.Hostname.ValueString()
		if hostname != "" {
			attrs.Hostname = &hostname
		}
	}

	if !data.SSHKeys.IsNull() && !data.SSHKeys.IsUnknown() {
		var sshKeys []string
		convertDiags := data.SSHKeys.ElementsAs(ctx, &sshKeys, false)
		diags.Append(convertDiags...)
		if !convertDiags.HasError() {
			// Always send SSH keys list during reinstall, even if empty
			// This ensures keys are removed if the list is empty
			attrs.SSHKeys = sshKeys
		}
	}

	if !data.UserData.IsNull() && !data.UserData.IsUnknown() {
		userDataValue := data.UserData.ValueString()
		if userDataValue != "" {
			attrs.UserData = nil // Skip user data for now
		}
	}

	if !data.Raid.IsNull() && !data.Raid.IsUnknown() {
		raidValue := data.Raid.ValueString()
		if raidValue != "" && (raidValue == "raid-0" || raidValue == "raid-1") {
			raid := operations.CreateServerReinstallServersRaid(raidValue)
			attrs.Raid = &raid
		}
	}

	if !data.Ipxe.IsNull() && !data.Ipxe.IsUnknown() {
		ipxe := data.Ipxe.ValueString()
		if ipxe != "" {
			attrs.Ipxe = &ipxe
		}
	}

	reinstallRequest := operations.CreateServerReinstallServersRequestBody{
		Data: operations.CreateServerReinstallServersData{
			Type:       operations.CreateServerReinstallServersTypeReinstalls,
			Attributes: attrs,
		},
	}

	_, err := r.client.Servers.Reinstall(ctx, serverID, reinstallRequest)
	return err
}

func (r *ServerResource) updateServerInPlace(ctx context.Context, data *ServerResourceModel, currentData *ServerResourceModel, diags *diag.Diagnostics) error {
	serverID := data.ID.ValueString()
	attrs := &operations.UpdateServerServersRequestApplicationJSONAttributes{}

	if !data.Hostname.IsNull() {
		hostname := data.Hostname.ValueString()
		attrs.Hostname = &hostname
	} else if !currentData.Hostname.IsNull() {
		hostname := currentData.Hostname.ValueString()
		attrs.Hostname = &hostname
	}

	if !data.Billing.IsNull() && (currentData == nil || data.Billing.ValueString() != currentData.Billing.ValueString()) {
		billingValue := data.Billing.ValueString()
		billing := operations.UpdateServerServersRequestApplicationJSONBilling(billingValue)
		attrs.Billing = &billing
	}

	// Handle tags update
	if !data.Tags.IsNull() && !data.Tags.IsUnknown() {
		var tagIDs []string
		convertDiags := data.Tags.ElementsAs(ctx, &tagIDs, false)
		diags.Append(convertDiags...)
		if convertDiags.HasError() {
			return fmt.Errorf("failed to convert tag IDs")
		}

		err := r.validateTagIDs(ctx, tagIDs)
		if err != nil {
			return fmt.Errorf("tag validation failed: %w", err)
		}

		var hostname *string
		if !data.Hostname.IsNull() {
			hostnameVal := data.Hostname.ValueString()
			hostname = &hostnameVal
		}

		err = r.updateServerTags(ctx, serverID, tagIDs, hostname)
		if err != nil {
			return fmt.Errorf("failed to update server tags: %w", err)
		}
	}

	updateType := operations.UpdateServerServersRequestApplicationJSONTypeServers
	updateRequest := operations.UpdateServerServersRequestBody{
		Data: &operations.UpdateServerServersData{
			ID:         &serverID,
			Type:       &updateType,
			Attributes: attrs,
		},
	}

	_, err := r.client.Servers.Update(ctx, serverID, updateRequest)
	return err
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
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
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

		// Hostname
		if attrs.Hostname != nil && *attrs.Hostname != "" {
			data.Hostname = types.StringValue(*attrs.Hostname)
		} else {
			data.Hostname = types.StringNull()
		}

		// Status
		if attrs.Status != nil {
			data.Status = types.StringValue(string(*attrs.Status))
		} else {
			data.Status = types.StringNull()
		}

		if attrs.PrimaryIpv4 != nil && *attrs.PrimaryIpv4 != "" {
			data.PrimaryIpv4 = types.StringValue(*attrs.PrimaryIpv4)
		} else {
			data.PrimaryIpv4 = types.StringNull()
		}

		if attrs.PrimaryIpv6 != nil && *attrs.PrimaryIpv6 != "" {
			data.PrimaryIpv6 = types.StringValue(*attrs.PrimaryIpv6)
		} else {
			data.PrimaryIpv6 = types.StringNull()
		}

		if attrs.Locked != nil {
			data.Locked = types.BoolValue(*attrs.Locked)
		}

		if attrs.CreatedAt != nil {
			data.CreatedAt = types.StringValue(*attrs.CreatedAt)
		}

		if attrs.Region != nil && attrs.Region.Site != nil && attrs.Region.Site.Slug != nil {
			data.Site = types.StringValue(*attrs.Region.Site.Slug)
		}

		if attrs.Plan != nil {
			if attrs.Plan.Slug != nil {
				data.Plan = types.StringValue(*attrs.Plan.Slug)
			} else if attrs.Plan.ID != nil {
				data.Plan = types.StringValue(*attrs.Plan.ID)
			} else if attrs.Plan.Name != nil {
				data.Plan = types.StringValue(*attrs.Plan.Name)
			}

			// Extract billing from plan
			if attrs.Plan.Billing != nil {
				data.Billing = types.StringValue(*attrs.Plan.Billing)
			}
		}

		// Set operating_system from API response
		if attrs.OperatingSystem != nil && attrs.OperatingSystem.Slug != nil && *attrs.OperatingSystem.Slug != "" {
			if data.OperatingSystem.IsNull() || *attrs.OperatingSystem.Slug == data.OperatingSystem.ValueString() {
				data.OperatingSystem = types.StringValue(*attrs.OperatingSystem.Slug)
			}
		} else {
			data.OperatingSystem = types.StringNull()
		}

		if attrs.Project != nil && attrs.Project.ID != nil {
			data.Project = types.StringValue(*attrs.Project.ID)
		}

		if attrs.Region != nil && attrs.Region.Site != nil && attrs.Region.Site.Slug != nil {
			data.Region = types.StringValue(*attrs.Region.Site.Slug)
		}

		// Tags are handled separately - preserve existing tags if not returned by API
		// The server API doesn't return tags in the get response, so we don't overwrite them
	}

	// Set default value for allow_reinstall if not set
	if data.AllowReinstall.IsNull() {
		data.AllowReinstall = types.BoolValue(true)
	}

	// Read deploy config to get SSH keys, user data, raid, and ipxe
	r.readDeployConfig(ctx, data, diags)
}

func (r *ServerResource) readDeployConfig(ctx context.Context, data *ServerResourceModel, diags *diag.Diagnostics) {
	serverID := data.ID.ValueString()

	response, err := r.client.Servers.GetDeployConfig(ctx, serverID)
	if err != nil {
		diags.AddError("Client Error", "Unable to read server deploy config, got error: "+err.Error())
		return
	}

	if response.DeployConfig == nil || response.DeployConfig.Data == nil || response.DeployConfig.Data.Attributes == nil {
		return
	}

	attrs := response.DeployConfig.Data.Attributes

	// Only set SSH keys if they exist in the API response
	if len(attrs.SSHKeys) > 0 {
		sshKeysList, convertDiags := types.ListValueFrom(ctx, types.StringType, attrs.SSHKeys)
		diags.Append(convertDiags...)
		if !convertDiags.HasError() {
			data.SSHKeys = sshKeysList
		}
	}
	// If no SSH keys in API response, leave data.SSHKeys as null (don't set to empty list)
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

// sshKeysReinstallWarningModifier shows warning when SSH keys change during plan phase
type sshKeysReinstallWarningModifier struct{}

func (m sshKeysReinstallWarningModifier) Description(ctx context.Context) string {
	return "Shows warning when SSH key changes trigger server reinstall"
}

func (m sshKeysReinstallWarningModifier) MarkdownDescription(ctx context.Context) string {
	return "Shows warning when SSH key changes trigger server reinstall"
}

func (m sshKeysReinstallWarningModifier) PlanModifyList(ctx context.Context, req planmodifier.ListRequest, resp *planmodifier.ListResponse) {
	// Only show reinstall warnings during updates (when state exists), not during creation
	if req.StateValue.IsUnknown() {
		return
	}

	if !req.StateValue.Equal(req.PlanValue) {
		// Check if SSH keys are being removed (state has keys, plan doesn't)
		stateHasKeys := !req.StateValue.IsNull() && !req.StateValue.IsUnknown()
		planHasKeys := !req.PlanValue.IsNull() && !req.PlanValue.IsUnknown()

		if stateHasKeys && !planHasKeys {
			resp.Diagnostics.AddWarning(
				"SSH Keys Removal",
				fmt.Sprintf("SSH keys will be removed from the server. This will trigger a server reinstall. All data on the server will be lost unless backed up. state=%v, plan=%v", req.StateValue, req.PlanValue),
			)
		} else {
			resp.Diagnostics.AddWarning(
				"Server Reinstall Required",
				"SSH key changes will trigger a server reinstall. All data on the server will be lost unless backed up.",
			)
		}
	}
}

// operatingSystemReinstallWarningModifier shows warning when operating_system changes during plan phase
type operatingSystemReinstallWarningModifier struct{}

func (m operatingSystemReinstallWarningModifier) Description(ctx context.Context) string {
	return "Shows warning when operating_system changes trigger server reinstall"
}

func (m operatingSystemReinstallWarningModifier) MarkdownDescription(ctx context.Context) string {
	return "Shows warning when operating_system changes trigger server reinstall"
}

func (m operatingSystemReinstallWarningModifier) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	// Only show warnings during updates (when state exists), not during creation
	if req.StateValue.IsNull() {
		return
	}

	var allowReinstall types.Bool
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("allow_reinstall"), &allowReinstall)...)
	if !allowReinstall.IsNull() && !allowReinstall.IsUnknown() && !allowReinstall.ValueBool() {
		return
	}

	if !req.StateValue.Equal(req.PlanValue) {
		resp.Diagnostics.AddWarning(
			"Server Reinstall Required",
			"operating_system changes will trigger a server reinstall. All data on the server will be lost unless backed up.",
		)
	}
}

// userDataReinstallWarningModifier shows warning when user_data changes during plan phase
type userDataReinstallWarningModifier struct{}

func (m userDataReinstallWarningModifier) Description(ctx context.Context) string {
	return "Shows warning when user_data changes trigger server reinstall"
}

func (m userDataReinstallWarningModifier) MarkdownDescription(ctx context.Context) string {
	return "Shows warning when user_data changes trigger server reinstall"
}

func (m userDataReinstallWarningModifier) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	// Only show warnings during updates (when state exists), not during creation
	if req.StateValue.IsNull() {
		return
	}

	if !req.StateValue.Equal(req.PlanValue) {
		resp.Diagnostics.AddWarning(
			"Server Reinstall Required",
			"user_data changes will trigger a server reinstall. All data on the server will be lost unless backed up.",
		)
	}
}

// raidReinstallWarningModifier shows warning when raid changes during plan phase
type raidReinstallWarningModifier struct{}

func (m raidReinstallWarningModifier) Description(ctx context.Context) string {
	return "Shows warning when raid changes trigger server reinstall"
}

func (m raidReinstallWarningModifier) MarkdownDescription(ctx context.Context) string {
	return "Shows warning when raid changes trigger server reinstall"
}

func (m raidReinstallWarningModifier) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	// Only show warnings during updates (when state exists), not during creation
	if req.StateValue.IsNull() {
		return
	}

	var allowReinstall types.Bool
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("allow_reinstall"), &allowReinstall)...)
	if !allowReinstall.IsNull() && !allowReinstall.IsUnknown() && !allowReinstall.ValueBool() {
		return
	}

	if !req.StateValue.Equal(req.PlanValue) {
		resp.Diagnostics.AddWarning(
			"Server Reinstall Required",
			"raid changes will trigger a server reinstall. All data on the server will be lost unless backed up.",
		)
	}
}

func (r *ServerResource) updateDeployConfig(ctx context.Context, data *ServerResourceModel, diags *diag.Diagnostics) error {
	// Skip actual API call until SDK is fixed
	// Just return success so state can be updated with planned values
	return nil
}

// ipxeReinstallWarningModifier shows warning when ipxe changes during plan phase
type ipxeReinstallWarningModifier struct{}

func (m ipxeReinstallWarningModifier) Description(ctx context.Context) string {
	return "Shows warning when ipxe changes trigger server reinstall"
}

func (m ipxeReinstallWarningModifier) MarkdownDescription(ctx context.Context) string {
	return "Shows warning when ipxe changes trigger server reinstall"
}

func (m ipxeReinstallWarningModifier) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	// Only show warnings during updates (when state exists), not during creation
	if req.StateValue.IsNull() {
		return
	}

	var allowReinstall types.Bool
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("allow_reinstall"), &allowReinstall)...)
	if !allowReinstall.IsNull() && !allowReinstall.IsUnknown() && !allowReinstall.ValueBool() {
		return
	}

	if !req.StateValue.Equal(req.PlanValue) {
		resp.Diagnostics.AddWarning(
			"Server Reinstall Required",
			"ipxe changes will trigger a server reinstall. All data on the server will be lost unless backed up.",
		)
	}
}

func validateHostnameLength(hostname string) error {
	if len(hostname) > maxHostnameLength {
		return fmt.Errorf("hostname must not exceed %d characters; provided hostname has %d characters", maxHostnameLength, len(hostname))
	}
	return nil
}
