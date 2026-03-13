// Use Case 7: Drug Discovery Molecular Dynamics Parameter Sweep
//
// A pharmaceutical company screens drug candidates against a protein target
// using molecular dynamics (MD) simulations. The workflow generates binding
// pose predictions for each compound, runs MD simulations to estimate
// binding free energy, ranks candidates, and feeds promising ones into
// a second round of longer simulations.
//
// The sweep has a hard deadline: results must be ready for the regulatory
// submission committee meeting. Compounds are prioritized by predicted
// efficacy from a pre-screening ML model.
//
// DAG Structure:
//
//   prepare_target ──┐
//                    ├──> dock_compound_001 ──> md_short_001 ──┐
//                    ├──> dock_compound_002 ──> md_short_002 ──┤
//                    ├──> ...                                  ├──> rank_candidates ──┬──> md_long_top1 ──┐
//                    ├──> dock_compound_N   ──> md_short_N   ──┘                     ├──> md_long_top2 ──├──> final_report
//   prepare_forcefields ──────────────────────────(all docking depends on this)      └──> md_long_top3 ──┘

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

type Compound struct {
	ID               string
	Name             string
	SMILES           string
	MolWeight        float64
	LogP             float64
	PredictedBinding float64 // ML pre-screening score (lower = better, kcal/mol)
	Priority         int     // derived from predicted binding
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

	// Target protein
	targetProtein := map[string]interface{}{
		"name":       "SARS-CoV-2 Main Protease (Mpro)",
		"pdb_id":     "7L13",
		"chain":      "A",
		"binding_site": map[string]interface{}{
			"center_x": -10.8, "center_y": 15.3, "center_z": 68.5,
			"size_x": 25.0, "size_y": 25.0, "size_z": 25.0,
		},
		"active_site_residues": []string{"H41", "C145", "M49", "Y54", "F140", "L141", "N142", "G143", "S144", "H163", "H164", "M165", "E166", "Q189"},
	}

	// Drug candidate library (simulated, with ML pre-screening scores)
	rng := rand.New(rand.NewSource(42))
	compounds := make([]Compound, 25)
	prefixes := []string{"Nirmatrelvir", "Ensitrelvir", "Simnotrelvir", "Leritrelvir", "Lufotrelvir"}
	for i := 0; i < 25; i++ {
		prefix := prefixes[i%len(prefixes)]
		predicted := -3.0 - rng.Float64()*9.0 // -3 to -12 kcal/mol
		priority := int(-predicted * 2)        // better binding = higher priority
		compounds[i] = Compound{
			ID:               fmt.Sprintf("CPD-%04d", i+1),
			Name:             fmt.Sprintf("%s-analog-%02d", prefix, i+1),
			SMILES:           generateDummySMILES(rng),
			MolWeight:        250.0 + rng.Float64()*300.0,
			LogP:             rng.Float64()*5.0 - 1.0,
			PredictedBinding: predicted,
			Priority:         priority,
		}
	}

	// How many top compounds get longer simulations
	topN := 3

	// Deadline: regulatory submission meeting
	deadline := time.Now().Add(48 * time.Hour)

	fmt.Println("=== Drug Discovery MD Parameter Sweep ===")
	fmt.Printf("Target: %s (PDB: %s)\n", targetProtein["name"], targetProtein["pdb_id"])
	fmt.Printf("Compound library: %d candidates\n", len(compounds))
	fmt.Printf("Top-N for long MD: %d\n", topN)
	fmt.Printf("Deadline: %s (regulatory committee meeting)\n\n", deadline.Format("2006-01-02 15:04"))

	fmt.Println("Compounds (sorted by predicted binding):")
	for i, c := range compounds {
		if i < 5 || i >= len(compounds)-2 {
			fmt.Printf("  %-8s %-30s  predicted: %.1f kcal/mol  priority: %d\n",
				c.ID, c.Name, c.PredictedBinding, c.Priority)
		} else if i == 5 {
			fmt.Printf("  ... (%d more compounds) ...\n", len(compounds)-7)
		}
	}
	fmt.Println()

	workflow := core.CreateWorkflowSpec(colonyName)

	// ================================================================
	// Stage 1a: Prepare protein target
	// ================================================================
	prepTarget := core.CreateEmptyFunctionSpec()
	prepTarget.NodeName = "prepare_target"
	prepTarget.FuncName = "prepare_protein"
	prepTarget.Conditions.ColonyName = colonyName
	prepTarget.Conditions.ExecutorType = "md-worker"
	prepTarget.Conditions.CPU = "4"
	prepTarget.Conditions.Memory = "8Gi"
	prepTarget.MaxExecTime = 1800
	prepTarget.MaxWaitTime = 600
	prepTarget.Priority = 20
	prepTarget.KwArgs = map[string]interface{}{
		"pdb_id":   targetProtein["pdb_id"],
		"chain":    targetProtein["chain"],
		"steps": []string{
			"download_pdb",
			"remove_water",
			"remove_ligands",
			"add_hydrogens",
			"assign_charges",
			"minimize_structure",
			"check_clashes",
		},
		"protonation_ph":  7.4,
		"forcefield":      "amber14sb",
		"output_pdbqt":    "colonyfs:///pharma/targets/mpro_prepared.pdbqt",
		"output_topology": "colonyfs:///pharma/targets/mpro_topology.top",
	}
	workflow.AddFunctionSpec(prepTarget)

	// ================================================================
	// Stage 1b: Prepare force field parameters
	// ================================================================
	prepFF := core.CreateEmptyFunctionSpec()
	prepFF.NodeName = "prepare_forcefields"
	prepFF.FuncName = "prepare_forcefields"
	prepFF.Conditions.ColonyName = colonyName
	prepFF.Conditions.ExecutorType = "md-worker"
	prepFF.Conditions.CPU = "4"
	prepFF.Conditions.Memory = "8Gi"
	prepFF.MaxExecTime = 1800
	prepFF.MaxWaitTime = 600
	prepFF.Priority = 20
	prepFF.KwArgs = map[string]interface{}{
		"protein_forcefield": "amber14sb",
		"ligand_forcefield":  "gaff2",
		"water_model":        "tip3p",
		"ion_parameters":     "joung_cheatham",
		"box_type":           "dodecahedron",
		"box_padding_nm":     1.2,
		"salt_concentration_mol": 0.15,
		"output":             "colonyfs:///pharma/forcefields/",
	}
	workflow.AddFunctionSpec(prepFF)

	// ================================================================
	// Stage 2: Docking - predict binding poses (parallel per compound)
	// ================================================================
	dockNodeNames := []string{}
	for _, cpd := range compounds {
		dockSpec := core.CreateEmptyFunctionSpec()
		dockName := fmt.Sprintf("dock_%s", cpd.ID)
		dockNodeNames = append(dockNodeNames, dockName)
		dockSpec.NodeName = dockName
		dockSpec.FuncName = "dock_compound"
		dockSpec.Conditions.ColonyName = colonyName
		dockSpec.Conditions.ExecutorType = "md-worker"
		dockSpec.Conditions.Dependencies = []string{"prepare_target", "prepare_forcefields"}
		dockSpec.Conditions.CPU = "2"
		dockSpec.Conditions.Memory = "4Gi"
		dockSpec.MaxExecTime = 600
		dockSpec.MaxWaitTime = 1800
		dockSpec.Priority = cpd.Priority
		dockSpec.KwArgs = map[string]interface{}{
			"compound_id":   cpd.ID,
			"compound_name": cpd.Name,
			"smiles":        cpd.SMILES,
			"mol_weight":    cpd.MolWeight,
			"logp":          cpd.LogP,
			"receptor":      "colonyfs:///pharma/targets/mpro_prepared.pdbqt",
			"docking_engine": "autodock_vina",
			"binding_site":   targetProtein["binding_site"],
			"exhaustiveness": 32,
			"num_modes":      9,
			"energy_range":   3.0,
			"output_poses":   fmt.Sprintf("colonyfs:///pharma/docking/%s/poses.pdbqt", cpd.ID),
			"output_scores":  fmt.Sprintf("colonyfs:///pharma/docking/%s/scores.json", cpd.ID),
		}
		workflow.AddFunctionSpec(dockSpec)
	}

	// ================================================================
	// Stage 3: Short MD simulations (10 ns) - validate binding poses
	// ================================================================
	mdShortNodeNames := []string{}
	for _, cpd := range compounds {
		mdSpec := core.CreateEmptyFunctionSpec()
		mdName := fmt.Sprintf("md_short_%s", cpd.ID)
		mdShortNodeNames = append(mdShortNodeNames, mdName)
		mdSpec.NodeName = mdName
		mdSpec.FuncName = "run_md_simulation"
		mdSpec.Conditions.ColonyName = colonyName
		mdSpec.Conditions.ExecutorType = "hpc-md-worker"
		mdSpec.Conditions.Dependencies = []string{fmt.Sprintf("dock_%s", cpd.ID)}
		mdSpec.Conditions.GPU = core.GPU{Name: "A100", Memory: "40Gi", Count: 1}
		mdSpec.Conditions.CPU = "8"
		mdSpec.Conditions.Memory = "32Gi"
		mdSpec.Conditions.WallTime = 7200
		mdSpec.MaxExecTime = 7200
		mdSpec.MaxWaitTime = 3600
		mdSpec.Priority = cpd.Priority
		mdSpec.KwArgs = map[string]interface{}{
			"compound_id":     cpd.ID,
			"compound_name":   cpd.Name,
			"simulation_type": "production_md",
			"duration_ns":     10,
			"timestep_fs":     2.0,
			"temperature_k":   310.15,
			"pressure_bar":    1.0,
			"ensemble":        "NPT",
			"thermostat":      "v-rescale",
			"barostat":        "parrinello-rahman",
			"input_pose":      fmt.Sprintf("colonyfs:///pharma/docking/%s/poses.pdbqt", cpd.ID),
			"forcefield_dir":  "colonyfs:///pharma/forcefields/",
			"engine":          "gromacs",
			"gpu_acceleration": true,
			"output": map[string]interface{}{
				"trajectory": fmt.Sprintf("colonyfs:///pharma/md_short/%s/trajectory.xtc", cpd.ID),
				"energy":     fmt.Sprintf("colonyfs:///pharma/md_short/%s/energy.edr", cpd.ID),
				"rmsd":       fmt.Sprintf("colonyfs:///pharma/md_short/%s/rmsd.xvg", cpd.ID),
				"binding_fe": fmt.Sprintf("colonyfs:///pharma/md_short/%s/binding_energy.json", cpd.ID),
			},
			"analysis": []string{"rmsd", "rmsf", "binding_free_energy_mmgbsa", "hydrogen_bonds", "contact_frequency"},
		}
		workflow.AddFunctionSpec(mdSpec)
	}

	// ================================================================
	// Stage 4: Rank candidates based on short MD results
	// ================================================================
	rankSpec := core.CreateEmptyFunctionSpec()
	rankSpec.NodeName = "rank_candidates"
	rankSpec.FuncName = "rank_compounds"
	rankSpec.Conditions.ColonyName = colonyName
	rankSpec.Conditions.ExecutorType = "md-worker"
	rankSpec.Conditions.CPU = "4"
	rankSpec.Conditions.Memory = "16Gi"
	rankSpec.MaxExecTime = 1800
	rankSpec.MaxWaitTime = 600
	rankSpec.Priority = 25
	for _, name := range mdShortNodeNames {
		rankSpec.Conditions.Dependencies = append(rankSpec.Conditions.Dependencies, name)
	}
	rankSpec.KwArgs = map[string]interface{}{
		"results_dir":    "colonyfs:///pharma/md_short/",
		"ranking_criteria": []map[string]interface{}{
			{"metric": "binding_free_energy", "weight": 0.4, "direction": "minimize"},
			{"metric": "pose_stability_rmsd", "weight": 0.2, "direction": "minimize"},
			{"metric": "hydrogen_bond_count", "weight": 0.15, "direction": "maximize"},
			{"metric": "contact_frequency", "weight": 0.15, "direction": "maximize"},
			{"metric": "drug_likeness_qed", "weight": 0.1, "direction": "maximize"},
		},
		"filters": map[string]interface{}{
			"max_rmsd_nm":          0.5,
			"min_binding_fe_kcal": -5.0,
			"max_mol_weight":       500,
		},
		"top_n":       topN,
		"output":      "colonyfs:///pharma/ranking/candidate_ranking.json",
		"output_plots": "colonyfs:///pharma/ranking/plots/",
	}
	workflow.AddFunctionSpec(rankSpec)

	// ================================================================
	// Stage 5: Long MD simulations (100 ns) for top-N candidates
	// ================================================================
	mdLongNodeNames := []string{}
	for i := 1; i <= topN; i++ {
		mdLongName := fmt.Sprintf("md_long_top%d", i)
		mdLongNodeNames = append(mdLongNodeNames, mdLongName)

		mdLongSpec := core.CreateEmptyFunctionSpec()
		mdLongSpec.NodeName = mdLongName
		mdLongSpec.FuncName = "run_md_simulation"
		mdLongSpec.Conditions.ColonyName = colonyName
		mdLongSpec.Conditions.ExecutorType = "hpc-md-worker"
		mdLongSpec.Conditions.Dependencies = []string{"rank_candidates"}
		mdLongSpec.Conditions.GPU = core.GPU{Name: "A100", Memory: "80Gi", Count: 1}
		mdLongSpec.Conditions.CPU = "16"
		mdLongSpec.Conditions.Memory = "64Gi"
		mdLongSpec.Conditions.WallTime = 86400 // 24 hours
		mdLongSpec.MaxExecTime = 86400
		mdLongSpec.MaxWaitTime = 7200
		mdLongSpec.Priority = 30 // high priority -- these are the top candidates
		mdLongSpec.KwArgs = map[string]interface{}{
			"compound_rank":   i,
			"simulation_type": "extended_production_md",
			"duration_ns":     100,
			"timestep_fs":     2.0,
			"temperature_k":   310.15,
			"pressure_bar":    1.0,
			"ensemble":        "NPT",
			"thermostat":      "v-rescale",
			"barostat":        "parrinello-rahman",
			"engine":          "gromacs",
			"gpu_acceleration": true,
			"enhanced_sampling": map[string]interface{}{
				"method":      "replica_exchange",
				"num_replicas": 4,
				"temp_range":  []float64{300, 310, 320, 340},
			},
			"analysis": []string{
				"rmsd", "rmsf", "binding_free_energy_mmgbsa",
				"binding_free_energy_fep",
				"hydrogen_bonds", "contact_frequency",
				"principal_component_analysis",
				"binding_kinetics_estimation",
				"water_bridge_analysis",
			},
			"output_dir": fmt.Sprintf("colonyfs:///pharma/md_long/top%d/", i),
		}
		workflow.AddFunctionSpec(mdLongSpec)
	}

	// ================================================================
	// Stage 6: Final report for regulatory submission
	// ================================================================
	reportSpec := core.CreateEmptyFunctionSpec()
	reportSpec.NodeName = "final_report"
	reportSpec.FuncName = "generate_pharma_report"
	reportSpec.Conditions.ColonyName = colonyName
	reportSpec.Conditions.ExecutorType = "report-generator"
	reportSpec.Conditions.Dependencies = mdLongNodeNames
	reportSpec.Conditions.CPU = "4"
	reportSpec.Conditions.Memory = "16Gi"
	reportSpec.MaxExecTime = 3600
	reportSpec.MaxWaitTime = 600
	reportSpec.Priority = 25
	reportSpec.KwArgs = map[string]interface{}{
		"target_protein":  targetProtein["name"],
		"pdb_id":          targetProtein["pdb_id"],
		"total_compounds": len(compounds),
		"top_n":           topN,
		"ranking":         "colonyfs:///pharma/ranking/candidate_ranking.json",
		"md_long_dir":     "colonyfs:///pharma/md_long/",
		"report_sections": []string{
			"executive_summary",
			"target_description",
			"compound_library_overview",
			"docking_results",
			"short_md_screening",
			"candidate_ranking",
			"extended_md_results",
			"binding_free_energy_comparison",
			"admet_predictions",
			"selectivity_assessment",
			"recommendations",
		},
		"format":      "pdf",
		"template":    "regulatory_drug_screening_v3",
		"output":      "colonyfs:///pharma/reports/mpro_screening_final.pdf",
		"deadline":    deadline.Format(time.RFC3339),
		"notify":      []string{"drug-discovery@pharma.example.com", "regulatory@pharma.example.com"},
		"attachments": []string{"raw_data_archive", "trajectory_summaries", "statistical_analysis"},
	}
	workflow.AddFunctionSpec(reportSpec)

	// Submit workflow
	c := client.CreateColoniesClient(host, port, true, false)
	graph, err := c.SubmitWorkflowSpec(workflow, executorPrvKey)
	if err != nil {
		fmt.Printf("ERROR submitting workflow: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\nSubmitted drug discovery parameter sweep\n")
	fmt.Printf("  ProcessGraph ID: %s\n", graph.ID)
	fmt.Printf("  Total processes: %d\n", len(graph.ProcessIDs))
	fmt.Printf("  Roots: %d (target prep + forcefield prep)\n", len(graph.Roots))
	fmt.Println("\nDAG structure:")
	fmt.Println("  Stage 1 - Prepare:    target protein + force fields (2 roots, parallel)")
	fmt.Printf("  Stage 2 - Docking:    %d compounds in parallel (prioritized by predicted binding)\n", len(compounds))
	fmt.Printf("  Stage 3 - Short MD:   %d x 10 ns simulations in parallel (GPU, each depends on docking)\n", len(compounds))
	fmt.Println("  Stage 4 - Ranking:    1 process (depends on all short MD, selects top-N)")
	fmt.Printf("  Stage 5 - Long MD:    %d x 100 ns simulations (GPU, replica exchange)\n", topN)
	fmt.Println("  Stage 6 - Report:     1 regulatory submission report (depends on all long MD)")
	fmt.Println()
	fmt.Println("Scheduling dimensions:")
	fmt.Println("  - Massive fan-out (25 docking + 25 short MD) competing for GPU resources")
	fmt.Println("  - Priority ordering: better predicted candidates get GPU time first")
	fmt.Println("  - Hard deadline: regulatory committee in 48 hours")
	fmt.Println("  - Two-phase funnel: wide screening → narrow deep analysis")
	fmt.Println("  - Shares HPC resources with other use cases on the same colony")
}

func generateDummySMILES(rng *rand.Rand) string {
	fragments := []string{
		"CC(=O)N", "c1ccc(cc1)", "C(=O)O", "NC(=O)", "C1CCNCC1",
		"c1ccncc1", "C(F)(F)F", "OC(=O)", "c1ccc2[nH]ccc2c1",
		"CC(C)C", "CCOC", "c1cnc2ccccc2n1",
	}
	n := 3 + rng.Intn(4)
	smiles := ""
	for i := 0; i < n; i++ {
		smiles += fragments[rng.Intn(len(fragments))]
	}
	return smiles
}

func init() {
	rand.New(rand.NewSource(time.Now().UnixNano()))
}
