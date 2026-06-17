package latitudesh

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
	iprovider "github.com/latitudesh/terraform-provider-latitudesh/v2/internal/provider"
)

var _ resource.Resource = &VirtualMachineResource{}
var _ resource.ResourceWithImportState = &VirtualMachineResource{}

func NewVirtualMachineResource() resource.Resource {
	return &VirtualMachineResource{}
}

// VirtualMachineResource defines the resource implementation.
type VirtualMachineResource struct {
	client         *latitudeshgosdk.Latitudesh
	defaultProject string
}

// VirtualMachineResourceModel describes the resource data model.
type VirtualMachineResourceModel struct {
	ID              types.String `tfsdk:"id"`
	Name            types.String `tfsdk:"name"`
	Plan            types.String `tfsdk:"plan"`
	Project         types.String `tfsdk:"project"`
	OperatingSystem types.String `tfsdk:"operating_system"`
	SSHKeys         types.List   `tfsdk:"ssh_keys"`
	Status          types.String `tfsdk:"status"`
	PrimaryIPv4     types.String `tfsdk:"primary_ipv4"`
	CreatedAt       types.String `tfsdk:"created_at"`
	SSHUser         types.String `tfsdk:"ssh_user"`
	VCPU            types.Int64  `tfsdk:"vcpu"`
	RAM             types.String `tfsdk:"ram"`
	Storage         types.String `tfsdk:"storage"`
	GPU             types.String `tfsdk:"gpu"`
}

func (r *VirtualMachineResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_virtual_machine"
}

func (r *VirtualMachineResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Virtual Machine resource. Creates and manages a virtual machine on [Latitude.sh](https://latitude.sh/).",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Virtual machine identifier",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"plan": schema.StringAttribute{
				MarkdownDescription: "The plan (ID or slug) for the virtual machine. Changing this forces a new resource.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The virtual machine name (hostname). Defaults to `my-vm` if not set.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"project": schema.StringAttribute{
				MarkdownDescription: "The project (ID or slug) to deploy the virtual machine. If not set, the provider-level `project` is used. Changing this forces a new resource.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"operating_system": schema.StringAttribute{
				MarkdownDescription: "The operating system slug for the virtual machine. If not specified, the API defaults to `ubuntu-24-04` for CPU plans or `ubuntu24_ml_in_a_box` for GPU plans. Changing this forces a new resource.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"ssh_keys": schema.ListAttribute{
				MarkdownDescription: "List of SSH key IDs to add to the virtual machine. Changing this forces a new resource.",
				ElementType:         types.StringType,
				Optional:            true,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.RequiresReplace(),
				},
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "Virtual machine status",
				Computed:            true,
			},
			"primary_ipv4": schema.StringAttribute{
				MarkdownDescription: "The primary IPv4 address of the virtual machine",
				Computed:            true,
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "The timestamp for when the virtual machine was created",
				Computed:            true,
			},
			"ssh_user": schema.StringAttribute{
				MarkdownDescription: "The default SSH username for the virtual machine",
				Computed:            true,
			},
			"vcpu": schema.Int64Attribute{
				MarkdownDescription: "Number of vCPUs",
				Computed:            true,
			},
			"ram": schema.StringAttribute{
				MarkdownDescription: "Amount of RAM",
				Computed:            true,
			},
			"storage": schema.StringAttribute{
				MarkdownDescription: "Amount of storage",
				Computed:            true,
			},
			"gpu": schema.StringAttribute{
				MarkdownDescription: "GPU information, if any",
				Computed:            true,
			},
		},
	}
}

func (r *VirtualMachineResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	deps := iprovider.ConfigureFromProviderData(req.ProviderData, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	r.client = deps.Client
	r.defaultProject = deps.DefaultProject
}

func (r *VirtualMachineResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data VirtualMachineResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	project := ""
	if !data.Project.IsNull() && !data.Project.IsUnknown() && data.Project.ValueString() != "" {
		project = data.Project.ValueString()
	} else if r.defaultProject != "" {
		project = r.defaultProject
	}
	if project == "" {
		resp.Diagnostics.AddError("Missing project",
			"Set `project` on this resource or define a default in the provider block (provider `latitudesh` { project = \"...\" }).")
		return
	}

	plan := data.Plan.ValueString()
	attrs := &components.VirtualMachinePayloadAttributes{
		Plan:    &plan,
		Project: &project,
	}

	if !data.Name.IsNull() && !data.Name.IsUnknown() && data.Name.ValueString() != "" {
		name := data.Name.ValueString()
		attrs.Name = &name
	}

	if !data.OperatingSystem.IsNull() && !data.OperatingSystem.IsUnknown() && data.OperatingSystem.ValueString() != "" {
		os := data.OperatingSystem.ValueString()
		attrs.OperatingSystem = &os
	}

	if !data.SSHKeys.IsNull() && !data.SSHKeys.IsUnknown() {
		var sshKeys []string
		resp.Diagnostics.Append(data.SSHKeys.ElementsAs(ctx, &sshKeys, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
		attrs.SSHKeys = sshKeys
	}

	payload := components.VirtualMachinePayload{
		Data: &components.VirtualMachinePayloadData{
			Type:       components.VirtualMachinePayloadTypeVirtualMachines.ToPointer(),
			Attributes: attrs,
		},
	}

	result, err := r.client.VirtualMachines.Create(ctx, payload)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to create virtual machine, got error: "+err.Error())
		return
	}

	if result.VirtualMachine == nil || result.VirtualMachine.Data == nil || result.VirtualMachine.Data.ID == nil {
		resp.Diagnostics.AddError("API Error", "Failed to get virtual machine ID from response")
		return
	}

	id := *result.VirtualMachine.Data.ID
	data.ID = types.StringValue(id)
	data.Project = types.StringValue(project)

	// Persist the ID before the (potentially long) wait so the VM is tracked in
	// state even if polling times out; otherwise it leaks as an orphan.
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.waitForVMReady(ctx, id, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	r.readVirtualMachine(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *VirtualMachineResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data VirtualMachineResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.readVirtualMachine(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	// If the VM no longer exists, drop it from state.
	if data.ID.IsNull() {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *VirtualMachineResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data VirtualMachineResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Load the existing ID from state; desired attributes come from the plan.
	var id types.String
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("id"), &id)...)
	if resp.Diagnostics.HasError() {
		return
	}
	data.ID = id

	idStr := id.ValueString()
	name := data.Name.ValueString()

	updatePayload := components.VirtualMachineUpdatePayload{
		Data: components.VirtualMachineUpdatePayloadData{
			Type: components.VirtualMachineUpdatePayloadTypeVirtualMachines,
			ID:   &idStr,
			Attributes: components.VirtualMachineUpdatePayloadAttributes{
				Name: name,
			},
		},
	}

	_, err := r.client.VirtualMachines.UpdateVirtualMachine(ctx, idStr, updatePayload)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to update virtual machine, got error: "+err.Error())
		return
	}

	r.readVirtualMachine(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *VirtualMachineResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data VirtualMachineResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := data.ID.ValueString()

	_, err := r.client.VirtualMachines.Delete(ctx, id)
	if err != nil {
		var apiErr *components.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			return
		}
		if strings.Contains(err.Error(), "404") {
			return
		}
		resp.Diagnostics.AddError("Client Error", "Unable to delete virtual machine, got error: "+err.Error())
		return
	}
}

func (r *VirtualMachineResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *VirtualMachineResource) waitForVMReady(ctx context.Context, id string, diags *diag.Diagnostics) {
	const (
		timeout      = 10 * time.Minute
		pollInterval = 15 * time.Second
	)

	deadline := time.Now().Add(timeout)
	lastStatus := ""

	for time.Now().Before(deadline) {
		result, err := r.client.VirtualMachines.Get(ctx, id)
		if err != nil {
			// Tolerate transient errors and keep polling until the deadline.
			select {
			case <-ctx.Done():
				diags.AddError("Client Error", "Context cancelled while waiting for virtual machine to be ready: "+ctx.Err().Error())
				return
			case <-time.After(pollInterval):
				continue
			}
		}

		if result.VirtualMachine != nil && result.VirtualMachine.Data != nil && result.VirtualMachine.Data.Attributes != nil {
			attrs := result.VirtualMachine.Data.Attributes
			if attrs.Status != nil {
				lastStatus = string(*attrs.Status)
			}
			hasIP := attrs.PrimaryIpv4 != nil && *attrs.PrimaryIpv4 != ""
			if attrs.Status != nil && *attrs.Status == components.VirtualMachineAttributesStatusRunning && hasIP {
				return
			}
		}

		select {
		case <-ctx.Done():
			diags.AddError("Client Error", "Context cancelled while waiting for virtual machine to be ready: "+ctx.Err().Error())
			return
		case <-time.After(pollInterval):
		}
	}

	diags.AddError(
		"Timeout waiting for virtual machine",
		fmt.Sprintf("Virtual machine %q did not reach a running state with a primary IPv4 within %s (last status: %q).", id, timeout, lastStatus),
	)
}

func (r *VirtualMachineResource) readVirtualMachine(ctx context.Context, data *VirtualMachineResourceModel, diags *diag.Diagnostics) {
	id := data.ID.ValueString()

	result, err := r.client.VirtualMachines.Get(ctx, id)
	if err != nil {
		var apiErr *components.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			data.ID = types.StringNull()
			return
		}
		diags.AddError("Client Error", "Unable to read virtual machine, got error: "+err.Error())
		return
	}

	if result.VirtualMachine == nil || result.VirtualMachine.Data == nil {
		data.ID = types.StringNull()
		return
	}

	vm := result.VirtualMachine.Data
	if vm.ID != nil {
		data.ID = types.StringValue(*vm.ID)
	}

	a := vm.Attributes
	if a == nil {
		return
	}

	if a.Name != nil {
		data.Name = types.StringValue(*a.Name)
	}

	if a.Status != nil {
		data.Status = types.StringValue(string(*a.Status))
	} else {
		data.Status = types.StringNull()
	}

	if a.PrimaryIpv4 != nil {
		data.PrimaryIPv4 = types.StringValue(*a.PrimaryIpv4)
	} else {
		data.PrimaryIPv4 = types.StringNull()
	}

	if a.CreatedAt != nil {
		data.CreatedAt = types.StringValue(*a.CreatedAt)
	}

	if (data.OperatingSystem.IsNull() || data.OperatingSystem.IsUnknown()) && a.OperatingSystem != nil && a.OperatingSystem.Slug != nil {
		data.OperatingSystem = types.StringValue(*a.OperatingSystem.Slug)
	}

	if (data.Plan.IsNull() || data.Plan.IsUnknown()) && a.Plan != nil && a.Plan.ID != nil {
		data.Plan = types.StringValue(*a.Plan.ID)
	}

	if (data.Project.IsNull() || data.Project.IsUnknown()) && a.Project != nil {
		if a.Project.Slug != nil {
			data.Project = types.StringValue(*a.Project.Slug)
		} else if a.Project.ID != nil {
			data.Project = types.StringValue(*a.Project.ID)
		}
	}

	if a.Credentials != nil && a.Credentials.Username != nil {
		data.SSHUser = types.StringValue(*a.Credentials.Username)
	} else {
		data.SSHUser = types.StringNull()
	}

	if a.Specs != nil {
		if a.Specs.Vcpu != nil {
			data.VCPU = types.Int64Value(*a.Specs.Vcpu)
		} else {
			data.VCPU = types.Int64Null()
		}
		if a.Specs.RAM != nil {
			data.RAM = types.StringValue(*a.Specs.RAM)
		} else {
			data.RAM = types.StringNull()
		}
		if a.Specs.Storage != nil {
			data.Storage = types.StringValue(*a.Specs.Storage)
		} else {
			data.Storage = types.StringNull()
		}
		if a.Specs.Gpu != nil {
			data.GPU = types.StringValue(*a.Specs.Gpu)
		} else {
			data.GPU = types.StringNull()
		}
	} else {
		data.VCPU = types.Int64Null()
		data.RAM = types.StringNull()
		data.Storage = types.StringNull()
		data.GPU = types.StringNull()
	}

	if data.SSHKeys.IsUnknown() {
		data.SSHKeys = types.ListNull(types.StringType)
	}
}
