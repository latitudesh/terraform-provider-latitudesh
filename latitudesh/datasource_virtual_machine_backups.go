package latitudesh

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"

	iprovider "github.com/latitudesh/terraform-provider-latitudesh/v2/internal/provider"
)

var (
	_ datasource.DataSource              = &VirtualMachineBackupsDataSource{}
	_ datasource.DataSourceWithConfigure = &VirtualMachineBackupsDataSource{}
)

func NewVirtualMachineBackupsDataSource() datasource.DataSource {
	return &VirtualMachineBackupsDataSource{}
}

type VirtualMachineBackupsDataSource struct {
	client *latitudeshgosdk.Latitudesh
}

type VirtualMachineBackupsDataSourceModel struct {
	// Synthetic identifier: "<virtual_machine or all>[/<status>]".
	ID types.String `tfsdk:"id"`

	// Optional selectors.
	VirtualMachine types.String `tfsdk:"virtual_machine"`
	Status         types.String `tfsdk:"status"`

	// Newest first, so backups[0] is the most recent match.
	Backups types.List `tfsdk:"backups"`
}

type VirtualMachineBackupItemModel struct {
	ID                 types.String `tfsdk:"id"`
	VirtualMachine     types.String `tfsdk:"virtual_machine"`
	VirtualMachineName types.String `tfsdk:"virtual_machine_name"`
	Status             types.String `tfsdk:"status"`
	SizeBytes          types.Int64  `tfsdk:"size_bytes"`
	ExpiresAt          types.String `tfsdk:"expires_at"`
	FailureReason      types.String `tfsdk:"failure_reason"`
	CreatedAt          types.String `tfsdk:"created_at"`
	Project            types.String `tfsdk:"project"`
}

var virtualMachineBackupItemObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"id":                   types.StringType,
		"virtual_machine":      types.StringType,
		"virtual_machine_name": types.StringType,
		"status":               types.StringType,
		"size_bytes":           types.Int64Type,
		"expires_at":           types.StringType,
		"failure_reason":       types.StringType,
		"created_at":           types.StringType,
		"project":              types.StringType,
	},
}

func (d *VirtualMachineBackupsDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_virtual_machine_backups"
}

func (d *VirtualMachineBackupsDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	deps := iprovider.ConfigureFromProviderData(req.ProviderData, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	d.client = deps.Client
}

func (d *VirtualMachineBackupsDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Virtual machine backups data source - list the team's VM backups, optionally scoped to one virtual machine and filtered by status, newest first. `backups[0].id` of a `Ready` filter is the natural input for `backup_id` on `latitudesh_virtual_machine`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Synthetic identifier for this query: the `virtual_machine` filter (or `all`), followed by `/<status>` when a status filter is set.",
				Computed:            true,
			},
			"virtual_machine": schema.StringAttribute{
				MarkdownDescription: "Virtual machine ID to scope the list to. When omitted, every backup in the team is listed.",
				Optional:            true,
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "Only return backups with this status (`Creating`, `Ready`, `Failed`, or `Archived`; case-insensitive).",
				Optional:            true,
			},
			"backups": schema.ListNestedAttribute{
				MarkdownDescription: "The matching backups, newest first.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							MarkdownDescription: "Backup ID.",
							Computed:            true,
						},
						"virtual_machine": schema.StringAttribute{
							MarkdownDescription: "ID of the virtual machine the backup was taken from.",
							Computed:            true,
						},
						"virtual_machine_name": schema.StringAttribute{
							MarkdownDescription: "Name of the virtual machine the backup was taken from.",
							Computed:            true,
						},
						"status": schema.StringAttribute{
							MarkdownDescription: "Backup status (`Creating`, `Ready`, `Failed`, or `Archived`).",
							Computed:            true,
						},
						"size_bytes": schema.Int64Attribute{
							MarkdownDescription: "Backup size in bytes, once known.",
							Computed:            true,
						},
						"expires_at": schema.StringAttribute{
							MarkdownDescription: "Timestamp when the backup expires, if it does.",
							Computed:            true,
						},
						"failure_reason": schema.StringAttribute{
							MarkdownDescription: "Reason the backup failed, when `status` is `Failed`.",
							Computed:            true,
						},
						"created_at": schema.StringAttribute{
							MarkdownDescription: "Timestamp when the backup was created.",
							Computed:            true,
						},
						"project": schema.StringAttribute{
							MarkdownDescription: "Project (slug, falling back to ID) that owns the backup.",
							Computed:            true,
						},
					},
				},
			},
		},
	}
}

func (d *VirtualMachineBackupsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data VirtualMachineBackupsDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if d.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured.")
		return
	}

	if data.VirtualMachine.IsUnknown() || data.Status.IsUnknown() {
		resp.Diagnostics.AddError(
			"Unknown filter value",
			"'virtual_machine' and 'status' must be known at plan time. Please provide concrete values or omit them.",
		)
		return
	}

	vmFilter := strings.TrimSpace(data.VirtualMachine.ValueString())
	statusFilter := strings.TrimSpace(data.Status.ValueString())

	var page *components.VirtualMachineBackups
	if vmFilter != "" {
		res, err := d.client.VirtualMachineBackups.ListForVirtualMachine(ctx, vmFilter)
		if err != nil {
			var apiErr *components.APIError
			if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
				resp.Diagnostics.AddError("Virtual Machine Not Found", fmt.Sprintf("No virtual machine exists with ID %q.", vmFilter))
				return
			}
			resp.Diagnostics.AddError("Client Error", "Unable to list virtual machine backups, got error: "+err.Error())
			return
		}
		page = res.VirtualMachineBackups
	} else {
		res, err := d.client.VirtualMachineBackups.List(ctx)
		if err != nil {
			resp.Diagnostics.AddError("Client Error", "Unable to list virtual machine backups, got error: "+err.Error())
			return
		}
		page = res.VirtualMachineBackups
	}

	id := "all"
	if vmFilter != "" {
		id = vmFilter
	}
	if statusFilter != "" {
		id += "/" + statusFilter
	}
	data.ID = types.StringValue(id)

	backups := make([]components.VirtualMachineBackupAttributes, 0)
	if page != nil {
		for _, b := range page.Data {
			if backupMatchesStatus(b, statusFilter) {
				backups = append(backups, b)
			}
		}
	}
	sortVirtualMachineBackupsNewestFirst(backups)

	items := make([]VirtualMachineBackupItemModel, 0, len(backups))
	for i := range backups {
		items = append(items, virtualMachineBackupItemValue(&backups[i]))
	}

	list, diags := types.ListValueFrom(ctx, virtualMachineBackupItemObjectType, items)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	data.Backups = list

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// virtualMachineBackupItemValue maps one SDK backup into the list item model.
// It shares the attribute mapper with the singular data source and resource,
// adding the source VM's name and the owning project on top.
func virtualMachineBackupItemValue(b *components.VirtualMachineBackupAttributes) VirtualMachineBackupItemModel {
	fields := mapVirtualMachineBackupAttrs(b.Attributes)
	item := VirtualMachineBackupItemModel{
		ID:                 types.StringPointerValue(b.ID),
		VirtualMachine:     fields.VirtualMachine,
		VirtualMachineName: types.StringNull(),
		Status:             fields.Status,
		SizeBytes:          fields.SizeBytes,
		ExpiresAt:          fields.ExpiresAt,
		FailureReason:      fields.FailureReason,
		CreatedAt:          fields.CreatedAt,
		Project:            types.StringNull(),
	}
	if b.Attributes == nil {
		return item
	}
	if b.Attributes.VirtualMachine != nil {
		item.VirtualMachineName = types.StringPointerValue(b.Attributes.VirtualMachine.Name)
	}
	if label := projectLabel(b.Attributes.Project); label != "" {
		item.Project = types.StringValue(label)
	}
	return item
}

// sortVirtualMachineBackupsNewestFirst orders backups by created_at descending.
// Entries without a parseable timestamp sort last; the sort is stable so the
// API's own order is kept among them.
func sortVirtualMachineBackupsNewestFirst(backups []components.VirtualMachineBackupAttributes) {
	sort.SliceStable(backups, func(i, j int) bool {
		return backupCreatedAt(backups[i]).After(backupCreatedAt(backups[j]))
	})
}

func backupCreatedAt(b components.VirtualMachineBackupAttributes) time.Time {
	if b.Attributes == nil || b.Attributes.CreatedAt == nil {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, *b.Attributes.CreatedAt)
	if err != nil {
		return time.Time{}
	}
	return t
}

// backupMatchesStatus applies the optional, case-insensitive status filter. An
// empty filter matches everything; a backup with no status never matches a
// non-empty one.
func backupMatchesStatus(b components.VirtualMachineBackupAttributes, want string) bool {
	if want == "" {
		return true
	}
	if b.Attributes == nil || b.Attributes.Status == nil {
		return false
	}
	return strings.EqualFold(string(*b.Attributes.Status), want)
}
