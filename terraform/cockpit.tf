# Activate Cockpit (managed Loki/Grafana) for the project.
resource "scaleway_cockpit" "main" {}

# Push token for Grafana Alloy to ship logs to Cockpit.
resource "scaleway_cockpit_token" "alloy" {
  project_id = data.scaleway_account_project.current.id
  name       = "gradebee-alloy"

  scopes {
    query_logs  = false
    write_logs  = true
    setup_logs  = false
  }
}
