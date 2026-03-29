package monitoring

// GrafanaDatasourceYAML returns the Grafana datasource provisioning YAML
// that auto-configures Prometheus as the default data source.
func GrafanaDatasourceYAML(prometheusHost string) string {
	return `apiVersion: 1
datasources:
  - name: Prometheus
    type: prometheus
    access: proxy
    url: http://` + prometheusHost + `:9090
    isDefault: true
    editable: false
`
}

// GrafanaDashboardProviderYAML returns the dashboard provisioner config.
const GrafanaDashboardProviderYAML = `apiVersion: 1
providers:
  - name: "Town OS Status"
    orgId: 1
    folder: ""
    type: file
    disableDeletion: true
    editable: false
    options:
      path: /etc/grafana/provisioning/dashboard-json
      foldersFromFilesStructure: false
`
