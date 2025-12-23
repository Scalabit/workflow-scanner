# Wait for SQL Admin API to propagate
resource "time_sleep" "wait_for_sql_api" {
  depends_on = [google_project_service.apis]
  create_duration = "60s"
}

# CloudSQL PostgreSQL instance
resource "google_sql_database_instance" "workflow_scanner_db" {
  name             = "workflow-scanner-db"
  database_version = "POSTGRES_15"
  region          = var.region
  project         = var.project_id
  
  depends_on = [time_sleep.wait_for_sql_api]

  settings {
    tier              = "db-f1-micro"
    availability_type = "ZONAL"
    disk_type         = "PD_SSD"
    disk_size         = 10

    backup_configuration {
      enabled    = true
      start_time = "03:00"
    }

    ip_configuration {
      ipv4_enabled = false
    }

    database_flags {
      name  = "log_statement"
      value = "all"
    }
  }

  deletion_protection = false
}

# Database
resource "google_sql_database" "workflow_scanner" {
  name     = "workflow_scanner"
  instance = google_sql_database_instance.workflow_scanner_db.name
  project  = var.project_id
}

# Database user
resource "google_sql_user" "workflow_user" {
  name     = "workflow_user"
  instance = google_sql_database_instance.workflow_scanner_db.name
  password = random_password.db_password.result
  project  = var.project_id
}

# Random password for database user
resource "random_password" "db_password" {
  length  = 16
  special = true
}

# Store database password in Secret Manager
resource "google_secret_manager_secret" "db_password" {
  secret_id = "workflow-scanner-db-password"
  project   = var.project_id

  replication {
    user_managed {
      replicas {
        location = var.region
      }
    }
  }
}

resource "google_secret_manager_secret_version" "db_password" {
  secret      = google_secret_manager_secret.db_password.id
  secret_data = random_password.db_password.result
}

# Output the DATABASE_URL
locals {
  database_url = "postgresql://${google_sql_user.workflow_user.name}:${random_password.db_password.result}@/${google_sql_database.workflow_scanner.name}?host=/cloudsql/${var.project_id}:${var.region}:${google_sql_database_instance.workflow_scanner_db.name}"
}

# Store DATABASE_URL in Secret Manager
resource "google_secret_manager_secret" "database_url" {
  secret_id = "workflow-scanner-database-url"
  project   = var.project_id

  replication {
    user_managed {
      replicas {
        location = var.region
      }
    }
  }
}

resource "google_secret_manager_secret_version" "database_url" {
  secret      = google_secret_manager_secret.database_url.id
  secret_data = local.database_url
}

# IAM binding for Cloud Run to access CloudSQL
resource "google_project_iam_member" "cloud_run_sql_client" {
  project = var.project_id
  role    = "roles/cloudsql.client"
  member  = "serviceAccount:${google_service_account.cloud_run_sa.email}"
}

# IAM binding for Cloud Run to access Secret Manager
resource "google_project_iam_member" "cloud_run_secret_accessor" {
  project = var.project_id
  role    = "roles/secretmanager.secretAccessor"
  member  = "serviceAccount:${google_service_account.cloud_run_sa.email}"
}