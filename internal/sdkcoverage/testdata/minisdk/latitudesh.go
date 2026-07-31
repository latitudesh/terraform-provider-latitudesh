// A frozen miniature of the latitudesh-go-sdk root package, used to pin the
// parser and the prefix classifier against the SDK's real naming quirks without
// depending on any particular version being present in the module cache.
//
// This tree lives under testdata/ so the go tool ignores it; it only ever has to
// be parseable, not buildable.
package latitudeshgosdk

type Latitudesh struct {
	SDKVersion string

	VirtualMachines *VirtualMachines
	SSHKeys         *SSHKeys
	BlockStorage    *BlockStorage
	ElasticIps      *ElasticIps
	Tags            *Tags
	VirtualNetworks *VirtualNetworks
	Firewalls       *Firewalls
	Projects        *Projects
	Servers         *Servers
	Events          *Events
	Oddities        *Oddities

	sdkConfiguration config.SDKConfiguration
	hooks            *hooks.Hooks
}

// VirtualMachines mixes bare CRUD with a suffixed Update.
type VirtualMachines struct {
	rootSDK *Latitudesh
}

func (s *VirtualMachines) Create() {}
func (s *VirtualMachines) List()   {}
func (s *VirtualMachines) Get()    {}
func (s *VirtualMachines) Delete() {}

func (s *VirtualMachines) UpdateVirtualMachine()       {}
func (s *VirtualMachines) ShowVirtualMachineMetrics()  {}
func (s *VirtualMachines) CreateVirtualMachineAction() {}
func (s *VirtualMachines) DestroyNetworkAttachment()   {}
func (s *VirtualMachines) unexportedShouldBeSkipped()  {}

// SSHKeys reads a single key via Retrieve rather than Get.
type SSHKeys struct {
	rootSDK *Latitudesh
}

func (s *SSHKeys) List()              {}
func (s *SSHKeys) Get()               {}
func (s *SSHKeys) ListAll()           {}
func (s *SSHKeys) Create()            {}
func (s *SSHKeys) Retrieve()          {}
func (s *SSHKeys) Update()            {}
func (s *SSHKeys) Delete()            {}
func (s *SSHKeys) ModifyProjectKey()  {}
func (s *SSHKeys) RemoveFromProject() {}

// BlockStorage names methods after HTTP verbs and has no update.
type BlockStorage struct {
	rootSDK *Latitudesh
}

func (s *BlockStorage) GetStorageVolumes()       {}
func (s *BlockStorage) PostStorageVolumes()      {}
func (s *BlockStorage) GetStorageVolume()        {}
func (s *BlockStorage) DeleteStorageVolumes()    {}
func (s *BlockStorage) PostStorageVolumesMount() {}

// ElasticIps mixes "Ips" and "IP" casing within one group.
type ElasticIps struct {
	rootSDK *Latitudesh
}

func (s *ElasticIps) ListElasticIps()  {}
func (s *ElasticIps) CreateElasticIP() {}
func (s *ElasticIps) GetElasticIP()    {}
func (s *ElasticIps) UpdateElasticIP() {}
func (s *ElasticIps) DeleteElasticIP() {}

// Tags is readable only through List — it has no Get at all.
type Tags struct {
	rootSDK *Latitudesh
}

func (s *Tags) List()   {}
func (s *Tags) Create() {}
func (s *Tags) Update() {}
func (s *Tags) Delete() {}

// VirtualNetworks is a delete-only fragment, not a resource owner.
type VirtualNetworks struct {
	rootSDK *Latitudesh
}

func (s *VirtualNetworks) Delete() {}

// Events is read-only, which maps to a data source rather than a resource.
type Events struct {
	rootSDK *Latitudesh
}

func (s *Events) List() {}

// Oddities carries a method name no prefix rule recognizes.
type Oddities struct {
	rootSDK *Latitudesh
}

func (s *Oddities) FrobnicateWidget() {}
func (s *Oddities) Get()              {}
