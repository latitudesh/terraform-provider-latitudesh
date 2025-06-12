# Upgrading to Latitude.sh Terraform Provider v2.0.0

Hey there! I'm excited to share that we've just released **Latitude.sh Terraform Provider v2.0.0**. A complete architectural upgrade that brings significant improvements while maintaining compatibility for most configurations.

After months of work migrating from HashiCorp's Plugin SDK v2 to the modern **Plugin Framework v1.15.0** and our **new Go SDK**, here's what changed:

### **Performance**
- **30-50% faster** operations compared to v1.x
- Reduced memory footprint and better resource efficiency
- Optimized API calls and state management

### **Reliability**
- **Compile-time type checking** prevents those frustrating runtime errors
- **Enhanced error messages** that actually help you fix issues
- **Improved state management** that handles edge cases gracefully

### **Future-proof architecture**
- Built on HashiCorp's latest and actively maintained framework
- **Ready for Terraform 2.0** when it arrives
- Modern patterns that make adding new features easier

## ⚠️ Breaking Changes

While we've maintained compatibility for most resources, there are some important changes due to new API endpoints:

### **SSH Keys & User Data - Now Team-Scoped**

In v1.x, SSH keys and user data were project-scoped. In v2.0.0, they're **team-scoped** for better management:

**v1.x (Old):**
```hcl
resource "latitudesh_ssh_key" "deploy" {
  project    = latitudesh_project.main.id  # ❌ No longer needed
  name       = "deployment-key"
  public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5..."
}

resource "latitudesh_user_data" "setup" {
  project     = latitudesh_project.main.id  # ❌ No longer needed
  description = "Server initialization script"
  content     = base64encode("#!/bin/bash\napt-get update")
}
```

**v2.0.0 (New):**
```hcl
resource "latitudesh_ssh_key" "deploy" {
  name       = "deployment-key"
  public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5..."
  tags       = ["deploy", "production"]  # ✅ Better organization
}

resource "latitudesh_user_data" "setup" {
  description = "Server initialization script"
  content     = base64encode(<<-EOF
    #!/bin/bash
    apt-get update
    apt-get install -y docker.io
    systemctl enable docker
    systemctl start docker
  EOF
  )
}
```

### **What This Means**
- **SSH keys** are now shared across all projects in your team
- **User data** scripts are team-wide and reusable across projects  
- **No migration required** - existing resources will import correctly
- **Better reusability** - create once, use in multiple projects

## Your configs mostly stay the same

**Most configurations work without changes.** Here's what stays the same:

```hcl
# ✅ These work exactly the same in v2.0.0
resource "latitudesh_project" "main" {
  name             = "production-infrastructure"
  description      = "Main production environment"
  environment      = "Production"
  provisioning_type = "on_demand"
}

resource "latitudesh_server" "web" {
  project          = latitudesh_project.main.id
  hostname         = "web-server-01"
  site             = "ASH"
  plan             = "c3-small-x86"
  operating_system = "ubuntu_20_04_x64_lts"
  billing          = "hourly"
  
  user_data = latitudesh_user_data.setup.id  # ✅ Still works
  ssh_keys  = [latitudesh_ssh_key.deploy.id] # ✅ Still works
  
  tags = ["web", "production", "frontend"]
}

# Your provider config stays the same too
provider "latitudesh" {
  auth_token = var.latitude_auth_token
}
```

## 🎯  All 11 resources and 3 data sources are fully functional:

### 💻 **Core Infrastructure**
- ✅ `latitudesh_server` - Full server lifecycle with enhanced update capabilities
- ✅ `latitudesh_project` - Project management
- ✅ `latitudesh_ssh_key` - SSH key management 

### 🌐 **Networking & Security**
- ✅ `latitudesh_virtual_network` - Network infrastructure management
- ✅ `latitudesh_vlan_assignment` - Server-to-VLAN assignments
- ✅ `latitudesh_firewall` - Security rule configuration
- ✅ `latitudesh_firewall_assignment` - Rule-to-server assignments

### 👥 **Team & Organization**
- ✅ `latitudesh_member` - Team member management
- ✅ `latitudesh_tag` - Resource organization and tagging
- ✅ `latitudesh_user_data` - Server initialization scripts (now team-scoped)

### 📊 **Data Sources for Planning**
- ✅ `data.latitudesh_plan` - Hardware specs, CPU, memory, GPU details
- ✅ `data.latitudesh_region` - Location and availability information  
- ✅ `data.latitudesh_role` - Team role definitions

## 🛠️ Upgrade guide:

### Step 1: Backup your state

**Always backup before major upgrades**:

```bash
# For local state files
cp terraform.tfstate terraform.tfstate.backup-v1

# For remote state (adjust for your backend)
terraform state pull > terraform.tfstate.backup-v1
```

### Step 2: Update your provider version

Update your `versions.tf` or main configuration:

```hcl
terraform {
  required_providers {
    latitudesh = {
      source  = "latitudesh/latitudesh"
      version = "~> 2.0.0"  # 🎉 Welcome to v2!
    }
  }
  # Pro tip: Also pin your Terraform version for consistency
  required_version = ">= 1.0"
}
```

### Step 3: Update SSH key and user data resources

Remove the `project` attribute from SSH keys and user data:

```hcl
resource "latitudesh_ssh_key" "deploy" {
  # project = latitudesh_project.main.id  # ❌ Remove this line
  name       = "deployment-key"
  public_key = var.ssh_public_key
}

resource "latitudesh_user_data" "setup" {
  # project = latitudesh_project.main.id  # ❌ Remove this line  
  description = "Server setup"
  content     = var.user_data_content
}
```

### Step 4: Test in development first

```bash
# 1. In your dev environment, upgrade the provider
terraform init -upgrade

# 2. Check what Terraform thinks will change
terraform plan

# 3. Look for the expected changes:
# - SSH keys and user data will show attribute removals
# - Servers should show no changes (they still reference the same resources)
```

### Step 5: Apply the changes

```bash
# Apply the changes
terraform apply

# Validate everything works
terraform plan  # Should show no further changes
```

## What you'll see during upgrade

During your first `terraform plan` after upgrading, you'll see changes like:

```diff
# latitudesh_ssh_key.deploy will be updated in-place
~ resource "latitudesh_ssh_key" "deploy" {
    ~ project = "project-123" -> null
    # Other attributes unchanged
}

# latitudesh_user_data.setup will be updated in-place  
~ resource "latitudesh_user_data" "setup" {
    ~ project = "project-123" -> null
    # Other attributes unchanged
}

# latitudesh_server.web - no changes needed
  resource "latitudesh_server" "web" {
    # Server references still work perfectly
    user_data = latitudesh_user_data.setup.id
    ssh_keys  = [latitudesh_ssh_key.deploy.id]
  }
```

**This is expected!** Your SSH keys and user data become team-scoped, which actually makes them more reusable.

## Troubleshooting

### If Something Goes Wrong

1. **Check the error messages** - they're much clearer in v2.0.0
2. **Restore your state backup**:
   ```bash
   cp terraform.tfstate.backup-v1 terraform.tfstate
   ```
3. **Downgrade temporarily**:
   ```hcl
   terraform {
     required_providers {
       latitudesh = {
         source  = "latitudesh/latitudesh"
         version = "~> 1.2.0"  # Back to v1.x
       }
     }
   }
   ```
4. **Reinitialize**: `terraform init -upgrade`

### Common migration issues & solutions

**Q: "terraform plan shows SSH key/user data changes"**
A: Expected! Remove the `project` attribute from these resources. They're now team-scoped.

**Q: "My servers can't find SSH keys or user data"**  
A: Check that the resource references (`.id`) are correct. The resources still work, just without project scoping.

**Q: "I get authentication errors after upgrading"**
A: The provider authentication hasn't changed. Double-check your `auth_token` is correctly set.

### Migration Benefits

The team-scoped approach brings several advantages:

- **Reusability**: SSH keys work across all projects
- **Simplified management**: No need to recreate keys per project  
- **Better organization**: Use tags instead of project boundaries
- **Reduced duplication**: One user data script for multiple projects

## Need help?

- **Email**: [support@latitude.sh](mailto:support@latitude.sh) 
- **Community**: [latitude.sh/community](https://latitude.sh/community)
- **GitHub Issues**: [Create an issue](https://github.com/latitudesh/terraform-provider-latitudesh/issues) with the `migration-v2` label

---

*Happy Terraforming! 🌍*

**— The Latitude.sh Team**

*Last updated: January 2025* 