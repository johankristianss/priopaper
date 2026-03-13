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

	c := client.CreateColoniesClient(host, port, true, false)

	fmt.Println("Dummy executor started, waiting for process assignments...")
	fmt.Printf("  Colony: %s\n", colonyName)
	fmt.Printf("  Server: %s:%d\n", host, port)
	fmt.Println()

	for {
		process, err := c.Assign(colonyName, 30, "", "", executorPrvKey)
		if err != nil {
			continue
		}

		funcName := process.FunctionSpec.FuncName
		nodeName := process.FunctionSpec.NodeName
		graphID := process.ProcessGraphID

		fmt.Printf("[%s] Assigned process %s\n", time.Now().Format("15:04:05"), process.ID[:12])
		fmt.Printf("  Function: %s  Node: %s\n", funcName, nodeName)
		if graphID != "" {
			fmt.Printf("  Graph: %s\n", graphID[:12])
		}

		if len(process.FunctionSpec.Args) > 0 {
			fmt.Printf("  Args: %v\n", process.FunctionSpec.Args)
		}
		if len(process.FunctionSpec.KwArgs) > 0 {
			fmt.Printf("  KwArgs: %v\n", process.FunctionSpec.KwArgs)
		}
		if len(process.Input) > 0 {
			fmt.Printf("  Input from parents: %v\n", process.Input)
		}

		// Simulate work with variable duration based on function name
		duration := simulateWork(funcName)
		fmt.Printf("  Simulating work for %v...\n", duration)
		time.Sleep(duration)

		// Generate dummy output based on function type
		output := generateOutput(funcName, nodeName, process.FunctionSpec.KwArgs)

		err = c.CloseWithOutput(process.ID, output, executorPrvKey)
		if err != nil {
			fmt.Printf("  ERROR closing process: %v\n", err)
			_ = c.Fail(process.ID, []string{err.Error()}, executorPrvKey)
			continue
		}

		fmt.Printf("  Completed with output: %v\n\n", output)
	}
}

func simulateWork(funcName string) time.Duration {
	base := map[string]int{
		// Seismic
		"ingest_seismogram":   200,
		"detect_triggers":     500,
		"group_events":        300,
		"locate_event":        800,
		// Earth observation
		"fetch_sentinel2":     1000,
		"generate_datacube":   1500,
		"detect_anomalies":    2000,
		"generate_alert":      100,
		// Agentic AI
		"parse_prompt":        300,
		"identify_tasks":      500,
		"generate_dag":        800,
		"validate_dag":        200,
		"execute_task":        1000,
		"evaluate_results":    400,
		// ETL
		"ingest_data":         500,
		"clean_data":          800,
		"transform_data":      1000,
		"aggregate_data":      600,
		"store_results":       300,
		"trigger_downstream":  100,
		// Automation
		"provision_vms":       2000,
		"configure_os":        1500,
		"deploy_kubernetes":   3000,
		"deploy_application":  1000,
		"validate_deployment": 500,
		"output_credentials":  100,
		// HPC ML
		"prepare_dataset":     1000,
		"train_model":         3000,
		"evaluate_model":      500,
		"package_model":       300,
		"push_to_registry":    500,
		"deploy_inference":    1000,
		"setup_monitoring":    300,
		// Parameter sweep
		"generate_parameters": 200,
		"run_simulation":      1500,
		"collect_results":     300,
		"postprocess":         800,
		"aggregate_sweep":     500,
		"generate_report":     400,
		// IoT edge monitoring
		"collect_sensors":             300,
		"validate_readings":           150,
		"aggregate_metrics":           200,
		"update_dashboard":            100,
		"emergency_collect":           100,
		"classify_anomaly_severity":   150,
		"actuate_response":            50,
		"alert_operators":             200,
		"notify_scada":                150,
		"log_incident":                300,
	}

	ms := 500
	if v, ok := base[funcName]; ok {
		ms = v
	}

	// Add some randomness
	jitter := rand.Intn(ms/2 + 1)
	return time.Duration(ms+jitter) * time.Millisecond
}

func generateOutput(funcName, nodeName string, kwargs map[string]interface{}) []interface{} {
	switch funcName {
	case "ingest_seismogram":
		station := ""
		if s, ok := kwargs["station"]; ok {
			station = fmt.Sprintf("%v", s)
		}
		return []interface{}{
			map[string]interface{}{
				"station":      station,
				"samples":      rand.Intn(10000) + 5000,
				"sample_rate":  100,
				"duration_sec": rand.Intn(60) + 30,
			},
		}
	case "detect_triggers":
		n := rand.Intn(5)
		triggers := make([]map[string]interface{}, n)
		for i := 0; i < n; i++ {
			triggers[i] = map[string]interface{}{
				"trigger_time": time.Now().Add(-time.Duration(rand.Intn(3600)) * time.Second).Format(time.RFC3339),
				"amplitude":    rand.Float64() * 10,
				"snr":          rand.Float64()*20 + 5,
			}
		}
		return []interface{}{triggers}
	case "locate_event":
		return []interface{}{
			map[string]interface{}{
				"latitude":   58.0 + rand.Float64()*5,
				"longitude":  12.0 + rand.Float64()*8,
				"depth_km":   rand.Float64() * 30,
				"magnitude":  rand.Float64()*4 + 0.5,
				"confidence": rand.Float64()*0.3 + 0.7,
			},
		}
	case "detect_anomalies":
		return []interface{}{
			map[string]interface{}{
				"anomalies_found": rand.Intn(10),
				"ndvi_change":     -rand.Float64() * 0.5,
				"area_affected_ha": rand.Float64() * 100,
			},
		}
	case "train_model":
		return []interface{}{
			map[string]interface{}{
				"loss":       rand.Float64()*0.5 + 0.01,
				"accuracy":   rand.Float64()*0.3 + 0.7,
				"epochs":     100,
				"model_path": "/models/best_model.pt",
			},
		}
	case "run_simulation":
		return []interface{}{
			map[string]interface{}{
				"converged":  rand.Float64() > 0.1,
				"iterations": rand.Intn(1000) + 100,
				"result":     rand.Float64() * 100,
			},
		}
	case "emergency_collect":
		return []interface{}{
			map[string]interface{}{
				"samples_captured": 500,
				"sample_rate_hz":   100,
				"duration_sec":     5,
				"latency_ms":       rand.Intn(200) + 50,
			},
		}
	case "classify_anomaly_severity":
		severities := []string{"critical", "warning", "info"}
		return []interface{}{
			map[string]interface{}{
				"confirmed_severity": severities[rand.Intn(len(severities))],
				"confidence":         rand.Float64()*0.3 + 0.7,
				"false_alarm":        rand.Float64() < 0.1,
			},
		}
	case "actuate_response":
		return []interface{}{
			map[string]interface{}{
				"actuator_confirmed": true,
				"response_time_ms":   rand.Intn(500) + 100,
				"action_taken":       "dosing_adjusted",
			},
		}
	default:
		return []interface{}{
			map[string]interface{}{
				"status":    "completed",
				"node":      nodeName,
				"timestamp": time.Now().Format(time.RFC3339),
			},
		}
	}
}

func init() {
	rand.New(rand.NewSource(time.Now().UnixNano()))
}

// Ensure core is used
var _ = core.WAITING
