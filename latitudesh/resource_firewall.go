package latitudesh

import (
	"context"
	"fmt"
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
		switch strings.ToUpper(protocol) {
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

	result, err := r.client.Firewalls.Create(ctx, createRequest)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to create firewall, got error: "+err.Error())
		return
	}

	// Check for successful status codes (200 or 201)
	if result == nil {
		resp.Diagnostics.AddError("API Error", "Create firewall returned nil response")
		return
	}

	httpStatus := result.HTTPMeta.Response.StatusCode
	if httpStatus != 200 && httpStatus != 201 {
		resp.Diagnostics.AddError("API Error", fmt.Sprintf("Create firewall returned unexpected status code: %d", httpStatus))
		return
	}

	// Use List endpoint to find the firewall and get the correct ID
	r.findFirewallByProjectAndName(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	// Read the resource to populate all attributes
	r.readFirewall(ctx, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Helper function to find firewall by project and name
func (r *FirewallResource) findFirewallByProjectAndName(ctx context.Context, data *FirewallResourceModel, diags *diag.Diagnostics) {
	project := data.Project.ValueString()
	name := data.Name.ValueString()

	// Get firewalls filtered by project
	response, err := r.client.Firewalls.List(ctx, &project, nil, nil)
	if err != nil {
		diags.AddError("Client Error", "Unable to list firewalls to find created firewall, got error: "+err.Error())
		return
	}

	// Check if we have firewalls data
	if response.Firewalls == nil || response.Firewalls.Data == nil {
		diags.AddError("API Error", "No firewalls found for project after creation")
		return
	}

	// Look for firewall with matching name and project
	for _, firewall := range response.Firewalls.Data {
		if firewall.ID != nil && firewall.Attributes != nil {
			// Match by name
			if firewall.Attributes.Name != nil && *firewall.Attributes.Name == name {
				// Additional check for project if available
				if firewall.Attributes.Project != nil {
					if (firewall.Attributes.Project.ID != nil && *firewall.Attributes.Project.ID == project) ||
						(firewall.Attributes.Project.Slug != nil && *firewall.Attributes.Project.Slug == project) {
						data.ID = types.StringValue(*firewall.ID)
						// Add debug info to confirm ID is set
						diags.AddWarning("Debug", fmt.Sprintf("Firewall ID set to: %s for name: %s in project: %s", *firewall.ID, name, project))
						return
					}
				} else {
					// If no project info in attributes, assume it's the right one since we filtered by project
					data.ID = types.StringValue(*firewall.ID)
					// Add debug info to confirm ID is set
					diags.AddWarning("Debug", fmt.Sprintf("Firewall ID set to: %s for name: %s (no project validation)", *firewall.ID, name))
					return
				}
			}
		}
	}

	// If we get here, we couldn't find the matching firewall
	diags.AddError("API Error", fmt.Sprintf("Firewall was created but couldn't find it in the list with matching project (%s) and name (%s). Found %d firewalls total.", project, name, len(response.Firewalls.Data)))
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
		switch strings.ToUpper(protocol) {
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

	_, err := r.client.Firewalls.Update(ctx, firewallID, updateRequest)
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

	_, err := r.client.Firewalls.Delete(ctx, firewallID)
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

	result, err := r.client.Firewalls.Get(ctx, firewallID)
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

	// Handle either direct attributes OR nested data structure, not both
	var attributes *components.FirewallAttributes
	if firewall.Attributes != nil {
		// Use direct attributes if available
		attributes = firewall.Attributes
	} else if firewall.Data != nil && firewall.Data.Attributes != nil {
		// Only fall back to nested data if direct attributes are not available
		dataAttrs := firewall.Data.Attributes
		attributes = &components.FirewallAttributes{
			Name: dataAttrs.Name,
		}

		// Convert project if it exists
		if dataAttrs.Project != nil {
			attributes.Project = &components.FirewallProject{
				ID:   dataAttrs.Project.ID,
				Slug: dataAttrs.Project.Slug,
				Name: dataAttrs.Project.Name,
			}
		}

		// Convert rules
		if dataAttrs.Rules != nil {
			var rules []components.Rules
			for _, rule := range dataAttrs.Rules {
				rules = append(rules, components.Rules{
					From:     rule.From,
					To:       rule.To,
					Port:     rule.Port,
					Protocol: rule.Protocol,
				})
			}
			attributes.Rules = rules
		}
	}

	if attributes != nil {
		if attributes.Name != nil {
			data.Name = types.StringValue(*attributes.Name)
		}

		if attributes.Project != nil && attributes.Project.ID != nil {
			data.Project = types.StringValue(*attributes.Project.ID)
		}

		// Only update rules if we don't have configured rules to preserve user configuration
		// This prevents the API from adding extra rules that would cause count mismatches
		if len(data.Rules) == 0 {
			// This is import/initial read - populate rules from API
			var rules []FirewallRuleModel
			for _, rule := range attributes.Rules {
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
					// Normalize protocol to match user input case
					protocol := *rule.Protocol
					// Keep the original case if it matches common conventions
					if strings.ToUpper(protocol) == "TCP" {
						ruleModel.Protocol = types.StringValue("TCP")
					} else if strings.ToUpper(protocol) == "UDP" {
						ruleModel.Protocol = types.StringValue("UDP")
					} else {
						ruleModel.Protocol = types.StringValue(protocol)
					}
				}
				rules = append(rules, ruleModel)
			}
			data.Rules = rules
		}
		// If we have configured rules, preserve them to avoid count mismatches from API-added rules
	}
}
