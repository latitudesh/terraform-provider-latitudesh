package latitudesh

import (
	"context"
	"errors"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
	iprovider "github.com/latitudesh/terraform-provider-latitudesh/v2/internal/provider"
)

var _ datasource.DataSource = &VirtualMachineBackupDataSource{}

func NewVirtualMachineBackupDataSource() datasource.DataSource {
	return &VirtualMachineBackupDataSource{}
}

type VirtualMachineBackupDataSource struct {
	client *latitudeshgosdk.Latitudesh
}

type VirtualMachineBackupDataSourceModel struct {
	ID types.String `tfsdk:"id"`

	VirtualMachine types.String `tfsdk:"virtual_machine"`
	Status         types.String `tfsdk:"status"`
	SizeBytes      types.Int64  `tfsdk:"size_bytes"`
	ExpiresAt      types.String `tfsdk:"expires_at"`
	FailureReason  types.String `tfsdk:"failure_reason"`
	CreatedAt      types.String `tfsdk:"created_at"`
}

func (d *VirtualMachineBackupDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_virtual_machine_backup"
}

func (d *VirtualMachineBackupDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	deps := iprovider.ConfigureFromProviderData(req.ProviderData, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	d.client = deps.Client
}

func (d *VirtualMachineBackupDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Virtual Machine Backup data source - lookup a backup by id.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Virtual machine backup identifier to look up.",
				Required:            true,
			},
			"virtual_machine": schema.StringAttribute{
				MarkdownDescription: "The ID of the virtual machine this backup belongs to.",
				Computed:            true,
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "Backup status (`Creating`, `Ready`, `Failed`, or `Archived`).",
				Computed:            true,
			},
			"size_bytes": schema.Int64Attribute{
				MarkdownDescription: "Backup size in bytes.",
				Computed:            true,
			},
			"expires_at": schema.StringAttribute{
				MarkdownDescription: "Timestamp when the backup expires.",
				Computed:            true,
			},
			"failure_reason": schema.StringAttribute{
				MarkdownDescription: "Reason the backup failed, when `status` is `Failed`.",
				Computed:            true,
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "The timestamp for when the backup was created.",
				Computed:            true,
			},
		},
	}
}

func (d *VirtualMachineBackupDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data VirtualMachineBackupDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := data.ID.ValueString()

	result, err := d.client.VirtualMachineBackups.Get(ctx, id)
	if err != nil {
		var apiErr *components.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			resp.Diagnostics.AddError("Not Found", "No virtual machine backup exists with ID "+id)
			return
		}
		resp.Diagnostics.AddError("Client Error", "Unable to read virtual machine backup, got error: "+err.Error())
		return
	}

	if result.VirtualMachineBackup == nil || result.VirtualMachineBackup.Data == nil {
		resp.Diagnostics.AddError("Not Found", "No virtual machine backup exists with ID "+id)
		return
	}

	obj := result.VirtualMachineBackup.Data
	if obj.ID != nil {
		data.ID = types.StringValue(*obj.ID)
	}

	fields := mapVirtualMachineBackupAttrs(obj.Attributes)
	data.VirtualMachine = fields.VirtualMachine
	data.Status = fields.Status
	data.SizeBytes = fields.SizeBytes
	data.ExpiresAt = fields.ExpiresAt
	data.FailureReason = fields.FailureReason
	data.CreatedAt = fields.CreatedAt

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
