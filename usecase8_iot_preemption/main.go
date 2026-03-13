// Use Case 8: Real-Time IoT Edge Processing with Preemption
//
// A municipal water treatment plant (Norrvatten, serving northern Stockholm)
// monitors water quality across 4 treatment stages using edge gateways.
// Each gateway runs a ColonyOS executor with limited capacity.
//
// Normal operation: periodic monitoring DAGs run every 30 seconds.
//   collect_sensors → validate_readings → aggregate_metrics → update_dashboard
//
// Anomaly response: when a sensor breaches a threshold, an emergency DAG
// is submitted that MUST preempt normal monitoring work. Hard real-time
// constraint: anomaly detection → actuator response within 2 seconds.
//
//   emergency_collect → classify_severity → actuate_response → notify_scada → log_incident
//                                         ↘ alert_operators ↗
//
// The preemption requirement arises because edge gateways have limited
// executor capacity (1-2 concurrent processes). If a chlorine residual
// drops below safe levels while 3 normal monitoring DAGs are queued,
// the emergency DAG must jump the queue AND potentially interrupt running
// normal-priority work.
//
// This use case demonstrates:
//   - Hard real-time latency constraints on edge devices
//   - Preemption of lower-priority work by safety-critical work
//   - Mixed criticality levels on shared, resource-constrained executors
//   - Edge-to-cloud priority propagation (alerts escalate to cloud)

package main

import (
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"time"

	"github.com/colonyos/colonies/pkg/client"
	"github.com/colonyos/colonies/pkg/core"
)

// TreatmentStage represents a stage in the water treatment process
type TreatmentStage struct {
	Name     string
	Location string
	Sensors  []Sensor
	Gateway  string // edge gateway ID
}

// Sensor represents an IoT sensor at the plant
type Sensor struct {
	ID          string
	Type        string  // chlorine, turbidity, pH, flow, pressure, temperature
	Unit        string
	MinSafe     float64 // safe operating range lower bound
	MaxSafe     float64 // safe operating range upper bound
	SampleRateHz float64
}

// AnomalyEvent represents a detected threshold breach
type AnomalyEvent struct {
	SensorID    string
	Stage       string
	Type        string
	Value       float64
	Threshold   float64
	Severity    string // critical, warning, info
	Timestamp   time.Time
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

	// ================================================================
	// Treatment plant layout: 4 stages, each with an edge gateway
	// ================================================================
	stages := []TreatmentStage{
		{
			Name:     "raw_intake",
			Location: "Görvälnverket intake, Lake Mälaren",
			Gateway:  "edge-gw-01",
			Sensors: []Sensor{
				{ID: "RAW-TURB-01", Type: "turbidity", Unit: "NTU", MinSafe: 0, MaxSafe: 4.0, SampleRateHz: 10},
				{ID: "RAW-PH-01", Type: "pH", Unit: "pH", MinSafe: 6.5, MaxSafe: 8.5, SampleRateHz: 1},
				{ID: "RAW-TEMP-01", Type: "temperature", Unit: "°C", MinSafe: 0.5, MaxSafe: 25.0, SampleRateHz: 0.1},
				{ID: "RAW-FLOW-01", Type: "flow", Unit: "m³/h", MinSafe: 500, MaxSafe: 4000, SampleRateHz: 1},
			},
		},
		{
			Name:     "coagulation_flocculation",
			Location: "Rapid mix + floc basins",
			Gateway:  "edge-gw-02",
			Sensors: []Sensor{
				{ID: "COAG-PH-01", Type: "pH", Unit: "pH", MinSafe: 5.8, MaxSafe: 7.2, SampleRateHz: 2},
				{ID: "COAG-TURB-01", Type: "turbidity", Unit: "NTU", MinSafe: 0, MaxSafe: 2.0, SampleRateHz: 10},
				{ID: "COAG-DOSE-01", Type: "coagulant_dose", Unit: "mg/L", MinSafe: 5, MaxSafe: 80, SampleRateHz: 0.5},
				{ID: "FLOC-TURB-01", Type: "turbidity", Unit: "NTU", MinSafe: 0, MaxSafe: 1.5, SampleRateHz: 10},
			},
		},
		{
			Name:     "filtration",
			Location: "Granular activated carbon filters",
			Gateway:  "edge-gw-03",
			Sensors: []Sensor{
				{ID: "FILT-TURB-01", Type: "turbidity", Unit: "NTU", MinSafe: 0, MaxSafe: 0.5, SampleRateHz: 10},
				{ID: "FILT-PRES-01", Type: "pressure_diff", Unit: "kPa", MinSafe: 0, MaxSafe: 35, SampleRateHz: 1},
				{ID: "FILT-FLOW-01", Type: "flow", Unit: "m³/h", MinSafe: 200, MaxSafe: 1200, SampleRateHz: 1},
				{ID: "FILT-HEAD-01", Type: "headloss", Unit: "m", MinSafe: 0, MaxSafe: 2.5, SampleRateHz: 0.5},
			},
		},
		{
			Name:     "disinfection_distribution",
			Location: "UV + chloramine dosing, clear well",
			Gateway:  "edge-gw-04",
			Sensors: []Sensor{
				{ID: "DIS-CL2-01", Type: "chlorine_residual", Unit: "mg/L", MinSafe: 0.2, MaxSafe: 1.5, SampleRateHz: 2},
				{ID: "DIS-CL2-02", Type: "chlorine_residual", Unit: "mg/L", MinSafe: 0.2, MaxSafe: 1.5, SampleRateHz: 2},
				{ID: "DIS-UV-01", Type: "uv_transmittance", Unit: "%", MinSafe: 75, MaxSafe: 99, SampleRateHz: 1},
				{ID: "DIS-PH-01", Type: "pH", Unit: "pH", MinSafe: 7.0, MaxSafe: 8.2, SampleRateHz: 1},
				{ID: "DIS-PRES-01", Type: "pressure", Unit: "bar", MinSafe: 2.0, MaxSafe: 8.0, SampleRateHz: 1},
			},
		},
	}

	c := client.CreateColoniesClient(host, port, true, false)

	fmt.Println("=== Water Treatment Plant IoT Monitoring (Norrvatten / Görvälnverket) ===")
	fmt.Printf("Treatment stages: %d\n", len(stages))
	totalSensors := 0
	for _, s := range stages {
		totalSensors += len(s.Sensors)
	}
	fmt.Printf("Total sensors: %d\n", totalSensors)
	fmt.Println()

	baseTime := time.Now()
	monitoringInterval := 30 // seconds
	numCycles := 6

	// Simulate anomaly events that trigger emergency DAGs
	anomalies := []AnomalyEvent{
		{
			SensorID:  "DIS-CL2-01",
			Stage:     "disinfection_distribution",
			Type:      "chlorine_residual",
			Value:     0.08,
			Threshold: 0.2,
			Severity:  "critical",
			Timestamp: baseTime.Add(45 * time.Second), // during cycle 2
		},
		{
			SensorID:  "FILT-PRES-01",
			Stage:     "filtration",
			Type:      "pressure_diff",
			Value:     38.5,
			Threshold: 35.0,
			Severity:  "warning",
			Timestamp: baseTime.Add(95 * time.Second), // during cycle 4
		},
		{
			SensorID:  "RAW-TURB-01",
			Stage:     "raw_intake",
			Type:      "turbidity",
			Value:     12.3,
			Threshold: 4.0,
			Severity:  "critical",
			Timestamp: baseTime.Add(155 * time.Second), // during cycle 6
		},
	}

	anomalyIdx := 0

	for cycle := 0; cycle < numCycles; cycle++ {
		cycleStart := baseTime.Add(time.Duration(cycle*monitoringInterval) * time.Second)
		cycleEnd := cycleStart.Add(time.Duration(monitoringInterval) * time.Second)
		cycleID := fmt.Sprintf("MON%03d", cycle+1)

		fmt.Printf("--- Monitoring Cycle %s: %s to %s ---\n",
			cycleID,
			cycleStart.Format("15:04:05"),
			cycleEnd.Format("15:04:05"),
		)

		// ============================================================
		// Normal monitoring DAG for each treatment stage
		// ============================================================
		for _, stage := range stages {
			workflow := core.CreateWorkflowSpec(colonyName)

			// Node 1: Collect sensor readings from PLCs via Modbus/OPC-UA
			collectSpec := core.CreateEmptyFunctionSpec()
			collectSpec.NodeName = fmt.Sprintf("collect_%s", stage.Name)
			collectSpec.FuncName = "collect_sensors"
			collectSpec.Conditions.ColonyName = colonyName
			collectSpec.Conditions.ExecutorType = "edge-iot"
			collectSpec.MaxExecTime = 5
			collectSpec.MaxWaitTime = 10
			collectSpec.Priority = 0 // normal priority
			collectSpec.Label = cycleID

			sensorIDs := make([]string, len(stage.Sensors))
			sensorConfigs := make([]map[string]interface{}, len(stage.Sensors))
			for i, s := range stage.Sensors {
				sensorIDs[i] = s.ID
				sensorConfigs[i] = map[string]interface{}{
					"id":              s.ID,
					"type":            s.Type,
					"unit":            s.Unit,
					"sample_rate_hz":  s.SampleRateHz,
					"min_safe":        s.MinSafe,
					"max_safe":        s.MaxSafe,
				}
			}

			collectSpec.KwArgs = map[string]interface{}{
				"cycle_id":     cycleID,
				"stage":        stage.Name,
				"gateway":      stage.Gateway,
				"location":     stage.Location,
				"protocol":     "opc-ua",
				"endpoint":     fmt.Sprintf("opc.tcp://%s.norr.local:4840", stage.Gateway),
				"sensors":      sensorConfigs,
				"window_start": cycleStart.Format(time.RFC3339),
				"window_end":   cycleEnd.Format(time.RFC3339),
			}
			workflow.AddFunctionSpec(collectSpec)

			// Node 2: Validate readings against safe ranges
			validateSpec := core.CreateEmptyFunctionSpec()
			validateSpec.NodeName = fmt.Sprintf("validate_%s", stage.Name)
			validateSpec.FuncName = "validate_readings"
			validateSpec.Conditions.ColonyName = colonyName
			validateSpec.Conditions.ExecutorType = "edge-iot"
			validateSpec.Conditions.Dependencies = []string{fmt.Sprintf("collect_%s", stage.Name)}
			validateSpec.MaxExecTime = 3
			validateSpec.MaxWaitTime = 10
			validateSpec.Priority = 0
			validateSpec.Label = cycleID
			validateSpec.KwArgs = map[string]interface{}{
				"cycle_id":    cycleID,
				"stage":       stage.Name,
				"sensor_ids":  sensorIDs,
				"validation":  map[string]interface{}{
					"check_range":       true,
					"check_rate_change": true,
					"max_rate_change":   0.1, // max 10% change per sample
					"check_flatline":    true,
					"flatline_duration": 60,  // seconds
				},
			}
			workflow.AddFunctionSpec(validateSpec)

			// Node 3: Aggregate metrics (rolling averages, trends)
			aggregateSpec := core.CreateEmptyFunctionSpec()
			aggregateSpec.NodeName = fmt.Sprintf("aggregate_%s", stage.Name)
			aggregateSpec.FuncName = "aggregate_metrics"
			aggregateSpec.Conditions.ColonyName = colonyName
			aggregateSpec.Conditions.ExecutorType = "edge-iot"
			aggregateSpec.Conditions.Dependencies = []string{fmt.Sprintf("validate_%s", stage.Name)}
			aggregateSpec.MaxExecTime = 3
			aggregateSpec.MaxWaitTime = 10
			aggregateSpec.Priority = 0
			aggregateSpec.Label = cycleID
			aggregateSpec.KwArgs = map[string]interface{}{
				"cycle_id":      cycleID,
				"stage":         stage.Name,
				"aggregations":  []string{"mean", "min", "max", "stddev", "trend_slope"},
				"window_sizes":  []int{30, 300, 3600},  // 30s, 5min, 1h rolling windows
				"output_target": "influxdb://timeseries.norr.local:8086/water_quality",
			}
			workflow.AddFunctionSpec(aggregateSpec)

			// Node 4: Update operator dashboard
			dashboardSpec := core.CreateEmptyFunctionSpec()
			dashboardSpec.NodeName = fmt.Sprintf("dashboard_%s", stage.Name)
			dashboardSpec.FuncName = "update_dashboard"
			dashboardSpec.Conditions.ColonyName = colonyName
			dashboardSpec.Conditions.ExecutorType = "edge-iot"
			dashboardSpec.Conditions.Dependencies = []string{fmt.Sprintf("aggregate_%s", stage.Name)}
			dashboardSpec.MaxExecTime = 2
			dashboardSpec.MaxWaitTime = 10
			dashboardSpec.Priority = 0
			dashboardSpec.Label = cycleID
			dashboardSpec.KwArgs = map[string]interface{}{
				"cycle_id":       cycleID,
				"stage":          stage.Name,
				"dashboard_url":  "https://grafana.norr.local/d/water-quality",
				"panel_id":       stage.Name,
				"retention_days": 90,
			}
			workflow.AddFunctionSpec(dashboardSpec)

			graph, err := c.SubmitWorkflowSpec(workflow, executorPrvKey)
			if err != nil {
				fmt.Printf("  ERROR submitting monitoring DAG for %s: %v\n", stage.Name, err)
				continue
			}
			fmt.Printf("  [NORMAL] %s monitoring: ProcessGraph %s (%d processes)\n",
				stage.Name, graph.ID, len(graph.ProcessIDs))
		}

		// ============================================================
		// Check if an anomaly occurs during this cycle
		// ============================================================
		for anomalyIdx < len(anomalies) &&
			anomalies[anomalyIdx].Timestamp.Before(cycleEnd) &&
			anomalies[anomalyIdx].Timestamp.After(cycleStart) {

			anomaly := anomalies[anomalyIdx]
			anomalyIdx++

			fmt.Printf("\n  *** ANOMALY DETECTED at %s ***\n", anomaly.Timestamp.Format("15:04:05"))
			fmt.Printf("  Sensor: %s (%s)\n", anomaly.SensorID, anomaly.Type)
			fmt.Printf("  Value: %.2f %s (threshold: %.2f)\n",
				anomaly.Value, anomaly.Type, anomaly.Threshold)
			fmt.Printf("  Severity: %s\n", anomaly.Severity)
			fmt.Printf("  Stage: %s\n", anomaly.Stage)

			emergencyID := fmt.Sprintf("EMRG-%s-%s", anomaly.SensorID, anomaly.Timestamp.Format("150405"))

			// ========================================================
			// Emergency response DAG - MUST preempt normal monitoring
			// ========================================================
			emergencyWorkflow := core.CreateWorkflowSpec(colonyName)

			// Priority is set to maximum (255) for critical, 200 for warning
			emergencyPriority := 200
			if anomaly.Severity == "critical" {
				emergencyPriority = 255
			}

			// Node 1: Emergency high-rate sensor collection
			emergCollect := core.CreateEmptyFunctionSpec()
			emergCollect.NodeName = "emergency_collect"
			emergCollect.FuncName = "emergency_collect"
			emergCollect.Conditions.ColonyName = colonyName
			emergCollect.Conditions.ExecutorType = "edge-iot"
			emergCollect.MaxExecTime = 1       // must complete within 1 second
			emergCollect.MaxWaitTime = 2        // hard real-time: max 2s wait
			emergCollect.Priority = emergencyPriority
			emergCollect.Label = emergencyID
			emergCollect.KwArgs = map[string]interface{}{
				"emergency_id":  emergencyID,
				"trigger_sensor": anomaly.SensorID,
				"trigger_value":  anomaly.Value,
				"trigger_threshold": anomaly.Threshold,
				"severity":       anomaly.Severity,
				"stage":          anomaly.Stage,
				"sample_rate_hz": 100, // burst mode: 100 Hz for all sensors in stage
				"duration_sec":   5,   // 5-second burst capture
				"preempt":        true, // signal to executor: preempt lower-priority work
				"latency_budget_ms": 500, // must start within 500ms
			}
			emergencyWorkflow.AddFunctionSpec(emergCollect)

			// Node 2: Classify severity and determine response
			classifySeverity := core.CreateEmptyFunctionSpec()
			classifySeverity.NodeName = "classify_severity"
			classifySeverity.FuncName = "classify_anomaly_severity"
			classifySeverity.Conditions.ColonyName = colonyName
			classifySeverity.Conditions.ExecutorType = "edge-iot"
			classifySeverity.Conditions.Dependencies = []string{"emergency_collect"}
			classifySeverity.MaxExecTime = 1
			classifySeverity.MaxWaitTime = 2
			classifySeverity.Priority = emergencyPriority
			classifySeverity.Label = emergencyID
			classifySeverity.KwArgs = map[string]interface{}{
				"emergency_id":  emergencyID,
				"stage":         anomaly.Stage,
				"classification_model": "water_anomaly_classifier_v3.onnx",
				"features": []string{
					"rate_of_change", "deviation_from_mean",
					"spatial_correlation", "temporal_pattern",
					"concurrent_alarms",
				},
				"severity_levels": map[string]interface{}{
					"critical": "immediate actuator response required",
					"warning":  "operator notification, prepare response",
					"info":     "log and continue monitoring",
				},
				"false_alarm_check": map[string]interface{}{
					"require_n_consecutive": 3,
					"cross_validate_sensors": true,
				},
			}
			emergencyWorkflow.AddFunctionSpec(classifySeverity)

			// Node 3a: Actuate response (for critical: close valve, adjust dosing)
			actuateSpec := core.CreateEmptyFunctionSpec()
			actuateSpec.NodeName = "actuate_response"
			actuateSpec.FuncName = "actuate_response"
			actuateSpec.Conditions.ColonyName = colonyName
			actuateSpec.Conditions.ExecutorType = "edge-iot"
			actuateSpec.Conditions.Dependencies = []string{"classify_severity"}
			actuateSpec.MaxExecTime = 1
			actuateSpec.MaxWaitTime = 1 // tightest latency budget
			actuateSpec.Priority = emergencyPriority
			actuateSpec.Label = emergencyID

			// Actuator commands depend on the type of anomaly
			actuatorCommands := map[string]interface{}{}
			switch anomaly.Type {
			case "chlorine_residual":
				actuatorCommands = map[string]interface{}{
					"action":       "increase_dosing",
					"target_valve": "CL2-DOSE-VALVE-01",
					"protocol":     "modbus-tcp",
					"endpoint":     fmt.Sprintf("modbus://%s.norr.local:502", "edge-gw-04"),
					"register":     40001,
					"current_dose": 1.2,
					"target_dose":  2.5,
					"ramp_rate":    0.5, // mg/L per minute
					"safety_interlock": map[string]interface{}{
						"max_dose":        4.0,
						"confirm_timeout": 5,
					},
				}
			case "pressure_diff":
				actuatorCommands = map[string]interface{}{
					"action":        "backwash_filter",
					"target_valve":  "FILT-BW-VALVE-03",
					"protocol":      "modbus-tcp",
					"sequence":      []string{"close_inlet", "open_backwash", "run_30min", "close_backwash", "open_inlet"},
					"estimated_min": 35,
				}
			case "turbidity":
				actuatorCommands = map[string]interface{}{
					"action":         "increase_coagulant",
					"target_valve":   "COAG-DOSE-PUMP-01",
					"current_dose":   25.0,
					"target_dose":    45.0,
					"also_alert":     "reduce_intake_flow",
					"intake_target":  0.7, // reduce to 70% of current flow
				}
			}
			actuateSpec.KwArgs = map[string]interface{}{
				"emergency_id":     emergencyID,
				"severity":         anomaly.Severity,
				"actuator_command": actuatorCommands,
				"require_confirmation": anomaly.Severity == "critical",
				"timeout_ms":       2000, // hard real-time deadline
			}
			emergencyWorkflow.AddFunctionSpec(actuateSpec)

			// Node 3b: Alert operators (parallel with actuate for critical)
			alertSpec := core.CreateEmptyFunctionSpec()
			alertSpec.NodeName = "alert_operators"
			alertSpec.FuncName = "alert_operators"
			alertSpec.Conditions.ColonyName = colonyName
			alertSpec.Conditions.ExecutorType = "edge-iot"
			alertSpec.Conditions.Dependencies = []string{"classify_severity"}
			alertSpec.MaxExecTime = 5
			alertSpec.MaxWaitTime = 10
			alertSpec.Priority = emergencyPriority - 10 // slightly lower than actuator
			alertSpec.Label = emergencyID
			alertSpec.KwArgs = map[string]interface{}{
				"emergency_id": emergencyID,
				"severity":     anomaly.Severity,
				"channels": map[string]interface{}{
					"scada": map[string]interface{}{
						"endpoint": "opc.tcp://scada.norr.local:4840",
						"alarm_id": fmt.Sprintf("WQ-%s", anomaly.SensorID),
						"priority": 1,
					},
					"sms": map[string]interface{}{
						"recipients": []string{"+46701234567", "+46709876543"},
						"template":   "ALERT: %s at %s. Value: %.2f (threshold: %.2f). Severity: %s",
					},
					"email": map[string]interface{}{
						"to":      []string{"driftcentral@norrvatten.se", "jour@norrvatten.se"},
						"subject": fmt.Sprintf("[%s] Water quality alert: %s", anomaly.Severity, anomaly.Type),
					},
				},
			}
			emergencyWorkflow.AddFunctionSpec(alertSpec)

			// Node 4: Notify SCADA and log incident (depends on both actuate + alert)
			notifyScada := core.CreateEmptyFunctionSpec()
			notifyScada.NodeName = "notify_scada"
			notifyScada.FuncName = "notify_scada"
			notifyScada.Conditions.ColonyName = colonyName
			notifyScada.Conditions.ExecutorType = "edge-iot"
			notifyScada.Conditions.Dependencies = []string{"actuate_response", "alert_operators"}
			notifyScada.MaxExecTime = 5
			notifyScada.MaxWaitTime = 30
			notifyScada.Priority = emergencyPriority - 20
			notifyScada.Label = emergencyID
			notifyScada.KwArgs = map[string]interface{}{
				"emergency_id":  emergencyID,
				"scada_system":  "ABB Ability Symphony Plus",
				"historian":     "osisoft-pi://historian.norr.local/waterquality",
				"log_retention": "10y", // regulatory requirement
			}
			emergencyWorkflow.AddFunctionSpec(notifyScada)

			// Node 5: Log full incident for regulatory compliance
			logIncident := core.CreateEmptyFunctionSpec()
			logIncident.NodeName = "log_incident"
			logIncident.FuncName = "log_incident"
			logIncident.Conditions.ColonyName = colonyName
			logIncident.Conditions.ExecutorType = "edge-iot"
			logIncident.Conditions.Dependencies = []string{"notify_scada"}
			logIncident.MaxExecTime = 10
			logIncident.MaxWaitTime = 60
			logIncident.Priority = emergencyPriority - 30
			logIncident.Label = emergencyID
			logIncident.KwArgs = map[string]interface{}{
				"emergency_id": emergencyID,
				"incident_report": map[string]interface{}{
					"trigger_sensor":    anomaly.SensorID,
					"trigger_value":     anomaly.Value,
					"trigger_threshold": anomaly.Threshold,
					"severity":          anomaly.Severity,
					"stage":             anomaly.Stage,
					"timestamp":         anomaly.Timestamp.Format(time.RFC3339Nano),
					"response_actions":  "automated",
				},
				"regulatory": map[string]interface{}{
					"authority":        "Livsmedelsverket",
					"regulation":       "SLVFS 2001:30",
					"notify_if_critical": true,
					"report_within_hours": 24,
				},
				"storage": map[string]interface{}{
					"primary":   "postgresql://incidents.norr.local/water_incidents",
					"archive":   "s3://norrvatten-incidents-archive/",
					"retention": "10 years",
				},
			}
			emergencyWorkflow.AddFunctionSpec(logIncident)

			graph, err := c.SubmitWorkflowSpec(emergencyWorkflow, executorPrvKey)
			if err != nil {
				fmt.Printf("  ERROR submitting emergency DAG: %v\n", err)
				continue
			}

			fmt.Printf("  [EMERGENCY] %s response: ProcessGraph %s (%d processes, priority=%d)\n",
				anomaly.Severity, graph.ID, len(graph.ProcessIDs), emergencyPriority)
			fmt.Printf("  *** Emergency DAG must PREEMPT %d queued normal monitoring DAGs ***\n\n",
				len(stages))
		}

		fmt.Println()
	}

	// Print summary
	fmt.Println("=== Summary ===")
	fmt.Printf("Monitoring cycles: %d (every %d seconds)\n", numCycles, monitoringInterval)
	fmt.Printf("Normal monitoring DAGs per cycle: %d (one per treatment stage)\n", len(stages))
	fmt.Printf("Total normal DAGs submitted: %d\n", numCycles*len(stages))
	fmt.Printf("Anomaly events: %d\n", len(anomalies))
	fmt.Println()
	fmt.Println("Normal monitoring DAG (per stage, priority=0):")
	fmt.Println("  collect_sensors → validate_readings → aggregate_metrics → update_dashboard")
	fmt.Println()
	fmt.Println("Emergency response DAG (priority=200-255, PREEMPTS normal):")
	fmt.Println("  emergency_collect → classify_severity → actuate_response → notify_scada → log_incident")
	fmt.Println("                                        ↘ alert_operators  ↗")
	fmt.Println()
	fmt.Println("Key preemption requirement:")
	fmt.Println("  Edge gateways have limited executor capacity (1-2 slots).")
	fmt.Println("  When an anomaly triggers, the emergency DAG MUST preempt")
	fmt.Println("  any queued or running normal monitoring processes.")
	fmt.Println("  Hard real-time constraint: anomaly → actuator within 2 seconds.")

	// Prevent unused import warning
	_ = rand.Intn
}
