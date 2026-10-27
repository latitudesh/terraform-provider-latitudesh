package latitudesh

// These tests exercise the server public_network_id attribute (the server
// bonded onto a latitudesh_public_network) against the local mock in
// resource_server_deploy_config_test.go. They run under TF_ACC without
// credentials or real resources.
//
// The API takes public_network = true and public_network_id together on
// create, and reports the attachment back on GET /servers/{id} as
// public_network.id, the same pn_ hash latitudesh_public_network.id carries.

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
)

func testAccServerPublicNetworkConfig(publicNetworkID string) string {
	extra := ""
	if publicNetworkID != "" {
		extra = fmt.Sprintf("  public_network_id = %q", publicNetworkID)
	}
	return testAccServerDeployConfig(testServerMockOS, extra)
}

// checkCreatePublicNetwork asserts the public network keys of the last create
// payload. want == "" means both keys must be absent.
func checkCreatePublicNetwork(m *mockDeployConfigServerAPI, want string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		m.mu.Lock()
		defer m.mu.Unlock()
		flag, hasFlag := m.createAttrs["public_network"]
		id, hasID := m.createAttrs["public_network_id"]
		if want == "" {
			if hasFlag || hasID {
				return fmt.Errorf("create payload carried public_network = %v, public_network_id = %v; want both omitted", flag, id)
			}
			return nil
		}
		if flag != true || id != want {
			return fmt.Errorf("create payload public_network = %v, public_network_id = %v; want true, %q", flag, id, want)
		}
		return nil
	}
}

func checkCreatesAndDeletes(m *mockDeployConfigServerAPI, creates, deletes int) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.creates != creates || m.deletes != deletes {
			return fmt.Errorf("got %d create(s) and %d delete(s), want %d and %d", m.creates, m.deletes, creates, deletes)
		}
		return nil
	}
}

func TestAccServer_PublicNetworkSentOnCreate(t *testing.T) {
	mock, server := newDeployConfigServerMock(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockDeployConfigServerDestroyed(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccServerPublicNetworkConfig("pn_mock_1"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("latitudesh_server.test_item", "public_network_id", "pn_mock_1"),
					checkCreatePublicNetwork(mock, "pn_mock_1"),
				),
			},
			{
				Config:   testAccServerPublicNetworkConfig("pn_mock_1"),
				PlanOnly: true,
			},
		},
	})
}

func TestAccServer_PublicNetworkOmittedByDefault(t *testing.T) {
	mock, server := newDeployConfigServerMock(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockDeployConfigServerDestroyed(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccServerPublicNetworkConfig(""),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckNoResourceAttr("latitudesh_server.test_item", "public_network_id"),
					checkCreatePublicNetwork(mock, ""),
				),
			},
			{
				Config:   testAccServerPublicNetworkConfig(""),
				PlanOnly: true,
			},
		},
	})
}

// Moving the server to another network is a new deploy: the old server is
// destroyed and a new one is created on the new network.
func TestAccServer_PublicNetworkChangeReplaces(t *testing.T) {
	mock, server := newDeployConfigServerMock(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockDeployConfigServerDestroyed(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccServerPublicNetworkConfig("pn_mock_1"),
				Check:  checkCreatesAndDeletes(mock, 1, 0),
			},
			{
				Config: testAccServerPublicNetworkConfig("pn_mock_2"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("latitudesh_server.test_item", "public_network_id", "pn_mock_2"),
					checkCreatesAndDeletes(mock, 2, 1),
					checkCreatePublicNetwork(mock, "pn_mock_2"),
				),
			},
		},
	})
}

// Removing public_network_id from the configuration must not destroy the
// server: the attachment stays and the attribute keeps reporting it.
func TestAccServer_PublicNetworkRemovedKeepsAttachment(t *testing.T) {
	mock, server := newDeployConfigServerMock(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockDeployConfigServerDestroyed(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccServerPublicNetworkConfig("pn_mock_1"),
			},
			{
				Config:   testAccServerPublicNetworkConfig(""),
				PlanOnly: true,
			},
			{
				Config: testAccServerPublicNetworkConfig(""),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("latitudesh_server.test_item", "public_network_id", "pn_mock_1"),
					checkCreatesAndDeletes(mock, 1, 0),
				),
			},
		},
	})
}

// A server attached outside this configuration (the import case) reports its
// network without the configuration declaring it, and plans clean.
func TestAccServer_PublicNetworkReadBackWhenNotConfigured(t *testing.T) {
	mock, server := newDeployConfigServerMock(t)
	mock.attachOnCreate = "pn_mock_9"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockDeployConfigServerDestroyed(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccServerPublicNetworkConfig(""),
				Check:  resource.TestCheckResourceAttr("latitudesh_server.test_item", "public_network_id", "pn_mock_9"),
			},
			{
				Config:   testAccServerPublicNetworkConfig(""),
				PlanOnly: true,
			},
		},
	})
}

// The API omits public_network while a bond is being set up. Reading that
// right after create must keep the configured ID instead of nulling it, which
// Terraform would reject as an inconsistent result.
func TestAccServer_PublicNetworkKeptWhenAPIOmitsIt(t *testing.T) {
	mock, server := newDeployConfigServerMock(t)
	mock.hidePublicNetwork = true

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMockDeployConfigServerDestroyed(mock),
		Steps: []resource.TestStep{
			{
				Config: testAccServerPublicNetworkConfig("pn_mock_1"),
				Check:  resource.TestCheckResourceAttr("latitudesh_server.test_item", "public_network_id", "pn_mock_1"),
			},
			{
				Config:   testAccServerPublicNetworkConfig("pn_mock_1"),
				PlanOnly: true,
			},
		},
	})
}

func TestPublicNetworkIDFromAPI(t *testing.T) {
	t.Parallel()

	id := func(s string) *components.ServerDataPublicNetwork { return &components.ServerDataPublicNetwork{ID: &s} }

	cases := []struct {
		name  string
		prior types.String
		api   *components.ServerDataPublicNetwork
		want  types.String
	}{
		{"reported network fills a null prior", types.StringNull(), id("pn_a"), types.StringValue("pn_a")},
		{"reported network replaces a different prior", types.StringValue("pn_a"), id("pn_b"), types.StringValue("pn_b")},
		{"missing network keeps the prior", types.StringValue("pn_a"), nil, types.StringValue("pn_a")},
		{"missing id keeps the prior", types.StringValue("pn_a"), &components.ServerDataPublicNetwork{}, types.StringValue("pn_a")},
		{"empty id keeps the prior", types.StringValue("pn_a"), id(""), types.StringValue("pn_a")},
		{"missing network leaves unknown for the caller", types.StringUnknown(), nil, types.StringUnknown()},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := publicNetworkIDFromAPI(tc.prior, tc.api); !got.Equal(tc.want) {
				t.Fatalf("publicNetworkIDFromAPI(%v) = %v, want %v", tc.prior, got, tc.want)
			}
		})
	}
}
