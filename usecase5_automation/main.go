// Use Case 5: Cloud Burst for Seismic Processing
//
// The ColonyOS process queue for seismic processing (use case 1) has built up
// -- too many events to locate, too few on-premise executors. A cloud burst
// workflow is triggered to scale out:
//
//   1. Check queue depth and estimate required capacity
//   2. Provision cloud VMs (Exoscale/Hetzner)
//   3. Install K3s on each VM
//   4. Deploy seismic-processor executor containers via K3s
//   5. Transfer velocity model and calibration data via ColonyFS
//   6. Register executors with the colony and start processing
//   7. Monitor costs and queue depth
//   8. When queue drains, tear down cloud resources
//
// DAG Structure:
//
//   assess_queue ──> estimate_capacity ──> provision_vm_1 ──> install_k3s_1 ──> deploy_executor_1 ──┐
//                                     ──> provision_vm_2 ──> install_k3s_2 ──> deploy_executor_2 ──┤
//                                     ──> provision_vm_3 ──> install_k3s_3 ──> deploy_executor_3 ──┤
//                                                                                                  │
//                    transfer_velocity_model ──────────────────────────────────────────────────────┤
//                    transfer_calibration_data ────────────────────────────────────────────────────┤
//                                                                                                  │
//                    ┌─────────────────────────────────────────────────────────────────────────────┘
//                    │
//                    └──> start_executors ──> monitor_costs ──> monitor_queue ──> teardown

package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/colonyos/colonies/pkg/client"
	"github.com/colonyos/colonies/pkg/core"
)

type CloudVM struct {
	Name     string
	Size     string
	CPUs     int
	MemGB    int
	DiskGB   int
	CostPerH float64 // EUR per hour
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

	// Cloud burst configuration
	cloudProvider := "hetzner"
	region := "fsn1" // Falkenstein, Germany (closest to Kiruna with good pricing)
	burstID := "burst-seismic-20260313-001"

	// VMs to provision for the burst
	vms := []CloudVM{
		{Name: "seismic-burst-01", Size: "cpx31", CPUs: 4, MemGB: 8, DiskGB: 160, CostPerH: 0.0504},
		{Name: "seismic-burst-02", Size: "cpx31", CPUs: 4, MemGB: 8, DiskGB: 160, CostPerH: 0.0504},
		{Name: "seismic-burst-03", Size: "cpx31", CPUs: 4, MemGB: 8, DiskGB: 160, CostPerH: 0.0504},
	}

	// Executor container configuration
	executorImage := "colonyos/seismic-processor:v2.4.1"
	executorsPerVM := 2 // run 2 executor containers per VM

	// Cost limits
	maxBudgetEUR := 50.0
	maxHours := 8

	totalCostPerH := 0.0
	for _, vm := range vms {
		totalCostPerH += vm.CostPerH
	}

	fmt.Println("=== Cloud Burst - Seismic Processing Scale-Out ===")
	fmt.Printf("Burst ID: %s\n", burstID)
	fmt.Printf("Reason: Process queue backlog exceeds on-premise capacity\n")
	fmt.Printf("Cloud provider: %s, Region: %s\n", cloudProvider, region)
	fmt.Printf("VMs: %d x %s (%d vCPUs, %d GB RAM each)\n",
		len(vms), vms[0].Size, vms[0].CPUs, vms[0].MemGB)
	fmt.Printf("Executors per VM: %d (total: %d)\n", executorsPerVM, executorsPerVM*len(vms))
	fmt.Printf("Cost: EUR %.4f/h (total), max budget: EUR %.2f, max duration: %dh\n\n",
		totalCostPerH, maxBudgetEUR, maxHours)

	workflow := core.CreateWorkflowSpec(colonyName)

	// ================================================================
	// Stage 1: Assess current queue depth
	// ================================================================
	assessSpec := core.CreateEmptyFunctionSpec()
	assessSpec.NodeName = "assess_queue"
	assessSpec.FuncName = "assess_queue_depth"
	assessSpec.Conditions.ColonyName = colonyName
	assessSpec.Conditions.ExecutorType = "infra-provisioner"
	assessSpec.MaxExecTime = 30
	assessSpec.MaxWaitTime = 60
	assessSpec.Priority = 50 // high priority -- this is urgent
	assessSpec.KwArgs = map[string]interface{}{
		"burst_id":      burstID,
		"colony_name":   colonyName,
		"executor_type": "seismic-processor",
		"functions":     []string{"pick_pwave", "classify_events", "locate_event_3d"},
		"metrics": []string{
			"waiting_count",
			"running_count",
			"avg_wait_time_sec",
			"p95_wait_time_sec",
			"throughput_per_min",
		},
	}
	workflow.AddFunctionSpec(assessSpec)

	// ================================================================
	// Stage 2: Estimate required capacity
	// ================================================================
	estimateSpec := core.CreateEmptyFunctionSpec()
	estimateSpec.NodeName = "estimate_capacity"
	estimateSpec.FuncName = "estimate_burst_capacity"
	estimateSpec.Conditions.ColonyName = colonyName
	estimateSpec.Conditions.ExecutorType = "infra-provisioner"
	estimateSpec.Conditions.Dependencies = []string{"assess_queue"}
	estimateSpec.MaxExecTime = 30
	estimateSpec.MaxWaitTime = 60
	estimateSpec.Priority = 50
	estimateSpec.KwArgs = map[string]interface{}{
		"burst_id":        burstID,
		"target_drain_hours": 2,
		"avg_process_time_sec": map[string]interface{}{
			"pick_pwave":       5,
			"classify_events":  15,
			"locate_event_3d":  30,
		},
		"max_budget_eur":   maxBudgetEUR,
		"max_duration_hours": maxHours,
		"vm_options": []map[string]interface{}{
			{"size": "cpx21", "cpus": 3, "mem_gb": 4, "cost_per_h": 0.0252},
			{"size": "cpx31", "cpus": 4, "mem_gb": 8, "cost_per_h": 0.0504},
			{"size": "cpx41", "cpus": 8, "mem_gb": 16, "cost_per_h": 0.1008},
		},
		"executors_per_vm": executorsPerVM,
	}
	workflow.AddFunctionSpec(estimateSpec)

	// ================================================================
	// Stage 3: Provision cloud VMs (parallel)
	// ================================================================
	for _, vm := range vms {
		provisionSpec := core.CreateEmptyFunctionSpec()
		provisionSpec.NodeName = fmt.Sprintf("provision_%s", vm.Name)
		provisionSpec.FuncName = "provision_cloud_vm"
		provisionSpec.Conditions.ColonyName = colonyName
		provisionSpec.Conditions.ExecutorType = "infra-provisioner"
		provisionSpec.Conditions.Dependencies = []string{"estimate_capacity"}
		provisionSpec.MaxExecTime = 300
		provisionSpec.MaxWaitTime = 120
		provisionSpec.Priority = 50
		provisionSpec.KwArgs = map[string]interface{}{
			"burst_id":       burstID,
			"cloud_provider": cloudProvider,
			"region":         region,
			"vm": map[string]interface{}{
				"name":     vm.Name,
				"size":     vm.Size,
				"cpus":     vm.CPUs,
				"mem_gb":   vm.MemGB,
				"disk_gb":  vm.DiskGB,
				"image":    "ubuntu-24.04",
				"ssh_keys": []string{"colonyos-deploy"},
				"labels": map[string]string{
					"purpose":  "cloud-burst",
					"burst_id": burstID,
					"colony":   colonyName,
				},
			},
			"firewall": map[string]interface{}{
				"name": fmt.Sprintf("%s-fw", burstID),
				"rules": []map[string]interface{}{
					{"port": 22, "protocol": "tcp", "source": "10.0.0.0/8"},
					{"port": 6443, "protocol": "tcp", "source": "10.0.0.0/8"},
					{"port": "30000-32767", "protocol": "tcp", "source": "0.0.0.0/0"},
				},
			},
		}
		workflow.AddFunctionSpec(provisionSpec)
	}

	// ================================================================
	// Stage 4: Install K3s on each VM (parallel, each depends on its VM)
	// ================================================================
	for _, vm := range vms {
		k3sSpec := core.CreateEmptyFunctionSpec()
		k3sSpec.NodeName = fmt.Sprintf("install_k3s_%s", vm.Name)
		k3sSpec.FuncName = "install_k3s"
		k3sSpec.Conditions.ColonyName = colonyName
		k3sSpec.Conditions.ExecutorType = "config-manager"
		k3sSpec.Conditions.Dependencies = []string{fmt.Sprintf("provision_%s", vm.Name)}
		k3sSpec.MaxExecTime = 600
		k3sSpec.MaxWaitTime = 300
		k3sSpec.Priority = 50
		k3sSpec.KwArgs = map[string]interface{}{
			"burst_id": burstID,
			"vm_name":  vm.Name,
			"k3s": map[string]interface{}{
				"version":        "v1.31.4+k3s1",
				"mode":           "standalone",
				"disable":        []string{"traefik", "servicelb", "local-storage"},
				"kubelet_args":   []string{"--max-pods=50"},
				"write_kubeconfig_mode": "0644",
			},
			"post_install": []string{
				"kubectl create namespace seismic",
				"kubectl label namespace seismic purpose=cloud-burst",
			},
		}
		workflow.AddFunctionSpec(k3sSpec)
	}

	// ================================================================
	// Stage 5: Deploy seismic-processor executor containers (parallel)
	// ================================================================
	deployNodeNames := []string{}
	for _, vm := range vms {
		deploySpec := core.CreateEmptyFunctionSpec()
		deployName := fmt.Sprintf("deploy_executor_%s", vm.Name)
		deployNodeNames = append(deployNodeNames, deployName)
		deploySpec.NodeName = deployName
		deploySpec.FuncName = "deploy_executor_k3s"
		deploySpec.Conditions.ColonyName = colonyName
		deploySpec.Conditions.ExecutorType = "config-manager"
		deploySpec.Conditions.Dependencies = []string{fmt.Sprintf("install_k3s_%s", vm.Name)}
		deploySpec.MaxExecTime = 600
		deploySpec.MaxWaitTime = 300
		deploySpec.Priority = 50
		deploySpec.KwArgs = map[string]interface{}{
			"burst_id":  burstID,
			"vm_name":   vm.Name,
			"namespace": "seismic",
			"executor": map[string]interface{}{
				"image":    executorImage,
				"replicas": executorsPerVM,
				"resources": map[string]interface{}{
					"requests": map[string]string{"cpu": "1500m", "memory": "2Gi"},
					"limits":   map[string]string{"cpu": "2000m", "memory": "3Gi"},
				},
				"env": map[string]string{
					"COLONIES_SERVER_HOST":   host,
					"COLONIES_SERVER_PORT":   portStr,
					"COLONIES_COLONY_NAME":   colonyName,
					"COLONIES_EXECUTOR_TYPE": "seismic-processor",
					"EXECUTOR_BURST_ID":      burstID,
				},
				"env_secrets": []map[string]string{
					{"name": "COLONIES_EXECUTOR_PRVKEY", "secret": "seismic-executor-key", "key": "prvkey"},
				},
			},
			"health_check": map[string]interface{}{
				"initial_delay_sec": 15,
				"period_sec":        10,
				"endpoint":          "/health",
			},
		}
		workflow.AddFunctionSpec(deploySpec)
	}

	// ================================================================
	// Stage 5b: Transfer velocity model and calibration data via ColonyFS
	// (runs in parallel with VM provisioning, needed before executors start)
	// ================================================================
	transferModelSpec := core.CreateEmptyFunctionSpec()
	transferModelSpec.NodeName = "transfer_velocity_model"
	transferModelSpec.FuncName = "transfer_colonyfs_data"
	transferModelSpec.Conditions.ColonyName = colonyName
	transferModelSpec.Conditions.ExecutorType = "data-mover"
	transferModelSpec.Conditions.Dependencies = []string{"estimate_capacity"}
	transferModelSpec.MaxExecTime = 600
	transferModelSpec.MaxWaitTime = 120
	transferModelSpec.Priority = 50
	transferModelSpec.KwArgs = map[string]interface{}{
		"burst_id":  burstID,
		"transfers": []map[string]interface{}{
			{
				"source":      "colonyfs:///models/LKAB-Kiruna-3D-v4/",
				"destination": "colonyfs:///burst/" + burstID + "/models/",
				"description": "3D P/S-wave velocity model for Kiruna mine",
				"size_est_mb": 250,
			},
			{
				"source":      "colonyfs:///models/LKAB-Kiruna-3D-v4/grid/",
				"destination": "colonyfs:///burst/" + burstID + "/models/grid/",
				"description": "Pre-computed travel time grids",
				"size_est_mb": 1200,
			},
		},
	}
	workflow.AddFunctionSpec(transferModelSpec)

	transferCalibSpec := core.CreateEmptyFunctionSpec()
	transferCalibSpec.NodeName = "transfer_calibration_data"
	transferCalibSpec.FuncName = "transfer_colonyfs_data"
	transferCalibSpec.Conditions.ColonyName = colonyName
	transferCalibSpec.Conditions.ExecutorType = "data-mover"
	transferCalibSpec.Conditions.Dependencies = []string{"estimate_capacity"}
	transferCalibSpec.MaxExecTime = 300
	transferCalibSpec.MaxWaitTime = 120
	transferCalibSpec.Priority = 50
	transferCalibSpec.KwArgs = map[string]interface{}{
		"burst_id":  burstID,
		"transfers": []map[string]interface{}{
			{
				"source":      "colonyfs:///calibration/geophones/",
				"destination": "colonyfs:///burst/" + burstID + "/calibration/geophones/",
				"description": "Geophone response curves and orientation corrections",
				"size_est_mb": 15,
			},
			{
				"source":      "colonyfs:///models/rocksigma_event_classifier_v6.onnx",
				"destination": "colonyfs:///burst/" + burstID + "/models/classifier.onnx",
				"description": "ML event classifier model",
				"size_est_mb": 45,
			},
			{
				"source":      "colonyfs:///calibration/magnitude/LKAB-Kiruna-ML-v2.json",
				"destination": "colonyfs:///burst/" + burstID + "/calibration/magnitude.json",
				"description": "Local magnitude calibration parameters",
				"size_est_mb": 1,
			},
		},
	}
	workflow.AddFunctionSpec(transferCalibSpec)

	// ================================================================
	// Stage 6: Start executors (wait for both deploys and data transfers)
	// ================================================================
	startDeps := append(deployNodeNames, "transfer_velocity_model", "transfer_calibration_data")
	startSpec := core.CreateEmptyFunctionSpec()
	startSpec.NodeName = "start_executors"
	startSpec.FuncName = "start_burst_executors"
	startSpec.Conditions.ColonyName = colonyName
	startSpec.Conditions.ExecutorType = "config-manager"
	startSpec.Conditions.Dependencies = startDeps
	startSpec.MaxExecTime = 300
	startSpec.MaxWaitTime = 120
	startSpec.Priority = 50
	startSpec.KwArgs = map[string]interface{}{
		"burst_id":    burstID,
		"colony_name": colonyName,
		"register_executors": true,
		"executor_names": []string{
			"seismic-burst-01-a", "seismic-burst-01-b",
			"seismic-burst-02-a", "seismic-burst-02-b",
			"seismic-burst-03-a", "seismic-burst-03-b",
		},
		"executor_type":  "seismic-processor",
		"functions":      []string{"pick_pwave", "classify_events", "locate_event_3d"},
		"data_paths": map[string]interface{}{
			"velocity_model": "colonyfs:///burst/" + burstID + "/models/",
			"classifier":     "colonyfs:///burst/" + burstID + "/models/classifier.onnx",
			"calibration":    "colonyfs:///burst/" + burstID + "/calibration/",
		},
		"verify_assignment": true,
	}
	workflow.AddFunctionSpec(startSpec)

	// ================================================================
	// Stage 7: Monitor costs
	// ================================================================
	costSpec := core.CreateEmptyFunctionSpec()
	costSpec.NodeName = "monitor_costs"
	costSpec.FuncName = "monitor_burst_costs"
	costSpec.Conditions.ColonyName = colonyName
	costSpec.Conditions.ExecutorType = "infra-provisioner"
	costSpec.Conditions.Dependencies = []string{"start_executors"}
	costSpec.MaxExecTime = 600
	costSpec.MaxWaitTime = 120
	costSpec.Priority = 40
	costSpec.KwArgs = map[string]interface{}{
		"burst_id":       burstID,
		"cloud_provider": cloudProvider,
		"vms":            []string{"seismic-burst-01", "seismic-burst-02", "seismic-burst-03"},
		"cost_per_hour":  totalCostPerH,
		"max_budget_eur": maxBudgetEUR,
		"alert_thresholds": map[string]interface{}{
			"warn_pct":  70,
			"critical_pct": 90,
		},
		"report_to": []string{"gruvdrift@lkab.com", "ops@rocksigma.se"},
		"pricing": map[string]interface{}{
			"vm_cost_per_h":     totalCostPerH,
			"bandwidth_per_gb":  0.01,
			"storage_per_gb_mo": 0.0440,
		},
	}
	workflow.AddFunctionSpec(costSpec)

	// ================================================================
	// Stage 8: Monitor queue depth and decide when to tear down
	// ================================================================
	monitorQueueSpec := core.CreateEmptyFunctionSpec()
	monitorQueueSpec.NodeName = "monitor_queue"
	monitorQueueSpec.FuncName = "monitor_burst_queue"
	monitorQueueSpec.Conditions.ColonyName = colonyName
	monitorQueueSpec.Conditions.ExecutorType = "infra-provisioner"
	monitorQueueSpec.Conditions.Dependencies = []string{"monitor_costs"}
	monitorQueueSpec.MaxExecTime = 3600 // runs until queue drains or budget exhausted
	monitorQueueSpec.MaxWaitTime = 120
	monitorQueueSpec.Priority = 40
	monitorQueueSpec.KwArgs = map[string]interface{}{
		"burst_id":       burstID,
		"colony_name":    colonyName,
		"executor_type":  "seismic-processor",
		"check_interval_sec": 60,
		"teardown_conditions": map[string]interface{}{
			"queue_depth_below":   5,
			"sustained_minutes":   10,
			"budget_exceeded":     true,
			"max_duration_exceeded": true,
		},
		"max_duration_hours": maxHours,
		"max_budget_eur":     maxBudgetEUR,
		"metrics_output":     "colonyfs:///burst/" + burstID + "/metrics/queue_depth.csv",
	}
	workflow.AddFunctionSpec(monitorQueueSpec)

	// ================================================================
	// Stage 9: Tear down cloud resources
	// ================================================================
	teardownSpec := core.CreateEmptyFunctionSpec()
	teardownSpec.NodeName = "teardown"
	teardownSpec.FuncName = "teardown_burst"
	teardownSpec.Conditions.ColonyName = colonyName
	teardownSpec.Conditions.ExecutorType = "infra-provisioner"
	teardownSpec.Conditions.Dependencies = []string{"monitor_queue"}
	teardownSpec.MaxExecTime = 600
	teardownSpec.MaxWaitTime = 120
	teardownSpec.Priority = 50
	teardownSpec.KwArgs = map[string]interface{}{
		"burst_id":       burstID,
		"cloud_provider": cloudProvider,
		"actions": []map[string]interface{}{
			{
				"action":      "unregister_executors",
				"colony_name": colonyName,
				"executor_names": []string{
					"seismic-burst-01-a", "seismic-burst-01-b",
					"seismic-burst-02-a", "seismic-burst-02-b",
					"seismic-burst-03-a", "seismic-burst-03-b",
				},
			},
			{
				"action": "delete_vms",
				"vms":    []string{"seismic-burst-01", "seismic-burst-02", "seismic-burst-03"},
			},
			{
				"action":   "delete_firewall",
				"firewall": fmt.Sprintf("%s-fw", burstID),
			},
			{
				"action":     "cleanup_colonyfs",
				"path":       "colonyfs:///burst/" + burstID + "/",
				"keep_metrics": true,
			},
		},
		"generate_report": map[string]interface{}{
			"output":           "colonyfs:///burst/" + burstID + "/report.json",
			"include_costs":    true,
			"include_metrics":  true,
			"include_timeline": true,
			"notify":           []string{"gruvdrift@lkab.com", "ops@rocksigma.se"},
		},
	}
	workflow.AddFunctionSpec(teardownSpec)

	// Submit workflow
	c := client.CreateColoniesClient(host, port, true, false)
	graph, err := c.SubmitWorkflowSpec(workflow, executorPrvKey)
	if err != nil {
		fmt.Printf("ERROR submitting workflow: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Submitted cloud burst workflow\n")
	fmt.Printf("  ProcessGraph ID: %s\n", graph.ID)
	fmt.Printf("  Total processes: %d\n", len(graph.ProcessIDs))
	fmt.Printf("  Roots: %d\n", len(graph.Roots))
	fmt.Println("\nDAG structure:")
	fmt.Println("  Stage 1 - Assess queue:        check backlog depth + wait times")
	fmt.Println("  Stage 2 - Estimate capacity:    calculate VMs needed within budget")
	fmt.Println("  Stage 3 - Provision VMs:        3 Hetzner VMs in parallel")
	fmt.Println("  Stage 4 - Install K3s:          K3s on each VM (parallel)")
	fmt.Println("  Stage 5 - Deploy executors:     2 executor containers per VM (parallel)")
	fmt.Println("          + Transfer data:        velocity model + calibration via ColonyFS (parallel)")
	fmt.Println("  Stage 6 - Start executors:      register with colony, verify assignment")
	fmt.Println("  Stage 7 - Monitor costs:        track spend against EUR 50 budget")
	fmt.Println("  Stage 8 - Monitor queue:        wait until backlog drains or budget exhausted")
	fmt.Println("  Stage 9 - Teardown:             unregister executors, delete VMs, cleanup, report")
}
