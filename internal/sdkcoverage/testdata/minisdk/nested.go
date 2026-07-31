// Second file in the frozen mini SDK, exercising multi-file indexing and the two
// nested-subgroup shapes the real SDK uses.
package latitudeshgosdk

// Firewalls nests a subgroup and also flattens some assignment methods onto
// itself, exactly as the real SDK does.
type Firewalls struct {
	Assignments *Assignments

	rootSDK *Latitudesh
}

func (s *Firewalls) List()                      {}
func (s *Firewalls) Create()                    {}
func (s *Firewalls) Get()                       {}
func (s *Firewalls) Update()                    {}
func (s *Firewalls) Delete()                    {}
func (s *Firewalls) ListAssignments()           {}
func (s *Firewalls) DeleteAssignment()          {}
func (s *Firewalls) GetAllFirewallAssignments() {}

type Assignments struct {
	rootSDK *Latitudesh
}

func (s *Assignments) Create() {}

// Projects nests a subgroup whose field name differs from its type name, so
// methods must be resolved by type. This is the case that breaks any
// field-name-based lookup.
type Projects struct {
	SSHKeys *LatitudeshProjectsSSHKeys

	rootSDK *Latitudesh
}

func (s *Projects) List()   {}
func (s *Projects) Create() {}
func (s *Projects) Update() {}
func (s *Projects) Delete() {}

type LatitudeshProjectsSSHKeys struct {
	rootSDK *Latitudesh
}

func (s *LatitudeshProjectsSSHKeys) PostProjectSSHKey() {}

// Servers has clean CRUD plus a pile of action methods.
type Servers struct {
	rootSDK *Latitudesh
}

func (s *Servers) List()               {}
func (s *Servers) Create()             {}
func (s *Servers) Get()                {}
func (s *Servers) Update()             {}
func (s *Servers) Delete()             {}
func (s *Servers) Lock()               {}
func (s *Servers) Unlock()             {}
func (s *Servers) RunAction()          {}
func (s *Servers) StartRescueMode()    {}
func (s *Servers) ExitRescueMode()     {}
func (s *Servers) ScheduleDeletion()   {}
func (s *Servers) UnscheduleDeletion() {}
func (s *Servers) Reinstall()          {}
