package latitudesh

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

const mdbResourceName = "latitudesh_managed_database.test"

const mdbAllowlist1 = `  trusted_sources = ["203.0.113.10/32"]
  parameters = {
    work_mem = "64MB"
  }
`

const mdbAllowlist2 = `  trusted_sources = ["203.0.113.10/32", "198.51.100.0/24"]
  parameters = {
    work_mem = "128MB"
  }
`

// mdbConfig builds a config for one managed database. providerProject, when
// non-empty, sets the provider-level default project. body is the resource's
// attributes.
func mdbConfig(providerProject, body string) string {
	providerLine := ""
	if providerProject != "" {
		providerLine = fmt.Sprintf("  project    = %q\n", providerProject)
	}
	return fmt.Sprintf(`
provider "latitudesh" {
  auth_token = "mock-token"
%s}

resource "latitudesh_managed_database" "test" {
%s
}
`, providerLine, body)
}

func mdbBody(project, plan, region, extra string) string {
	projectLine := ""
	if project != "" {
		projectLine = fmt.Sprintf("  project = %q\n", project)
	}
	return fmt.Sprintf(`%s  name    = "api"
  region  = %q
  plan    = %q
  engine  = "postgres"
%s`, projectLine, region, plan, extra)
}

// mdbCheckLog asserts the managed-database requests logged since the last
// reset, e.g. "POST GET GET".
func mdbCheckLog(m *mdbMock, check func(methods string) error) resource.TestCheckFunc {
	return func(*terraform.State) error {
		return check(m.methods())
	}
}

func mdbCheckReplaced(m *mdbMock) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		methods := m.methods()
		del, post := strings.Index(methods, "DELETE"), strings.Index(methods, "POST")
		if del < 0 || post < 0 || del > post {
			return fmt.Errorf("want DELETE then POST, got %q", methods)
		}
		_, last := m.ids()
		rs := s.RootModule().Resources[mdbResourceName]
		if rs == nil || rs.Primary.ID != last {
			return fmt.Errorf("state ID is not the new database %s", last)
		}
		return nil
	}
}

func TestManagedDatabase_MockLifecycle(t *testing.T) {
	mock, server := newMdbMock(t)

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMdbMockDestroyed(mock),
		Steps: []resource.TestStep{
			{
				// Create. The POST carries no credentials and says
				// "provisioning"; status and credentials must come from the
				// final GET. trusted_sources is applied by a PATCH once the
				// database is ready.
				PreConfig: mock.resetLog,
				Config:    mdbConfig("", mdbBody("proj_mock_1", "db.psql.small", "ASH", mdbAllowlist1)),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(mdbResourceName, "id", "mdb_mock_1"),
					resource.TestCheckResourceAttr(mdbResourceName, "status", "running"),
					resource.TestCheckResourceAttr(mdbResourceName, "credentials.host", "db.mock.internal"),
					resource.TestCheckResourceAttr(mdbResourceName, "credentials.port", "5432"),
					resource.TestCheckResourceAttr(mdbResourceName, "credentials.password", "mock-secret"),
					resource.TestCheckResourceAttr(mdbResourceName, "project", "proj_mock_1"),
					resource.TestCheckResourceAttr(mdbResourceName, "billing", "monthly"),
					resource.TestCheckResourceAttr(mdbResourceName, "trusted_sources.#", "1"),
					resource.TestCheckTypeSetElemAttr(mdbResourceName, "trusted_sources.*", "203.0.113.10/32"),
					resource.TestCheckResourceAttr(mdbResourceName, "parameters.work_mem", "64MB"),
					mdbCheckLog(mock, func(methods string) error {
						// POST, the readiness GETs, the PATCH, more GETs.
						i := strings.Index(methods, "PATCH")
						if !strings.HasPrefix(methods, "POST GET GET") || i < 0 || !strings.Contains(methods[i:], "GET") {
							return fmt.Errorf("want POST, GETs, PATCH, GETs; got %q", methods)
						}
						return nil
					}),
				),
			},
			{
				// The server adds parameter keys of its own: no diff.
				PreConfig: func() {
					mock.set(func(m *mdbMock) { m.extraParams = map[string]string{"shared_buffers": "128MB"} })
				},
				Config:   mdbConfig("", mdbBody("proj_mock_1", "db.psql.small", "ASH", mdbAllowlist1)),
				PlanOnly: true,
			},
			{
				// trusted_sources and parameters update in place.
				PreConfig: mock.resetLog,
				Config:    mdbConfig("", mdbBody("proj_mock_1", "db.psql.small", "ASH", mdbAllowlist2)),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(mdbResourceName, "id", "mdb_mock_1"),
					resource.TestCheckResourceAttr(mdbResourceName, "trusted_sources.#", "2"),
					resource.TestCheckResourceAttr(mdbResourceName, "parameters.work_mem", "128MB"),
					mdbCheckLog(mock, func(methods string) error {
						if strings.Contains(methods, "POST") || strings.Contains(methods, "DELETE") || strings.Count(methods, "PATCH") != 1 {
							return fmt.Errorf("want one PATCH and no POST or DELETE, got %q", methods)
						}
						return nil
					}),
				),
			},
			{
				ResourceName:            mdbResourceName,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"timeouts", "parameters"},
			},
			{
				PreConfig: mock.resetLog,
				Config:    mdbConfig("", mdbBody("proj_mock_1", "db.psql.medium", "ASH", mdbAllowlist2)),
				Check: resource.ComposeTestCheckFunc(
					mdbCheckReplaced(mock),
					resource.TestCheckResourceAttr(mdbResourceName, "plan", "db.psql.medium"),
				),
			},
			{
				PreConfig: mock.resetLog,
				Config:    mdbConfig("", mdbBody("proj_mock_1", "db.psql.medium", "MIA", mdbAllowlist2)),
				Check: resource.ComposeTestCheckFunc(
					mdbCheckReplaced(mock),
					resource.TestCheckResourceAttr(mdbResourceName, "region", "MIA"),
				),
			},
			{
				// The database is deleted outside Terraform: refresh removes it.
				PreConfig: func() {
					_, last := mock.ids()
					mock.deleteOutOfBand(last)
				},
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				Check: func(s *terraform.State) error {
					if _, ok := s.RootModule().Resources[mdbResourceName]; ok {
						return fmt.Errorf("%s is still in state after a 404", mdbResourceName)
					}
					return nil
				},
			},
		},
	})
}

func TestManagedDatabase_MockProjectSlugCreate(t *testing.T) {
	mock, server := newMdbMock(t)
	mock.projects["my-slug"] = "proj_mock_1"

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMdbMockDestroyed(mock),
		Steps: []resource.TestStep{
			{
				Config: mdbConfig("", mdbBody("my-slug", "db.psql.small", "ASH", "")),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(mdbResourceName, "project", "my-slug"),
					func(*terraform.State) error {
						for _, r := range mock.requests() {
							if r.Method == "POST" && strings.Contains(r.Body, `"project_id":"proj_mock_1"`) {
								return nil
							}
						}
						return fmt.Errorf("no POST carried project_id proj_mock_1: %+v", mock.requests())
					},
				),
			},
		},
	})
}

// A rejected allowlist fails the create and taints the database; once the
// value is accepted, the next apply replaces it.
func TestManagedDatabase_MockAllowlistPatchFails(t *testing.T) {
	mock, server := newMdbMock(t)
	mock.patchStatus = 422
	cfg := mdbConfig("", mdbBody("proj_mock_1", "db.psql.small", "ASH", mdbAllowlist1))

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMdbMockDestroyed(mock),
		Steps: []resource.TestStep{
			{
				Config:      cfg,
				ExpectError: regexp.MustCompile(`trusted_sources rejected by the API`),
			},
			{
				PreConfig: func() {
					mock.set(func(m *mdbMock) { m.patchStatus = 0 })
					mock.resetLog()
				},
				Config: cfg,
				Check: resource.ComposeTestCheckFunc(
					mdbCheckReplaced(mock),
					resource.TestCheckResourceAttr(mdbResourceName, "id", "mdb_mock_2"),
					resource.TestCheckResourceAttr(mdbResourceName, "trusted_sources.#", "1"),
				),
			},
		},
	})
}

// The API returns credentials only on the create response: create still
// succeeds, a later update doesn't wait for credentials, and the
// credentials from the create response stay in state.
func TestManagedDatabase_MockUpdateCredentialsAbsent(t *testing.T) {
	mock, server := newMdbMock(t)
	mock.postCreds = true
	mock.noShowCreds = true

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMdbMockDestroyed(mock),
		Steps: []resource.TestStep{
			{
				Config: mdbConfig("", mdbBody("proj_mock_1", "db.psql.small", "ASH", mdbAllowlist1)),
				Check:  resource.TestCheckResourceAttr(mdbResourceName, "credentials.host", "db.mock.internal"),
			},
			{
				Config: mdbConfig("", mdbBody("proj_mock_1", "db.psql.small", "ASH", mdbAllowlist2)),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(mdbResourceName, "trusted_sources.#", "2"),
					resource.TestCheckResourceAttr(mdbResourceName, "credentials.host", "db.mock.internal"),
					resource.TestCheckResourceAttr(mdbResourceName, "credentials.password", "mock-secret"),
				),
			},
		},
	})
}

// mdbAssertNoReplace runs in the next step's PreConfig: Check doesn't run on
// an ExpectError step, and the post-test destroy adds its own DELETE.
func mdbAssertNoReplace(t *testing.T, m *mdbMock) func() {
	return func() {
		if methods := m.methods(); strings.Contains(methods, "DELETE") || strings.Contains(methods, "POST") {
			t.Errorf("the failed step sent %q; want no DELETE or POST", methods)
		}
		m.resetLog()
	}
}

func TestManagedDatabase_MockDefaults(t *testing.T) {
	mock, server := newMdbMock(t)
	body := func(extra string) string { return mdbBody("", "db.psql.small", "ASH", extra) }

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMdbMockDestroyed(mock),
		Steps: []resource.TestStep{
			{
				Config: mdbConfig("proj_mock_1", body(mdbAllowlist1)),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(mdbResourceName, "project", "proj_mock_1"),
					resource.TestCheckResourceAttr(mdbResourceName, "billing", "monthly"),
				),
			},
			{
				// project and billing are unset: an in-place update must not
				// plan a replacement.
				PreConfig: mock.resetLog,
				Config:    mdbConfig("proj_mock_1", body(mdbAllowlist2)),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(mdbResourceName, "id", "mdb_mock_1"),
					mdbCheckLog(mock, func(methods string) error {
						if strings.Contains(methods, "POST") || strings.Contains(methods, "DELETE") || !strings.Contains(methods, "PATCH") {
							return fmt.Errorf("want a PATCH and no POST or DELETE, got %q", methods)
						}
						return nil
					}),
				),
			},
			{
				// The provider default moves: a plan-time error, not a
				// replacement of every database that inherits it.
				PreConfig:   mock.resetLog,
				Config:      mdbConfig("proj_mock_2", body(mdbAllowlist2)),
				ExpectError: regexp.MustCompile(`Default project changed`),
			},
			{
				PreConfig: mdbAssertNoReplace(t, mock),
				Config:    mdbConfig("proj_mock_1", body(mdbAllowlist2)),
				PlanOnly:  true,
			},
		},
	})
}

func TestManagedDatabase_MockBilling(t *testing.T) {
	mock, server := newMdbMock(t)
	cfg := func(plan, billing string) string {
		return mdbConfig("", mdbBody("proj_mock_1", plan, "ASH", fmt.Sprintf("  billing = %q\n", billing)))
	}

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMdbMockDestroyed(mock),
		Steps: []resource.TestStep{
			{
				Config: cfg("db.psql.small", "monthly"),
				Check:  resource.TestCheckResourceAttr(mdbResourceName, "billing", "monthly"),
			},
			{
				PreConfig:   mock.resetLog,
				Config:      cfg("db.psql.small", "yearly"),
				ExpectError: regexp.MustCompile(`Billing can't be changed`),
			},
			{
				PreConfig: mdbAssertNoReplace(t, mock),
				Config:    cfg("db.psql.small", "monthly"),
				PlanOnly:  true,
			},
			{
				// A replacement is a new database, so billing may change with it.
				PreConfig: mock.resetLog,
				Config:    cfg("db.psql.medium", "yearly"),
				Check: resource.ComposeTestCheckFunc(
					mdbCheckReplaced(mock),
					resource.TestCheckResourceAttr(mdbResourceName, "billing", "yearly"),
				),
			},
			{
				// The documented way to change billing: taint, then apply.
				PreConfig: mock.resetLog,
				Taint:     []string{mdbResourceName},
				Config:    cfg("db.psql.medium", "monthly"),
				Check: resource.ComposeTestCheckFunc(
					mdbCheckReplaced(mock),
					resource.TestCheckResourceAttr(mdbResourceName, "billing", "monthly"),
				),
			},
		},
	})
}

func TestManagedDatabase_MockBillingAdopt(t *testing.T) {
	mock, server := newMdbMock(t)
	mock.omitBilling = true
	mock.seed(mdbRecord{ID: "mdb_seed_1", Name: "api", ProjectID: "proj_mock_1", Region: "ASH", Plan: "db.psql.small", Engine: "postgres", Billing: "monthly"})
	without := mdbConfig("", mdbBody("proj_mock_1", "db.psql.small", "ASH", ""))
	with := mdbConfig("", mdbBody("proj_mock_1", "db.psql.small", "ASH", "  billing = \"monthly\"\n"))

	resource.Test(t, resource.TestCase{
		IsUnitTest:               true,
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactoriesWithMock(server),
		CheckDestroy:             testAccCheckMdbMockDestroyed(mock),
		Steps: []resource.TestStep{
			{
				Config:             without,
				ResourceName:       mdbResourceName,
				ImportState:        true,
				ImportStateId:      "mdb_seed_1",
				ImportStatePersist: true,
			},
			{
				PreConfig: mock.resetLog,
				Config:    with,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(mdbResourceName, "id", "mdb_seed_1"),
					resource.TestCheckResourceAttr(mdbResourceName, "billing", "monthly"),
					mdbCheckLog(mock, func(methods string) error {
						if strings.Contains(methods, "POST") || strings.Contains(methods, "DELETE") || strings.Contains(methods, "PATCH") {
							return fmt.Errorf("adoption sent %q; want reads only", methods)
						}
						return nil
					}),
				),
			},
		},
	})
}
