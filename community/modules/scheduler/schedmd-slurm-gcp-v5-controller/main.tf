/**
 * Copyright 2022 Google LLC
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
*/

locals {
  ghpc_startup_script_controller = var.controller_startup_script == "" ? [] : [{
    filename = "ghpc_startup.sh"
    content  = var.controller_startup_script
  }]
  ghpc_startup_script_compute = var.compute_startup_script == "" ? [] : [{
    filename = "ghpc_startup.sh"
    content  = var.compute_startup_script
  }]
  # Since deployment name may be used to create a cluster name, we remove any invalid character from the beginning
  # Also, slurm imposed a lot of restrictions to this name, so we format it to an acceptable string
  tmp_cluster_name   = substr(replace(lower(var.deployment_name), "/^[^a-z]*|[^a-z0-9]/", ""), 0, 10)
  slurm_cluster_name = var.slurm_cluster_name != null ? var.slurm_cluster_name : local.tmp_cluster_name

  enable_public_ip_access_config = var.disable_controller_public_ips ? [] : [{ nat_ip = null, network_tier = null }]
  access_config                  = length(var.access_config) == 0 ? local.enable_public_ip_access_config : var.access_config

  # Handle VM image format from 2 sources, prioritize source_image* variables
  # over instance_image
  source_image_input_used = var.source_image != "" || var.source_image_family != "" || var.source_image_project != ""
  source_image            = local.source_image_input_used ? var.source_image : lookup(var.instance_image, "name", "")
  source_image_family     = local.source_image_input_used ? var.source_image_family : lookup(var.instance_image, "family", "")
  source_image_project    = local.source_image_input_used ? var.source_image_project : lookup(var.instance_image, "project", "")
}

data "google_compute_default_service_account" "default" {
  project = var.project_id
}

module "slurm_controller_instance" {
  source = "github.com/SchedMD/slurm-gcp.git//terraform/slurm_cluster/modules/slurm_controller_instance?ref=5.3.0"

  access_config                      = local.access_config
  slurm_cluster_name                 = local.slurm_cluster_name
  instance_template                  = module.slurm_controller_template.self_link
  project_id                         = var.project_id
  region                             = var.region
  network                            = var.network_self_link == null ? "" : var.network_self_link
  subnetwork                         = var.subnetwork_self_link == null ? "" : var.subnetwork_self_link
  subnetwork_project                 = var.subnetwork_project == null ? "" : var.subnetwork_project
  zone                               = var.zone
  static_ips                         = var.static_ips
  cgroup_conf_tpl                    = var.cgroup_conf_tpl
  cloud_parameters                   = var.cloud_parameters
  cloudsql                           = var.cloudsql
  controller_startup_scripts         = local.ghpc_startup_script_controller
  compute_startup_scripts            = local.ghpc_startup_script_compute
  controller_startup_scripts_timeout = var.controller_startup_scripts_timeout
  compute_startup_scripts_timeout    = var.compute_startup_scripts_timeout
  login_startup_scripts_timeout      = var.login_startup_scripts_timeout
  enable_devel                       = var.enable_devel
  enable_cleanup_compute             = var.enable_cleanup_compute
  enable_cleanup_subscriptions       = var.enable_cleanup_subscriptions
  enable_reconfigure                 = var.enable_reconfigure
  enable_bigquery_load               = var.enable_bigquery_load
  epilog_scripts                     = var.epilog_scripts
  disable_default_mounts             = var.disable_default_mounts
  login_network_storage              = var.network_storage
  network_storage                    = var.network_storage
  partitions                         = var.partition
  prolog_scripts                     = var.prolog_scripts
  slurmdbd_conf_tpl                  = var.slurmdbd_conf_tpl
  slurm_conf_tpl                     = var.slurm_conf_tpl
}

module "slurm_controller_template" {
  source = "github.com/SchedMD/slurm-gcp.git//terraform/slurm_cluster/modules/slurm_instance_template?ref=5.3.0"

  additional_disks         = var.additional_disks
  can_ip_forward           = var.can_ip_forward
  slurm_cluster_name       = local.slurm_cluster_name
  disable_smt              = var.disable_smt
  disk_auto_delete         = var.disk_auto_delete
  disk_labels              = var.labels
  disk_size_gb             = var.disk_size_gb
  disk_type                = var.disk_type
  enable_confidential_vm   = var.enable_confidential_vm
  enable_oslogin           = var.enable_oslogin
  enable_shielded_vm       = var.enable_shielded_vm
  gpu                      = var.gpu
  labels                   = var.labels
  machine_type             = var.machine_type
  metadata                 = var.metadata
  min_cpu_platform         = var.min_cpu_platform
  network_ip               = var.network_ip != null ? var.network_ip : ""
  on_host_maintenance      = var.on_host_maintenance
  preemptible              = var.preemptible
  project_id               = var.project_id
  region                   = var.region
  shielded_instance_config = var.shielded_instance_config
  slurm_instance_role      = "controller"
  source_image_family      = local.source_image_family
  source_image_project     = local.source_image_project
  source_image             = local.source_image
  network                  = var.network_self_link == null ? "" : var.network_self_link
  subnetwork_project       = var.subnetwork_project == null ? "" : var.subnetwork_project
  subnetwork               = var.subnetwork_self_link == null ? "" : var.subnetwork_self_link
  tags                     = concat([local.slurm_cluster_name], var.tags)
  service_account = var.service_account != null ? var.service_account : {
    email  = data.google_compute_default_service_account.default.email
    scopes = ["https://www.googleapis.com/auth/cloud-platform"]
  }
}
