variable "project_id" {
  type = string
}

variable "project_number" {
  type = string
}

variable "region" {
  type = string
}

variable "zone" {
  type = string
}


variable "label" {
  type = string
}

variable "basename" {
  type = string
}

variable "budgetamount" {
  type = string
}

variable "billing_account" {
  type = string
}


variable "location" {
  type = string
}

locals {
  sabuild   = "${var.project_number}@cloudbuild.gserviceaccount.com"
  sacompute = "${var.project_number}-compute@developer.gserviceaccount.com"
  safunctionuser = "${var.basename}-functions-sa"
  safunction = "${local.safunctionuser}@${var.project_id}.iam.gserviceaccount.com"
}

provider "google-beta" {
    user_project_override = true
    billing_project = var.project_id
}

# Handle services
variable "gcp_service_list" {
  description = "The list of apis necessary for the project"
  type        = list(string)
  default = [
    "cloudresourcemanager.googleapis.com",
    "cloudbilling.googleapis.com",
    "billingbudgets.googleapis.com",
    "cloudbuild.googleapis.com",
    "compute.googleapis.com",
    "cloudfunctions.googleapis.com",
    "storage.googleapis.com",
    "run.googleapis.com",
    "iam.googleapis.com"
  ]
}

resource "google_project_service" "all" {
  for_each           = toset(var.gcp_service_list)
  project            = var.project_number
  service            = each.key
  disable_on_destroy = false
}


resource "google_service_account" "functions_accounts" {
  account_id   = local.safunctionuser
  description = "Service Account for the costsentry to run as"
  display_name = local.safunction
  project            = var.project_number
}


# Handle Permissions
variable "build_roles_list" {
  description = "The list of roles that fucntions needs for"
  type        = list(string)
  default = [
    "roles/run.admin", 
    "roles/compute.instanceAdmin",
    "roles/iam.serviceAccountUser"     
  ]
}

resource "google_project_iam_member" "allbuild" {
  for_each   = toset(var.build_roles_list)
  project    = var.project_number
  role       = each.key
  member     = "serviceAccount:${google_service_account.functions_accounts.email}"
  depends_on = [google_project_service.all,google_service_account.functions_accounts]
}


resource "google_pubsub_topic" "costsentry" {
  name = "${var.basename}-billing-channel"
  project    = var.project_number
}


resource "google_cloud_run_service" "app" {
  name     = "${var.basename}-run-service"
  location = var.region
  project  = var.project_id

  metadata {
      labels = {"${var.label}"=true}
  }

  template {
    spec {
      containers {
        image = "us-docker.pkg.dev/cloudrun/container/hello"
      }
    }

    metadata {
        
        annotations = {
            "autoscaling.knative.dev/maxScale" = "1000"
            "run.googleapis.com/client-name"   = "terraform"
        }
    }
  }
  autogenerate_revision_name = true
  depends_on = [google_project_service.all]
}

data "google_iam_policy" "noauth" {
  binding {
    role = "roles/run.invoker"
    members = [
      "allUsers",
    ]
  }
}

resource "google_cloud_run_service_iam_policy" "noauth_app" {
  location    = google_cloud_run_service.app.location
  project     = google_cloud_run_service.app.project
  service     = google_cloud_run_service.app.name
  policy_data = data.google_iam_policy.noauth.policy_data
}

resource "google_compute_instance" "example" {
  name         = "${var.basename}-example"
  machine_type = "n1-standard-1"
  zone         = var.zone
  project      = var.project_id
  tags                    = ["http-server"]
  labels = {"${var.label}"=true}

  boot_disk {
    auto_delete = true
    device_name = "${var.basename}-example"
    initialize_params {
      image = "family/debian-10"
      size  = 200
      type  = "pd-standard"
    }
  }

  network_interface {
    network = "default"
    access_config {
      // Ephemeral public IP
    }
  }

  depends_on = [google_project_service.all]
}

resource "null_resource" "budgetset" {


    triggers = {
        billing_account = var.billing_account
        basename = var.basename
        project_id = var.project_id

    }

    provisioner "local-exec" {
        command = <<-EOT
        gcloud beta billing budgets create --display-name ${var.basename}-budget \
        --billing-account ${var.billing_account} --budget-amount ${var.budgetamount} \
        --all-updates-rule-pubsub-topic=projects/${var.project_id}/topics/${var.basename}-billing-channel 
        EOT
    }

    provisioner "local-exec" {
        when    = destroy
        command = <<-EOT
        gcloud beta billing budgets delete $(gcloud beta billing budgets list --format="value(NAME)" --billing-account ${self.triggers.billing_account}  --filter="displayName:${self.triggers.basename}-budget") -q --project=${self.triggers.project_id}
        EOT
    }

    depends_on = [
        google_project_service.all,
        google_pubsub_topic.costsentry
    ]
}




resource "google_storage_bucket" "function_bucket" {
  name     = "${var.project_id}-function-deployer"
  project  = var.project_number
  location = var.location
}

resource "null_resource" "cloudbuild_function" {
  provisioner "local-exec" {
    command = <<-EOT
    cp code/function/function.go .
    cp code/function/go.mod .
    zip index.zip function.go
    zip index.zip go.mod
    rm go.mod
    rm function.go
    EOT
  }

  depends_on = [
    google_project_service.all
  ]
}

resource "google_storage_bucket_object" "archive" {
  name   = "index.zip"
  bucket = google_storage_bucket.function_bucket.name
  source = "index.zip"
  depends_on = [
    google_project_service.all,
    google_storage_bucket.function_bucket,
    null_resource.cloudbuild_function
  ]
}

resource "google_cloudfunctions_function" "function" {
  name    = var.basename
  project = var.project_id
  region  = var.region
  runtime = "go116"
  service_account_email = google_service_account.functions_accounts.email
  available_memory_mb   = 128
  source_archive_bucket = google_storage_bucket.function_bucket.name
  source_archive_object = google_storage_bucket_object.archive.name
  entry_point           = "LimitUsage"
  event_trigger {
    event_type = "google.pubsub.topic.publish"
    resource   = google_pubsub_topic.costsentry.name
  }

  environment_variables = {
    GOOGLE_CLOUD_PROJECT = var.project_id
    LABEL= var.label
  }

  depends_on = [
    google_storage_bucket.function_bucket,
    google_storage_bucket_object.archive,
    google_project_service.all
  ]
}