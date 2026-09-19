package latitudesh

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/ephemeral"
	"github.com/hashicorp/terraform-plugin-framework/ephemeral/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"

	iprovider "github.com/latitudesh/terraform-provider-latitudesh/v2/internal/provider"
)

var (
	_ ephemeral.EphemeralResource              = &LksKubeconfigEphemeral{}
	_ ephemeral.EphemeralResourceWithConfigure = &LksKubeconfigEphemeral{}
)

func NewLksKubeconfigEphemeral() ephemeral.EphemeralResource {
	return &LksKubeconfigEphemeral{}
}

// LksKubeconfigEphemeral reads a cluster's kubeconfig for the duration of a
// Terraform run. It is ephemeral rather than a data source because a
// kubeconfig carries cluster-admin credentials: a data source would write them
// into state and into every plan file, where they are neither encrypted nor
// redacted. That an LKS kubeconfig does NOT expire sharpens the point rather
// than softening it — a leaked one stays valid indefinitely, unlike the
// 15-minute EKS token or DigitalOcean's 7-day default.
//
// It implements neither Close nor Renew, and both omissions are deliberate:
// nothing is provisioned here to revoke afterwards, and a credential with no
// expiry cannot go stale mid-apply the way aws_eks_cluster_auth's does. A node
// pool apply can run for an hour; this survives it.
//
// The schema is exactly the API's surface — one document. An earlier revision
// also decomposed the YAML into host/cluster_ca_certificate/client_*/token the
// way upcloud_kubernetes_cluster does. That was dropped: the endpoint
// specifies a single field ("Full kubeconfig YAML") and says nothing about
// which authentication method LKS issues, so a schema carrying both a
// certificate pair and a token would have been publishing a guess as though it
// were contract. Callers who want the pieces can yamldecode() the document in
// configuration, where the assumption is theirs and visible.
type LksKubeconfigEphemeral struct {
	client *latitudeshgosdk.Latitudesh
}

type LksKubeconfigEphemeralModel struct {
	ClusterID  types.String `tfsdk:"cluster_id"`
	Kubeconfig types.String `tfsdk:"kubeconfig"`
}

func (e *LksKubeconfigEphemeral) Metadata(ctx context.Context, req ephemeral.MetadataRequest, resp *ephemeral.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_lks_kubeconfig"
}

func (e *LksKubeconfigEphemeral) Configure(ctx context.Context, req ephemeral.ConfigureRequest, resp *ephemeral.ConfigureResponse) {
	deps := iprovider.ConfigureFromProviderData(req.ProviderData, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	e.client = deps.Client
}

func (e *LksKubeconfigEphemeral) Schema(ctx context.Context, req ephemeral.SchemaRequest, resp *ephemeral.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads an LKS (Latitude Kubernetes Service) cluster's kubeconfig for the duration of the Terraform run. " +
			"The credentials never reach state or plan files, which is why this is an ephemeral resource rather than a data source — " +
			"a kubeconfig grants cluster-admin. Requires Terraform 1.10 or later.\n\n" +
			"The control plane must be up: `GET /lks/clusters/{id}/kubeconfig` answers 409 `NOT_READY` until `latitudesh_lks.status` is `ready`. " +
			"For an existing cluster take the id from `latitudesh_lks_clusters`; for one created in the same configuration reference the `latitudesh_lks` resource, so the read is ordered after its create returns.\n\n" +
			"An LKS kubeconfig does not expire, so this needs no renewal and stays valid across a long apply — and a copy that leaks stays valid too, which is the reason not to persist one.",
		Attributes: map[string]schema.Attribute{
			"cluster_id": schema.StringAttribute{
				MarkdownDescription: "ID of the cluster to read the kubeconfig from.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"kubeconfig": schema.StringAttribute{
				MarkdownDescription: "The kubeconfig document, verbatim as the API returns it. This is the whole of what the endpoint exposes; use `yamldecode()` to pull fields out of it.",
				Computed:            true,
				Sensitive:           true,
			},
		},
	}
}

func (e *LksKubeconfigEphemeral) Open(ctx context.Context, req ephemeral.OpenRequest, resp *ephemeral.OpenResponse) {
	var data LksKubeconfigEphemeralModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if e.client == nil {
		resp.Diagnostics.AddError("Client not configured", "The provider client was not configured.")
		return
	}

	clusterID := data.ClusterID.ValueString()

	result, err := e.client.Lks.GetLksClusterKubeconfig(ctx, clusterID)
	if err != nil {
		if lksClusterNotFound(err) {
			resp.Diagnostics.AddError("Not Found", "No LKS cluster exists with ID \""+clusterID+"\"")
			return
		}
		resp.Diagnostics.AddError("Client Error",
			fmt.Sprintf("Unable to read the kubeconfig for LKS cluster %q: %s\n\n"+
				"A 409 NOT_READY here means the control plane is still coming up — reference the cluster resource so the read is ordered after it.",
				clusterID, err))
		return
	}

	if result == nil || result.LksClusterKubeconfig == nil ||
		result.LksClusterKubeconfig.Data == nil ||
		result.LksClusterKubeconfig.Data.Attributes == nil ||
		result.LksClusterKubeconfig.Data.Attributes.Kubeconfig == nil {
		resp.Diagnostics.AddError("Unexpected API response",
			"The kubeconfig response did not include a kubeconfig document.")
		return
	}

	data.Kubeconfig = types.StringValue(*result.LksClusterKubeconfig.Data.Attributes.Kubeconfig)

	resp.Diagnostics.Append(resp.Result.Set(ctx, &data)...)
}
