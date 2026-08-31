package latitudesh

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
	"github.com/latitudesh/latitudesh-go-sdk/models/operations"
	iprovider "github.com/latitudesh/terraform-provider-latitudesh/v2/internal/provider"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ datasource.DataSource = &EventDataSource{}
var _ datasource.DataSourceWithConfigure = &EventDataSource{}

func NewEventDataSource() datasource.DataSource {
	return &EventDataSource{}
}

type EventDataSource struct {
	client *latitudeshgosdk.Latitudesh
}

type EventDataSourceModel struct {
	// Filters
	FilterAuthor       types.String `tfsdk:"filter_author"`
	FilterProject      types.String `tfsdk:"filter_project"`
	FilterTargetName   types.List   `tfsdk:"filter_target_name"`
	FilterTargetID     types.String `tfsdk:"filter_target_id"`
	FilterAction       types.String `tfsdk:"filter_action"`
	FilterCreatedAtGte types.String `tfsdk:"filter_created_at_gte"`
	FilterCreatedAtLte types.String `tfsdk:"filter_created_at_lte"`
	FilterCreatedAt    types.List   `tfsdk:"filter_created_at"`
	PageSize           types.Int64  `tfsdk:"page_size"`
	PageNumber         types.Int64  `tfsdk:"page_number"`
	Sort               types.String `tfsdk:"sort"`

	// Attributes
	ID         types.String `tfsdk:"id"`
	TotalCount types.Int64  `tfsdk:"total_count"`
	Events     types.List   `tfsdk:"events"`
}

type EventModel struct {
	ID        types.String `tfsdk:"id"`
	Type      types.String `tfsdk:"type"`
	Action    types.String `tfsdk:"action"`
	CreatedAt types.String `tfsdk:"created_at"`
	Author    types.Object `tfsdk:"author"`
	Project   types.Object `tfsdk:"project"`
	Team      types.Object `tfsdk:"team"`
	Target    types.Object `tfsdk:"target"`
}

type EventAuthorModel struct {
	ID    types.String `tfsdk:"id"`
	Name  types.String `tfsdk:"name"`
	Email types.String `tfsdk:"email"`
}

type EventProjectModel struct {
	ID   types.String `tfsdk:"id"`
	Name types.String `tfsdk:"name"`
	Slug types.String `tfsdk:"slug"`
}

type EventTeamModel struct {
	ID   types.String `tfsdk:"id"`
	Name types.String `tfsdk:"name"`
}

type EventTargetModel struct {
	ID   types.String `tfsdk:"id"`
	Name types.String `tfsdk:"name"`
}

var eventAuthorObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"id":    types.StringType,
		"name":  types.StringType,
		"email": types.StringType,
	},
}

var eventProjectObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"id":   types.StringType,
		"name": types.StringType,
		"slug": types.StringType,
	},
}

var eventTeamObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"id":   types.StringType,
		"name": types.StringType,
	},
}

var eventTargetObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"id":   types.StringType,
		"name": types.StringType,
	},
}

var eventObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"id":         types.StringType,
		"type":       types.StringType,
		"action":     types.StringType,
		"created_at": types.StringType,
		"author":     eventAuthorObjectType,
		"project":    eventProjectObjectType,
		"team":       eventTeamObjectType,
		"target":     eventTargetObjectType,
	},
}

func eventAuthorValue(ctx context.Context, a *components.Author) (types.Object, diag.Diagnostics) {
	if a == nil {
		return types.ObjectNull(eventAuthorObjectType.AttrTypes), nil
	}
	model := EventAuthorModel{
		ID:    types.StringPointerValue(a.ID),
		Name:  types.StringPointerValue(a.Name),
		Email: types.StringPointerValue(a.Email),
	}
	return types.ObjectValueFrom(ctx, eventAuthorObjectType.AttrTypes, model)
}

func eventProjectValue(ctx context.Context, p *components.EventDataProject) (types.Object, diag.Diagnostics) {
	if p == nil {
		return types.ObjectNull(eventProjectObjectType.AttrTypes), nil
	}
	model := EventProjectModel{
		ID:   types.StringPointerValue(p.ID),
		Name: types.StringPointerValue(p.Name),
		Slug: types.StringPointerValue(p.Slug),
	}
	return types.ObjectValueFrom(ctx, eventProjectObjectType.AttrTypes, model)
}

func eventTeamValue(ctx context.Context, tm *components.EventDataTeam) (types.Object, diag.Diagnostics) {
	if tm == nil {
		return types.ObjectNull(eventTeamObjectType.AttrTypes), nil
	}
	model := EventTeamModel{
		ID:   types.StringPointerValue(tm.ID),
		Name: types.StringPointerValue(tm.Name),
	}
	return types.ObjectValueFrom(ctx, eventTeamObjectType.AttrTypes, model)
}

func eventTargetValue(ctx context.Context, tg *components.Target) (types.Object, diag.Diagnostics) {
	if tg == nil {
		return types.ObjectNull(eventTargetObjectType.AttrTypes), nil
	}
	model := EventTargetModel{
		ID:   types.StringPointerValue(tg.ID),
		Name: types.StringPointerValue(tg.Name),
	}
	return types.ObjectValueFrom(ctx, eventTargetObjectType.AttrTypes, model)
}

// eventsValue maps the raw event log entries to their Terraform representation.
// It always returns a known (never null) list so `[for e in data.events : ...]`
// never fails on a null iteratee, even when no events match the filters.
func eventsValue(ctx context.Context, items []components.EventData) (types.List, diag.Diagnostics) {
	var diags diag.Diagnostics

	models := make([]EventModel, 0, len(items))
	for _, e := range items {
		model := EventModel{
			ID:        types.StringPointerValue(e.ID),
			Type:      types.StringNull(),
			Action:    types.StringNull(),
			CreatedAt: types.StringNull(),
			Author:    types.ObjectNull(eventAuthorObjectType.AttrTypes),
			Project:   types.ObjectNull(eventProjectObjectType.AttrTypes),
			Team:      types.ObjectNull(eventTeamObjectType.AttrTypes),
			Target:    types.ObjectNull(eventTargetObjectType.AttrTypes),
		}

		if e.Type != nil {
			model.Type = types.StringValue(string(*e.Type))
		}

		if e.Attributes != nil {
			attrs := e.Attributes

			model.Action = types.StringPointerValue(attrs.Action)
			model.CreatedAt = types.StringPointerValue(attrs.CreatedAt)

			author, d := eventAuthorValue(ctx, attrs.Author)
			diags.Append(d...)
			model.Author = author

			project, d := eventProjectValue(ctx, attrs.Project)
			diags.Append(d...)
			model.Project = project

			team, d := eventTeamValue(ctx, attrs.Team)
			diags.Append(d...)
			model.Team = team

			target, d := eventTargetValue(ctx, attrs.Target)
			diags.Append(d...)
			model.Target = target

			// attrs.Properties ("Additional event-specific data") is generated
			// by the SDK as an empty struct with no fields, so there is
			// nothing typed to map here even though the API doc says it
			// carries per-event data.
		}

		models = append(models, model)
	}

	list, d := types.ListValueFrom(ctx, eventObjectType, models)
	diags.Append(d...)
	return list, diags
}

// eventsQueryID derives a stable identifier for this data source read from the
// filters actually sent to the API. There is no natural single-record ID for a
// list endpoint, so identity is the query itself: the same filters (and page)
// always hash to the same ID, and a changed filter changes it.
func eventsQueryID(req operations.GetEventsRequest) string {
	var b strings.Builder
	write := func(s string) {
		b.WriteString(s)
		b.WriteByte('\x00')
	}

	write(derefString(req.FilterAuthor))
	write(derefString(req.FilterProject))
	write(strings.Join(req.FilterTargetName, ","))
	write(derefString(req.FilterTargetID))
	write(derefString(req.FilterAction))
	write(derefString(req.FilterCreatedAtGte))
	write(derefString(req.FilterCreatedAtLte))
	write(strings.Join(req.FilterCreatedAt, ","))
	if req.PageSize != nil {
		write(strconv.FormatInt(*req.PageSize, 10))
	} else {
		write("")
	}
	if req.PageNumber != nil {
		write(strconv.FormatInt(*req.PageNumber, 10))
	} else {
		write("")
	}
	write(derefString(req.Sort))

	sum := sha256.Sum256([]byte(b.String()))
	return "events-" + hex.EncodeToString(sum[:])
}

func (d *EventDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_event"
}

func (d *EventDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	deps := iprovider.ConfigureFromProviderData(req.ProviderData, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	d.client = deps.Client
}

func (d *EventDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Events data source - list account activity events (actions performed by users), optionally filtered by author, project, target, action, or a creation-date range.",
		Attributes: map[string]schema.Attribute{
			"filter_author": schema.StringAttribute{
				MarkdownDescription: "Restrict results to events performed by this author ID or email.",
				Optional:            true,
			},
			"filter_project": schema.StringAttribute{
				MarkdownDescription: "Restrict results to events on this project ID.",
				Optional:            true,
			},
			"filter_target_name": schema.ListAttribute{
				MarkdownDescription: "Restrict results to these event target type(s) (e.g. `server`, `virtual_machine`).",
				ElementType:         types.StringType,
				Optional:            true,
			},
			"filter_target_id": schema.StringAttribute{
				MarkdownDescription: "Restrict results to events on this target ID.",
				Optional:            true,
			},
			"filter_action": schema.StringAttribute{
				MarkdownDescription: "Restrict results to events with this action.",
				Optional:            true,
			},
			"filter_created_at_gte": schema.StringAttribute{
				MarkdownDescription: "Restrict results to events created at or after this timestamp (ISO `yyyy-MM-dd'T'HH:mm:ss`).",
				Optional:            true,
			},
			"filter_created_at_lte": schema.StringAttribute{
				MarkdownDescription: "Restrict results to events created at or before this timestamp (ISO `yyyy-MM-dd'T'HH:mm:ss`).",
				Optional:            true,
			},
			"filter_created_at": schema.ListAttribute{
				MarkdownDescription: "Restrict results to events created within this inclusive date range: `[date1, date2]` (ISO `yyyy-MM-dd'T'HH:mm:ss`).",
				ElementType:         types.StringType,
				Optional:            true,
			},
			"page_size": schema.Int64Attribute{
				MarkdownDescription: "Number of events to return per page. Defaults to the API's own default (20) when unset.",
				Optional:            true,
			},
			"page_number": schema.Int64Attribute{
				MarkdownDescription: "Page number to return (starts at 1). Defaults to the API's own default (1) when unset. Results are not aggregated across pages; set this to page through the full result set.",
				Optional:            true,
			},
			"sort": schema.StringAttribute{
				MarkdownDescription: "Comma-separated sort fields. Prefix a field with `-` for descending order. Supported: `created_at`.",
				Optional:            true,
			},

			"id": schema.StringAttribute{
				MarkdownDescription: "Identifier for this query, derived from the filters and page requested. Not an event ID.",
				Computed:            true,
			},
			"total_count": schema.Int64Attribute{
				MarkdownDescription: "Total number of events matching the filters, across all pages.",
				Computed:            true,
			},
			"events": schema.ListNestedAttribute{
				MarkdownDescription: "Events matching the filters, for the requested page.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							MarkdownDescription: "Event identifier.",
							Computed:            true,
						},
						"type": schema.StringAttribute{
							MarkdownDescription: "Resource type, as returned by the API.",
							Computed:            true,
						},
						"action": schema.StringAttribute{
							MarkdownDescription: "The action performed.",
							Computed:            true,
						},
						"created_at": schema.StringAttribute{
							MarkdownDescription: "Timestamp when the event was created.",
							Computed:            true,
						},
						"author": schema.SingleNestedAttribute{
							MarkdownDescription: "The user who performed the action, when the event has one.",
							Computed:            true,
							Attributes: map[string]schema.Attribute{
								"id": schema.StringAttribute{
									MarkdownDescription: "Author ID.",
									Computed:            true,
								},
								"name": schema.StringAttribute{
									MarkdownDescription: "Author name.",
									Computed:            true,
								},
								"email": schema.StringAttribute{
									MarkdownDescription: "Author email.",
									Computed:            true,
								},
							},
						},
						"project": schema.SingleNestedAttribute{
							MarkdownDescription: "The project the event belongs to, when it belongs to one.",
							Computed:            true,
							Attributes: map[string]schema.Attribute{
								"id": schema.StringAttribute{
									MarkdownDescription: "Project ID.",
									Computed:            true,
								},
								"name": schema.StringAttribute{
									MarkdownDescription: "Project name.",
									Computed:            true,
								},
								"slug": schema.StringAttribute{
									MarkdownDescription: "Project slug.",
									Computed:            true,
								},
							},
						},
						"team": schema.SingleNestedAttribute{
							MarkdownDescription: "The team the event belongs to, when it belongs to one.",
							Computed:            true,
							Attributes: map[string]schema.Attribute{
								"id": schema.StringAttribute{
									MarkdownDescription: "Team ID.",
									Computed:            true,
								},
								"name": schema.StringAttribute{
									MarkdownDescription: "Team name.",
									Computed:            true,
								},
							},
						},
						"target": schema.SingleNestedAttribute{
							MarkdownDescription: "The resource the action was performed on, when the event has one.",
							Computed:            true,
							Attributes: map[string]schema.Attribute{
								"id": schema.StringAttribute{
									MarkdownDescription: "Target ID.",
									Computed:            true,
								},
								"name": schema.StringAttribute{
									MarkdownDescription: "Target name.",
									Computed:            true,
								},
							},
						},
					},
				},
			},
		},
	}
}

func (d *EventDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data EventDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if d.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured.")
		return
	}

	statsTotal := "count"
	request := operations.GetEventsRequest{
		StatsTotal: &statsTotal,
	}

	if !data.FilterAuthor.IsNull() {
		v := data.FilterAuthor.ValueString()
		request.FilterAuthor = &v
	}
	if !data.FilterProject.IsNull() {
		v := data.FilterProject.ValueString()
		request.FilterProject = &v
	}
	if !data.FilterTargetName.IsNull() {
		var v []string
		resp.Diagnostics.Append(data.FilterTargetName.ElementsAs(ctx, &v, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
		request.FilterTargetName = v
	}
	if !data.FilterTargetID.IsNull() {
		v := data.FilterTargetID.ValueString()
		request.FilterTargetID = &v
	}
	if !data.FilterAction.IsNull() {
		v := data.FilterAction.ValueString()
		request.FilterAction = &v
	}
	if !data.FilterCreatedAtGte.IsNull() {
		v := data.FilterCreatedAtGte.ValueString()
		request.FilterCreatedAtGte = &v
	}
	if !data.FilterCreatedAtLte.IsNull() {
		v := data.FilterCreatedAtLte.ValueString()
		request.FilterCreatedAtLte = &v
	}
	if !data.FilterCreatedAt.IsNull() {
		var v []string
		resp.Diagnostics.Append(data.FilterCreatedAt.ElementsAs(ctx, &v, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
		request.FilterCreatedAt = v
	}
	if !data.PageSize.IsNull() {
		v := data.PageSize.ValueInt64()
		request.PageSize = &v
	}
	if !data.PageNumber.IsNull() {
		v := data.PageNumber.ValueInt64()
		request.PageNumber = &v
	}
	if !data.Sort.IsNull() {
		v := data.Sort.ValueString()
		request.Sort = &v
	}

	data.ID = types.StringValue(eventsQueryID(request))

	res, err := d.client.Events.List(ctx, request)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to list events, got error: %s", err.Error()))
		return
	}
	if res.Events == nil {
		resp.Diagnostics.AddError("API Error", "Events response did not contain any data.")
		return
	}

	events, diags := eventsValue(ctx, res.Events.Data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	data.Events = events

	data.TotalCount = types.Int64Null()
	if res.Events.Meta != nil && res.Events.Meta.Stats != nil && res.Events.Meta.Stats.Total != nil {
		data.TotalCount = types.Int64PointerValue(res.Events.Meta.Stats.Total.Count)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
