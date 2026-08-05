package latitudesh

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/action"
	"github.com/hashicorp/terraform-plugin-framework/action/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/operations"
	iprovider "github.com/latitudesh/terraform-provider-latitudesh/v2/internal/provider"
)

// defaultReinstallWait bounds the status poll when wait_for_ready is left at its
// default. It matches the server resource's own update timeout default so a
// reinstall through the action and a reinstall through the resource give up at
// the same point.
const defaultReinstallWait = 30 * time.Minute

var (
	_ action.Action                   = &ServerReinstallAction{}
	_ action.ActionWithConfigure      = &ServerReinstallAction{}
	_ action.ActionWithValidateConfig = &ServerReinstallAction{}
)

func NewServerReinstallAction() action.Action {
	return &ServerReinstallAction{}
}

// ServerReinstallAction reinstalls an existing server. Unlike the reinstall the
// server resource performs on update, it is invoked deliberately — nothing about
// the desired state has to change for it to run, which is what makes it usable
// for recovery (a server stuck in failed_deployment, a corrupted disk) and for
// re-imaging on a schedule.
type ServerReinstallAction struct {
	client *latitudeshgosdk.Latitudesh
}

type ServerReinstallActionModel struct {
	ServerID          types.String      `tfsdk:"server_id"`
	OperatingSystem   types.String      `tfsdk:"operating_system"`
	Hostname          types.String      `tfsdk:"hostname"`
	SSHKeys           types.List        `tfsdk:"ssh_keys"`
	UserData          types.String      `tfsdk:"user_data"`
	Raid              types.String      `tfsdk:"raid"`
	DiskLayout        []DiskLayoutModel `tfsdk:"disk_layout"`
	Ipxe              types.String      `tfsdk:"ipxe"`
	PersistentNetboot types.Bool        `tfsdk:"persistent_netboot"`
	WaitForReady      types.Bool        `tfsdk:"wait_for_ready"`
	WaitTimeout       types.String      `tfsdk:"wait_timeout"`
}

func (a *ServerReinstallAction) Metadata(ctx context.Context, req action.MetadataRequest, resp *action.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_server_reinstall"
}

func (a *ServerReinstallAction) Schema(ctx context.Context, req action.SchemaRequest, resp *action.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reinstalls a server, erasing its disks and redeploying the operating system. " +
			"Every attribute other than `server_id` is optional: omit an attribute and the reinstall keeps the value " +
			"the server was last deployed with. Setting one to something the `latitudesh_server` resource does not " +
			"declare leaves that resource drifted, so the next plan will show a diff.",
		Attributes: map[string]schema.Attribute{
			"server_id": schema.StringAttribute{
				MarkdownDescription: "The ID of the server to reinstall.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"operating_system": schema.StringAttribute{
				MarkdownDescription: "The operating system slug to install. Defaults to the server's current operating system.",
				Optional:            true,
			},
			"hostname": schema.StringAttribute{
				MarkdownDescription: "The hostname to set on reinstall. Defaults to the server's current hostname.",
				Optional:            true,
			},
			"ssh_keys": schema.ListAttribute{
				MarkdownDescription: "SSH key IDs to install. An empty list installs no keys; omitting the attribute keeps the current ones.",
				ElementType:         types.StringType,
				Optional:            true,
			},
			"user_data": schema.StringAttribute{
				MarkdownDescription: "The ID of a `latitudesh_user_data` to apply on reinstall.",
				Optional:            true,
			},
			"raid": schema.StringAttribute{
				MarkdownDescription: "RAID mode: `raid-0` or `raid-1`. Mutually exclusive with `disk_layout`.",
				Optional:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("raid-0", "raid-1"),
				},
			},
			"disk_layout": schema.ListNestedAttribute{
				MarkdownDescription: "Custom disk layout made of one or more disk groups, used instead of `raid`. Mutually exclusive with `raid` and `ipxe`. Supersedes the layout the server was deployed with.",
				Optional:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"count": schema.Int64Attribute{
							MarkdownDescription: "Number of disks to include in this group.",
							Required:            true,
						},
						"role": schema.StringAttribute{
							MarkdownDescription: "Role of this disk group: `os`, `storage`, or `raw`.",
							Required:            true,
							Validators: []validator.String{
								stringvalidator.OneOf("os", "storage", "raw"),
							},
						},
						"raid_level": schema.StringAttribute{
							MarkdownDescription: "RAID level for this disk group: `raid-0` or `raid-1`. Requires `count >= 2`.",
							Optional:            true,
							Validators: []validator.String{
								stringvalidator.OneOf("raid-0", "raid-1"),
							},
						},
						"mount_point": schema.StringAttribute{
							MarkdownDescription: "Mount point for this disk group, e.g. `/var/lib`. Required for the `storage` role.",
							Optional:            true,
						},
					},
				},
			},
			"ipxe": schema.StringAttribute{
				MarkdownDescription: "The iPXE script to boot: either a URL pointing at the script, or the script encoded in base64. Required when `operating_system = \"ipxe\"`.",
				Optional:            true,
			},
			"persistent_netboot": schema.BoolAttribute{
				MarkdownDescription: "Keep network boot enabled so the server iPXE-boots on every reboot instead of booting from disk. Only supported with the `ipxe` operating system.",
				Optional:            true,
			},
			"wait_for_ready": schema.BoolAttribute{
				MarkdownDescription: "Wait for the server to finish redeploying and report `on`. Defaults to `true`; " +
					"set to `false` to return as soon as the API accepts the reinstall.",
				Optional: true,
			},
			"wait_timeout": schema.StringAttribute{
				MarkdownDescription: "How long to wait for the reinstall to finish, as a Go duration (for example `45m`). Defaults to `30m`. Ignored when `wait_for_ready` is `false`.",
				Optional:            true,
			},
		},
	}
}

func (a *ServerReinstallAction) Configure(ctx context.Context, req action.ConfigureRequest, resp *action.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	deps := iprovider.ConfigureFromProviderData(req.ProviderData, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	a.client = deps.Client
}

func (a *ServerReinstallAction) ValidateConfig(ctx context.Context, req action.ValidateConfigRequest, resp *action.ValidateConfigResponse) {
	var data ServerReinstallActionModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := requiresIpxeAttribute(data.OperatingSystem, data.Ipxe); err != nil {
		resp.Diagnostics.AddError("Invalid iPXE Configuration", err.Error())
	}

	// Mirrors ServerResource.ValidateConfig: the same three-way exclusivity the
	// resource enforces, so a reinstall through either path is rejected for the
	// same reasons at plan time rather than by an API 422 at apply.
	if len(data.DiskLayout) > 0 {
		if !data.Raid.IsNull() && !data.Raid.IsUnknown() {
			resp.Diagnostics.AddError(
				"Conflicting disk configuration",
				"raid and disk_layout are mutually exclusive. Set only one of them.",
			)
		}
		if !data.Ipxe.IsNull() && !data.Ipxe.IsUnknown() {
			resp.Diagnostics.AddError(
				"Conflicting disk configuration",
				"ipxe and disk_layout are mutually exclusive. Set only one of them.",
			)
		}
		validateDiskLayoutGroups(data.DiskLayout, &resp.Diagnostics)
	}

	if _, err := reinstallWaitTimeout(data.WaitTimeout); err != nil {
		resp.Diagnostics.AddAttributeError(
			path.Root("wait_timeout"),
			"Invalid Wait Timeout",
			err.Error(),
		)
	}
}

func (a *ServerReinstallAction) Invoke(ctx context.Context, req action.InvokeRequest, resp *action.InvokeResponse) {
	var data ServerReinstallActionModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	timeout, err := reinstallWaitTimeout(data.WaitTimeout)
	if err != nil {
		resp.Diagnostics.AddError("Invalid Wait Timeout", err.Error())
		return
	}

	serverID := data.ServerID.ValueString()
	attrs, diags := reinstallAttributesFromAction(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.SendProgress(action.InvokeProgressEvent{
		Message: fmt.Sprintf("Requesting reinstall of server %s", serverID),
	})

	_, err = a.client.Servers.Reinstall(ctx, serverID, operations.CreateServerReinstallServersRequestBody{
		Data: operations.CreateServerReinstallServersData{
			Type:       operations.CreateServerReinstallServersTypeReinstalls,
			Attributes: attrs,
		},
	})
	if err != nil {
		resp.Diagnostics.AddError("Reinstall Error", "Unable to reinstall server "+serverID+": "+err.Error())
		return
	}

	if !data.WaitForReady.IsNull() && !data.WaitForReady.IsUnknown() && !data.WaitForReady.ValueBool() {
		resp.Diagnostics.AddWarning(
			"Reinstall Not Awaited",
			fmt.Sprintf("Server %s is reinstalling. With wait_for_ready = false the action returns before the "+
				"server is back on, so anything downstream may run against a server that is still deploying.", serverID),
		)
		return
	}

	waitForServerStatus(ctx, a.client, serverID, "reinstall", timeout, true, func(message string) {
		resp.SendProgress(action.InvokeProgressEvent{Message: message})
	}, &resp.Diagnostics)
}

// reinstallWaitTimeout resolves wait_timeout, which is a plain string rather than
// the resource timeouts block: that helper only covers resources and data sources.
func reinstallWaitTimeout(configured types.String) (time.Duration, error) {
	if configured.IsNull() || configured.IsUnknown() || configured.ValueString() == "" {
		return defaultReinstallWait, nil
	}
	timeout, err := time.ParseDuration(configured.ValueString())
	if err != nil {
		return 0, fmt.Errorf("%q is not a valid duration: %w", configured.ValueString(), err)
	}
	if timeout <= 0 {
		return 0, fmt.Errorf("%q must be a positive duration", configured.ValueString())
	}
	return timeout, nil
}

// reinstallAttributesFromAction mirrors ServerResource.reinstallServer, but reads
// the action config instead of resource state: an omitted attribute is left off
// the payload so the API keeps whatever the server was deployed with.
func reinstallAttributesFromAction(ctx context.Context, data *ServerReinstallActionModel) (*operations.CreateServerReinstallServersAttributes, diag.Diagnostics) {
	var diags diag.Diagnostics
	attrs := &operations.CreateServerReinstallServersAttributes{}

	if isSet(data.OperatingSystem) {
		os := operations.CreateServerReinstallServersOperatingSystem(data.OperatingSystem.ValueString())
		attrs.OperatingSystem = &os
	}

	if isSet(data.Hostname) {
		hostname := data.Hostname.ValueString()
		attrs.Hostname = &hostname
	}

	if !data.SSHKeys.IsNull() && !data.SSHKeys.IsUnknown() {
		var sshKeys []string
		diags.Append(data.SSHKeys.ElementsAs(ctx, &sshKeys, false)...)
		if diags.HasError() {
			return nil, diags
		}
		// An explicit empty list is meaningful: it installs no keys.
		attrs.SSHKeys = sshKeys
	}

	if isSet(data.UserData) {
		userData := data.UserData.ValueString()
		attrs.UserData = &userData
	}

	// disk_layout supersedes raid, and ValidateConfig rejects setting both.
	if len(data.DiskLayout) > 0 {
		attrs.DiskLayout = reinstallDiskLayout(data.DiskLayout)
	} else if isSet(data.Raid) {
		raid := operations.CreateServerReinstallServersRaid(data.Raid.ValueString())
		attrs.Raid = &raid
	}

	if isSet(data.Ipxe) {
		ipxe := data.Ipxe.ValueString()
		attrs.Ipxe = &ipxe
	}

	if !data.PersistentNetboot.IsNull() && !data.PersistentNetboot.IsUnknown() {
		persistentNetboot := data.PersistentNetboot.ValueBool()
		attrs.PersistentNetboot = &persistentNetboot
	}

	return attrs, diags
}

func isSet(value types.String) bool {
	return !value.IsNull() && !value.IsUnknown() && value.ValueString() != ""
}
