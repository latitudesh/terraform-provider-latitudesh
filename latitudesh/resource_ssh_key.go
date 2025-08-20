package latitudesh

import (
	"context"
	"net/http"
	"strings"

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
var _ resource.Resource = &SSHKeyResource{}
var _ resource.ResourceWithImportState = &SSHKeyResource{}

func NewSSHKeyResource() resource.Resource {
	return &SSHKeyResource{}
}

// SSHKeyResource defines the resource implementation.
type SSHKeyResource struct {
	client *latitudeshgosdk.Latitudesh
}

// SSHKeyResourceModel describes the resource data model.
type SSHKeyResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	PublicKey   types.String `tfsdk:"public_key"`
	Tags        types.List   `tfsdk:"tags"`
	Fingerprint types.String `tfsdk:"fingerprint"`
	CreatedAt   types.String `tfsdk:"created_at"`
	UpdatedAt   types.String `tfsdk:"updated_at"`
}

func (r *SSHKeyResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ssh_key"
}

func (r *SSHKeyResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "SSH Key resource",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "SSH key identifier",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The SSH key name",
				Required:            true,
			},
			"public_key": schema.StringAttribute{
				MarkdownDescription: "The SSH public key",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"tags": schema.ListAttribute{
				MarkdownDescription: "List of SSH key tags",
				ElementType:         types.StringType,
				Optional:            true,
			},
			"fingerprint": schema.StringAttribute{
				MarkdownDescription: "The SSH key fingerprint",
				Computed:            true,
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "The timestamp for when the SSH key was created",
				Computed:            true,
			},
			"updated_at": schema.StringAttribute{
				MarkdownDescription: "The timestamp for the last time the SSH key was updated",
				Computed:            true,
			},
		},
	}
}

func (r *SSHKeyResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	deps := iprovider.ConfigureFromProviderData(req.ProviderData, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	r.client = deps.Client
}

func (r *SSHKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data SSHKeyResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	name := data.Name.ValueString()
	publicKey := data.PublicKey.ValueString()

	createRequest := operations.PostSSHKeySSHKeysRequestBody{
		Data: operations.PostSSHKeySSHKeysData{
			Type: operations.PostSSHKeySSHKeysTypeSSHKeys,
			Attributes: &operations.PostSSHKeySSHKeysAttributes{
				Name:      &name,
				PublicKey: &publicKey,
			},
		},
	}

	result, err := r.client.SSHKeys.Create(ctx, createRequest)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to create SSH key, got error: "+err.Error())
		return
	}

	if result.Object == nil || result.Object.Data == nil || result.Object.Data.ID == nil {
		resp.Diagnostics.AddError("API Error", "Failed to get SSH key ID from response")
		return
	}

	data.ID = types.StringValue(*result.Object.Data.ID)

	// Update tags if provided
	if !data.Tags.IsNull() && !data.Tags.IsUnknown() {
		r.updateSSHKey(ctx, &data, &resp.Diagnostics)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	// Read the resource to populate computed attributes
	r.readSSHKey(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *SSHKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data SSHKeyResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	r.readSSHKey(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *SSHKeyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data SSHKeyResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	r.updateSSHKey(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	// Read the resource to populate computed attributes
	r.readSSHKey(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *SSHKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data SSHKeyResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	keyID := data.ID.ValueString()

	_, err := r.client.SSHKeys.Delete(ctx, keyID)
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			return
		}
		resp.Diagnostics.AddError("Client Error", "Unable to delete SSH key, got error: "+err.Error())
		return
	}
}

func (r *SSHKeyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	var data SSHKeyResourceModel
	data.ID = types.StringValue(req.ID)

	r.readSSHKey(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *SSHKeyResource) readSSHKey(ctx context.Context, data *SSHKeyResourceModel, diags *diag.Diagnostics) {
	keyID := data.ID.ValueString()

	result, err := r.client.SSHKeys.Retrieve(ctx, keyID)
	if err != nil {
		// Check if the SSH key was deleted
		if apiErr, ok := err.(*components.APIError); ok && apiErr.StatusCode == http.StatusNotFound {
			data.ID = types.StringNull()
			return
		}
		diags.AddError("Client Error", "Unable to read SSH key, got error: "+err.Error())
		return
	}

	if result.Object == nil || result.Object.Data == nil {
		data.ID = types.StringNull()
		return
	}

	sshKey := result.Object.Data

	data.Tags = types.ListNull(types.StringType)

	if sshKey.Attributes != nil {
		if sshKey.Attributes.Name != nil {
			data.Name = types.StringValue(*sshKey.Attributes.Name)
		}

		if sshKey.Attributes.PublicKey != nil {
			data.PublicKey = types.StringValue(*sshKey.Attributes.PublicKey)
		}

		if sshKey.Attributes.Fingerprint != nil {
			data.Fingerprint = types.StringValue(*sshKey.Attributes.Fingerprint)
		}

		if sshKey.Attributes.CreatedAt != nil {
			data.CreatedAt = types.StringValue(*sshKey.Attributes.CreatedAt)
		}

		if sshKey.Attributes.UpdatedAt != nil {
			data.UpdatedAt = types.StringValue(*sshKey.Attributes.UpdatedAt)
		}
	}
}

func (r *SSHKeyResource) updateSSHKey(ctx context.Context, data *SSHKeyResourceModel, diags *diag.Diagnostics) {
	keyID := data.ID.ValueString()
	name := data.Name.ValueString()

	var tags []string
	if !data.Tags.IsNull() && !data.Tags.IsUnknown() {
		for _, tag := range data.Tags.Elements() {
			tags = append(tags, tag.(types.String).ValueString())
		}
	}

	updateRequest := operations.PutSSHKeySSHKeysRequestBody{
		Data: operations.PutSSHKeySSHKeysData{
			ID:   &keyID,
			Type: operations.PutSSHKeySSHKeysTypeSSHKeys,
			Attributes: &operations.PutSSHKeySSHKeysAttributes{
				Name: &name,
				Tags: tags,
			},
		},
	}

	_, err := r.client.SSHKeys.Update(ctx, keyID, updateRequest)
	if err != nil {
		diags.AddError("Client Error", "Unable to update SSH key, got error: "+err.Error())
		return
	}
}
