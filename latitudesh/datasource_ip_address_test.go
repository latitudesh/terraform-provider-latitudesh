package latitudesh

// These tests exercise the latitudesh_ip_address data source against a local
// mock of the Latitude.sh API, injected through the provider's httpClient
// (the same hook resource_virtual_machine_site_test.go uses). They run under
// TF_ACC without requiring credentials or creating real resources.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"

	"github.com/latitudesh/latitudesh-go-sdk/models/components"
)

func ipStrPtr(s string) *string { return &s }
func ipBoolPtr(b bool) *bool    { return &b }

// mockIPAddressAPI serves a fixed catalog of IP addresses: a management IP
// and an additional IP both assigned to "sv_1" (single-IP-per-server happy
// path lives on sv_1's management IP; sv_1 itself has two IPs so it also
// covers the ambiguous-server_id case), plus an unassigned additional IP.
type mockIPAddressAPI struct {
	ips []components.IPAddressData
}

func newMockIPAddressAPI() *mockIPAddressAPI {
	family := components.IPAddressDataFamilyIPv4
	typ := components.IPAddressDataAttributesTypePublic
	createdAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	mgmt := components.IPAddressData{
		ID:   ipStrPtr("ip_mgmt1"),
		Type: components.IPAddressDataTypeIPAddresses.ToPointer(),
		Attributes: &components.IPAddressDataAttributes{
			Address:    ipStrPtr("203.0.113.10"),
			Cidr:       ipStrPtr("203.0.113.10/32"),
			Family:     &family,
			Gateway:    ipStrPtr("203.0.113.1"),
			Netmask:    ipStrPtr("255.255.255.255"),
			Type:       &typ,
			Public:     ipBoolPtr(true),
			Management: ipBoolPtr(true),
			Additional: ipBoolPtr(false),
			Available:  ipBoolPtr(false),
			Project: &components.IPAddressDataProject{
				ID:   ipStrPtr("proj_1"),
				Name: ipStrPtr("Test Project"),
			},
			Region: &components.IPAddressDataRegion{
				ID:   ipStrPtr("reg_1"),
				Name: ipStrPtr("Ashburn"),
				Location: &components.IPAddressDataLocation{
					ID:   ipStrPtr("loc_1"),
					Name: ipStrPtr("Ashburn, VA"),
					Slug: ipStrPtr("ASH"),
				},
			},
			// Real assignment: server_id/hostname/assigned_at all populated.
			Assignment: &components.Assignment{
				ServerID:   ipStrPtr("sv_1"),
				Hostname:   ipStrPtr("web-01"),
				AssignedAt: ipStrPtr("2026-01-01T00:00:00Z"),
			},
			// Not an Elastic IP: API returns an empty object, not null.
			Elastic:   &components.Elastic{},
			CreatedAt: &createdAt,
		},
	}

	additionalOnSv1 := components.IPAddressData{
		ID:   ipStrPtr("ip_add1"),
		Type: components.IPAddressDataTypeIPAddresses.ToPointer(),
		Attributes: &components.IPAddressDataAttributes{
			Address:    ipStrPtr("203.0.113.11"),
			Cidr:       ipStrPtr("203.0.113.11/32"),
			Family:     &family,
			Type:       &typ,
			Public:     ipBoolPtr(true),
			Management: ipBoolPtr(false),
			Additional: ipBoolPtr(true),
			Available:  ipBoolPtr(false),
			Project: &components.IPAddressDataProject{
				ID:   ipStrPtr("proj_1"),
				Name: ipStrPtr("Test Project"),
			},
			Assignment: &components.Assignment{
				ServerID: ipStrPtr("sv_1"),
			},
			Elastic: &components.Elastic{},
		},
	}

	unassigned := components.IPAddressData{
		ID:   ipStrPtr("ip_add2"),
		Type: components.IPAddressDataTypeIPAddresses.ToPointer(),
		Attributes: &components.IPAddressDataAttributes{
			Address:    ipStrPtr("203.0.113.20"),
			Cidr:       ipStrPtr("203.0.113.20/32"),
			Family:     &family,
			Type:       &typ,
			Public:     ipBoolPtr(true),
			Management: ipBoolPtr(false),
			Additional: ipBoolPtr(true),
			Available:  ipBoolPtr(true),
			Project: &components.IPAddressDataProject{
				ID:   ipStrPtr("proj_1"),
				Name: ipStrPtr("Test Project"),
			},
			// Unassigned: API returns an empty object, not null.
			Assignment: &components.Assignment{},
			Elastic:    &components.Elastic{},
		},
	}

	return &mockIPAddressAPI{ips: []components.IPAddressData{mgmt, additionalOnSv1, unassigned}}
}

func (m *mockIPAddressAPI) handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/vnd.api+json")

	switch {
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/ips/"):
		id := strings.TrimPrefix(r.URL.Path, "/ips/")
		for _, ip := range m.ips {
			if ip.ID != nil && *ip.ID == id {
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(components.IPAddress{Data: &ip})
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":[{"status":"404"}]}`))

	case r.Method == http.MethodGet && r.URL.Path == "/ips":
		q := r.URL.Query()
		addressPrefix := q.Get("filter[address]")
		serverFilter := q.Get("filter[server]")

		var matched []components.IPAddressData
		for _, ip := range m.ips {
			if ip.Attributes == nil {
				continue
			}
			if addressPrefix != "" {
				if ip.Attributes.Address == nil || !strings.HasPrefix(*ip.Attributes.Address, addressPrefix) {
					continue
				}
			}
			if serverFilter != "" {
				if ip.Attributes.Assignment == nil || ip.Attributes.Assignment.ServerID == nil || *ip.Attributes.Assignment.ServerID != serverFilter {
					continue
				}
			}
			matched = append(matched, ip)
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(components.IPAddresses{Data: matched})

	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":[{"status":"404"}]}`))
	}
}

func testAccIPAddressProviderConfig(dataBlock string) string {
	return fmt.Sprintf(`
provider "latitudesh" {
  auth_token = "mock-token"
}

%s
`, dataBlock)
}

func TestAccDataSourceIPAddress_ByID(t *testing.T) {
	mock := newMockIPAddressAPI()
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: testAccIPAddressProviderConfig(`
data "latitudesh_ip_address" "by_id" {
  id = "ip_mgmt1"
}
`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.latitudesh_ip_address.by_id", "address", "203.0.113.10"),
					resource.TestCheckResourceAttr("data.latitudesh_ip_address.by_id", "family", "IPv4"),
					resource.TestCheckResourceAttr("data.latitudesh_ip_address.by_id", "management", "true"),
					resource.TestCheckResourceAttr("data.latitudesh_ip_address.by_id", "assignment.server_id", "sv_1"),
					resource.TestCheckResourceAttr("data.latitudesh_ip_address.by_id", "assignment.hostname", "web-01"),
					resource.TestCheckResourceAttr("data.latitudesh_ip_address.by_id", "project.id", "proj_1"),
					resource.TestCheckResourceAttr("data.latitudesh_ip_address.by_id", "region.name", "Ashburn"),
					resource.TestCheckResourceAttr("data.latitudesh_ip_address.by_id", "region.location.slug", "ASH"),
					resource.TestCheckResourceAttrSet("data.latitudesh_ip_address.by_id", "created_at"),
				),
			},
		},
	})
}

func TestAccDataSourceIPAddress_ByAddress(t *testing.T) {
	mock := newMockIPAddressAPI()
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: testAccIPAddressProviderConfig(`
data "latitudesh_ip_address" "by_address" {
  address = "203.0.113.20"
}
`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.latitudesh_ip_address.by_address", "id", "ip_add2"),
					resource.TestCheckResourceAttr("data.latitudesh_ip_address.by_address", "additional", "true"),
					resource.TestCheckResourceAttr("data.latitudesh_ip_address.by_address", "available", "true"),
				),
			},
		},
	})
}

func TestAccDataSourceIPAddress_ByServerID_Ambiguous(t *testing.T) {
	mock := newMockIPAddressAPI()
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		Steps: []resource.TestStep{
			{
				Config: testAccIPAddressProviderConfig(`
data "latitudesh_ip_address" "by_server" {
  server_id = "sv_1"
}
`),
				ExpectError: regexp.MustCompile(`has 2 IP addresses assigned`),
			},
		},
	})
}
