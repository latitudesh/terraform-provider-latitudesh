package latitudesh

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
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
	ReinstallReason types.String `tfsdk:"reinstall_reason"`
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
<<<<<<< HEAD
				Computed:            true,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
					sshKeysReinstallWarningModifier{},
				},
=======
>>>>>>> a5e9f67 (fix: ssh keys computing)
			},
			"user_data": schema.StringAttribute{
				MarkdownDescription: "User data ID to assign to the server (reference to latitudesh_user_data resource)",
				Optional:            true,
				PlanModifiers: []planmodifier.String{
					userDataReinstallWarningModifier{},
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
				MarkdownDescription: "The server billing type (hourly, monthly, yearly)",
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
			},
			"reinstall_reason": schema.StringAttribute{
				MarkdownDescription: "Reason for the last server reinstallation",
				Computed:            true,
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

	// Store the planned values that we want to preserve
	plannedHostname := data.Hostname
	plannedProject := data.Project
	plannedSite := data.Site
	plannedPlan := data.Plan
	plannedOperatingSystem := data.OperatingSystem
	plannedBilling := data.Billing
	plannedTags := data.Tags
<<<<<<< HEAD
	plannedSSHKeys := data.SSHKeys
	plannedUserData := data.UserData
	plannedRaid := data.Raid
	plannedIpxe := data.Ipxe
	plannedAllowReinstall := data.AllowReinstall
	plannedReinstallReason := data.ReinstallReason
=======
>>>>>>> a5e9f67 (fix: ssh keys computing)

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
<<<<<<< HEAD
	if !plannedSSHKeys.IsNull() {
		data.SSHKeys = plannedSSHKeys
	}
	if !plannedUserData.IsNull() {
		data.UserData = plannedUserData
	}
	if !plannedRaid.IsNull() {
		data.Raid = plannedRaid
	}
	if !plannedIpxe.IsNull() {
		data.Ipxe = plannedIpxe
	}
	if !plannedAllowReinstall.IsNull() {
		data.AllowReinstall = plannedAllowReinstall
	}
	if !plannedReinstallReason.IsNull() {
		data.ReinstallReason = plannedReinstallReason
	}
=======
>>>>>>> a5e9f67 (fix: ssh keys computing)

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

	// Also read deploy config to get SSH keys, raid, user data, and ipxe
	r.readDeployConfig(ctx, &data, &resp.Diagnostics)
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

	// Get current state to compare changes
	resp.Diagnostics.Append(req.State.Get(ctx, &currentData)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Read deploy config to get accurate SSH keys for comparison
	r.readDeployConfig(ctx, &currentData, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	// Determine what changed to decide between reinstall vs in-place update
	// Compare planned config vs current state (including current SSH keys from API)
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
			resp.Diagnostics.AddError(
				"Reinstall Required But Not Allowed",
				"Changes to operating_system, ssh_keys, user_data, raid, or ipxe require server reinstallation, "+
					"but allow_reinstall is set to false. Either set allow_reinstall=true or remove the conflicting changes.",
			)
			return
		}

		// Set the reinstall reason for tracking
		data.ReinstallReason = types.StringValue(reason)

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

		// Read deploy config to get updated SSH keys after reinstall
		r.readDeployConfig(ctx, &data, &resp.Diagnostics)
		if resp.Diagnostics.HasError() {
			return
		}
	} else {
		// Clear reinstall reason for in-place updates
		data.ReinstallReason = types.StringNull()

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

	if !planned.SSHKeys.Equal(current.SSHKeys) {
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
		if !convertDiags.HasError() && len(sshKeys) > 0 {
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

	if !data.Billing.IsNull() {
		billingValue := data.Billing.ValueString()
		billing := operations.UpdateServerServersRequestApplicationJSONBilling(billingValue)
		attrs.Billing = &billing
	} else if !currentData.Billing.IsNull() {
		billingValue := currentData.Billing.ValueString()
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
	var data ServerResourceModel
	data.ID = types.StringValue(req.ID)

	// Initialize SSH keys since API doesn't return them and import creates fresh model
	data.SSHKeys = types.ListNull(types.StringType)

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

		if attrs.OperatingSystem != nil && attrs.OperatingSystem.Slug != nil {
			data.OperatingSystem = types.StringValue(*attrs.OperatingSystem.Slug)
		}

		if attrs.Project != nil && attrs.Project.ID != nil {
			data.Project = types.StringValue(*attrs.Project.ID)
		}

		if attrs.Region != nil && attrs.Region.Site != nil && attrs.Region.Site.Slug != nil {
			data.Region = types.StringValue(*attrs.Region.Site.Slug)
		}

		data.Tags = types.ListNull(types.StringType)
	}

	// Set default value for allow_reinstall if not set
	if data.AllowReinstall.IsNull() {
		data.AllowReinstall = types.BoolValue(true)
	}

	// Set default value for reinstall_reason if not set
	if data.ReinstallReason.IsNull() {
		data.ReinstallReason = types.StringValue("Initial creation")
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

	if attrs.SSHKeys != nil && len(attrs.SSHKeys) > 0 {
		sshKeysList, convertDiags := types.ListValueFrom(ctx, types.StringType, attrs.SSHKeys)
		diags.Append(convertDiags...)
		if !convertDiags.HasError() {
			data.SSHKeys = sshKeysList
		}
	} else {
		emptyList, convertDiags := types.ListValueFrom(ctx, types.StringType, []string{})
		diags.Append(convertDiags...)
		if !convertDiags.HasError() {
			data.SSHKeys = emptyList
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

// sshKeysReinstallWarningModifier shows warning when SSH keys change during plan phase
type sshKeysReinstallWarningModifier struct{}

func (m sshKeysReinstallWarningModifier) Description(ctx context.Context) string {
	return "Shows warning when SSH key changes trigger server reinstall"
}

func (m sshKeysReinstallWarningModifier) MarkdownDescription(ctx context.Context) string {
	return "Shows warning when SSH key changes trigger server reinstall"
}

func (m sshKeysReinstallWarningModifier) PlanModifyList(ctx context.Context, req planmodifier.ListRequest, resp *planmodifier.ListResponse) {
	var allowReinstall types.Bool
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("allow_reinstall"), &allowReinstall)...)
	if !allowReinstall.IsNull() && !allowReinstall.IsUnknown() && !allowReinstall.ValueBool() {
		return
	}

	if !req.StateValue.Equal(req.PlanValue) {
		resp.Diagnostics.AddWarning(
			"Server Reinstall Required",
			"SSH key changes will trigger a server reinstall. All data on the server will be lost unless backed up.",
		)
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
	var allowReinstall types.Bool
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("allow_reinstall"), &allowReinstall)...)
	if !allowReinstall.IsNull() && !allowReinstall.IsUnknown() && !allowReinstall.ValueBool() {
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

// ipxeReinstallWarningModifier shows warning when ipxe changes during plan phase
type ipxeReinstallWarningModifier struct{}

func (m ipxeReinstallWarningModifier) Description(ctx context.Context) string {
	return "Shows warning when ipxe changes trigger server reinstall"
}

func (m ipxeReinstallWarningModifier) MarkdownDescription(ctx context.Context) string {
	return "Shows warning when ipxe changes trigger server reinstall"
}

func (m ipxeReinstallWarningModifier) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
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
