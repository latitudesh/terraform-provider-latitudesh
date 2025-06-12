# latitudesh-go-sdk
<!-- Start Summary [summary] -->
## Summary

Latitude.sh API: The Latitude.sh API is a RESTful API to manage your Latitude.sh account. It allows you to perform the same actions as the Latitude.sh dashboard.
<!-- End Summary [summary] -->

<!-- Start Table of Contents [toc] -->
## Table of Contents
<!-- $toc-max-depth=2 -->
* [latitudesh-go-sdk](#latitudesh-go-sdk)
  * [SDK Installation](#sdk-installation)
  * [SDK Example Usage](#sdk-example-usage)
  * [Authentication](#authentication)
  * [Available Resources and Operations](#available-resources-and-operations)
  * [Pagination](#pagination)
  * [Retries](#retries)
  * [Error Handling](#error-handling)
  * [Server Selection](#server-selection)
  * [Custom HTTP Client](#custom-http-client)

<!-- End Table of Contents [toc] -->

<!-- Start SDK Installation [installation] -->
## SDK Installation

To add the SDK as a dependency to your project:
```bash
go get github.com/latitudesh/latitudesh-go-sdk
```
<!-- End SDK Installation [installation] -->

<!-- Start SDK Example Usage [usage] -->
## SDK Example Usage

### Example

```go
package main

import (
	"context"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"log"
	"os"
)

func main() {
	ctx := context.Background()

	s := latitudeshgosdk.New(
		latitudeshgosdk.WithSecurity(os.Getenv("LATITUDESH_BEARER")),
	)

	res, err := s.APIKeys.GetAPIKeys(ctx)
	if err != nil {
		log.Fatal(err)
	}
	if res.APIKey != nil {
		// handle response
	}
}

```
<!-- End SDK Example Usage [usage] -->

<!-- Start Authentication [security] -->
## Authentication

### Per-Client Security Schemes

This SDK supports the following security scheme globally:

| Name     | Type   | Scheme  | Environment Variable |
| -------- | ------ | ------- | -------------------- |
| `Bearer` | apiKey | API key | `LATITUDESH_BEARER`  |

You can configure it using the `WithSecurity` option when initializing the SDK client instance. For example:
```go
package main

import (
	"context"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"log"
	"os"
)

func main() {
	ctx := context.Background()

	s := latitudeshgosdk.New(
		latitudeshgosdk.WithSecurity(os.Getenv("LATITUDESH_BEARER")),
	)

	res, err := s.APIKeys.GetAPIKeys(ctx)
	if err != nil {
		log.Fatal(err)
	}
	if res.APIKey != nil {
		// handle response
	}
}

```
<!-- End Authentication [security] -->

<!-- Start Available Resources and Operations [operations] -->
## Available Resources and Operations

<details open>
<summary>Available methods</summary>

### [APIKeys](docs/sdks/apikeys/README.md)

* [GetAPIKeys](docs/sdks/apikeys/README.md#getapikeys) - List API Keys
* [PostAPIKey](docs/sdks/apikeys/README.md#postapikey) - Create API Key
* [UpdateAPIKey](docs/sdks/apikeys/README.md#updateapikey) - Regenerate API Key
* [DeleteAPIKey](docs/sdks/apikeys/README.md#deleteapikey) - Delete API Key

### [Billing](docs/sdks/billing/README.md)

* [GetBillingUsage](docs/sdks/billing/README.md#getbillingusage) - List Billing Usage

### [Events](docs/sdks/events/README.md)

* [GetEvents](docs/sdks/events/README.md#getevents) - List all Events

### [Firewalls](docs/sdks/firewalls/README.md)

* [CreateFirewall](docs/sdks/firewalls/README.md#createfirewall) - Create a firewall
* [ListFirewalls](docs/sdks/firewalls/README.md#listfirewalls) - List firewalls
* [GetFirewall](docs/sdks/firewalls/README.md#getfirewall) - Retrieve Firewall
* [UpdateFirewall](docs/sdks/firewalls/README.md#updatefirewall) - Update Firewall
* [DeleteFirewall](docs/sdks/firewalls/README.md#deletefirewall) - Delete Firewall
* [GetFirewallAssignments](docs/sdks/firewalls/README.md#getfirewallassignments) - Firewall Assignments
* [DeleteFirewallAssignment](docs/sdks/firewalls/README.md#deletefirewallassignment) - Delete Firewall Assignment

#### [Firewalls.Assignments](docs/sdks/assignments/README.md)

* [Create](docs/sdks/assignments/README.md#create) - Firewall Assignment

### [IPAddresses](docs/sdks/ipaddresses/README.md)

* [GetIps](docs/sdks/ipaddresses/README.md#getips) - List IPs
* [GetIP](docs/sdks/ipaddresses/README.md#getip) - Retrieve an IP


### [OperatingSystems](docs/sdks/operatingsystems/README.md)

* [GetPlansOperatingSystem](docs/sdks/operatingsystems/README.md#getplansoperatingsystem) - List all operating systems available

### [Plans](docs/sdks/plans/README.md)

* [GetPlans](docs/sdks/plans/README.md#getplans) - List all Plans
* [GetPlan](docs/sdks/plans/README.md#getplan) - Retrieve a Plan
* [GetBandwidthPlans](docs/sdks/plans/README.md#getbandwidthplans) - List all bandwidth plans
* [UpdatePlansBandwidth](docs/sdks/plans/README.md#updateplansbandwidth) - Buy or remove bandwidth packages
* [GetStoragePlans](docs/sdks/plans/README.md#getstorageplans) - List all Storage Plans

#### [Plans.VM](docs/sdks/vm/README.md)

* [List](docs/sdks/vm/README.md#list) - List all Virtual Machines Plans

### [PrivateNetworks](docs/sdks/privatenetworks/README.md)

* [GetVirtualNetworks](docs/sdks/privatenetworks/README.md#getvirtualnetworks) - List all Virtual Networks
* [CreateVirtualNetwork](docs/sdks/privatenetworks/README.md#createvirtualnetwork) - Create a Virtual Network
* [UpdateVirtualNetwork](docs/sdks/privatenetworks/README.md#updatevirtualnetwork) - Update a Virtual Network
* [GetVirtualNetwork](docs/sdks/privatenetworks/README.md#getvirtualnetwork) - Retrieve a Virtual Network
* [GetVirtualNetworksAssignments](docs/sdks/privatenetworks/README.md#getvirtualnetworksassignments) - List all servers assigned to virtual networks
* [AssignServerVirtualNetwork](docs/sdks/privatenetworks/README.md#assignservervirtualnetwork) - Assign Virtual network
* [DeleteVirtualNetworksAssignments](docs/sdks/privatenetworks/README.md#deletevirtualnetworksassignments) - Delete Virtual Network Assignment

### [Projects](docs/sdks/projects/README.md)

* [GetProjects](docs/sdks/projects/README.md#getprojects) - List all Projects
* [CreateProject](docs/sdks/projects/README.md#createproject) - Create a Project
* [UpdateProject](docs/sdks/projects/README.md#updateproject) - Update a Project
* [DeleteProject](docs/sdks/projects/README.md#deleteproject) - Delete a Project

#### [~~Projects.SSHKeys~~](docs/sdks/latitudeshsshkeys/README.md)

* [~~PostProjectSSHKey~~](docs/sdks/latitudeshsshkeys/README.md#postprojectsshkey) - Create a Project SSH Key :warning: **Deprecated**

### [~~ProjectsUserData~~](docs/sdks/projectsuserdata/README.md)

* [~~DeleteProjectUserData~~](docs/sdks/projectsuserdata/README.md#deleteprojectuserdata) - Delete a Project User Data :warning: **Deprecated**

### [Regions](docs/sdks/regions/README.md)

* [GetRegions](docs/sdks/regions/README.md#getregions) - List all Regions
* [GetRegion](docs/sdks/regions/README.md#getregion) - Retrieve a Region

### [Roles](docs/sdks/roles/README.md)

* [GetRoles](docs/sdks/roles/README.md#getroles) - List all Roles
* [GetRoleID](docs/sdks/roles/README.md#getroleid) - Retrieve Role

### [Servers](docs/sdks/servers/README.md)

* [GetServers](docs/sdks/servers/README.md#getservers) - List all Servers
* [CreateServer](docs/sdks/servers/README.md#createserver) - Deploy Server
* [GetServer](docs/sdks/servers/README.md#getserver) - Retrieve a Server
* [UpdateServer](docs/sdks/servers/README.md#updateserver) - Update Server
* [DestroyServer](docs/sdks/servers/README.md#destroyserver) - Remove Server
* [GetServerDeployConfig](docs/sdks/servers/README.md#getserverdeployconfig) - Retrieve Deploy Config
* [UpdateServerDeployConfig](docs/sdks/servers/README.md#updateserverdeployconfig) - Update Deploy Config
* [ServerLock](docs/sdks/servers/README.md#serverlock) - Lock the server
* [ServerUnlock](docs/sdks/servers/README.md#serverunlock) - Unlock the server
* [CreateServerOutOfBand](docs/sdks/servers/README.md#createserveroutofband) - Start Out of Band Connection
* [GetServerOutOfBand](docs/sdks/servers/README.md#getserveroutofband) - List Out of Band Connections
* [RunAction](docs/sdks/servers/README.md#runaction) - Run Server Action
* [CreateIpmiSession](docs/sdks/servers/README.md#createipmisession) - Generate IPMI credentials
* [ServerStartRescueMode](docs/sdks/servers/README.md#serverstartrescuemode) - Puts a Server in rescue mode
* [ServerExitRescueMode](docs/sdks/servers/README.md#serverexitrescuemode) - Exits rescue mode for a Server
* [ServerScheduleDeletion](docs/sdks/servers/README.md#serverscheduledeletion) - Schedule the server deletion
* [ServerUnscheduleDeletion](docs/sdks/servers/README.md#serverunscheduledeletion) - Unschedule the server deletion
* [CreateServerReinstall](docs/sdks/servers/README.md#createserverreinstall) - Run Server Reinstall

### [SSHKeys](docs/sdks/sshkeys/README.md)

* [~~List~~](docs/sdks/sshkeys/README.md#list) - List all Project SSH Keys :warning: **Deprecated**
* [~~GetProjectSSHKey~~](docs/sdks/sshkeys/README.md#getprojectsshkey) - Retrieve a Project SSH Key :warning: **Deprecated**
* [~~PutProjectSSHKey~~](docs/sdks/sshkeys/README.md#putprojectsshkey) - Update a Project SSH Key :warning: **Deprecated**
* [~~DeleteProjectSSHKey~~](docs/sdks/sshkeys/README.md#deleteprojectsshkey) - Delete a Project SSH Key :warning: **Deprecated**
* [GetSSHKeys](docs/sdks/sshkeys/README.md#getsshkeys) - List all SSH Keys
* [PostSSHKey](docs/sdks/sshkeys/README.md#postsshkey) - Create a SSH Key
* [GetSSHKey](docs/sdks/sshkeys/README.md#getsshkey) - Retrieve a SSH Key
* [PutSSHKey](docs/sdks/sshkeys/README.md#putsshkey) - Update a SSH Key
* [DeleteSSHKey](docs/sdks/sshkeys/README.md#deletesshkey) - Delete a SSH Key

### [Storage](docs/sdks/storage/README.md)

* [PostStorageFilesystems](docs/sdks/storage/README.md#poststoragefilesystems) - Create a filesystem for a project
* [GetStorageFilesystems](docs/sdks/storage/README.md#getstoragefilesystems) - List filesystems
* [DeleteStorageFilesystems](docs/sdks/storage/README.md#deletestoragefilesystems) - Delete a filesystem for a project
* [PatchStorageFilesystems](docs/sdks/storage/README.md#patchstoragefilesystems) - Update a filesystem for a project

### [Tags](docs/sdks/tags/README.md)

* [GetTags](docs/sdks/tags/README.md#gettags) - List all Tags
* [CreateTag](docs/sdks/tags/README.md#createtag) - Create a Tag
* [UpdateTag](docs/sdks/tags/README.md#updatetag) - Update Tag
* [DestroyTag](docs/sdks/tags/README.md#destroytag) - Delete Tag

### [TeamMembers](docs/sdks/teammembers/README.md)

* [PostTeamMembers](docs/sdks/teammembers/README.md#postteammembers) - Add a Team Member

### [Teams](docs/sdks/teams/README.md)

* [GetTeam](docs/sdks/teams/README.md#getteam) - Retrieve the team
* [PostTeam](docs/sdks/teams/README.md#postteam) - Create a team
* [PatchCurrentTeam](docs/sdks/teams/README.md#patchcurrentteam) - Update a team

#### [Teams.Members](docs/sdks/members/README.md)

* [GetTeamMembers](docs/sdks/members/README.md#getteammembers) - List all Team Members

### [TeamsMembers](docs/sdks/teamsmembers/README.md)

* [DestroyTeamMember](docs/sdks/teamsmembers/README.md#destroyteammember) - Remove a Team Member

### [Traffic](docs/sdks/traffic/README.md)

* [GetTrafficConsumption](docs/sdks/traffic/README.md#gettrafficconsumption) - Retrieve Traffic consumption
* [GetTrafficQuota](docs/sdks/traffic/README.md#gettrafficquota) - Retrieve Traffic Quota

### [UserData](docs/sdks/userdata/README.md)

* [~~List~~](docs/sdks/userdata/README.md#list) - List all Project User Data :warning: **Deprecated**
* [~~PostProjectUserData~~](docs/sdks/userdata/README.md#postprojectuserdata) - Create a Project User Data :warning: **Deprecated**
* [~~Get~~](docs/sdks/userdata/README.md#get) - Retrieve a Project User Data :warning: **Deprecated**
* [~~PutProjectUserData~~](docs/sdks/userdata/README.md#putprojectuserdata) - Update a Project User Data :warning: **Deprecated**
* [GetUsersData](docs/sdks/userdata/README.md#getusersdata) - List all User Data
* [PostUserData](docs/sdks/userdata/README.md#postuserdata) - Create an User Data
* [GetUserData](docs/sdks/userdata/README.md#getuserdata) - Retrieve an User Data
* [PatchUserData](docs/sdks/userdata/README.md#patchuserdata) - Update an User Data
* [DeleteUserData](docs/sdks/userdata/README.md#deleteuserdata) - Delete an User Data

### [UserProfile](docs/sdks/userprofile/README.md)

* [GetUserProfile](docs/sdks/userprofile/README.md#getuserprofile) - Get user profile
* [PatchUserProfile](docs/sdks/userprofile/README.md#patchuserprofile) - Update User Profile
* [GetUserTeams](docs/sdks/userprofile/README.md#getuserteams) - List User Teams

### [VirtualMachines](docs/sdks/virtualmachines/README.md)

* [CreateVirtualMachine](docs/sdks/virtualmachines/README.md#createvirtualmachine) - Create a Virtual Machine
* [IndexVirtualMachine](docs/sdks/virtualmachines/README.md#indexvirtualmachine) - Get Teams Virtual Machines
* [ShowVirtualMachine](docs/sdks/virtualmachines/README.md#showvirtualmachine) - Get a Virtual Machine
* [DestroyVirtualMachine](docs/sdks/virtualmachines/README.md#destroyvirtualmachine) - Destroy a Virtual Machine

### [VirtualNetworks](docs/sdks/virtualnetworks/README.md)

* [Delete](docs/sdks/virtualnetworks/README.md#delete) - Delete a Virtual Network

### [VPNSessions](docs/sdks/vpnsessions/README.md)

* [GetVpnSessions](docs/sdks/vpnsessions/README.md#getvpnsessions) - List all Active VPN Sessions
* [PostVpnSession](docs/sdks/vpnsessions/README.md#postvpnsession) - Create a VPN Session
* [PutVpnSession](docs/sdks/vpnsessions/README.md#putvpnsession) - Refresh a VPN Session
* [DeleteVpnSession](docs/sdks/vpnsessions/README.md#deletevpnsession) - Delete a VPN Session

</details>
<!-- End Available Resources and Operations [operations] -->

<!-- Start Pagination [pagination] -->
## Pagination

Some of the endpoints in this SDK support pagination. To use pagination, you make your SDK calls as usual, but the
returned response object will have a `Next` method that can be called to pull down the next group of results. If the
return value of `Next` is `nil`, then there are no more pages to be fetched.

Here's an example of one such pagination call:
```go
package main

import (
	"context"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/operations"
	"log"
	"os"
)

func main() {
	ctx := context.Background()

	s := latitudeshgosdk.New(
		latitudeshgosdk.WithSecurity(os.Getenv("LATITUDESH_BEARER")),
	)

	res, err := s.Events.GetEvents(ctx, operations.GetEventsRequest{})
	if err != nil {
		log.Fatal(err)
	}
	if res.Object != nil {
		for {
			// handle items

			res, err = res.Next()

			if err != nil {
				// handle error
			}

			if res == nil {
				break
			}
		}
	}
}

```
<!-- End Pagination [pagination] -->

<!-- Start Retries [retries] -->
## Retries

Some of the endpoints in this SDK support retries. If you use the SDK without any configuration, it will fall back to the default retry strategy provided by the API. However, the default retry strategy can be overridden on a per-operation basis, or across the entire SDK.

To change the default retry strategy for a single API call, simply provide a `retry.Config` object to the call by using the `WithRetries` option:
```go
package main

import (
	"context"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/retry"
	"log"
	"models/operations"
	"os"
)

func main() {
	ctx := context.Background()

	s := latitudeshgosdk.New(
		latitudeshgosdk.WithSecurity(os.Getenv("LATITUDESH_BEARER")),
	)

	res, err := s.APIKeys.GetAPIKeys(ctx, operations.WithRetries(
		retry.Config{
			Strategy: "backoff",
			Backoff: &retry.BackoffStrategy{
				InitialInterval: 1,
				MaxInterval:     50,
				Exponent:        1.1,
				MaxElapsedTime:  100,
			},
			RetryConnectionErrors: false,
		}))
	if err != nil {
		log.Fatal(err)
	}
	if res.APIKey != nil {
		// handle response
	}
}

```

If you'd like to override the default retry strategy for all operations that support retries, you can use the `WithRetryConfig` option at SDK initialization:
```go
package main

import (
	"context"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/retry"
	"log"
	"os"
)

func main() {
	ctx := context.Background()

	s := latitudeshgosdk.New(
		latitudeshgosdk.WithRetryConfig(
			retry.Config{
				Strategy: "backoff",
				Backoff: &retry.BackoffStrategy{
					InitialInterval: 1,
					MaxInterval:     50,
					Exponent:        1.1,
					MaxElapsedTime:  100,
				},
				RetryConnectionErrors: false,
			}),
		latitudeshgosdk.WithSecurity(os.Getenv("LATITUDESH_BEARER")),
	)

	res, err := s.APIKeys.GetAPIKeys(ctx)
	if err != nil {
		log.Fatal(err)
	}
	if res.APIKey != nil {
		// handle response
	}
}

```
<!-- End Retries [retries] -->

<!-- Start Error Handling [errors] -->
## Error Handling

Handling errors in this SDK should largely match your expectations. All operations return a response object or an error, they will never return both.

By Default, an API error will return `components.APIError`. When custom error responses are specified for an operation, the SDK may also return their associated error. You can refer to respective *Errors* tables in SDK docs for more details on possible error types for each operation.

For example, the `PostAPIKey` function may return the following errors:

| Error Type             | Status Code | Content Type             |
| ---------------------- | ----------- | ------------------------ |
| components.ErrorObject | 400, 422    | application/vnd.api+json |
| components.APIError    | 4XX, 5XX    | \*/\*                    |

### Example

```go
package main

import (
	"context"
	"errors"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
	"log"
	"os"
)

func main() {
	ctx := context.Background()

	s := latitudeshgosdk.New(
		latitudeshgosdk.WithSecurity(os.Getenv("LATITUDESH_BEARER")),
	)

	res, err := s.APIKeys.PostAPIKey(ctx, components.CreateAPIKey{
		Data: &components.Data{
			Type: components.CreateAPIKeyTypeAPIKeys,
			Attributes: &components.CreateAPIKeyAttributes{
				Name: latitudeshgosdk.String("App Token"),
			},
		},
	})
	if err != nil {

		var e *components.ErrorObject
		if errors.As(err, &e) {
			// handle error
			log.Fatal(e.Error())
		}

		var e *components.APIError
		if errors.As(err, &e) {
			// handle error
			log.Fatal(e.Error())
		}
	}
}

```
<!-- End Error Handling [errors] -->

<!-- Start Server Selection [server] -->
## Server Selection

### Select Server by Index

You can override the default server globally using the `WithServerIndex(serverIndex int)` option when initializing the SDK client instance. The selected server will then be used as the default on the operations that use it. This table lists the indexes associated with the available servers:

| #   | Server                    | Variables          | Description |
| --- | ------------------------- | ------------------ | ----------- |
| 0   | `https://api.latitude.sh` | `latitude_api_key` |             |
| 1   | `http://api.latitude.sh`  | `latitude_api_key` |             |

If the selected server has variables, you may override its default values using the associated option(s):

| Variable           | Option                                      | Default                        | Description |
| ------------------ | ------------------------------------------- | ------------------------------ | ----------- |
| `latitude_api_key` | `WithLatitudeAPIKey(latitudeAPIKey string)` | `"<insert your api key here>"` |             |

#### Example

```go
package main

import (
	"context"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"log"
	"os"
)

func main() {
	ctx := context.Background()

	s := latitudeshgosdk.New(
		latitudeshgosdk.WithServerIndex(1),
		latitudeshgosdk.WithLatitudeAPIKey("<value>"),
		latitudeshgosdk.WithSecurity(os.Getenv("LATITUDESH_BEARER")),
	)

	res, err := s.APIKeys.GetAPIKeys(ctx)
	if err != nil {
		log.Fatal(err)
	}
	if res.APIKey != nil {
		// handle response
	}
}

```

### Override Server URL Per-Client

The default server can also be overridden globally using the `WithServerURL(serverURL string)` option when initializing the SDK client instance. For example:
```go
package main

import (
	"context"
	latitudeshgosdk "github.com/latitudesh/latitudesh-go-sdk"
	"log"
	"os"
)

func main() {
	ctx := context.Background()

	s := latitudeshgosdk.New(
		latitudeshgosdk.WithServerURL("http://api.latitude.sh"),
		latitudeshgosdk.WithSecurity(os.Getenv("LATITUDESH_BEARER")),
	)

	res, err := s.APIKeys.GetAPIKeys(ctx)
	if err != nil {
		log.Fatal(err)
	}
	if res.APIKey != nil {
		// handle response
	}
}

```
<!-- End Server Selection [server] -->

<!-- Start Custom HTTP Client [http-client] -->
## Custom HTTP Client

The Go SDK makes API calls that wrap an internal HTTP client. The requirements for the HTTP client are very simple. It must match this interface:

```go
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}
```

The built-in `net/http` client satisfies this interface and a default client based on the built-in is provided by default. To replace this default with a client of your own, you can implement this interface yourself or provide your own client configured as desired. Here's a simple example, which adds a client with a 30 second timeout.

```go
import (
	"net/http"
	"time"
	"github.com/myorg/your-go-sdk"
)

var (
	httpClient = &http.Client{Timeout: 30 * time.Second}
	sdkClient  = sdk.New(sdk.WithClient(httpClient))
)
```

This can be a convenient way to configure timeouts, cookies, proxies, custom headers, and other low-level configuration.
<!-- End Custom HTTP Client [http-client] -->

<!-- Placeholder for Future Speakeasy SDK Sections -->
