package latitudesh

import (
	"context"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
	"github.com/latitudesh/latitudesh-go-sdk/models/operations"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &FirewallResource{}
var _ resource.ResourceWithImportState = &FirewallResource{}

func NewFirewallResource() resource.Resource {
	return &FirewallResource{}
}

// FirewallResource defines the resource implementation.
type FirewallResource struct {
	client *latitudeshgosdk.Latitudesh
}

// FirewallRuleModel describes a firewall rule.
type FirewallRuleModel struct {
	From     types.String `tfsdk:"from"`
	To       types.String `tfsdk:"to"`
	Port     types.String `tfsdk:"port"`
	Protocol types.String `tfsdk:"protocol"`
}

// FirewallResourceModel describes the resource data model.
type FirewallResourceModel struct {
	ID      types.String        `tfsdk:"id"`
	Name    types.String        `tfsdk:"name"`
	Project types.String        `tfsdk:"project"`
	Rules   []FirewallRuleModel `tfsdk:"rules"`
}

func (r *FirewallResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_firewall"
}

func (r *FirewallResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Firewall resource",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Firewall identifier",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The firewall name",
				Required:            true,
			},
			"project": schema.StringAttribute{
				MarkdownDescription: "The project id or slug",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
		},
		Blocks: map[string]schema.Block{
			"rules": schema.ListNestedBlock{
				MarkdownDescription: "Firewall rules",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"from": schema.StringAttribute{
							MarkdownDescription: "Source IP or range",
							Required:            true,
						},
						"to": schema.StringAttribute{
							MarkdownDescription: "Destination IP or range",
							Required:            true,
						},
						"port": schema.StringAttribute{
							MarkdownDescription: "Port or port range",
							Required:            true,
						},
						"protocol": schema.StringAttribute{
							MarkdownDescription: "Protocol (TCP, UDP, ICMP)",
							Required:            true,
						},
					},
				},
			},
		},
	}
}

func (r *FirewallResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	// Prevent panic if the provider has not been configured.
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

func (r *FirewallResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data FirewallResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	name := data.Name.ValueString()
	project := data.Project.ValueString()

	// Convert rules
	var rules []operations.CreateFirewallRules
	for _, rule := range data.Rules {
		from := rule.From.ValueString()
		to := rule.To.ValueString()
		port := rule.Port.ValueString()
		protocol := rule.Protocol.ValueString()

		// Convert string protocol to the proper type
		var protocolEnum operations.CreateFirewallProtocol
		switch protocol {
		case "TCP":
			protocolEnum = operations.CreateFirewallProtocolTCP
		case "UDP":
			protocolEnum = operations.CreateFirewallProtocolUDP
		default:
			protocolEnum = operations.CreateFirewallProtocolTCP // default to TCP
		}

		rules = append(rules, operations.CreateFirewallRules{
			From:     &from,
			To:       &to,
			Port:     &port,
			Protocol: &protocolEnum,
		})
	}

	createRequest := operations.CreateFirewallFirewallsRequestBody{
		Data: operations.CreateFirewallData{
			Type: operations.CreateFirewallTypeFirewalls,
			Attributes: &operations.CreateFirewallAttributes{
				Name:    name,
				Project: project,
				Rules:   rules,
			},
		},
	}

	result, err := r.client.Firewalls.CreateFirewall(ctx, createRequest)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to create firewall, got error: "+err.Error())
		return
	}

	if result.Firewall == nil || result.Firewall.ID == nil {
		resp.Diagnostics.AddError("API Error", "Failed to get firewall ID from response")
		return
	}

	data.ID = types.StringValue(*result.Firewall.ID)

	// Read the resource to populate all attributes
	r.readFirewall(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *FirewallResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data FirewallResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	r.readFirewall(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *FirewallResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data FirewallResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	firewallID := data.ID.ValueString()
	name := data.Name.ValueString()

	// Convert rules
	var rules []operations.UpdateFirewallFirewallsRules
	for _, rule := range data.Rules {
		from := rule.From.ValueString()
		to := rule.To.ValueString()
		port := rule.Port.ValueString()
		protocol := rule.Protocol.ValueString()

		// Convert string protocol to the proper type
		var protocolEnum operations.UpdateFirewallFirewallsProtocol
		switch protocol {
		case "TCP":
			protocolEnum = operations.UpdateFirewallFirewallsProtocolTCP
		case "UDP":
			protocolEnum = operations.UpdateFirewallFirewallsProtocolUDP
		default:
			protocolEnum = operations.UpdateFirewallFirewallsProtocolTCP // default to TCP
		}

		rules = append(rules, operations.UpdateFirewallFirewallsRules{
			From:     &from,
			To:       &to,
			Port:     &port,
			Protocol: &protocolEnum,
		})
	}

	updateRequest := operations.UpdateFirewallFirewallsRequestBody{
		Data: operations.UpdateFirewallFirewallsData{
			Type: operations.UpdateFirewallFirewallsTypeFirewalls,
			Attributes: &operations.UpdateFirewallFirewallsAttributes{
				Name:  &name,
				Rules: rules,
			},
		},
	}

	_, err := r.client.Firewalls.UpdateFirewall(ctx, firewallID, updateRequest)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to update firewall, got error: "+err.Error())
		return
	}

	// Read the resource to populate all attributes
	r.readFirewall(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *FirewallResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data FirewallResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	firewallID := data.ID.ValueString()

	_, err := r.client.Firewalls.DeleteFirewall(ctx, firewallID)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to delete firewall, got error: "+err.Error())
		return
	}
}

func (r *FirewallResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	var data FirewallResourceModel
	data.ID = types.StringValue(req.ID)

	r.readFirewall(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *FirewallResource) readFirewall(ctx context.Context, data *FirewallResourceModel, diags *diag.Diagnostics) {
	firewallID := data.ID.ValueString()

	result, err := r.client.Firewalls.GetFirewall(ctx, firewallID)
	if err != nil {
		// Check if the firewall was deleted
		if apiErr, ok := err.(*components.APIError); ok && apiErr.StatusCode == http.StatusNotFound {
			data.ID = types.StringNull()
			return
		}
		diags.AddError("Client Error", "Unable to read firewall, got error: "+err.Error())
		return
	}

	if result.Firewall == nil {
		data.ID = types.StringNull()
		return
	}

	firewall := result.Firewall
	if firewall.Attributes != nil {
		if firewall.Attributes.Name != nil {
			data.Name = types.StringValue(*firewall.Attributes.Name)
		}

		if firewall.Attributes.Project != nil && firewall.Attributes.Project.ID != nil {
			data.Project = types.StringValue(*firewall.Attributes.Project.ID)
		}

		// Convert rules
		var rules []FirewallRuleModel
		for _, rule := range firewall.Attributes.Rules {
			ruleModel := FirewallRuleModel{}
			if rule.From != nil {
				ruleModel.From = types.StringValue(*rule.From)
			}
			if rule.To != nil {
				ruleModel.To = types.StringValue(*rule.To)
			}
			if rule.Port != nil {
				ruleModel.Port = types.StringValue(*rule.Port)
			}
			if rule.Protocol != nil {
				ruleModel.Protocol = types.StringValue(*rule.Protocol)
			}
			rules = append(rules, ruleModel)
		}
		data.Rules = rules
	}
}
