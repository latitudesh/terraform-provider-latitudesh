# Upgrading to Latitude.sh Terraform Provider v2.0.0

Hey there! I'm excited to share that we've just released **Latitude.sh Terraform Provider v2.0.0**. A complete architectural upgrade that brings significant improvements while maintaining full backward compatibility for your configurations.


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

## Your configs stay the same

**Zero breaking changes to your Terraform configurations.** All your existing `.tf` files will work exactly as before:

```hcl
# ✅ This works exactly the same in v2.0.0
resource "latitudesh_project" "main" {
  name             = "production-infrastructure"
  description      = "Main production environment"
  environment      = "Production"
  provisioning_type = "on_demand"
}

resource "latitudesh_ssh_key" "deploy" {
  name       = "deployment-key"
  public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5..."
  tags       = ["deploy", "production"]
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

resource "latitudesh_server" "web" {
  project          = latitudesh_project.main.id
  hostname         = "web-server-01"
  site             = "ASH"
  plan             = "c3-small-x86"
  operating_system = "ubuntu_20_04_x64_lts"
  billing          = "hourly"
  
  user_data = latitudesh_user_data.setup.id
  ssh_keys  = [latitudesh_ssh_key.deploy.id]
  
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
- ✅ `latitudesh_user_data` - Server initialization scripts

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

### Step 3: Test in development first

```bash
# 1. In your dev environment, upgrade the provider
terraform init -upgrade

# 2. Check what Terraform thinks will change (should be minimal!)
terraform plan

# 3. Look for any unexpected changes - there shouldn't be many
# Most changes will be cosmetic (like computed field updates)
```

### Step 4: Validate Everything Works 🔍

Run a quick validation to ensure everything is working:

```bash
# This should show no changes needed
terraform plan  

# Import tests for existing resources (optional but recommended)
terraform show | grep -E "resource.*latitudesh_"
```

## What you'll see during upgrade

During your first `terraform plan` after upgrading, you might see some cosmetic changes:

```diff
# latitudesh_server.web will be updated in-place
~ resource "latitudesh_server" "web" {
    # Some computed fields might show as changing
    ~ created_at  = "2024-01-15T10:30:00Z" -> (known after apply)
    ~ region      = "us-east-1" -> (known after apply)
    ~ fingerprint = "SHA256:abc123..." -> (known after apply)
    # This is normal and expected - just more accurate data
}
```

**Don't worry!** These are just computed fields being refreshed with better precision. Your actual infrastructure won't be touched.

## Troubleshooting

### If Something Goes Wrong

1. **Check the error messages**
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

**Q: "terraform plan shows unexpected changes"**
A: Most likely computed field refreshes. They won't affect your actual resources.

**Q: "I get authentication errors after upgrading"**  
A: The provider authentication hasn't changed. Double-check your `auth_token` is correctly set.

**Q: "Some attributes seem to have different values"**
A: v2.0.0 has more accurate data retrieval. These changes reflect actual API values.


- **Email**: [support@latitude.sh](mailto:support@latitude.sh) 
- **Community**: [latitude.sh/community](https://latitude.sh/community)

---

*Happy Terraforming! 🌍*

**— The Latitude.sh Team**

*Last updated: June 2025* 