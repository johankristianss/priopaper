// Use Case 1: Seismic Processing Workload (RockSigma)
//
// Continuous mine seismology monitoring. Geophones installed throughout an
// underground mine produce a constant stream of seismogram data via RabbitMQ.
// Processing runs in a loop, submitting a new DAG for each time window.
//
// Each DAG has 3 steps:
//
//   Step 1 (P-wave picking): For each geophone, pick the P-wave arrival time
//           in the seismogram using an STA/LTA trigger + AIC refinement.
//
//   Step 2 (Event classification): An ML classifier groups the individual
//           picks across geophones into seismic events (blasts vs rockbursts
//           vs noise).
//
//   Step 3 (3D location): For each classified event, calculate the hypocenter
//           (x, y, z) inside the mine using a 3D velocity model and arrival
//           time inversion.
//
// DAG Structure (per time window):
//
//   pick_pwave_GEO01 ──┐
//   pick_pwave_GEO02 ──┤
//   pick_pwave_GEO03 ──┤
//   pick_pwave_GEO04 ──├──> classify_events ──┬──> locate_event_1
//   pick_pwave_GEO05 ──┤                      ├──> locate_event_2
//   pick_pwave_GEO06 ──┤                      └──> locate_event_3
//   pick_pwave_GEO07 ──┤
//   pick_pwave_GEO08 ──┤
//   pick_pwave_GEO09 ──┤
//   pick_pwave_GEO10 ──┤
//   pick_pwave_GEO11 ──┤
//   pick_pwave_GEO12 ──┘

package main

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/colonyos/colonies/pkg/client"
	"github.com/colonyos/colonies/pkg/core"
)

// Geophone represents a sensor installed in the mine
type Geophone struct {
	ID        string
	Level     string  // mine level name
	X         float64 // local mine coordinates (meters)
	Y         float64
	Z         float64 // depth below surface (negative = underground)
	Channel   string  // component (vertical/horizontal)
	Gain      float64 // sensor sensitivity
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

	// Geophones installed in the Kiruna mine (LKAB), realistic 3D positions
	// Coordinates in local mine grid (meters), Z negative = below surface
	geophones := []Geophone{
		{ID: "GEO01", Level: "L1165", X: 120.5, Y: 340.2, Z: -1165.0, Channel: "VZ", Gain: 14.0},
		{ID: "GEO02", Level: "L1165", X: 450.8, Y: 310.6, Z: -1165.0, Channel: "VZ", Gain: 14.0},
		{ID: "GEO03", Level: "L1165", X: 780.3, Y: 355.1, Z: -1165.0, Channel: "VZ", Gain: 14.0},
		{ID: "GEO04", Level: "L1252", X: 200.0, Y: 280.4, Z: -1252.0, Channel: "VZ", Gain: 28.0},
		{ID: "GEO05", Level: "L1252", X: 520.7, Y: 290.0, Z: -1252.0, Channel: "VZ", Gain: 28.0},
		{ID: "GEO06", Level: "L1252", X: 830.1, Y: 275.8, Z: -1252.0, Channel: "VZ", Gain: 28.0},
		{ID: "GEO07", Level: "L1350", X: 150.2, Y: 320.5, Z: -1350.0, Channel: "VZ", Gain: 28.0},
		{ID: "GEO08", Level: "L1350", X: 480.9, Y: 305.3, Z: -1350.0, Channel: "VZ", Gain: 28.0},
		{ID: "GEO09", Level: "L1350", X: 810.6, Y: 330.7, Z: -1350.0, Channel: "VZ", Gain: 28.0},
		{ID: "GEO10", Level: "L1450", X: 250.4, Y: 260.1, Z: -1450.0, Channel: "VZ", Gain: 56.0},
		{ID: "GEO11", Level: "L1450", X: 550.0, Y: 270.9, Z: -1450.0, Channel: "VZ", Gain: 56.0},
		{ID: "GEO12", Level: "L1450", X: 850.5, Y: 255.3, Z: -1450.0, Channel: "VZ", Gain: 56.0},
	}

	// Expected events per window (the classifier will output these)
	// In reality the number varies; we model a typical busy period
	eventsPerWindow := 3

	// 3D velocity model for the mine rock mass
	velocityModel := map[string]interface{}{
		"name":         "LKAB-Kiruna-3D-v4",
		"type":         "layered_3d",
		"p_velocity":   5800.0, // m/s average P-wave velocity in magnetite ore
		"s_velocity":   3350.0, // m/s average S-wave velocity
		"layers": []map[string]interface{}{
			{"z_top": 0, "z_bot": -800, "vp": 5200, "vs": 3000, "description": "waste rock"},
			{"z_top": -800, "z_bot": -1200, "vp": 5800, "vs": 3350, "description": "magnetite ore"},
			{"z_top": -1200, "z_bot": -1500, "vp": 6200, "vs": 3580, "description": "footwall"},
			{"z_top": -1500, "z_bot": -2000, "vp": 6000, "vs": 3460, "description": "deep rock"},
		},
	}

	// Continuous processing: submit a new workflow for each time window
	windowDurationSec := 60
	numWindows := 5

	c := client.CreateColoniesClient(host, port, true, false)

	fmt.Println("=== Mine Seismic Processing (RockSigma / LKAB Kiruna) ===")
	fmt.Printf("Mine: LKAB Kiruna iron ore mine\n")
	fmt.Printf("Geophones: %d (levels L1165 to L1450)\n", len(geophones))
	fmt.Printf("Window duration: %d seconds\n", windowDurationSec)
	fmt.Printf("Processing windows: %d (continuous stream)\n", numWindows)
	fmt.Printf("Expected events per window: ~%d\n", eventsPerWindow)
	fmt.Println()

	baseTime := time.Now()

	for w := 0; w < numWindows; w++ {
		windowStart := baseTime.Add(time.Duration(w*windowDurationSec) * time.Second)
		windowEnd := windowStart.Add(time.Duration(windowDurationSec) * time.Second)
		windowID := fmt.Sprintf("W%03d", w+1)

		fmt.Printf("--- Window %s: %s to %s ---\n",
			windowID,
			windowStart.Format("15:04:05"),
			windowEnd.Format("15:04:05"),
		)

		workflow := core.CreateWorkflowSpec(colonyName)

		// ================================================================
		// Step 1: P-wave arrival time picking on each geophone
		// ================================================================
		// Each geophone's seismogram is processed independently to find
		// the P-wave onset time. Uses STA/LTA for detection followed by
		// AIC (Akaike Information Criterion) for precise onset refinement.
		for _, geo := range geophones {
			pickSpec := core.CreateEmptyFunctionSpec()
			pickSpec.NodeName = fmt.Sprintf("pick_pwave_%s", geo.ID)
			pickSpec.FuncName = "pick_pwave"
			pickSpec.Conditions.ColonyName = colonyName
			pickSpec.Conditions.ExecutorType = "seismic-processor"
			pickSpec.MaxExecTime = 30
			pickSpec.MaxWaitTime = 60
			pickSpec.Label = windowID
			pickSpec.KwArgs = map[string]interface{}{
				"window_id":     windowID,
				"geophone_id":   geo.ID,
				"level":         geo.Level,
				"position":      map[string]float64{"x": geo.X, "y": geo.Y, "z": geo.Z},
				"channel":       geo.Channel,
				"gain":          geo.Gain,
				"sample_rate":   6000, // Hz, typical for mine seismology
				"window_start":  windowStart.Format(time.RFC3339Nano),
				"window_end":    windowEnd.Format(time.RFC3339Nano),
				"source":        fmt.Sprintf("rabbitmq://seismic.lkab.se/waveforms/%s", geo.ID),
				"detection": map[string]interface{}{
					"algorithm":       "STA/LTA",
					"sta_window_ms":   5.0,
					"lta_window_ms":   100.0,
					"trigger_ratio":   4.0,
					"detrigger_ratio": 2.0,
				},
				"refinement": map[string]interface{}{
					"algorithm":    "AIC",
					"window_ms":    20.0,
					"min_snr":      3.0,
				},
				"filter": map[string]interface{}{
					"type":    "bandpass",
					"low_hz":  50.0,
					"high_hz": 2000.0,
					"order":   4,
				},
			}
			workflow.AddFunctionSpec(pickSpec)
		}

		// ================================================================
		// Step 2: Event classification using ML
		// ================================================================
		// Collects all P-wave picks from the geophones, groups them by
		// temporal proximity, and classifies each group as:
		//   - blast (production or development)
		//   - rockburst / seismic event
		//   - noise / false trigger
		// Uses a trained neural network classifier.
		classifySpec := core.CreateEmptyFunctionSpec()
		classifySpec.NodeName = "classify_events"
		classifySpec.FuncName = "classify_events"
		classifySpec.Conditions.ColonyName = colonyName
		classifySpec.Conditions.ExecutorType = "seismic-processor"
		classifySpec.MaxExecTime = 60
		classifySpec.MaxWaitTime = 120
		classifySpec.Label = windowID

		for _, geo := range geophones {
			classifySpec.Conditions.Dependencies = append(
				classifySpec.Conditions.Dependencies,
				fmt.Sprintf("pick_pwave_%s", geo.ID),
			)
		}

		classifySpec.KwArgs = map[string]interface{}{
			"window_id":        windowID,
			"min_picks":        4,
			"association_window_ms": 200.0,
			"max_pick_residual_ms": 50.0,
			"classifier": map[string]interface{}{
				"model":   "rocksigma_event_classifier_v6.onnx",
				"type":    "cnn_1d",
				"classes": []string{"blast_production", "blast_development", "rockburst", "noise"},
				"features": []string{
					"pick_count", "pick_spread_ms", "amplitude_ratio",
					"frequency_content", "waveform_similarity",
					"spatial_distribution", "time_of_day",
				},
				"confidence_threshold": 0.7,
			},
			"blast_schedule": map[string]interface{}{
				"production_times": []string{"06:00", "14:00", "22:00"},
				"tolerance_min":    30,
			},
		}
		workflow.AddFunctionSpec(classifySpec)

		// ================================================================
		// Step 3: 3D hypocenter location for each event
		// ================================================================
		// For each event classified as blast or rockburst, invert the
		// P-wave arrival times to find the 3D source location (x, y, z)
		// inside the mine. Uses grid search + Simplex refinement with
		// the mine's 3D velocity model.
		for e := 1; e <= eventsPerWindow; e++ {
			eventID := fmt.Sprintf("%s-EVT%03d", windowID, e)

			locateSpec := core.CreateEmptyFunctionSpec()
			locateSpec.NodeName = fmt.Sprintf("locate_%s", eventID)
			locateSpec.FuncName = "locate_event_3d"
			locateSpec.Conditions.ColonyName = colonyName
			locateSpec.Conditions.ExecutorType = "seismic-processor"
			locateSpec.Conditions.Dependencies = []string{"classify_events"}
			locateSpec.MaxExecTime = 120
			locateSpec.MaxWaitTime = 300
			locateSpec.Label = windowID

			// Higher priority for larger (potentially more dangerous) events
			locateSpec.Priority = eventsPerWindow - e + 1

			locateSpec.KwArgs = map[string]interface{}{
				"window_id":       windowID,
				"event_id":        eventID,
				"event_index":     e,
				"velocity_model":  velocityModel,
				"location_method": map[string]interface{}{
					"algorithm":       "grid_search_simplex",
					"grid_spacing_m":  10.0,
					"simplex_tolerance_m": 0.5,
					"max_iterations":  1000,
				},
				"search_volume": map[string]interface{}{
					"x_min": 0.0, "x_max": 1000.0,
					"y_min": 0.0, "y_max": 600.0,
					"z_min": -1600.0, "z_max": -800.0,
				},
				"quality_criteria": map[string]interface{}{
					"min_picks":           4,
					"max_rms_residual_ms": 5.0,
					"max_gap_degrees":     180,
					"min_azimuthal_coverage": 90,
				},
				"magnitude_calculation": map[string]interface{}{
					"type":        "local_magnitude",
					"calibration": "LKAB-Kiruna-ML-v2",
					"attenuation": map[string]float64{
						"geometric": 1.0,
						"intrinsic_q": 200.0,
					},
				},
				"output_format": "seiscomp_xml",
				"alert_rules": map[string]interface{}{
					"magnitude_threshold": 1.5,
					"notify":              []string{"gruvdrift@lkab.com", "rocksigma-ops@rocksigma.se"},
					"sms_above_magnitude": 2.0,
				},
			}
			workflow.AddFunctionSpec(locateSpec)
		}

		// Submit this window's workflow
		graph, err := c.SubmitWorkflowSpec(workflow, executorPrvKey)
		if err != nil {
			fmt.Printf("  ERROR submitting workflow: %v\n", err)
			continue
		}

		fmt.Printf("  ProcessGraph ID: %s\n", graph.ID)
		fmt.Printf("  Processes: %d (%d picks + 1 classify + %d locations)\n",
			len(graph.ProcessIDs), len(geophones), eventsPerWindow)
		fmt.Println()
	}

	fmt.Println("=== Summary ===")
	fmt.Printf("Submitted %d workflows (one per %d-second window)\n", numWindows, windowDurationSec)
	fmt.Println("\nDAG structure (per window):")
	fmt.Printf("  Step 1 - P-wave picking:      %d geophones in parallel\n", len(geophones))
	fmt.Println("  Step 2 - Event classification: 1 ML classifier (depends on all picks)")
	fmt.Printf("  Step 3 - 3D location:          %d events in parallel (depends on classifier)\n", eventsPerWindow)
	fmt.Println("\nPipeline runs continuously, new window submitted every", windowDurationSec, "seconds")
}
