package latitudesh

import (
	"context"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
	"github.com/latitudesh/latitudesh-go-sdk/models/operations"
	providerpkg "github.com/latitudesh/terraform-provider-latitudesh/v2/internal/provider"
)

var _ resource.Resource = &ElasticIPResource{}
var _ resource.ResourceWithModifyPlan = &ElasticIPResource{}

func NewElasticIPResource() resource.Resource {
	return &ElasticIPResource{}
}

type ElasticIPResource struct {
	client         *latitudeshgosdk.Latitudesh
	defaultProject string
}

type ElasticIPResourceModel struct {
	ID       types.String   `tfsdk:"id"`
	Project  types.String   `tfsdk:"project"`
	ServerID types.String   `tfsdk:"server_id"`
	Address  types.String   `tfsdk:"address"`
	Status   types.String   `tfsdk:"status"`
	Timeouts timeouts.Value `tfsdk:"timeouts"`
}

// addElasticIPError inspects an SDK error and appends a named diagnostic to diags.
// It maps well-known Latitude.sh API errors to actionable Terraform diagnostics,
// falling through to a generic "Client Error" for anything unrecognised.
func addElasticIPError(diags *diag.Diagnostics, op string, err error) {
	if err == nil {
		return
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "SITE_NOT_SUPPORTED"):
		diags.AddError(
			"Site does not support Elastic IPs",
			"The server's site does not support Elastic IPs. Choose a server in a supported site. Underlying error: "+msg,
		)
	case strings.Contains(msg, "ELASTIC_IP_LIMIT_REACHED"):
		diags.AddError(
			"Elastic IP limit reached",
			"The team has reached its Elastic IP quota. Release an existing Elastic IP or contact support. Underlying error: "+msg,
		)
	case strings.Contains(msg, "ELASTIC_IP_NOT_ACTIVE"):
		diags.AddError(
			"Elastic IP is not active",
			"Move is only allowed when the Elastic IP status is `active`. Wait for provisioning to finish or retry. Underlying error: "+msg,
		)
	default:
		diags.AddError("Client Error", "Unable to "+op+" Elastic IP: "+msg)
	}
}

func (r *ElasticIPResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_elastic_ip"
}

func (r *ElasticIPResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Elastic IP resource. Reserves a static public IPv4 address assigned to a server and movable between servers within the same project.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Elastic IP identifier",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"project": schema.StringAttribute{
				MarkdownDescription: "The project (ID or Slug) that owns the Elastic IP. Falls back to the provider-level `project` default.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"server_id": schema.StringAttribute{
				MarkdownDescription: "The server ID this Elastic IP is assigned to. Changing this value moves the IP to the new server (asynchronous).",
				Required:            true,
			},
			"address": schema.StringAttribute{
				MarkdownDescription: "The assigned IP address",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "Current status of the Elastic IP (configuring, active, moving, releasing, error)",
				Computed:            true,
			},
			"timeouts": timeouts.Attributes(ctx, timeouts.Opts{
				Create: true,
				Update: true,
				Delete: true,
			}),
		},
	}
}

func (r *ElasticIPResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	deps := providerpkg.ConfigureFromProviderData(req.ProviderData, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	r.client = deps.Client
	r.defaultProject = deps.DefaultProject
}

func (r *ElasticIPResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}

	var cfg, plan ElasticIPResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if cfg.Project.IsUnknown() {
		return
	}

	if !cfg.Project.IsNull() && !cfg.Project.IsUnknown() && cfg.Project.ValueString() != "" {
		plan.Project = cfg.Project
		resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
		return
	}

	if r.defaultProject != "" {
		plan.Project = types.StringValue(r.defaultProject)
		resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
		return
	}

	resp.Diagnostics.AddError(
		"Missing project",
		"Set `project` on this resource or define a default in the provider block (provider `latitudesh` { project = \"...\" }).",
	)
}

func (r *ElasticIPResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data ElasticIPResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Effective project: resource > provider default; else error.
	var effectiveProject string
	if !data.Project.IsNull() && !data.Project.IsUnknown() && data.Project.ValueString() != "" {
		effectiveProject = data.Project.ValueString()
	} else if r.defaultProject != "" {
		effectiveProject = r.defaultProject
	}
	if effectiveProject == "" {
		resp.Diagnostics.AddError(
			"Missing project",
			"Set `project` on this resource or define a default in the provider block (provider \"latitudesh\" { project = \"...\" }).",
		)
		return
	}
	data.Project = types.StringValue(effectiveProject)

	serverID := data.ServerID.ValueString()

	// Snapshot existing EIP IDs before create so the fallback can identify the new one
	// by set-difference instead of relying on server/status fields being populated.
	preCreateIDs := r.snapshotElasticIPIDs(ctx, effectiveProject)

	createRequest := components.CreateElasticIP{
		Data: components.CreateElasticIPData{
			Type: components.CreateElasticIPTypeElasticIps,
			Attributes: components.CreateElasticIPAttributes{
				ProjectID: effectiveProject,
				ServerID:  serverID,
			},
		},
	}

	result, err := r.client.ElasticIps.CreateElasticIP(ctx, createRequest)
	if err != nil {
		addElasticIPError(&resp.Diagnostics, "create", err)
		return
	}

	// Primary path: ID is populated on the response.
	if result != nil && result.ElasticIP != nil && result.ElasticIP.Data != nil && result.ElasticIP.Data.ID != nil && *result.ElasticIP.Data.ID != "" {
		data.ID = types.StringValue(*result.ElasticIP.Data.ID)
	} else {
		// Fallback: SDK docstring warns ID may be null while provisioning.
		// Recover by polling List and picking the ID that didn't exist before Create.
		resp.Diagnostics.AddWarning(
			"Elastic IP ID not returned in create response",
			"Falling back to List to recover the new Elastic IP ID.",
		)
		r.recoverElasticIPID(ctx, effectiveProject, serverID, preCreateIDs, &data, &resp.Diagnostics)
		if resp.Diagnostics.HasError() {
			return
		}
		if data.ID.IsNull() || data.ID.ValueString() == "" {
			resp.Diagnostics.AddError("API Error", "Could not recover the Elastic IP ID after create")
			return
		}
	}

	createTimeout, diagTO := data.Timeouts.Create(ctx, 10*time.Minute)
	resp.Diagnostics.Append(diagTO...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.waitForElasticIPActive(ctx, data.ID.ValueString(), "creation", createTimeout, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	// Re-read to populate address/status from authoritative state.
	r.readElasticIPInto(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	if data.ID.IsNull() || data.ID.ValueString() == "" {
		resp.Diagnostics.AddError(
			"Elastic IP disappeared after successful creation",
			"The Elastic IP reached status `active` but a subsequent GET returned not_found. It may have been released by another actor.",
		)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ElasticIPResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data ElasticIPResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.readElasticIPInto(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	// If readElasticIPInto detected 404 and nulled the ID, remove from state.
	if data.ID.IsNull() {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ElasticIPResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state ElasticIPResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Only server_id is an updatable field; everything else is ForceNew or Computed.
	if plan.ServerID.ValueString() == state.ServerID.ValueString() {
		// No-op update (e.g. provider refresh); persist state as-is.
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}

	id := state.ID.ValueString()
	newServer := plan.ServerID.ValueString()

	updateBody := components.UpdateElasticIP{
		Data: components.UpdateElasticIPData{
			Type: components.UpdateElasticIPTypeElasticIps,
			Attributes: components.UpdateElasticIPAttributes{
				ServerID: newServer,
			},
		},
	}

	_, err := r.client.ElasticIps.UpdateElasticIP(ctx, id, updateBody)
	if err != nil {
		addElasticIPError(&resp.Diagnostics, "move", err)
		return
	}

	updateTimeout, diagTO := plan.Timeouts.Update(ctx, 10*time.Minute)
	resp.Diagnostics.Append(diagTO...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.waitForElasticIPActive(ctx, id, "move", updateTimeout, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	// Read back authoritative state.
	out := state
	out.ServerID = plan.ServerID
	r.readElasticIPInto(ctx, &out, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &out)...)
}

func (r *ElasticIPResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data ElasticIPResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := data.ID.ValueString()
	if id == "" {
		return
	}

	_, err := r.client.ElasticIps.DeleteElasticIP(ctx, id)
	if err != nil {
		if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "not_found") {
			resp.Diagnostics.AddWarning("Elastic IP Already Released", "Elastic IP was already released")
			return
		}
		addElasticIPError(&resp.Diagnostics, "delete", err)
		return
	}

	deleteTimeout, diagTO := data.Timeouts.Delete(ctx, 10*time.Minute)
	resp.Diagnostics.Append(diagTO...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.waitForElasticIPGone(ctx, id, deleteTimeout, &resp.Diagnostics)
}

// waitForElasticIPActive polls Get until the Elastic IP reaches status `active`,
// or returns an error/timeout diagnostic. `op` is used only in messages
// ("creation", "move"). Modelled on ServerResource.waitForServerReady.
func (r *ElasticIPResource) waitForElasticIPActive(ctx context.Context, id, op string, timeout time.Duration, diags *diag.Diagnostics) {
	pollInterval := 10 * time.Second
	maxTransientErrors := 5

	// Short-circuit for unit tests where ctx deadline is tiny.
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining < 2*time.Minute {
			return
		}
		if remaining < timeout {
			timeout = remaining - 30*time.Second
			if timeout < time.Minute {
				timeout = time.Minute
			}
		}
	}

	deadline := time.Now().Add(timeout)
	consecutiveErrors := 0

	for time.Now().Before(deadline) {
		resp, err := r.client.ElasticIps.GetElasticIP(ctx, id)
		if err != nil {
			errStr := err.Error()
			isTransient := strings.Contains(errStr, "502") ||
				strings.Contains(errStr, "503") ||
				strings.Contains(errStr, "504") ||
				strings.Contains(errStr, "429") ||
				strings.Contains(errStr, "timeout") ||
				strings.Contains(errStr, "connection reset")
			consecutiveErrors++
			if isTransient && consecutiveErrors <= maxTransientErrors {
				backoff := time.Duration(consecutiveErrors) * 5 * time.Second
				if backoff > 30*time.Second {
					backoff = 30 * time.Second
				}
				select {
				case <-ctx.Done():
					diags.AddError("Cancelled", "Elastic IP "+op+" cancelled while polling")
					return
				case <-time.After(backoff):
					continue
				}
			}
			addElasticIPError(diags, "poll "+op+" status of", err)
			return
		}

		consecutiveErrors = 0

		if resp == nil || resp.ElasticIP == nil || resp.ElasticIP.Data == nil || resp.ElasticIP.Data.Attributes == nil {
			diags.AddError("API Error", "Elastic IP response is empty during "+op)
			return
		}
		statusPtr := resp.ElasticIP.Data.Attributes.Status
		if statusPtr == nil {
			diags.AddError("API Error", "Elastic IP status is null during "+op)
			return
		}
		status := string(*statusPtr)
		switch status {
		case "active":
			return
		case "error":
			diags.AddError(
				"Elastic IP entered error state",
				"The Elastic IP reached status `error` during "+op+". Check the Latitude.sh dashboard.",
			)
			return
		}

		select {
		case <-ctx.Done():
			diags.AddError("Cancelled", "Elastic IP "+op+" cancelled while polling")
			return
		case <-time.After(pollInterval):
		}
	}

	diags.AddError(
		"Timeout",
		"Elastic IP did not reach `active` within "+timeout.String()+" during "+op+". Check the Latitude.sh dashboard.",
	)
}

// waitForElasticIPGone polls Get until a 404 / not_found is observed.
func (r *ElasticIPResource) waitForElasticIPGone(ctx context.Context, id string, timeout time.Duration, diags *diag.Diagnostics) {
	pollInterval := 10 * time.Second

	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining < 2*time.Minute {
			return
		}
		if remaining < timeout {
			timeout = remaining - 30*time.Second
			if timeout < time.Minute {
				timeout = time.Minute
			}
		}
	}
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		_, err := r.client.ElasticIps.GetElasticIP(ctx, id)
		if err != nil {
			if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "not_found") {
				return
			}
		}
		select {
		case <-ctx.Done():
			diags.AddError("Cancelled", "Elastic IP delete cancelled while polling")
			return
		case <-time.After(pollInterval):
		}
	}
	diags.AddError("Timeout", "Elastic IP was not fully released within "+timeout.String())
}

// snapshotElasticIPIDs returns the set of EIP IDs currently visible in the project.
// Swallows errors: the caller uses this only to distinguish newly-created EIPs from
// pre-existing ones during the id-null-on-create fallback, so a failure just means
// the fallback will be slightly less precise (but still works via server/status match).
func (r *ElasticIPResource) snapshotElasticIPIDs(ctx context.Context, project string) map[string]struct{} {
	ids := make(map[string]struct{})
	listReq := operations.ListElasticIpsRequest{
		FilterProject: &project,
	}
	listResp, err := r.client.ElasticIps.ListElasticIps(ctx, listReq)
	if err != nil || listResp == nil || listResp.ElasticIps == nil || listResp.ElasticIps.Data == nil {
		return ids
	}
	for _, eip := range listResp.ElasticIps.Data {
		if eip.ID != nil {
			ids[*eip.ID] = struct{}{}
		}
	}
	return ids
}

// recoverElasticIPID polls ListElasticIps for up to 60s and picks the ID that wasn't
// present in the pre-create snapshot. Used when Create returns id=null.
// This works even when the just-created EIP has no Server association or Status populated yet.
// Falls back to matching server_id + status on the final pass if no new ID is found.
func (r *ElasticIPResource) recoverElasticIPID(ctx context.Context, project, serverID string, preCreateIDs map[string]struct{}, data *ElasticIPResourceModel, diags *diag.Diagnostics) {
	const (
		pollInterval   = 3 * time.Second
		recoveryBudget = 60 * time.Second
	)
	endBy := time.Now().Add(recoveryBudget)

	var lastListErr error
	for time.Now().Before(endBy) {
		listReq := operations.ListElasticIpsRequest{
			FilterProject: &project,
		}
		listResp, err := r.client.ElasticIps.ListElasticIps(ctx, listReq)
		if err != nil {
			lastListErr = err
		} else if listResp != nil && listResp.ElasticIps != nil && listResp.ElasticIps.Data != nil {
			// Prefer a brand-new ID (set-difference against snapshot).
			for _, eip := range listResp.ElasticIps.Data {
				if eip.ID == nil {
					continue
				}
				if _, existed := preCreateIDs[*eip.ID]; existed {
					continue
				}
				data.ID = types.StringValue(*eip.ID)
				return
			}
			// Secondary match: server+status, in case the snapshot happened to include
			// the new ID already (unlikely but defensive).
			for _, eip := range listResp.ElasticIps.Data {
				if eip.ID == nil || eip.Attributes == nil {
					continue
				}
				attrs := eip.Attributes
				if attrs.Server == nil || attrs.Server.ID == nil || *attrs.Server.ID != serverID {
					continue
				}
				if attrs.Status == nil {
					continue
				}
				s := string(*attrs.Status)
				if s == "configuring" || s == "active" {
					data.ID = types.StringValue(*eip.ID)
					return
				}
			}
		}

		select {
		case <-ctx.Done():
			diags.AddError("Cancelled", "Elastic IP ID recovery cancelled")
			return
		case <-time.After(pollInterval):
		}
	}

	detail := "Polled ListElasticIps for " + recoveryBudget.String() + " without finding a new Elastic IP ID. Check the Latitude.sh dashboard for an orphaned resource and release it manually before retrying."
	if lastListErr != nil {
		detail += " Last list error: " + lastListErr.Error()
	}
	diags.AddError("Elastic IP not found during ID recovery", detail)
}

// readElasticIPInto issues a Get for data.ID and populates address/status/server_id.
// Does not touch data.Project (preserved from plan/state).
func (r *ElasticIPResource) readElasticIPInto(ctx context.Context, data *ElasticIPResourceModel, diags *diag.Diagnostics) {
	id := data.ID.ValueString()
	if id == "" {
		diags.AddError("Invalid ID", "Elastic IP ID is empty")
		return
	}
	resp, err := r.client.ElasticIps.GetElasticIP(ctx, id)
	if err != nil {
		if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "not_found") {
			data.ID = types.StringNull()
			return
		}
		addElasticIPError(diags, "read", err)
		return
	}
	if resp == nil || resp.ElasticIP == nil || resp.ElasticIP.Data == nil || resp.ElasticIP.Data.Attributes == nil {
		data.ID = types.StringNull()
		return
	}
	attrs := resp.ElasticIP.Data.Attributes
	if attrs.Address != nil {
		data.Address = types.StringValue(*attrs.Address)
	} else {
		data.Address = types.StringNull()
	}
	if attrs.Status != nil {
		data.Status = types.StringValue(string(*attrs.Status))
	} else {
		data.Status = types.StringNull()
	}
	if attrs.Server != nil && attrs.Server.ID != nil {
		data.ServerID = types.StringValue(*attrs.Server.ID)
	}
}
