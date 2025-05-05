package latitudesh

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	api "github.com/latitudesh/latitudesh-go"
)

var ErrServerDeleted = errors.New("server was removed during provisioning due to failure")

var triggerReinstall = []string{
	"operating_system",
	"ssh_keys",
	"user_data",
	"raid",
	"ipxe_url",
}

func resourceServer() *schema.Resource {
	return &schema.Resource{
		CreateContext: resourceServerCreate,
		ReadContext:   resourceServerRead,
		UpdateContext: resourceServerUpdate,
		DeleteContext: resourceServerDelete,
		Schema: map[string]*schema.Schema{
			"project": {
				Type:        schema.TypeString,
				Description: "The id or slug of the project",
				Required:    true,
				ForceNew:    true,
			},
			"site": {
				Type:        schema.TypeString,
				Description: "The server site",
				Required:    true,
				ForceNew:    true,
			},
			"plan": {
				Type:        schema.TypeString,
				Description: "The server plan",
				Required:    true,
				ForceNew:    true,
			},
			"operating_system": {
				Type: schema.TypeString,
				Description: `The server OS. 
				Updating operating_system will trigger a reinstall if allow_reinstall is set to true.`,
				Required: true,
			},
			"hostname": {
				Type:        schema.TypeString,
				Description: "The server hostname",
				Required:    true,
			},
			"ssh_keys": {
				Type: schema.TypeList,
				Elem: &schema.Schema{
					Type: schema.TypeString,
				},
				Description: `List of server SSH key ids. 
				Updating ssh_keys will trigger a reinstall if allow_reinstall is set to true.`,
				Optional: true,
			},
			"user_data": {
				Type: schema.TypeString,
				Description: `The id of user data to set on the server. 
				Updating user_data will trigger a reinstall if allow_reinstall is set to true.`,
				Optional: true,
			},
			"raid": {
				Type: schema.TypeString,
				Description: `RAID mode for the server. 
				Updating raid will trigger a reinstall if allow_reinstall is set to true.`,
				Optional: true,
			},
			"ipxe_url": {
				Type: schema.TypeString,
				Description: `Url for the iPXE script that will be used.	
				Updating ipxe_url will trigger a reinstall if allow_reinstall is set to true.`,
				Optional: true,
			},
			"billing": {
				Type: schema.TypeString,
				Description: `The server billing type. 
				Accepts hourly and monthly for on demand projects and yearly for reserved projects.`,
				Optional: true,
			},
			"primary_ipv4": {
				Type:        schema.TypeString,
				Description: "The server IP address",
				Computed:    true,
			},
			"tags": {
				Type: schema.TypeList,
				Elem: &schema.Schema{
					Type: schema.TypeString,
				},
				Description: "List of server tags",
				Optional:    true,
			},
			"locked": {
				Type:        schema.TypeBool,
				Description: "Lock/unlock the server. A locked server cannot be deleted or updated.",
				Optional:    true,
				Default:     false,
			},
			"created": {
				Type:        schema.TypeString,
				Description: "The timestamp for when the server was created",
				Computed:    true,
			},
			"updated": {
				Type:        schema.TypeString,
				Description: "The timestamp for the last time the server was updated",
				Computed:    true,
			},
			"allow_reinstall": {
				Type: schema.TypeBool,
				Description: `Allow server reinstallation when operating_system, ssh_keys, user_data, raid, or ipxe_url changes.
				WARNING: The reinstall will be triggered even if Terraform reports an in-place update.`,
				Optional: true,
				Default:  false,
				Deprecated: "This attribute is deprecated and will be removed in a future release. You should use the locked attribute. " +
					"Learn more: https://www.latitude.sh/changelog/block-destructive-actions-with-server-locking",
			},
		},
		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},
	}
}

func resourceServerCreate(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	var diags diag.Diagnostics
	c := m.(*api.Client)

	ssh_keys := parseSSHKeys(d)
	createRequest := &api.ServerCreateRequest{
		Data: api.ServerCreateData{
			Type: "servers",
			Attributes: api.ServerCreateAttributes{
				Project:         d.Get("project").(string),
				Site:            d.Get("site").(string),
				Plan:            d.Get("plan").(string),
				OperatingSystem: d.Get("operating_system").(string),
				Hostname:        d.Get("hostname").(string),
				SSHKeys:         ssh_keys,
				UserData:        d.Get("user_data").(string),
				Raid:            d.Get("raid").(string),
				IpxeUrl:         d.Get("ipxe_url").(string),
				Billing:         d.Get("billing").(string),
			},
		},
	}

	server, _, err := c.Servers.Create(createRequest)
	if err != nil {
		return diag.FromErr(err)
	}

	// Store original server ID for error reporting in case server is deleted
	originalServerID := server.ID

	// Set the resource ID in Terraform state
	d.SetId(server.ID)

	log.Printf("[INFO] Server %s created, waiting for provisioning to complete", server.ID)

	err = waitForServerProvisioning(ctx, d, c)

	if err != nil {
		return diag.Diagnostics{
			diag.Diagnostic{
				Severity: diag.Error,
				Summary:  "Server provisioning failed",
				Detail:   fmt.Sprintf("Server '%s' was deleted during provisioning. This likely indicates a provisioning failure on the Latitude.sh platform. The server is no longer in your account and will be recreated on the next apply.", originalServerID),
			},
		}
	}

	log.Printf("[INFO] Server %s provisioning completed successfully", server.ID)

	updatedServer, resp, err := c.Servers.Get(server.ID, nil)
	if err != nil || resp == nil || resp.StatusCode != 200 || updatedServer == nil {
		log.Printf("[ERROR] Server %s verification check failed after provisioning", server.ID)
		d.SetId("")
		return diag.Diagnostics{
			diag.Diagnostic{
				Severity: diag.Error,
				Summary:  "Server verification failed",
				Detail:   fmt.Sprintf("Server '%s' could not be verified after provisioning completed. Please check your Latitude.sh dashboard.", originalServerID),
			},
		}
	}

	// Use the updated server for setting attributes
	server = updatedServer

	if err := d.Set("hostname", &server.Hostname); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("primary_ipv4", &server.PrimaryIPv4); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("plan", &server.Plan.Slug); err != nil {
		return diag.FromErr(err)
	}

	if _, defined := d.GetOk("tags"); defined {
		ctx := context.WithValue(ctx, "justCreated", true)
		resourceServerUpdate(ctx, d, m)
	}

	// server.Project.ID is an interface{} type so we should verify before using
	var serverProjectId string
	switch server.Project.ID.(type) {
	case string:
		serverProjectId = server.Project.ID.(string)
	case float64:
		serverProjectId = strconv.FormatFloat(server.Project.ID.(float64), 'b', 2, 64)
	}

	if err := d.Set("project", serverProjectId); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("operating_system", &server.OperatingSystem.Slug); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("site", &server.Region.Site.Slug); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("created", &server.CreatedAt); err != nil {
		return diag.FromErr(err)
	}

	isLocked := d.Get("locked").(bool)
	if isLocked {
		server, _, err := c.Servers.Lock(server.ID)
		if err != nil {
			return diag.FromErr(err)
		}
		d.Set("locked", &server.Locked)
	}

	return diags
}

func resourceServerRead(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*api.Client)
	var diags diag.Diagnostics

	serverID := d.Id()
	log.Printf("[DEBUG] Reading server with ID: %s", serverID)

	server, resp, err := c.Servers.Get(serverID, nil)
	if err != nil {
		// Check if the server was deleted
		if resp != nil && (resp.StatusCode == 404 || resp.StatusCode == 410) {
			log.Printf("[WARN] Server %s not found (status code: %d), removing from state", serverID, resp.StatusCode)
			d.SetId("")
			return diag.Diagnostics{}
		}

		return diag.Errorf("error retrieving server: %s", err)
	}

	if server.Status != "on" && server.Status != "inventory" {
		log.Printf("[WARN] Server %s has unusual status: %s", serverID, server.Status)
		diags = append(diags, diag.Diagnostic{
			Severity: diag.Warning,
			Summary:  fmt.Sprintf("Server has unusual status: %s", server.Status),
			Detail:   fmt.Sprintf("The server exists but its status indicates it may not be fully operational. Please check your Latitude.sh dashboard for server ID: %s.", serverID),
		})
	}

	var serverProjectId string
	switch server.Project.ID.(type) {
	case string:
		serverProjectId = server.Project.ID.(string)
	case float64:
		serverProjectId = strconv.FormatFloat(server.Project.ID.(float64), 'b', 2, 64)
	}

	if err := d.Set("project", serverProjectId); err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("hostname", &server.Hostname); err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("primary_ipv4", &server.PrimaryIPv4); err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("operating_system", &server.OperatingSystem.Slug); err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("site", &server.Region.Site.Slug); err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("plan", &server.Plan.Slug); err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("created", &server.CreatedAt); err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("locked", &server.Locked); err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("tags", tagIDs(server.Tags)); err != nil {
		return diag.FromErr(err)
	}

	return diags
}

func resourceServerUpdate(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*api.Client)
	var diags diag.Diagnostics

	serverID := d.Id()
	tags := parseTags(d)

	// Unlocking server takes place before the update.
	if d.HasChange("locked") && !d.Get("locked").(bool) {
		server, _, err := c.Servers.Unlock(serverID)
		if err != nil {
			return diag.FromErr(err)
		}
		d.Set("locked", &server.Locked)
	}

	updateRequest := &api.ServerUpdateRequest{
		Data: api.ServerUpdateData{
			Type: "servers",
			ID:   serverID,
			Attributes: api.ServerUpdateAttributes{
				Hostname: d.Get("hostname").(string),
				Billing:  d.Get("billing").(string),
				Tags:     tags,
			},
		},
	}

	server, _, err := c.Servers.Update(serverID, updateRequest)
	if err != nil {
		return diag.FromErr(err)
	}

	justCreated, _ := ctx.Value("justCreated").(bool)
	if d.HasChanges(triggerReinstall...) && !justCreated {
		if d.Get("allow_reinstall").(bool) {
			diags = append(diags, serverReinstall(c, serverID, ctx, d)...)
		}
	}

	err = d.Set("updated", time.Now().Format(time.RFC850))
	if err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("hostname", &server.Hostname); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("tags", tagIDs(server.Tags)); err != nil {
		return diag.FromErr(err)
	}

	// Locking the server takes place after the update.
	if d.HasChange("locked") && d.Get("locked").(bool) {
		server, _, err := c.Servers.Lock(serverID)
		if err != nil {
			return diag.FromErr(err)
		}
		d.Set("locked", &server.Locked)
	}

	return diags
}

func resourceServerDelete(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	c := m.(*api.Client)

	var diags diag.Diagnostics

	serverID := d.Id()

	_, err := c.Servers.Delete(serverID)
	if err != nil {
		return diag.FromErr(err)
	}

	d.SetId("")

	return diags
}

func serverReinstall(c *api.Client, serverID string, ctx context.Context, d *schema.ResourceData) diag.Diagnostics {
	ssh_keys := parseSSHKeys(d)
	var diags diag.Diagnostics

	_, err := c.Servers.Reinstall(serverID, &api.ServerReinstallRequest{
		Data: api.ServerReinstallData{
			Type: "reinstalls",
			Attributes: api.ServerReinstallAttributes{
				OperatingSystem: d.Get("operating_system").(string),
				Hostname:        d.Get("hostname").(string),
				SSHKeys:         ssh_keys,
				UserData:        d.Get("user_data").(string),
				Raid:            d.Get("raid").(string),
				IpxeUrl:         d.Get("ipxe_url").(string),
			},
		},
	})

	if err != nil {
		return diag.FromErr(err)
	}

	diags = append(diags, diag.Diagnostic{
		Severity: diag.Warning,
		Summary:  "Your server is being reinstalled",
		Detail: "[WARN] The changes made to the server resource will trigger a reinstallation. All disks will be erased." +
			"Please note that this process may take some time to complete.",
	})

	return diags
}

func parseSSHKeys(d *schema.ResourceData) []string {
	ssh_keys := d.Get("ssh_keys").([]interface{})
	ssh_keys_slice := make([]string, len(ssh_keys))
	for i, ssh_key := range ssh_keys {
		ssh_keys_slice[i] = ssh_key.(string)
	}
	return ssh_keys_slice
}

func waitForServerProvisioning(ctx context.Context, d *schema.ResourceData, client *api.Client) error {
	serverID := d.Id()
	timeoutMinutes := 30
	iterationSeconds := 30
	maxIterations := (timeoutMinutes * 60) / iterationSeconds
	requiredConsecutiveSuccesses := 3
	consecutiveSuccessCount := 0

	expectedProject := d.Get("project").(string)
	expectedOS := d.Get("operating_system").(string)

	log.Printf("[INFO] Waiting for server %s to be provisioned, checking every %d seconds with timeout of %d minutes",
		serverID, iterationSeconds, timeoutMinutes)

	for i := 0; i < maxIterations; i++ {
		server, resp, err := client.Servers.Get(serverID, nil)

		if err != nil {
			d.SetId("")
			return fmt.Errorf("server %s was deleted during provisioning (HTTP %d response)",
				serverID, resp.StatusCode)
		}

		log.Printf("[INFO] Server %s status: %s (attempt %d/%d)",
			serverID, server.Status, i+1, maxIterations)

		serverProjectID := server.Project.ID.(string)

		if server.Status == "on" &&
			serverProjectID == expectedProject &&
			server.OperatingSystem.Slug == expectedOS {

			consecutiveSuccessCount++
			log.Printf("[INFO] Server %s conditions met (%d/%d): status=on, project=%s, os=%s",
				serverID, consecutiveSuccessCount, requiredConsecutiveSuccesses,
				serverProjectID, server.OperatingSystem.Slug)

			// Return success only after meeting conditions for required number of consecutive iterations
			if consecutiveSuccessCount >= requiredConsecutiveSuccesses {
				log.Printf("[INFO] Server %s is now confirmed stable after %d consecutive successful checks",
					serverID, requiredConsecutiveSuccesses)
				return nil
			}
		} else {
			if consecutiveSuccessCount > 0 {
				d.SetId("")
				return fmt.Errorf("server %s was deleted during provisioning (HTTP %d response)",
					serverID, resp.StatusCode)
			}
		}

		time.Sleep(time.Duration(iterationSeconds) * time.Second)
	}

	log.Printf("[ERROR] Timeout reached waiting for server %s to be provisioned after %d minutes",
		serverID, timeoutMinutes)
	return fmt.Errorf("timeout reached waiting for server %s to be provisioned after %d minutes",
		serverID, timeoutMinutes)
}
