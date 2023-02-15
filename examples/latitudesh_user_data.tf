resource "latitudesh_user_data" "user_data" {
    project = latitudesh_project.project.id
    description = "Update apt packages"
    content = "I2Nsb3VkLWNvbmZpZwoKYXB0OgoKcHJpbWFyeToKCXByaW1hcnk6CglyZXF1aXJlOiBbZGVmYXVsdF0KICB1cmk6IGh0dHA6Ly91cy5hcmNoaXZlLnVidW50dS5jb20vdWJ1bnR1Lwo="
}