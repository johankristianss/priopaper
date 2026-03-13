// Use Case 6: Cloud-to-HPC LLM Training Pipeline
//
// A user on a cloud platform wants to fine-tune an LLM for Swedish legal text.
// The training data is prepared in the cloud, transferred to an HPC system
// (Dardel at PDC/KTH) via ColonyOS, the model is trained on GPU nodes,
// then the trained model is transferred back to the cloud for deployment.
//
// The workflow spans two execution environments:
//   - Cloud (Kubernetes): data preparation, model registry, deployment, monitoring
//   - HPC (Dardel/SLURM): GPU training, evaluation
//
// ColonyOS bridges them -- executors on both sides pull work from the same colony.
// ColonyFS handles data transfer transparently.
//
// DAG Structure:
//
//   validate_dataset ──> prepare_tokenized ──> upload_to_hpc ──┐
//                                                               │
//   download_base_model ──> convert_format ──> upload_model ───┤
//                                                               │
//   ┌───────────────────────────────────────────────────────────┘
//   │
//   └──> train_model ──> evaluate_model ──> download_from_hpc ──> register_model ──> deploy_inference ──> validate_endpoint

package main

import (
	"fmt"
	"os"
	"strconv"

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

	// Training job configuration
	jobID := "train-llm-legal-sv-20260313"
	baseModel := "meta-llama/Llama-3.1-8B-Instruct"
	hpcSystem := "dardel"
	hpcAccount := "snic2026-1-42"
	hpcPartition := "gpu"

	fmt.Println("=== Cloud-to-HPC LLM Training Pipeline ===")
	fmt.Printf("Job ID: %s\n", jobID)
	fmt.Printf("Base model: %s\n", baseModel)
	fmt.Printf("Task: Fine-tune for Swedish legal text (court rulings, legislation, contracts)\n")
	fmt.Printf("Cloud: Kubernetes cluster (data prep, deployment)\n")
	fmt.Printf("HPC: %s at PDC/KTH (training, evaluation)\n\n", hpcSystem)

	workflow := core.CreateWorkflowSpec(colonyName)

	// ================================================================
	// CLOUD SIDE: Prepare training data
	// ================================================================

	// Step 1a: Validate and clean the dataset
	validateSpec := core.CreateEmptyFunctionSpec()
	validateSpec.NodeName = "validate_dataset"
	validateSpec.FuncName = "validate_dataset"
	validateSpec.Conditions.ColonyName = colonyName
	validateSpec.Conditions.ExecutorType = "cloud-worker"
	validateSpec.Conditions.Memory = "16Gi"
	validateSpec.MaxExecTime = 1800
	validateSpec.MaxWaitTime = 300
	validateSpec.Priority = 10
	validateSpec.KwArgs = map[string]interface{}{
		"job_id": jobID,
		"sources": []map[string]interface{}{
			{
				"name":        "swedish_court_rulings",
				"path":        "colonyfs:///datasets/legal-sv/court_rulings/",
				"format":      "jsonl",
				"records_est": 125000,
				"description": "Supreme Court and Appeals Court rulings 2000-2025",
			},
			{
				"name":        "swedish_legislation",
				"path":        "colonyfs:///datasets/legal-sv/sfs/",
				"format":      "jsonl",
				"records_est": 45000,
				"description": "Svensk Forfattningssamling (SFS) statutes",
			},
			{
				"name":        "legal_contracts",
				"path":        "colonyfs:///datasets/legal-sv/contracts/",
				"format":      "jsonl",
				"records_est": 30000,
				"description": "Anonymized commercial contracts",
			},
		},
		"validation": map[string]interface{}{
			"check_encoding":      "utf-8",
			"check_language":      "sv",
			"min_text_length":     100,
			"max_text_length":     32000,
			"dedup_method":        "minhash",
			"dedup_threshold":     0.85,
			"pii_detection":       true,
			"pii_action":          "redact",
			"quality_filter":      true,
			"quality_model":       "colonyfs:///models/text_quality_sv_v2.onnx",
			"quality_threshold":   0.6,
		},
		"output": "colonyfs:///jobs/" + jobID + "/validated/",
	}
	workflow.AddFunctionSpec(validateSpec)

	// Step 1b: Tokenize and prepare for training
	tokenizeSpec := core.CreateEmptyFunctionSpec()
	tokenizeSpec.NodeName = "prepare_tokenized"
	tokenizeSpec.FuncName = "prepare_training_data"
	tokenizeSpec.Conditions.ColonyName = colonyName
	tokenizeSpec.Conditions.ExecutorType = "cloud-worker"
	tokenizeSpec.Conditions.Memory = "32Gi"
	tokenizeSpec.Conditions.CPU = "8"
	tokenizeSpec.MaxExecTime = 3600
	tokenizeSpec.MaxWaitTime = 600
	tokenizeSpec.Priority = 10
	tokenizeSpec.Conditions.Dependencies = []string{"validate_dataset"}
	tokenizeSpec.KwArgs = map[string]interface{}{
		"job_id":       jobID,
		"input":        "colonyfs:///jobs/" + jobID + "/validated/",
		"tokenizer":    baseModel,
		"max_seq_len":  4096,
		"packing":      true,
		"train_split":  0.95,
		"val_split":    0.05,
		"format":       "instruction_tuning",
		"chat_template": "llama3",
		"columns": map[string]interface{}{
			"instruction": "instruction",
			"input":       "context",
			"output":      "response",
		},
		"output": map[string]interface{}{
			"train": "colonyfs:///jobs/" + jobID + "/tokenized/train/",
			"val":   "colonyfs:///jobs/" + jobID + "/tokenized/val/",
			"meta":  "colonyfs:///jobs/" + jobID + "/tokenized/meta.json",
		},
	}
	workflow.AddFunctionSpec(tokenizeSpec)

	// Step 1c: Upload tokenized data to HPC via ColonyFS
	uploadDataSpec := core.CreateEmptyFunctionSpec()
	uploadDataSpec.NodeName = "upload_to_hpc"
	uploadDataSpec.FuncName = "transfer_to_hpc"
	uploadDataSpec.Conditions.ColonyName = colonyName
	uploadDataSpec.Conditions.ExecutorType = "hpc-data-mover"
	uploadDataSpec.Conditions.Dependencies = []string{"prepare_tokenized"}
	uploadDataSpec.MaxExecTime = 7200
	uploadDataSpec.MaxWaitTime = 600
	uploadDataSpec.Priority = 10
	uploadDataSpec.KwArgs = map[string]interface{}{
		"job_id":    jobID,
		"direction": "cloud_to_hpc",
		"transfers": []map[string]interface{}{
			{
				"source":       "colonyfs:///jobs/" + jobID + "/tokenized/",
				"destination":  "/cfs/klemming/projects/" + hpcAccount + "/llm-legal/" + jobID + "/data/",
				"size_est_gb":  85,
				"description":  "Tokenized training data",
			},
		},
		"hpc": map[string]interface{}{
			"system":  hpcSystem,
			"account": hpcAccount,
		},
		"verify_checksum": true,
	}
	workflow.AddFunctionSpec(uploadDataSpec)

	// ================================================================
	// CLOUD SIDE: Prepare base model (parallel with data prep)
	// ================================================================

	// Step 2a: Download base model from HuggingFace
	downloadModelSpec := core.CreateEmptyFunctionSpec()
	downloadModelSpec.NodeName = "download_base_model"
	downloadModelSpec.FuncName = "download_model"
	downloadModelSpec.Conditions.ColonyName = colonyName
	downloadModelSpec.Conditions.ExecutorType = "cloud-worker"
	downloadModelSpec.Conditions.Memory = "32Gi"
	downloadModelSpec.Conditions.Storage = "100Gi"
	downloadModelSpec.MaxExecTime = 3600
	downloadModelSpec.MaxWaitTime = 300
	downloadModelSpec.Priority = 10
	downloadModelSpec.KwArgs = map[string]interface{}{
		"job_id":   jobID,
		"source":   "huggingface",
		"model_id": baseModel,
		"revision": "main",
		"files":    []string{"*.safetensors", "config.json", "tokenizer*", "special_tokens*"},
		"output":   "colonyfs:///jobs/" + jobID + "/base_model/",
	}
	workflow.AddFunctionSpec(downloadModelSpec)

	// Step 2b: Convert model to training format
	convertSpec := core.CreateEmptyFunctionSpec()
	convertSpec.NodeName = "convert_format"
	convertSpec.FuncName = "convert_model_format"
	convertSpec.Conditions.ColonyName = colonyName
	convertSpec.Conditions.ExecutorType = "cloud-worker"
	convertSpec.Conditions.Memory = "64Gi"
	convertSpec.Conditions.Dependencies = []string{"download_base_model"}
	convertSpec.MaxExecTime = 1800
	convertSpec.MaxWaitTime = 600
	convertSpec.Priority = 10
	convertSpec.KwArgs = map[string]interface{}{
		"job_id":        jobID,
		"input":         "colonyfs:///jobs/" + jobID + "/base_model/",
		"output_format": "megatron",
		"tensor_parallel": 4,
		"dtype":         "bfloat16",
		"output":        "colonyfs:///jobs/" + jobID + "/base_model_megatron/",
	}
	workflow.AddFunctionSpec(convertSpec)

	// Step 2c: Upload base model to HPC
	uploadModelSpec := core.CreateEmptyFunctionSpec()
	uploadModelSpec.NodeName = "upload_model"
	uploadModelSpec.FuncName = "transfer_to_hpc"
	uploadModelSpec.Conditions.ColonyName = colonyName
	uploadModelSpec.Conditions.ExecutorType = "hpc-data-mover"
	uploadModelSpec.Conditions.Dependencies = []string{"convert_format"}
	uploadModelSpec.MaxExecTime = 7200
	uploadModelSpec.MaxWaitTime = 600
	uploadModelSpec.Priority = 10
	uploadModelSpec.KwArgs = map[string]interface{}{
		"job_id":    jobID,
		"direction": "cloud_to_hpc",
		"transfers": []map[string]interface{}{
			{
				"source":       "colonyfs:///jobs/" + jobID + "/base_model_megatron/",
				"destination":  "/cfs/klemming/projects/" + hpcAccount + "/llm-legal/" + jobID + "/model/",
				"size_est_gb":  32,
				"description":  "Base model weights (Megatron format, TP=4)",
			},
		},
		"hpc": map[string]interface{}{
			"system":  hpcSystem,
			"account": hpcAccount,
		},
		"verify_checksum": true,
	}
	workflow.AddFunctionSpec(uploadModelSpec)

	// ================================================================
	// HPC SIDE: Train the model
	// ================================================================

	// Step 3: Fine-tune on Dardel GPU nodes
	trainSpec := core.CreateEmptyFunctionSpec()
	trainSpec.NodeName = "train_model"
	trainSpec.FuncName = "train_llm"
	trainSpec.Conditions.ColonyName = colonyName
	trainSpec.Conditions.ExecutorType = "hpc-worker"
	trainSpec.Conditions.Dependencies = []string{"upload_to_hpc", "upload_model"}
	trainSpec.Conditions.Nodes = 2
	trainSpec.Conditions.GPU = core.GPU{
		Name:   "A100",
		Memory: "40Gi",
		Count:  4,
	}
	trainSpec.Conditions.Memory = "512Gi"
	trainSpec.Conditions.WallTime = 86400 // 24 hours
	trainSpec.MaxExecTime = 86400
	trainSpec.MaxWaitTime = 14400 // may queue on HPC
	trainSpec.Priority = 10
	trainSpec.KwArgs = map[string]interface{}{
		"job_id":    jobID,
		"method":    "full_finetune",
		"framework": "megatron-lm",
		"model_path": "/cfs/klemming/projects/" + hpcAccount + "/llm-legal/" + jobID + "/model/",
		"data_path":  "/cfs/klemming/projects/" + hpcAccount + "/llm-legal/" + jobID + "/data/",
		"hyperparameters": map[string]interface{}{
			"learning_rate":      2e-5,
			"min_learning_rate":  2e-6,
			"lr_warmup_fraction": 0.03,
			"weight_decay":       0.1,
			"grad_clip":          1.0,
			"batch_size_global":  128,
			"batch_size_micro":   2,
			"seq_length":         4096,
			"epochs":             3,
			"optimizer":          "adam",
			"adam_beta1":          0.9,
			"adam_beta2":          0.95,
			"bf16":               true,
		},
		"parallelism": map[string]interface{}{
			"tensor_parallel":   4,
			"pipeline_parallel": 2,
			"data_parallel":     1,
		},
		"checkpointing": map[string]interface{}{
			"save_interval_steps": 500,
			"save_dir":            "/cfs/klemming/projects/" + hpcAccount + "/llm-legal/" + jobID + "/checkpoints/",
			"keep_last_n":         3,
		},
		"logging": map[string]interface{}{
			"log_interval_steps":  10,
			"eval_interval_steps": 250,
			"eval_iters":          50,
			"tensorboard_dir":     "/cfs/klemming/projects/" + hpcAccount + "/llm-legal/" + jobID + "/tb_logs/",
		},
		"hpc": map[string]interface{}{
			"system":    hpcSystem,
			"partition": hpcPartition,
			"account":   hpcAccount,
			"qos":       "normal",
			"modules":   []string{"PDC", "cuda/12.3", "nccl/2.20", "openmpi/4.1"},
			"container": "nvcr.io/nvidia/pytorch:24.12-py3",
		},
	}
	workflow.AddFunctionSpec(trainSpec)

	// Step 4: Evaluate the fine-tuned model on HPC
	evalSpec := core.CreateEmptyFunctionSpec()
	evalSpec.NodeName = "evaluate_model"
	evalSpec.FuncName = "evaluate_llm"
	evalSpec.Conditions.ColonyName = colonyName
	evalSpec.Conditions.ExecutorType = "hpc-worker"
	evalSpec.Conditions.Dependencies = []string{"train_model"}
	evalSpec.Conditions.GPU = core.GPU{Name: "A100", Memory: "40Gi", Count: 4}
	evalSpec.Conditions.Nodes = 1
	evalSpec.Conditions.Memory = "256Gi"
	evalSpec.Conditions.WallTime = 7200
	evalSpec.MaxExecTime = 7200
	evalSpec.MaxWaitTime = 3600
	evalSpec.Priority = 10
	evalSpec.KwArgs = map[string]interface{}{
		"job_id":      jobID,
		"model_path":  "/cfs/klemming/projects/" + hpcAccount + "/llm-legal/" + jobID + "/checkpoints/latest/",
		"benchmarks": []map[string]interface{}{
			{
				"name":        "legal_sv_qa",
				"dataset":     "/cfs/klemming/projects/" + hpcAccount + "/llm-legal/benchmarks/legal_qa_sv.jsonl",
				"metrics":     []string{"exact_match", "f1", "bleu"},
				"num_samples": 1000,
			},
			{
				"name":        "contract_clause_extraction",
				"dataset":     "/cfs/klemming/projects/" + hpcAccount + "/llm-legal/benchmarks/clause_extraction.jsonl",
				"metrics":     []string{"precision", "recall", "f1"},
				"num_samples": 500,
			},
			{
				"name":        "case_summarization",
				"dataset":     "/cfs/klemming/projects/" + hpcAccount + "/llm-legal/benchmarks/case_summary.jsonl",
				"metrics":     []string{"rouge_1", "rouge_2", "rouge_l", "bertscore"},
				"num_samples": 200,
			},
			{
				"name":        "legal_reasoning",
				"dataset":     "/cfs/klemming/projects/" + hpcAccount + "/llm-legal/benchmarks/reasoning.jsonl",
				"metrics":     []string{"accuracy", "chain_of_thought_quality"},
				"num_samples": 300,
			},
		},
		"compare_baseline": baseModel,
		"output_report":    "/cfs/klemming/projects/" + hpcAccount + "/llm-legal/" + jobID + "/eval_report.json",
		"hpc": map[string]interface{}{
			"system":    hpcSystem,
			"partition": hpcPartition,
			"account":   hpcAccount,
		},
	}
	workflow.AddFunctionSpec(evalSpec)

	// ================================================================
	// HPC → CLOUD: Transfer trained model back
	// ================================================================

	// Step 5: Download trained model from HPC to cloud via ColonyFS
	downloadSpec := core.CreateEmptyFunctionSpec()
	downloadSpec.NodeName = "download_from_hpc"
	downloadSpec.FuncName = "transfer_from_hpc"
	downloadSpec.Conditions.ColonyName = colonyName
	downloadSpec.Conditions.ExecutorType = "hpc-data-mover"
	downloadSpec.Conditions.Dependencies = []string{"evaluate_model"}
	downloadSpec.MaxExecTime = 7200
	downloadSpec.MaxWaitTime = 600
	downloadSpec.Priority = 10
	downloadSpec.KwArgs = map[string]interface{}{
		"job_id":    jobID,
		"direction": "hpc_to_cloud",
		"transfers": []map[string]interface{}{
			{
				"source":       "/cfs/klemming/projects/" + hpcAccount + "/llm-legal/" + jobID + "/checkpoints/latest/",
				"destination":  "colonyfs:///jobs/" + jobID + "/trained_model/",
				"size_est_gb":  32,
				"description":  "Fine-tuned model weights",
			},
			{
				"source":       "/cfs/klemming/projects/" + hpcAccount + "/llm-legal/" + jobID + "/eval_report.json",
				"destination":  "colonyfs:///jobs/" + jobID + "/eval_report.json",
				"size_est_gb":  0.001,
				"description":  "Evaluation report",
			},
			{
				"source":       "/cfs/klemming/projects/" + hpcAccount + "/llm-legal/" + jobID + "/tb_logs/",
				"destination":  "colonyfs:///jobs/" + jobID + "/tensorboard/",
				"size_est_gb":  2,
				"description":  "TensorBoard training logs",
			},
		},
		"hpc": map[string]interface{}{
			"system":  hpcSystem,
			"account": hpcAccount,
		},
		"verify_checksum":  true,
		"cleanup_hpc_data": false,
	}
	workflow.AddFunctionSpec(downloadSpec)

	// ================================================================
	// CLOUD SIDE: Register and deploy
	// ================================================================

	// Step 6: Register model in model registry
	registerSpec := core.CreateEmptyFunctionSpec()
	registerSpec.NodeName = "register_model"
	registerSpec.FuncName = "register_model"
	registerSpec.Conditions.ColonyName = colonyName
	registerSpec.Conditions.ExecutorType = "cloud-worker"
	registerSpec.Conditions.Dependencies = []string{"download_from_hpc"}
	registerSpec.MaxExecTime = 1800
	registerSpec.MaxWaitTime = 300
	registerSpec.Priority = 10
	registerSpec.KwArgs = map[string]interface{}{
		"job_id":       jobID,
		"model_path":   "colonyfs:///jobs/" + jobID + "/trained_model/",
		"eval_report":  "colonyfs:///jobs/" + jobID + "/eval_report.json",
		"registry":     "mlflow",
		"registry_url": "https://mlflow.platform.internal",
		"model_name":   "legal-sv-llama3-8b",
		"version":      "1.0.0",
		"metadata": map[string]interface{}{
			"base_model":      baseModel,
			"fine_tune_method": "full",
			"training_data":   "Swedish legal texts (court rulings, SFS, contracts)",
			"training_tokens": "~2B",
			"hpc_system":      hpcSystem,
			"hpc_hours":       "estimated 200 GPU-hours",
			"languages":       []string{"sv", "en"},
		},
		"convert_to": []string{"safetensors", "gguf_q4_k_m"},
		"tags":       []string{"legal", "swedish", "llama3", "instruction-tuned"},
	}
	workflow.AddFunctionSpec(registerSpec)

	// Step 7: Deploy as inference endpoint
	deploySpec := core.CreateEmptyFunctionSpec()
	deploySpec.NodeName = "deploy_inference"
	deploySpec.FuncName = "deploy_llm_inference"
	deploySpec.Conditions.ColonyName = colonyName
	deploySpec.Conditions.ExecutorType = "cloud-deployer"
	deploySpec.Conditions.Dependencies = []string{"register_model"}
	deploySpec.MaxExecTime = 1200
	deploySpec.MaxWaitTime = 300
	deploySpec.Priority = 10
	deploySpec.KwArgs = map[string]interface{}{
		"job_id":      jobID,
		"model_name":  "legal-sv-llama3-8b",
		"model_version": "1.0.0",
		"engine":      "vllm",
		"namespace":   "llm-inference",
		"deployment": map[string]interface{}{
			"replicas":          2,
			"gpu_type":          "A100",
			"gpu_count":         1,
			"tensor_parallel":   1,
			"max_model_len":     4096,
			"max_batch_size":    32,
			"dtype":             "bfloat16",
			"quantization":      nil,
		},
		"resources": map[string]interface{}{
			"requests": map[string]string{"cpu": "4", "memory": "32Gi", "nvidia.com/gpu": "1"},
			"limits":   map[string]string{"cpu": "8", "memory": "64Gi", "nvidia.com/gpu": "1"},
		},
		"api": map[string]interface{}{
			"type":            "openai_compatible",
			"endpoint":        "/v1/chat/completions",
			"auth":            "api_key",
			"rate_limit_rpm":  1000,
		},
		"ingress": map[string]interface{}{
			"host": "legal-llm.platform.internal",
			"tls":  true,
		},
	}
	workflow.AddFunctionSpec(deploySpec)

	// Step 8: Validate the deployed endpoint
	validateEndpointSpec := core.CreateEmptyFunctionSpec()
	validateEndpointSpec.NodeName = "validate_endpoint"
	validateEndpointSpec.FuncName = "validate_llm_endpoint"
	validateEndpointSpec.Conditions.ColonyName = colonyName
	validateEndpointSpec.Conditions.ExecutorType = "cloud-worker"
	validateEndpointSpec.Conditions.Dependencies = []string{"deploy_inference"}
	validateEndpointSpec.MaxExecTime = 600
	validateEndpointSpec.MaxWaitTime = 300
	validateEndpointSpec.Priority = 10
	validateEndpointSpec.KwArgs = map[string]interface{}{
		"job_id":   jobID,
		"endpoint": "https://legal-llm.platform.internal/v1/chat/completions",
		"tests": []map[string]interface{}{
			{
				"name":   "health_check",
				"method": "GET",
				"path":   "/health",
				"expect_status": 200,
			},
			{
				"name": "simple_completion",
				"prompt": "Sammanfatta den rattsliga inneborden av 6 kap. 1 § foraldrabalken.",
				"expect_language": "sv",
				"max_latency_ms":  5000,
			},
			{
				"name": "contract_analysis",
				"prompt": "Identifiera ansvarsbegransningsklausuler i foljande avtal: [test contract excerpt]",
				"expect_contains": []string{"ansvarsbegransning"},
				"max_latency_ms":  8000,
			},
			{
				"name":   "throughput_test",
				"concurrent_requests": 10,
				"target_rps":          5,
				"duration_sec":        60,
				"max_p99_latency_ms":  10000,
			},
		},
		"notify_on_success": []string{"ml-team@platform.internal"},
		"notify_on_failure": []string{"ml-team@platform.internal", "oncall@platform.internal"},
	}
	workflow.AddFunctionSpec(validateEndpointSpec)

	// Submit workflow
	c := client.CreateColoniesClient(host, port, true, false)
	graph, err := c.SubmitWorkflowSpec(workflow, executorPrvKey)
	if err != nil {
		fmt.Printf("ERROR submitting workflow: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Submitted cloud-to-HPC LLM training pipeline\n")
	fmt.Printf("  ProcessGraph ID: %s\n", graph.ID)
	fmt.Printf("  Total processes: %d\n", len(graph.ProcessIDs))
	fmt.Printf("  Roots: %d\n", len(graph.Roots))
	fmt.Println("\nDAG structure:")
	fmt.Println()
	fmt.Println("  CLOUD                          HPC (Dardel)")
	fmt.Println("  ─────                          ────────────")
	fmt.Println("  validate_dataset")
	fmt.Println("    |")
	fmt.Println("  prepare_tokenized")
	fmt.Println("    |")
	fmt.Println("  upload_to_hpc ─────────────────> train_model (2 nodes, 8xA100)")
	fmt.Println("                                     |")
	fmt.Println("  download_base_model               evaluate_model")
	fmt.Println("    |                                 |")
	fmt.Println("  convert_format                      |")
	fmt.Println("    |                                 |")
	fmt.Println("  upload_model ──────────────────>  (deps)")
	fmt.Println("                                     |")
	fmt.Println("  download_from_hpc <────────────── (transfer back)")
	fmt.Println("    |")
	fmt.Println("  register_model (MLflow)")
	fmt.Println("    |")
	fmt.Println("  deploy_inference (vLLM, 2 replicas)")
	fmt.Println("    |")
	fmt.Println("  validate_endpoint")
	fmt.Println()
	fmt.Println("  Data flow: Cloud → ColonyFS → HPC → ColonyFS → Cloud")
	fmt.Println("  Two parallel preparation tracks converge at train_model")
}
