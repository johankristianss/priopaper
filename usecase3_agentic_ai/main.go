// Use Case 3: Agentic AI Workflows (ColonyOS)
//
// Users submit natural-language goals. An agentic executor powered by an LLM
// plans the work, executes tool steps, reflects on results, and iteratively
// refines the workflow until the goal is achieved.
//
// The DAG is built dynamically:
//   1. An initial skeleton is submitted:  agent_query → llm_plan → plan_handler → completed
//   2. The plan_handler parses the LLM's plan and inserts tool steps + a reflect node
//   3. After tools execute, the reflect node asks the LLM to evaluate progress
//   4. If more work is needed, new tool steps + another reflect are inserted
//   5. When done, a finalization step summarizes results
//
// This program simulates multiple users submitting different goals concurrently.
// Each goal produces its own ProcessGraph that grows dynamically.
//
// Initial DAG (submitted):
//
//   agent_query ──> llm_plan ──> plan_handler ──> completed
//
// After plan_handler executes and inserts steps (dynamic):
//
//   plan_handler ──(insert)──> tool_step_1 ──> tool_step_2 ──> reflect_0 ──> completed
//
// After reflect_0 decides more work is needed (dynamic):
//
//   reflect_0 ──(insert)──> llm_reflect ──> reflect_handler ──(insert)──> tool_step_3 ──> reflect_1 ──> completed

package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/colonyos/colonies/pkg/client"
	"github.com/colonyos/colonies/pkg/core"
)

// UserGoal represents a goal submitted by a user to the agentic system
type UserGoal struct {
	User        string
	Goal        string
	Context     string
	MaxSteps    int
	MaxReflects int
	Priority    int
	Attachments []Attachment
}

type Attachment struct {
	Name  string
	Label string
	Type  string
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

	// Multiple users submitting goals concurrently
	goals := []UserGoal{
		{
			User: "researcher_anna",
			Goal: "Analyze the CSV file I uploaded containing 2 years of soil moisture " +
				"measurements from 12 sensors in Abisko. Find seasonal patterns, " +
				"correlate with temperature data from SMHI, and generate plots showing " +
				"the relationship between permafrost thaw depth and soil moisture.",
			Context:     "The sensors are at different depths (10cm, 30cm, 50cm, 100cm). The CSV has columns: timestamp, sensor_id, depth_cm, moisture_pct, temperature_c.",
			MaxSteps:    20,
			MaxReflects: 5,
			Priority:    10,
			Attachments: []Attachment{
				{Name: "abisko_soil_moisture_2024_2025.csv", Label: "uploads/anna", Type: "text/csv"},
			},
		},
		{
			User: "engineer_erik",
			Goal: "Write a Python script that reads MQTT messages from our IoT gateway " +
				"at mqtt://iot.factory.local:1883/sensors/# and stores them in InfluxDB. " +
				"Include reconnection logic, schema validation, and a Grafana dashboard JSON.",
			Context:     "InfluxDB runs at http://influxdb.factory.local:8086, org=factory, bucket=sensors. Messages are JSON with fields: device_id, metric, value, unit, timestamp.",
			MaxSteps:    15,
			MaxReflects: 3,
			Priority:    8,
		},
		{
			User: "student_maria",
			Goal: "Help me understand the ColonyOS scheduling algorithm. Find the relevant " +
				"source code, explain how process priority works, and create a diagram " +
				"showing the scheduling flow.",
			Context:     "I'm writing my master's thesis on distributed scheduling. I need to understand the Assign() flow.",
			MaxSteps:    10,
			MaxReflects: 3,
			Priority:    5,
		},
	}

	c := client.CreateColoniesClient(host, port, true, false)

	fmt.Println("=== Agentic AI Workflows ===")
	fmt.Printf("Concurrent user goals: %d\n\n", len(goals))

	for i, ug := range goals {
		fmt.Printf("[Goal %d] User: %s\n", i+1, ug.User)
		fmt.Printf("  Goal: %s\n", truncate(ug.Goal, 100))
		fmt.Printf("  Max steps: %d, Max reflects: %d, Priority: %d\n",
			ug.MaxSteps, ug.MaxReflects, ug.Priority)
		if len(ug.Attachments) > 0 {
			for _, att := range ug.Attachments {
				fmt.Printf("  Attachment: %s (%s)\n", att.Name, att.Type)
			}
		}

		// Build the initial agentic workflow skeleton.
		// This is the DAG that gets submitted. The agenticexecutor will
		// dynamically expand it by inserting child processes as the LLM
		// plans, executes tools, reflects, and iterates.
		workflow := core.CreateWorkflowSpec(colonyName)

		// Node 1: agent_query - entry point, sets up planning
		querySpec := core.CreateEmptyFunctionSpec()
		querySpec.NodeName = "agent_query"
		querySpec.FuncName = "agent_query"
		querySpec.Conditions.ColonyName = colonyName
		querySpec.Conditions.ExecutorType = "agenticexecutor"
		querySpec.MaxExecTime = 120
		querySpec.MaxWaitTime = 300
		querySpec.Priority = ug.Priority
		querySpec.KwArgs = map[string]interface{}{
			"goal":         ug.Goal,
			"context":      ug.Context,
			"max_steps":    ug.MaxSteps,
			"max_reflects": ug.MaxReflects,
		}
		if len(ug.Attachments) > 0 {
			atts := make([]map[string]interface{}, len(ug.Attachments))
			for j, att := range ug.Attachments {
				atts[j] = map[string]interface{}{
					"name":  att.Name,
					"label": att.Label,
					"type":  att.Type,
				}
			}
			querySpec.KwArgs["attachments"] = atts
		}
		workflow.AddFunctionSpec(querySpec)

		// Node 2: llm_plan - LLM generates the execution plan
		llmPlanSpec := core.CreateEmptyFunctionSpec()
		llmPlanSpec.NodeName = "llm_plan"
		llmPlanSpec.FuncName = "llm_chat"
		llmPlanSpec.Conditions.ColonyName = colonyName
		llmPlanSpec.Conditions.ExecutorType = "llmexecutor"
		llmPlanSpec.Conditions.Dependencies = []string{"agent_query"}
		llmPlanSpec.MaxExecTime = 120
		llmPlanSpec.MaxWaitTime = 300
		llmPlanSpec.Priority = ug.Priority
		llmPlanSpec.Args = []interface{}{
			// The actual prompt is constructed by agent_query at runtime,
			// including available tools, goal, context, and constraints.
			// Here we set a placeholder; agent_query replaces it via AddChild.
			fmt.Sprintf("[System: planning agent]\n\nGoal: %s\nContext: %s", ug.Goal, ug.Context),
			"",      // history
			"",      // model (use default)
			"false", // tools_enabled
		}
		workflow.AddFunctionSpec(llmPlanSpec)

		// Node 3: plan_handler - parses LLM plan, inserts tool steps + reflect
		planHandlerSpec := core.CreateEmptyFunctionSpec()
		planHandlerSpec.NodeName = "plan_handler"
		planHandlerSpec.FuncName = "agent_plan_handler"
		planHandlerSpec.Conditions.ColonyName = colonyName
		planHandlerSpec.Conditions.ExecutorType = "agenticexecutor"
		planHandlerSpec.Conditions.Dependencies = []string{"llm_plan"}
		planHandlerSpec.MaxExecTime = 60
		planHandlerSpec.MaxWaitTime = 300
		planHandlerSpec.Priority = ug.Priority
		planHandlerSpec.KwArgs = map[string]interface{}{
			"goal":         ug.Goal,
			"context":      ug.Context,
			"max_steps":    ug.MaxSteps,
			"max_reflects": ug.MaxReflects,
		}
		workflow.AddFunctionSpec(planHandlerSpec)

		// Node 4: completed - terminal node, always stays at the end
		// The insert mechanism ensures new steps are added before this node.
		completedSpec := core.CreateEmptyFunctionSpec()
		completedSpec.NodeName = "completed"
		completedSpec.FuncName = "agent_completed"
		completedSpec.Conditions.ColonyName = colonyName
		completedSpec.Conditions.ExecutorType = "agenticexecutor"
		completedSpec.Conditions.Dependencies = []string{"plan_handler"}
		completedSpec.MaxExecTime = 30
		completedSpec.MaxWaitTime = 300
		completedSpec.Priority = ug.Priority
		completedSpec.KwArgs = map[string]interface{}{
			"goal": ug.Goal,
		}
		workflow.AddFunctionSpec(completedSpec)

		// Submit the initial skeleton
		graph, err := c.SubmitWorkflowSpec(workflow, executorPrvKey)
		if err != nil {
			fmt.Printf("  ERROR submitting workflow: %v\n\n", err)
			continue
		}

		fmt.Printf("  ProcessGraph ID: %s\n", graph.ID)
		fmt.Printf("  Initial processes: %d (skeleton: query → llm_plan → plan_handler → completed)\n", len(graph.ProcessIDs))
		fmt.Println()

		// At runtime, the agenticexecutor dynamically expands this DAG:
		//
		// After plan_handler runs, it calls AddChild(insert=true) to insert
		// tool steps and a reflect node between plan_handler and completed:
		//
		//   plan_handler ──> tool_search_code ──> tool_read_file ──> tool_write_file ──> reflect_0 ──> completed
		//
		// When reflect_0 runs, it evaluates tool outputs. If more work is needed,
		// it inserts new steps:
		//
		//   reflect_0 ──> llm_reflect_0 ──> reflect_handler_0 ──> tool_fix_code ──> reflect_1 ──> completed
		//
		// This continues until the LLM decides the goal is achieved (done=true),
		// at which point a finalization step generates the summary:
		//
		//   reflect_N ──> llm_finalize ──> final_handler ──> completed
	}

	fmt.Println("=== How it works at runtime ===")
	fmt.Println()
	fmt.Println("The submitted DAG is just a skeleton. The agenticexecutor dynamically")
	fmt.Println("grows the ProcessGraph by calling AddChild(insert=true) to insert new")
	fmt.Println("processes between existing nodes.")
	fmt.Println()
	fmt.Println("Example expansion for Goal 1 (soil moisture analysis):")
	fmt.Println()
	fmt.Println("  agent_query")
	fmt.Println("    |")
	fmt.Println("  llm_plan                    (LLM generates plan with 4 tool steps)")
	fmt.Println("    |")
	fmt.Println("  plan_handler                (parses plan, inserts steps below)")
	fmt.Println("    |")
	fmt.Println("  tool_read_file              (read the uploaded CSV)")
	fmt.Println("    |")
	fmt.Println("  tool_fetch_smhi_data        (fetch temperature data from SMHI API)")
	fmt.Println("    |")
	fmt.Println("  tool_run_python             (run correlation analysis script)")
	fmt.Println("    |")
	fmt.Println("  tool_generate_plot          (create matplotlib visualization)")
	fmt.Println("    |")
	fmt.Println("  reflect_0                   (LLM evaluates: are the plots correct?)")
	fmt.Println("    |")
	fmt.Println("  llm_reflect_0               (LLM analysis)")
	fmt.Println("    |")
	fmt.Println("  reflect_handler_0           (decides: need permafrost depth overlay)")
	fmt.Println("    |")
	fmt.Println("  tool_run_python             (add permafrost thaw depth to plots)")
	fmt.Println("    |")
	fmt.Println("  tool_write_file             (save final report)")
	fmt.Println("    |")
	fmt.Println("  reflect_1                   (LLM evaluates: goal achieved)")
	fmt.Println("    |")
	fmt.Println("  llm_finalize                (generate summary)")
	fmt.Println("    |")
	fmt.Println("  final_handler               (format output)")
	fmt.Println("    |")
	fmt.Println("  completed                   (always terminal node)")
	fmt.Println()
	fmt.Println("Each goal runs as an independent ProcessGraph. Multiple users'")
	fmt.Println("goals execute concurrently, competing for executor resources.")
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
