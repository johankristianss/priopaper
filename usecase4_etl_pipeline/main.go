// Use Case 4: Multi-Tenant Data Platform with SLA-Based Pipelines
//
// A shared data platform serves multiple organizations, each running ETL
// pipelines with different service level agreements (Gold/Silver/Bronze).
// The platform must balance fairness across tenants while respecting SLA
// deadlines. Each tenant's pipeline has different data volumes, processing
// complexity, and delivery requirements.
//
// This simulates 3 tenants submitting pipelines concurrently:
//
//   Tenant A (Gold SLA):   Real-time fraud detection for a bank
//   Tenant B (Silver SLA): Daily logistics optimization for a shipping company
//   Tenant C (Bronze SLA): Weekly research analytics for a university
//
// Each tenant's pipeline is an independent DAG competing for shared executor
// resources on the same colony.
//
// DAG Structure (Tenant A - Gold, fraud detection):
//
//   ingest_transactions ──> enrich_geo ──────────────┐
//   ingest_accounts ──> enrich_risk_scores ──────────├──> join_features ──> score_fraud ──> alert_dispatch ──> update_dashboard
//   ingest_watchlists ──> normalize_watchlists ──────┘
//
// DAG Structure (Tenant B - Silver, logistics):
//
//   ingest_shipments ──> geocode_addresses ──> optimize_routes ──> generate_manifests ──> notify_drivers
//   ingest_fleet_gps ──> calc_fleet_status ──────────┘
//
// DAG Structure (Tenant C - Bronze, research):
//
//   ingest_publications ──> extract_metadata ──> build_citation_graph ──> compute_metrics ──> export_results

package main

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/colonyos/colonies/pkg/client"
	"github.com/colonyos/colonies/pkg/core"
)

type Tenant struct {
	ID              string
	Name            string
	SLA             string // Gold, Silver, Bronze
	BasePriority    int    // derived from SLA
	DeadlineMinutes int    // pipeline must complete within this window
	Contact         string
}

func main() {
	colonyName := os.Getenv("COLONIES_COLONY_NAME")
	executorPrvKey := os.Getenv("COLONIES_EXECUTOR_PRVKEY")
	host := os.Getenv("COLONIES_SERVER_HOST")
	portStr := os.Getenv("COLONIES_SERVER_PORT")
	port, err := strconv.Atoi(portStr)
	if err != nil {
		fmt.Println("ERROR: invalid COLONIES_SERVER_PORT:", err)
		os.Exit(1)
	}

	tenants := []Tenant{
		{ID: "tenant-a", Name: "Nordea Digital Banking", SLA: "Gold", BasePriority: 30, DeadlineMinutes: 5, Contact: "fraud-ops@nordea.example.com"},
		{ID: "tenant-b", Name: "PostNord Logistics", SLA: "Silver", BasePriority: 15, DeadlineMinutes: 60, Contact: "logistics-platform@postnord.example.com"},
		{ID: "tenant-c", Name: "Uppsala University", SLA: "Bronze", BasePriority: 5, DeadlineMinutes: 1440, Contact: "research-data@uu.example.se"},
	}

	c := client.CreateColoniesClient(host, port, true, false)
	now := time.Now()

	fmt.Println("=== Multi-Tenant Data Platform ===")
	fmt.Printf("Tenants: %d\n", len(tenants))
	for _, t := range tenants {
		fmt.Printf("  [%s] %-30s SLA: %-6s Priority: %d  Deadline: %d min\n",
			t.ID, t.Name, t.SLA, t.BasePriority, t.DeadlineMinutes)
	}
	fmt.Println()

	// ================================================================
	// Tenant A (Gold): Real-time fraud detection pipeline
	// ================================================================
	{
		t := tenants[0]
		deadline := now.Add(time.Duration(t.DeadlineMinutes) * time.Minute)
		fmt.Printf("--- %s [%s SLA, deadline: %s] ---\n", t.Name, t.SLA, deadline.Format("15:04:05"))

		wf := core.CreateWorkflowSpec(colonyName)

		// Ingest transaction stream
		ingestTx := core.CreateEmptyFunctionSpec()
		ingestTx.NodeName = "ingest_transactions"
		ingestTx.FuncName = "ingest_stream"
		ingestTx.Conditions.ColonyName = colonyName
		ingestTx.Conditions.ExecutorType = "etl-worker"
		ingestTx.MaxExecTime = 120
		ingestTx.MaxWaitTime = 30
		ingestTx.Priority = t.BasePriority
		ingestTx.KwArgs = map[string]interface{}{
			"tenant":  t.ID,
			"sla":     t.SLA,
			"source":  "kafka://kafka.nordea.internal:9092/transactions",
			"format":  "avro",
			"schema":  "transaction_v4",
			"window":  "tumbling_30s",
			"records_per_window": 15000,
			"fields":  []string{"tx_id", "account_from", "account_to", "amount", "currency", "timestamp", "merchant_id", "merchant_mcc", "ip_address", "device_id", "location_lat", "location_lon"},
			"output":  "colonyfs:///tenants/" + t.ID + "/raw/transactions/",
		}
		wf.AddFunctionSpec(ingestTx)

		// Ingest account profiles
		ingestAccounts := core.CreateEmptyFunctionSpec()
		ingestAccounts.NodeName = "ingest_accounts"
		ingestAccounts.FuncName = "ingest_batch"
		ingestAccounts.Conditions.ColonyName = colonyName
		ingestAccounts.Conditions.ExecutorType = "etl-worker"
		ingestAccounts.MaxExecTime = 60
		ingestAccounts.MaxWaitTime = 30
		ingestAccounts.Priority = t.BasePriority
		ingestAccounts.KwArgs = map[string]interface{}{
			"tenant":  t.ID,
			"source":  "postgresql://core-banking.nordea.internal:5432/accounts",
			"query":   "SELECT account_id, customer_id, account_type, open_date, avg_balance_30d, country, risk_tier FROM accounts WHERE updated_at > NOW() - INTERVAL '1 hour'",
			"records_est": 50000,
			"output":  "colonyfs:///tenants/" + t.ID + "/raw/accounts/",
		}
		wf.AddFunctionSpec(ingestAccounts)

		// Ingest watchlists
		ingestWatchlists := core.CreateEmptyFunctionSpec()
		ingestWatchlists.NodeName = "ingest_watchlists"
		ingestWatchlists.FuncName = "ingest_batch"
		ingestWatchlists.Conditions.ColonyName = colonyName
		ingestWatchlists.Conditions.ExecutorType = "etl-worker"
		ingestWatchlists.MaxExecTime = 60
		ingestWatchlists.MaxWaitTime = 30
		ingestWatchlists.Priority = t.BasePriority
		ingestWatchlists.KwArgs = map[string]interface{}{
			"tenant":  t.ID,
			"sources": []map[string]interface{}{
				{"name": "EU_sanctions", "url": "https://webgate.ec.europa.eu/fsd/fsf/public/files/xmlFullSanctionsList_1_1/content", "format": "xml"},
				{"name": "PEP_list", "path": "colonyfs:///reference/pep_global_v3.csv", "format": "csv"},
				{"name": "internal_blocklist", "path": "colonyfs:///tenants/" + t.ID + "/reference/blocklist.csv", "format": "csv"},
			},
			"output": "colonyfs:///tenants/" + t.ID + "/raw/watchlists/",
		}
		wf.AddFunctionSpec(ingestWatchlists)

		// Enrich: geo lookup for transactions
		enrichGeo := core.CreateEmptyFunctionSpec()
		enrichGeo.NodeName = "enrich_geo"
		enrichGeo.FuncName = "enrich_data"
		enrichGeo.Conditions.ColonyName = colonyName
		enrichGeo.Conditions.ExecutorType = "etl-worker"
		enrichGeo.Conditions.Dependencies = []string{"ingest_transactions"}
		enrichGeo.MaxExecTime = 60
		enrichGeo.MaxWaitTime = 30
		enrichGeo.Priority = t.BasePriority
		enrichGeo.KwArgs = map[string]interface{}{
			"tenant":     t.ID,
			"operation":  "geo_enrichment",
			"ip_to_geo":  true,
			"reverse_geocode": true,
			"add_fields": []string{"country_ip", "city_ip", "distance_to_home_km", "is_vpn", "is_tor"},
			"output":     "colonyfs:///tenants/" + t.ID + "/enriched/transactions_geo/",
		}
		wf.AddFunctionSpec(enrichGeo)

		// Enrich: risk scores for accounts
		enrichRisk := core.CreateEmptyFunctionSpec()
		enrichRisk.NodeName = "enrich_risk_scores"
		enrichRisk.FuncName = "enrich_data"
		enrichRisk.Conditions.ColonyName = colonyName
		enrichRisk.Conditions.ExecutorType = "etl-worker"
		enrichRisk.Conditions.Dependencies = []string{"ingest_accounts"}
		enrichRisk.MaxExecTime = 60
		enrichRisk.MaxWaitTime = 30
		enrichRisk.Priority = t.BasePriority
		enrichRisk.KwArgs = map[string]interface{}{
			"tenant":    t.ID,
			"operation": "risk_scoring",
			"model":     "colonyfs:///models/account_risk_scorer_v5.onnx",
			"features":  []string{"account_age_days", "avg_balance_30d", "tx_count_30d", "country_risk_rating", "risk_tier"},
			"output":    "colonyfs:///tenants/" + t.ID + "/enriched/accounts_risk/",
		}
		wf.AddFunctionSpec(enrichRisk)

		// Normalize watchlists
		normalizeWL := core.CreateEmptyFunctionSpec()
		normalizeWL.NodeName = "normalize_watchlists"
		normalizeWL.FuncName = "normalize_data"
		normalizeWL.Conditions.ColonyName = colonyName
		normalizeWL.Conditions.ExecutorType = "etl-worker"
		normalizeWL.Conditions.Dependencies = []string{"ingest_watchlists"}
		normalizeWL.MaxExecTime = 60
		normalizeWL.MaxWaitTime = 30
		normalizeWL.Priority = t.BasePriority
		normalizeWL.KwArgs = map[string]interface{}{
			"tenant":         t.ID,
			"operation":      "name_normalization",
			"transliterate":  true,
			"fuzzy_index":    true,
			"similarity_algo": "jaro_winkler",
			"output":         "colonyfs:///tenants/" + t.ID + "/enriched/watchlists_normalized/",
		}
		wf.AddFunctionSpec(normalizeWL)

		// Join all features
		joinFeatures := core.CreateEmptyFunctionSpec()
		joinFeatures.NodeName = "join_features"
		joinFeatures.FuncName = "join_datasets"
		joinFeatures.Conditions.ColonyName = colonyName
		joinFeatures.Conditions.ExecutorType = "etl-worker"
		joinFeatures.Conditions.Dependencies = []string{"enrich_geo", "enrich_risk_scores", "normalize_watchlists"}
		joinFeatures.MaxExecTime = 120
		joinFeatures.MaxWaitTime = 30
		joinFeatures.Priority = t.BasePriority + 5 // boost priority for critical path
		joinFeatures.KwArgs = map[string]interface{}{
			"tenant": t.ID,
			"joins": []map[string]interface{}{
				{"left": "transactions_geo", "right": "accounts_risk", "on": "account_from", "type": "left"},
				{"left": "result", "right": "watchlists_normalized", "on": "customer_name", "type": "fuzzy_left", "threshold": 0.92},
			},
			"output": "colonyfs:///tenants/" + t.ID + "/features/fraud_features/",
		}
		wf.AddFunctionSpec(joinFeatures)

		// Score fraud probability
		scoreFraud := core.CreateEmptyFunctionSpec()
		scoreFraud.NodeName = "score_fraud"
		scoreFraud.FuncName = "score_ml"
		scoreFraud.Conditions.ColonyName = colonyName
		scoreFraud.Conditions.ExecutorType = "ml-scorer"
		scoreFraud.Conditions.Dependencies = []string{"join_features"}
		scoreFraud.MaxExecTime = 60
		scoreFraud.MaxWaitTime = 15
		scoreFraud.Priority = t.BasePriority + 10 // highest priority -- latency critical
		scoreFraud.KwArgs = map[string]interface{}{
			"tenant":     t.ID,
			"model":      "colonyfs:///models/fraud_ensemble_v8.onnx",
			"model_type": "xgboost_ensemble",
			"thresholds": map[string]interface{}{
				"block":   0.95,
				"review":  0.75,
				"monitor": 0.50,
			},
			"explain":   true,
			"shap":      true,
			"output":    "colonyfs:///tenants/" + t.ID + "/scores/fraud/",
		}
		wf.AddFunctionSpec(scoreFraud)

		// Dispatch alerts
		alertDispatch := core.CreateEmptyFunctionSpec()
		alertDispatch.NodeName = "alert_dispatch"
		alertDispatch.FuncName = "dispatch_alerts"
		alertDispatch.Conditions.ColonyName = colonyName
		alertDispatch.Conditions.ExecutorType = "notification-service"
		alertDispatch.Conditions.Dependencies = []string{"score_fraud"}
		alertDispatch.MaxExecTime = 30
		alertDispatch.MaxWaitTime = 15
		alertDispatch.Priority = t.BasePriority + 10
		alertDispatch.KwArgs = map[string]interface{}{
			"tenant":      t.ID,
			"rules": map[string]interface{}{
				"block":   map[string]interface{}{"action": "block_transaction", "api": "https://core-banking.nordea.internal/api/v2/block", "notify": []string{"fraud-ops@nordea.example.com"}, "sms": true},
				"review":  map[string]interface{}{"action": "queue_for_review", "api": "https://case-management.nordea.internal/api/cases", "notify": []string{"fraud-analysts@nordea.example.com"}},
				"monitor": map[string]interface{}{"action": "log_only", "retention_days": 90},
			},
			"sla_deadline": deadline.Format(time.RFC3339),
		}
		wf.AddFunctionSpec(alertDispatch)

		// Update real-time dashboard
		updateDash := core.CreateEmptyFunctionSpec()
		updateDash.NodeName = "update_dashboard"
		updateDash.FuncName = "update_dashboard"
		updateDash.Conditions.ColonyName = colonyName
		updateDash.Conditions.ExecutorType = "etl-worker"
		updateDash.Conditions.Dependencies = []string{"alert_dispatch"}
		updateDash.MaxExecTime = 30
		updateDash.MaxWaitTime = 15
		updateDash.Priority = t.BasePriority
		updateDash.KwArgs = map[string]interface{}{
			"tenant":        t.ID,
			"dashboard":     "https://grafana.nordea.internal/d/fraud-realtime",
			"metrics":       []string{"tx_processed", "fraud_blocked", "fraud_review", "avg_latency_ms", "false_positive_rate"},
			"push_endpoint": "wss://grafana.nordea.internal/api/live/push",
		}
		wf.AddFunctionSpec(updateDash)

		graph, err := c.SubmitWorkflowSpec(wf, executorPrvKey)
		if err != nil {
			fmt.Printf("  ERROR: %v\n\n", err)
		} else {
			fmt.Printf("  ProcessGraph ID: %s\n", graph.ID)
			fmt.Printf("  Processes: %d  (deadline: %d min, priority: %d-%d)\n\n",
				len(graph.ProcessIDs), t.DeadlineMinutes, t.BasePriority, t.BasePriority+10)
		}
	}

	// ================================================================
	// Tenant B (Silver): Daily logistics optimization
	// ================================================================
	{
		t := tenants[1]
		deadline := now.Add(time.Duration(t.DeadlineMinutes) * time.Minute)
		fmt.Printf("--- %s [%s SLA, deadline: %s] ---\n", t.Name, t.SLA, deadline.Format("15:04:05"))

		wf := core.CreateWorkflowSpec(colonyName)

		// Ingest today's shipment orders
		ingestShipments := core.CreateEmptyFunctionSpec()
		ingestShipments.NodeName = "ingest_shipments"
		ingestShipments.FuncName = "ingest_batch"
		ingestShipments.Conditions.ColonyName = colonyName
		ingestShipments.Conditions.ExecutorType = "etl-worker"
		ingestShipments.MaxExecTime = 300
		ingestShipments.MaxWaitTime = 120
		ingestShipments.Priority = t.BasePriority
		ingestShipments.KwArgs = map[string]interface{}{
			"tenant":      t.ID,
			"sla":         t.SLA,
			"source":      "postgresql://orders.postnord.internal:5432/shipments",
			"query":       "SELECT * FROM shipments WHERE ship_date = CURRENT_DATE AND status = 'pending'",
			"records_est": 85000,
			"output":      "colonyfs:///tenants/" + t.ID + "/raw/shipments/" + now.Format("2006-01-02") + "/",
		}
		wf.AddFunctionSpec(ingestShipments)

		// Ingest fleet GPS positions
		ingestFleet := core.CreateEmptyFunctionSpec()
		ingestFleet.NodeName = "ingest_fleet_gps"
		ingestFleet.FuncName = "ingest_stream"
		ingestFleet.Conditions.ColonyName = colonyName
		ingestFleet.Conditions.ExecutorType = "etl-worker"
		ingestFleet.MaxExecTime = 120
		ingestFleet.MaxWaitTime = 60
		ingestFleet.Priority = t.BasePriority
		ingestFleet.KwArgs = map[string]interface{}{
			"tenant":  t.ID,
			"source":  "mqtt://fleet-tracker.postnord.internal:1883/vehicles/+/gps",
			"format":  "json",
			"window":  "snapshot",
			"vehicles_est": 2400,
			"fields":  []string{"vehicle_id", "lat", "lon", "speed_kmh", "heading", "fuel_pct", "capacity_used_pct", "driver_id", "status"},
			"output":  "colonyfs:///tenants/" + t.ID + "/raw/fleet/" + now.Format("2006-01-02") + "/",
		}
		wf.AddFunctionSpec(ingestFleet)

		// Geocode pickup and delivery addresses
		geocode := core.CreateEmptyFunctionSpec()
		geocode.NodeName = "geocode_addresses"
		geocode.FuncName = "geocode_batch"
		geocode.Conditions.ColonyName = colonyName
		geocode.Conditions.ExecutorType = "etl-worker"
		geocode.Conditions.Dependencies = []string{"ingest_shipments"}
		geocode.MaxExecTime = 600
		geocode.MaxWaitTime = 120
		geocode.Priority = t.BasePriority
		geocode.KwArgs = map[string]interface{}{
			"tenant":     t.ID,
			"geocoder":   "lantmateriet",
			"cache":      "colonyfs:///tenants/" + t.ID + "/reference/geocode_cache.db",
			"fields":     []string{"pickup_address", "delivery_address"},
			"country":    "SE",
			"output":     "colonyfs:///tenants/" + t.ID + "/enriched/shipments_geocoded/",
		}
		wf.AddFunctionSpec(geocode)

		// Calculate fleet availability
		fleetStatus := core.CreateEmptyFunctionSpec()
		fleetStatus.NodeName = "calc_fleet_status"
		fleetStatus.FuncName = "compute_fleet_status"
		fleetStatus.Conditions.ColonyName = colonyName
		fleetStatus.Conditions.ExecutorType = "etl-worker"
		fleetStatus.Conditions.Dependencies = []string{"ingest_fleet_gps"}
		fleetStatus.MaxExecTime = 120
		fleetStatus.MaxWaitTime = 60
		fleetStatus.Priority = t.BasePriority
		fleetStatus.KwArgs = map[string]interface{}{
			"tenant":       t.ID,
			"input":        "colonyfs:///tenants/" + t.ID + "/raw/fleet/" + now.Format("2006-01-02") + "/",
			"driver_hours": "colonyfs:///tenants/" + t.ID + "/reference/driver_schedules.csv",
			"compute":      []string{"remaining_capacity", "estimated_return_time", "hours_remaining", "fuel_range_km"},
			"output":       "colonyfs:///tenants/" + t.ID + "/enriched/fleet_status/",
		}
		wf.AddFunctionSpec(fleetStatus)

		// Optimize delivery routes
		optimizeRoutes := core.CreateEmptyFunctionSpec()
		optimizeRoutes.NodeName = "optimize_routes"
		optimizeRoutes.FuncName = "optimize_vrp"
		optimizeRoutes.Conditions.ColonyName = colonyName
		optimizeRoutes.Conditions.ExecutorType = "optimization-worker"
		optimizeRoutes.Conditions.Dependencies = []string{"geocode_addresses", "calc_fleet_status"}
		optimizeRoutes.Conditions.CPU = "8"
		optimizeRoutes.Conditions.Memory = "16Gi"
		optimizeRoutes.MaxExecTime = 1800
		optimizeRoutes.MaxWaitTime = 300
		optimizeRoutes.Priority = t.BasePriority + 5
		optimizeRoutes.KwArgs = map[string]interface{}{
			"tenant":     t.ID,
			"algorithm":  "adaptive_large_neighborhood_search",
			"objective":  "minimize_total_distance",
			"constraints": map[string]interface{}{
				"max_route_duration_h": 8,
				"max_stops_per_route":  30,
				"time_windows":         true,
				"vehicle_capacity":     true,
				"driver_breaks":        true,
				"avoid_tolls":          false,
			},
			"road_network":  "osrm://routing.postnord.internal:5000",
			"time_limit_sec": 600,
			"output":         "colonyfs:///tenants/" + t.ID + "/optimized/routes/" + now.Format("2006-01-02") + "/",
		}
		wf.AddFunctionSpec(optimizeRoutes)

		// Generate driver manifests
		manifests := core.CreateEmptyFunctionSpec()
		manifests.NodeName = "generate_manifests"
		manifests.FuncName = "generate_manifests"
		manifests.Conditions.ColonyName = colonyName
		manifests.Conditions.ExecutorType = "etl-worker"
		manifests.Conditions.Dependencies = []string{"optimize_routes"}
		manifests.MaxExecTime = 300
		manifests.MaxWaitTime = 120
		manifests.Priority = t.BasePriority
		manifests.KwArgs = map[string]interface{}{
			"tenant":         t.ID,
			"format":         "pdf_and_json",
			"include_maps":   true,
			"include_barcode": true,
			"group_by":       "driver_id",
			"output":         "colonyfs:///tenants/" + t.ID + "/manifests/" + now.Format("2006-01-02") + "/",
		}
		wf.AddFunctionSpec(manifests)

		// Notify drivers
		notifyDrivers := core.CreateEmptyFunctionSpec()
		notifyDrivers.NodeName = "notify_drivers"
		notifyDrivers.FuncName = "dispatch_notifications"
		notifyDrivers.Conditions.ColonyName = colonyName
		notifyDrivers.Conditions.ExecutorType = "notification-service"
		notifyDrivers.Conditions.Dependencies = []string{"generate_manifests"}
		notifyDrivers.MaxExecTime = 120
		notifyDrivers.MaxWaitTime = 60
		notifyDrivers.Priority = t.BasePriority
		notifyDrivers.KwArgs = map[string]interface{}{
			"tenant":   t.ID,
			"channel":  "push_notification",
			"app":      "PostNord Driver App",
			"template": "daily_route_assignment",
			"fallback": "sms",
			"sla_deadline": deadline.Format(time.RFC3339),
		}
		wf.AddFunctionSpec(notifyDrivers)

		graph, err := c.SubmitWorkflowSpec(wf, executorPrvKey)
		if err != nil {
			fmt.Printf("  ERROR: %v\n\n", err)
		} else {
			fmt.Printf("  ProcessGraph ID: %s\n", graph.ID)
			fmt.Printf("  Processes: %d  (deadline: %d min, priority: %d-%d)\n\n",
				len(graph.ProcessIDs), t.DeadlineMinutes, t.BasePriority, t.BasePriority+5)
		}
	}

	// ================================================================
	// Tenant C (Bronze): Weekly research bibliometrics
	// ================================================================
	{
		t := tenants[2]
		deadline := now.Add(time.Duration(t.DeadlineMinutes) * time.Minute)
		fmt.Printf("--- %s [%s SLA, deadline: %s] ---\n", t.Name, t.SLA, deadline.Format("15:04:05"))

		wf := core.CreateWorkflowSpec(colonyName)

		// Ingest publications from multiple sources
		ingestPubs := core.CreateEmptyFunctionSpec()
		ingestPubs.NodeName = "ingest_publications"
		ingestPubs.FuncName = "ingest_batch"
		ingestPubs.Conditions.ColonyName = colonyName
		ingestPubs.Conditions.ExecutorType = "etl-worker"
		ingestPubs.MaxExecTime = 3600
		ingestPubs.MaxWaitTime = 600
		ingestPubs.Priority = t.BasePriority
		ingestPubs.KwArgs = map[string]interface{}{
			"tenant": t.ID,
			"sla":    t.SLA,
			"sources": []map[string]interface{}{
				{"name": "DiVA", "api": "https://diva-portal.org/smash/export.jsf", "format": "mods_xml", "query": "uu.se", "records_est": 12000},
				{"name": "OpenAlex", "api": "https://api.openalex.org/works", "format": "json", "filter": "institutions.ror:https://ror.org/048a87296", "records_est": 45000},
				{"name": "Crossref", "api": "https://api.crossref.org/works", "format": "json", "filter": "has-affiliation:true,query.affiliation:Uppsala+University", "records_est": 38000},
			},
			"dedup_method":  "doi_first_then_title_similarity",
			"date_range":    "2024-01-01/2025-12-31",
			"output":        "colonyfs:///tenants/" + t.ID + "/raw/publications/",
		}
		wf.AddFunctionSpec(ingestPubs)

		// Extract and normalize metadata
		extractMeta := core.CreateEmptyFunctionSpec()
		extractMeta.NodeName = "extract_metadata"
		extractMeta.FuncName = "extract_metadata"
		extractMeta.Conditions.ColonyName = colonyName
		extractMeta.Conditions.ExecutorType = "etl-worker"
		extractMeta.Conditions.Dependencies = []string{"ingest_publications"}
		extractMeta.MaxExecTime = 1800
		extractMeta.MaxWaitTime = 600
		extractMeta.Priority = t.BasePriority
		extractMeta.KwArgs = map[string]interface{}{
			"tenant":  t.ID,
			"extract": []string{"authors", "affiliations", "departments", "subjects", "funding", "open_access_status", "citations_count"},
			"normalize_affiliations": true,
			"affiliation_mapping":    "colonyfs:///tenants/" + t.ID + "/reference/uu_department_mapping.csv",
			"output":                 "colonyfs:///tenants/" + t.ID + "/enriched/publications_metadata/",
		}
		wf.AddFunctionSpec(extractMeta)

		// Build citation network
		citationGraph := core.CreateEmptyFunctionSpec()
		citationGraph.NodeName = "build_citation_graph"
		citationGraph.FuncName = "build_graph"
		citationGraph.Conditions.ColonyName = colonyName
		citationGraph.Conditions.ExecutorType = "etl-worker"
		citationGraph.Conditions.Dependencies = []string{"extract_metadata"}
		citationGraph.Conditions.Memory = "32Gi"
		citationGraph.MaxExecTime = 3600
		citationGraph.MaxWaitTime = 600
		citationGraph.Priority = t.BasePriority
		citationGraph.KwArgs = map[string]interface{}{
			"tenant":       t.ID,
			"graph_type":   "citation_network",
			"node_types":   []string{"paper", "author", "department", "funder"},
			"edge_types":   []string{"cites", "authored_by", "affiliated_with", "funded_by"},
			"format":       "graphml",
			"output_graph": "colonyfs:///tenants/" + t.ID + "/graphs/citation_network.graphml",
			"output_adj":   "colonyfs:///tenants/" + t.ID + "/graphs/adjacency_sparse.npz",
		}
		wf.AddFunctionSpec(citationGraph)

		// Compute bibliometric indicators
		computeMetrics := core.CreateEmptyFunctionSpec()
		computeMetrics.NodeName = "compute_metrics"
		computeMetrics.FuncName = "compute_bibliometrics"
		computeMetrics.Conditions.ColonyName = colonyName
		computeMetrics.Conditions.ExecutorType = "etl-worker"
		computeMetrics.Conditions.Dependencies = []string{"build_citation_graph"}
		computeMetrics.Conditions.Memory = "16Gi"
		computeMetrics.MaxExecTime = 1800
		computeMetrics.MaxWaitTime = 600
		computeMetrics.Priority = t.BasePriority
		computeMetrics.KwArgs = map[string]interface{}{
			"tenant": t.ID,
			"metrics": map[string]interface{}{
				"per_department":  []string{"publication_count", "citation_count", "h_index", "field_weighted_citation_impact", "international_collaboration_pct", "open_access_pct", "top_10_pct_journals"},
				"per_author":     []string{"publication_count", "citation_count", "h_index", "co_author_count"},
				"per_subject":    []string{"publication_count", "growth_rate", "avg_citations"},
				"network":        []string{"pagerank", "betweenness_centrality", "clustering_coefficient", "community_detection"},
			},
			"comparison_group": "nordic_universities",
			"output":           "colonyfs:///tenants/" + t.ID + "/metrics/bibliometrics/",
		}
		wf.AddFunctionSpec(computeMetrics)

		// Export results
		exportResults := core.CreateEmptyFunctionSpec()
		exportResults.NodeName = "export_results"
		exportResults.FuncName = "export_results"
		exportResults.Conditions.ColonyName = colonyName
		exportResults.Conditions.ExecutorType = "etl-worker"
		exportResults.Conditions.Dependencies = []string{"compute_metrics"}
		exportResults.MaxExecTime = 600
		exportResults.MaxWaitTime = 300
		exportResults.Priority = t.BasePriority
		exportResults.KwArgs = map[string]interface{}{
			"tenant": t.ID,
			"exports": []map[string]interface{}{
				{"format": "excel", "output": "colonyfs:///tenants/" + t.ID + "/reports/bibliometrics_weekly.xlsx", "sheets": []string{"summary", "by_department", "by_author", "by_subject"}},
				{"format": "json_api", "endpoint": "https://research-portal.uu.se/api/v2/metrics/import", "auth": "bearer_token"},
				{"format": "csv", "output": "colonyfs:///tenants/" + t.ID + "/exports/metrics_" + now.Format("2006-01-02") + ".csv"},
			},
			"notify": []string{t.Contact},
		}
		wf.AddFunctionSpec(exportResults)

		graph, err := c.SubmitWorkflowSpec(wf, executorPrvKey)
		if err != nil {
			fmt.Printf("  ERROR: %v\n\n", err)
		} else {
			fmt.Printf("  ProcessGraph ID: %s\n", graph.ID)
			fmt.Printf("  Processes: %d  (deadline: %d min, priority: %d)\n\n",
				len(graph.ProcessIDs), t.DeadlineMinutes, t.BasePriority)
		}
	}

	fmt.Println("=== Scheduling Challenge ===")
	fmt.Println("All three pipelines compete for shared etl-worker executors.")
	fmt.Println()
	fmt.Println("  Gold (Nordea):    Priority 30-40, deadline 5 min  -- must preempt others")
	fmt.Println("  Silver (PostNord): Priority 15-20, deadline 60 min -- can wait, but not too long")
	fmt.Println("  Bronze (UU):      Priority 5,     deadline 24 h  -- best-effort, yield to others")
	fmt.Println()
	fmt.Println("The prioritization model must handle:")
	fmt.Println("  - SLA-based priority tiers with deadline awareness")
	fmt.Println("  - Tenant fairness (Bronze shouldn't starve completely)")
	fmt.Println("  - Priority escalation as deadlines approach")
	fmt.Println("  - Different pipeline shapes competing for the same executor pool")
}
