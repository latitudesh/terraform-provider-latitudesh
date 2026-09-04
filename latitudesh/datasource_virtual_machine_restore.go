package latitudesh

import (
	"context"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"

	iprovider "github.com/latitudesh/terraform-provider-latitudesh/v2/internal/provider"
)

var (
	_ datasource.DataSource              = &VirtualMachineRestoreDataSource{}
	_ datasource.DataSourceWithConfigure = &VirtualMachineRestoreDataSource{}
)

func NewVirtualMachineRestoreDataSource() datasource.DataSource {
	return &VirtualMachineRestoreDataSource{}
}

type VirtualMachineRestoreDataSource struct {
	client *latitudeshgosdk.Latitudesh
}

type VirtualMachineRestoreDataSourceModel struct {
	// Selector
	ID types.String `tfsdk:"id"`

	// Attributes
	Status             types.String `tfsdk:"status"`
	CreatedAt          types.String `tfsdk:"created_at"`
	BackupID           types.String `tfsdk:"backup_id"`
	VirtualMachineID   types.String `tfsdk:"virtual_machine_id"`
	VirtualMachineName types.String `tfsdk:"virtual_machine_name"`
	Project            types.String `tfsdk:"project"`
}

func (d *VirtualMachineRestoreDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_virtual_machine_restore"
}

func (d *VirtualMachineRestoreDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	deps := iprovider.ConfigureFromProviderData(req.ProviderData, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	d.client = deps.Client
}

func (d *VirtualMachineRestoreDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Virtual machine restore data source - look up a VM restore by id. Useful for polling a restore triggered outside Terraform until it reaches `Ready` and reading the resulting virtual machine.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "VM restore ID to look up.",
				Required:            true,
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "Restore status: `Creating`, `Ready`, or `Failed`.",
				Computed:            true,
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "Timestamp when the restore was created.",
				Computed:            true,
			},
			"backup_id": schema.StringAttribute{
				MarkdownDescription: "ID of the backup this restore was created from.",
				Computed:            true,
			},
			"virtual_machine_id": schema.StringAttribute{
				MarkdownDescription: "ID of the restored virtual machine. Null until the restore reaches `Ready`.",
				Computed:            true,
			},
			"virtual_machine_name": schema.StringAttribute{
				MarkdownDescription: "Name of the restored virtual machine. Null until the restore reaches `Ready`.",
				Computed:            true,
			},
			"project": schema.StringAttribute{
				MarkdownDescription: "Project (slug, falling back to ID) that owns the restore.",
				Computed:            true,
			},
		},
	}
}

func (d *VirtualMachineRestoreDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data VirtualMachineRestoreDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if d.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured.")
		return
	}

	if data.ID.IsUnknown() {
		resp.Diagnostics.AddError(
			"Unknown selector value",
			"'id' is unknown. Please provide a concrete value.",
		)
		return
	}

	restore, err := d.getByID(ctx, data.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Client Error", err.Error())
		return
	}
	if restore == nil {
		resp.Diagnostics.AddError("VM Restore Not Found", fmt.Sprintf("No VM restore exists with ID %q", data.ID.ValueString()))
		return
	}

	mapVirtualMachineRestoreToModel(restore, &data)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// mapVirtualMachineRestoreToModel maps a VM restore from the API response to
// the data source model. Every pointer is nil-checked: VirtualMachine in
// particular stays null until the restore reaches Ready, per the SDK's field
// doc comment.
func mapVirtualMachineRestoreToModel(restore *components.VirtualMachineRestoreAttributes, data *VirtualMachineRestoreDataSourceModel) {
	if restore.ID != nil {
		data.ID = types.StringValue(*restore.ID)
	}

	data.Status = types.StringNull()
	data.CreatedAt = types.StringNull()
	data.BackupID = types.StringNull()
	data.VirtualMachineID = types.StringNull()
	data.VirtualMachineName = types.StringNull()
	data.Project = types.StringNull()

	attrs := restore.Attributes
	if attrs == nil {
		return
	}

	if attrs.Status != nil {
		data.Status = types.StringValue(string(*attrs.Status))
	}
	data.CreatedAt = types.StringPointerValue(attrs.CreatedAt)

	if attrs.Backup != nil {
		data.BackupID = types.StringPointerValue(attrs.Backup.ID)
	}

	if attrs.VirtualMachine != nil {
		data.VirtualMachineID = types.StringPointerValue(attrs.VirtualMachine.ID)
		data.VirtualMachineName = types.StringPointerValue(attrs.VirtualMachine.Name)
	}

	if attrs.Project != nil {
		if attrs.Project.Slug != nil {
			data.Project = types.StringValue(*attrs.Project.Slug)
		} else if attrs.Project.ID != nil {
			data.Project = types.StringValue(*attrs.Project.ID)
		}
	}
}

func (d *VirtualMachineRestoreDataSource) getByID(ctx context.Context, id string) (*components.VirtualMachineRestoreAttributes, error) {
	res, err := d.client.VirtualMachineRestores.Get(ctx, id)
	if err != nil {
		if virtualMachineRestoreNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("unable to retrieve VM restore %q: %w", id, err)
	}
	if res.VirtualMachineRestore == nil || res.VirtualMachineRestore.Data == nil {
		return nil, nil
	}
	return res.VirtualMachineRestore.Data, nil
}

// virtualMachineRestoreNotFound reports whether err is a 404 from the get
// endpoint. GetVirtualMachineRestore's 404 falls through the SDK's generic
// 4xx branch, so it always surfaces as *components.APIError (unlike the
// marketplace app endpoint, which declares a typed 404 response).
func virtualMachineRestoreNotFound(err error) bool {
	apiErr, ok := err.(*components.APIError)
	return ok && apiErr.StatusCode == http.StatusNotFound
}
