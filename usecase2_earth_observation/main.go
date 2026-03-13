// Use Case 2: Earth Observation - Satellite Surveillance Jobs
//
// Multiple users define surveillance jobs, each monitoring a geographic area
// of interest for changes. When new Sentinel-2 imagery becomes available for
// a region, a workflow is automatically spawned to:
//
//   1. Download the new satellite scene
//   2. Run change detection against the previous baseline
//   3. Notify the user with results
//
// Multiple surveillance jobs run concurrently, set up by different users with
// different areas, change detection algorithms, and notification preferences.
//
// DAG Structure (per surveillance job, per new scene):
//
//   check_new_data ──> download_scene ──> preprocess ──> change_detection ──> notify_user
//
// This program simulates the continuous polling loop: for each job it checks
// for new data, and when a new scene is found it submits the processing DAG.

package main

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/colonyos/colonies/pkg/client"
	"github.com/colonyos/colonies/pkg/core"
)

// SurveillanceJob represents a user-defined monitoring task
type SurveillanceJob struct {
	ID          string
	User        string
	Email       string
	Phone       string
	Description string
	Priority    int

	// Area of interest
	AOI     [][]float64 // polygon as [lon, lat] pairs
	AOIName string

	// What to detect
	DetectionType string            // "deforestation", "construction", "flooding", "wildfire", "coastline_erosion"
	Algorithm     string            // change detection algorithm
	AlgorithmCfg  map[string]interface{}

	// Data preferences
	MaxCloudCover int
	MinInterval   int // minimum days between checks
	Bands         []string

	// Notification preferences
	NotifyOnChange   bool
	NotifyAlways     bool
	WebhookURL       string
	IncludeMapInMail bool
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

	// Surveillance jobs created by different users
	jobs := []SurveillanceJob{
		{
			ID:          "SRV-001",
			User:        "anna.lindberg",
			Email:       "anna.lindberg@lansstyrelsen.se",
			Phone:       "+46701234567",
			Description: "Monitor illegal logging in Norrbotten nature reserve",
			Priority:    15,
			AOIName:     "norrbotten_nature_reserve",
			AOI: [][]float64{
				{19.42, 66.35}, {19.88, 66.35}, {19.88, 66.62}, {19.42, 66.62}, {19.42, 66.35},
			},
			DetectionType: "deforestation",
			Algorithm:     "bfast_monitor",
			AlgorithmCfg: map[string]interface{}{
				"history_period":    "2023-01-01/2025-12-31",
				"harmonic_order":    3,
				"significance":     0.05,
				"min_segment_size":  10,
				"vegetation_index":  "NDVI",
				"min_change_area_ha": 0.25,
			},
			MaxCloudCover:    20,
			MinInterval:      5,
			Bands:            []string{"B04", "B08", "B03", "SCL"},
			NotifyOnChange:   true,
			NotifyAlways:     false,
			IncludeMapInMail: true,
		},
		{
			ID:          "SRV-002",
			User:        "erik.johansson",
			Email:       "erik.johansson@trafikverket.se",
			Phone:       "",
			Description: "Track construction progress on new E10 highway section",
			Priority:    8,
			AOIName:     "e10_construction_kiruna",
			AOI: [][]float64{
				{20.15, 67.82}, {20.45, 67.82}, {20.45, 67.90}, {20.15, 67.90}, {20.15, 67.82},
			},
			DetectionType: "construction",
			Algorithm:     "coherence_change",
			AlgorithmCfg: map[string]interface{}{
				"baseline_date":     "2025-09-01",
				"method":            "cross_correlation",
				"window_size_px":    15,
				"change_threshold":  0.35,
				"classification":    []string{"bare_soil", "pavement", "structure", "vegetation_removed"},
				"min_change_area_m2": 500,
			},
			MaxCloudCover:    30,
			MinInterval:      10,
			Bands:            []string{"B02", "B03", "B04", "B08", "B11", "B12", "SCL"},
			NotifyOnChange:   false,
			NotifyAlways:     true,
			IncludeMapInMail: true,
			WebhookURL:       "https://byggsystem.trafikverket.se/api/satellite-updates",
		},
		{
			ID:          "SRV-003",
			User:        "maria.svensson",
			Email:       "maria.svensson@msb.se",
			Phone:       "+46709876543",
			Description: "Flood risk monitoring along Torne river valley",
			Priority:    25,
			AOIName:     "torne_river_flood",
			AOI: [][]float64{
				{23.50, 65.80}, {24.30, 65.80}, {24.30, 66.40}, {23.50, 66.40}, {23.50, 65.80},
			},
			DetectionType: "flooding",
			Algorithm:     "water_index_threshold",
			AlgorithmCfg: map[string]interface{}{
				"water_index":       "MNDWI",
				"threshold":         0.0,
				"baseline_index":    "NDWI",
				"baseline_period":   "2025-07-01/2025-08-31",
				"flood_extent_only": false,
				"include_depth_est": true,
				"dem_source":        "colonyfs:///dem/torne_valley_2m.tif",
				"min_flood_area_ha": 1.0,
			},
			MaxCloudCover:    40,
			MinInterval:      3,
			Bands:            []string{"B03", "B04", "B08", "B11", "B12", "SCL"},
			NotifyOnChange:   true,
			NotifyAlways:     false,
			IncludeMapInMail: true,
		},
		{
			ID:          "SRV-004",
			User:        "per.gustafsson",
			Email:       "per.gustafsson@naturvardsverket.se",
			Phone:       "+46703456789",
			Description: "Wildfire scar monitoring in Ljusdal area (2018 fire follow-up)",
			Priority:    12,
			AOIName:     "ljusdal_fire_recovery",
			AOI: [][]float64{
				{15.80, 61.60}, {16.40, 61.60}, {16.40, 62.00}, {15.80, 62.00}, {15.80, 61.60},
			},
			DetectionType: "wildfire",
			Algorithm:     "burn_severity_change",
			AlgorithmCfg: map[string]interface{}{
				"index":              "dNBR",
				"pre_fire_composite": "2018-06-01/2018-07-01",
				"severity_classes": map[string]interface{}{
					"unburned":      []float64{-0.1, 0.1},
					"low":           []float64{0.1, 0.27},
					"moderate_low":  []float64{0.27, 0.44},
					"moderate_high": []float64{0.44, 0.66},
					"high":          []float64{0.66, 1.3},
				},
				"recovery_tracking":   true,
				"ndvi_recovery_target": 0.6,
			},
			MaxCloudCover:    25,
			MinInterval:      14,
			Bands:            []string{"B04", "B08", "B8A", "B11", "B12", "SCL"},
			NotifyOnChange:   true,
			NotifyAlways:     false,
			IncludeMapInMail: true,
		},
		{
			ID:          "SRV-005",
			User:        "sofia.bergstrom",
			Email:       "sofia.bergstrom@sgu.se",
			Phone:       "",
			Description: "Coastline erosion monitoring along Skane south coast",
			Priority:    10,
			AOIName:     "skane_coastline",
			AOI: [][]float64{
				{13.30, 55.35}, {14.30, 55.35}, {14.30, 55.45}, {13.30, 55.45}, {13.30, 55.35},
			},
			DetectionType: "coastline_erosion",
			Algorithm:     "shoreline_extraction",
			AlgorithmCfg: map[string]interface{}{
				"method":              "NDWI_Otsu",
				"subpixel":            true,
				"subpixel_method":     "polynomial_fit",
				"reference_shoreline": "colonyfs:///reference/skane_shoreline_2020.geojson",
				"buffer_m":            500,
				"transect_spacing_m":  50,
				"min_retreat_m":       2.0,
				"tide_correction":     true,
				"tide_model":          "FES2014",
			},
			MaxCloudCover:    15,
			MinInterval:      30,
			Bands:            []string{"B02", "B03", "B04", "B08", "B11", "SCL"},
			NotifyOnChange:   true,
			NotifyAlways:     false,
			IncludeMapInMail: true,
			WebhookURL:       "https://kustdata.sgu.se/api/erosion-alerts",
		},
	}

	c := client.CreateColoniesClient(host, port, true, false)

	fmt.Println("=== Earth Observation - Satellite Surveillance Service ===")
	fmt.Printf("Active surveillance jobs: %d\n\n", len(jobs))
	for _, job := range jobs {
		fmt.Printf("  [%s] %-35s (user: %s, priority: %d)\n",
			job.ID, job.Description, job.User, job.Priority)
		fmt.Printf("         Area: %s  Detection: %s  Algorithm: %s\n",
			job.AOIName, job.DetectionType, job.Algorithm)
	}
	fmt.Println()

	// Simulate new satellite data arriving for some of the jobs
	// In production, a cron process would poll Copernicus Data Space for each
	// job's AOI and spawn a workflow when a new scene is found.
	newScenes := []struct {
		JobIndex  int
		SceneID   string
		SceneDate string
		Satellite string
		Orbit     int
		CloudPct  float64
		Tiles     []string
	}{
		{
			JobIndex:  0, // norrbotten deforestation
			SceneID:   "S2B_MSIL2A_20260312T103629_N0511_R008_T34WDS_20260312T134521",
			SceneDate: "2026-03-12",
			Satellite: "Sentinel-2B",
			Orbit:     8,
			CloudPct:  12.3,
			Tiles:     []string{"T34WDS"},
		},
		{
			JobIndex:  1, // E10 construction
			SceneID:   "S2A_MSIL2A_20260311T102021_N0511_R065_T34WES_20260311T142315",
			SceneDate: "2026-03-11",
			Satellite: "Sentinel-2A",
			Orbit:     65,
			CloudPct:  28.1,
			Tiles:     []string{"T34WES"},
		},
		{
			JobIndex:  2, // Torne river flooding
			SceneID:   "S2B_MSIL2A_20260312T100559_N0511_R022_T35WMN_20260312T130812",
			SceneDate: "2026-03-12",
			Satellite: "Sentinel-2B",
			Orbit:     22,
			CloudPct:  35.7,
			Tiles:     []string{"T35WMN", "T35WNN"},
		},
		{
			JobIndex:  4, // Skane coastline
			SceneID:   "S2A_MSIL2A_20260310T104021_N0511_R008_T33UUB_20260310T140233",
			SceneDate: "2026-03-10",
			Satellite: "Sentinel-2A",
			Orbit:     8,
			CloudPct:  8.5,
			Tiles:     []string{"T33UUB", "T33UVB"},
		},
	}

	fmt.Printf("New satellite data available for %d jobs:\n\n", len(newScenes))

	for _, scene := range newScenes {
		job := jobs[scene.JobIndex]

		if scene.CloudPct > float64(job.MaxCloudCover) {
			fmt.Printf("[%s] Scene %s skipped: cloud cover %.1f%% > max %d%%\n\n",
				job.ID, scene.SceneID[:40], scene.CloudPct, job.MaxCloudCover)
			continue
		}

		fmt.Printf("[%s] New scene for %s\n", job.ID, job.AOIName)
		fmt.Printf("  Scene: %s\n", scene.SceneID)
		fmt.Printf("  Date: %s  Cloud: %.1f%%  Satellite: %s\n",
			scene.SceneDate, scene.CloudPct, scene.Satellite)

		workflow := core.CreateWorkflowSpec(colonyName)
		jobTag := fmt.Sprintf("%s_%s", job.ID, scene.SceneDate)

		// ================================================================
		// Step 1: Download the new satellite scene
		// ================================================================
		downloadSpec := core.CreateEmptyFunctionSpec()
		downloadSpec.NodeName = "download_scene"
		downloadSpec.FuncName = "download_sentinel2_scene"
		downloadSpec.Conditions.ColonyName = colonyName
		downloadSpec.Conditions.ExecutorType = "eo-downloader"
		downloadSpec.MaxExecTime = 900
		downloadSpec.MaxWaitTime = 300
		downloadSpec.Priority = job.Priority
		downloadSpec.Label = jobTag
		downloadSpec.KwArgs = map[string]interface{}{
			"job_id":        job.ID,
			"user":          job.User,
			"scene_id":      scene.SceneID,
			"satellite":     scene.Satellite,
			"tiles":         scene.Tiles,
			"bands":         job.Bands,
			"aoi_polygon":   job.AOI,
			"aoi_name":      job.AOIName,
			"api":           "copernicus_dataspace",
			"product_type":  "S2MSI2A",
			"output_dir":    fmt.Sprintf("colonyfs:///surveillance/%s/scenes/%s/", job.ID, scene.SceneDate),
		}
		workflow.AddFunctionSpec(downloadSpec)

		// ================================================================
		// Step 2: Preprocess - atmospheric correction, cloud masking, co-registration
		// ================================================================
		preprocessSpec := core.CreateEmptyFunctionSpec()
		preprocessSpec.NodeName = "preprocess"
		preprocessSpec.FuncName = "preprocess_scene"
		preprocessSpec.Conditions.ColonyName = colonyName
		preprocessSpec.Conditions.ExecutorType = "eo-processor"
		preprocessSpec.Conditions.Dependencies = []string{"download_scene"}
		preprocessSpec.MaxExecTime = 600
		preprocessSpec.MaxWaitTime = 300
		preprocessSpec.Priority = job.Priority
		preprocessSpec.Label = jobTag
		preprocessSpec.KwArgs = map[string]interface{}{
			"job_id":         job.ID,
			"scene_id":       scene.SceneID,
			"scene_date":     scene.SceneDate,
			"input_dir":      fmt.Sprintf("colonyfs:///surveillance/%s/scenes/%s/", job.ID, scene.SceneDate),
			"cloud_masking": map[string]interface{}{
				"method":         "SCL",
				"valid_classes":  []int{4, 5, 6, 7}, // vegetation, not-vegetation, water, unclassified
				"buffer_px":      2,
				"min_valid_pct":  50.0,
			},
			"coregistration": map[string]interface{}{
				"enabled":        true,
				"reference":      fmt.Sprintf("colonyfs:///surveillance/%s/reference/baseline.tif", job.ID),
				"method":         "phase_correlation",
				"max_shift_px":   5,
			},
			"target_resolution_m": 10,
			"target_crs":          "EPSG:3006",
			"output_dir":          fmt.Sprintf("colonyfs:///surveillance/%s/preprocessed/%s/", job.ID, scene.SceneDate),
		}
		workflow.AddFunctionSpec(preprocessSpec)

		// ================================================================
		// Step 3: Run change detection algorithm
		// ================================================================
		changeSpec := core.CreateEmptyFunctionSpec()
		changeSpec.NodeName = "change_detection"
		changeSpec.FuncName = "detect_changes"
		changeSpec.Conditions.ColonyName = colonyName
		changeSpec.Conditions.ExecutorType = "eo-processor"
		changeSpec.Conditions.Dependencies = []string{"preprocess"}
		changeSpec.MaxExecTime = 1800
		changeSpec.MaxWaitTime = 600
		changeSpec.Priority = job.Priority
		changeSpec.Label = jobTag
		changeSpec.KwArgs = map[string]interface{}{
			"job_id":           job.ID,
			"detection_type":   job.DetectionType,
			"algorithm":        job.Algorithm,
			"algorithm_config": job.AlgorithmCfg,
			"scene_date":       scene.SceneDate,
			"input_dir":        fmt.Sprintf("colonyfs:///surveillance/%s/preprocessed/%s/", job.ID, scene.SceneDate),
			"baseline_dir":     fmt.Sprintf("colonyfs:///surveillance/%s/reference/", job.ID),
			"history_dir":      fmt.Sprintf("colonyfs:///surveillance/%s/preprocessed/", job.ID),
			"output": map[string]interface{}{
				"change_mask":     fmt.Sprintf("colonyfs:///surveillance/%s/results/%s/change_mask.tif", job.ID, scene.SceneDate),
				"change_vectors":  fmt.Sprintf("colonyfs:///surveillance/%s/results/%s/changes.geojson", job.ID, scene.SceneDate),
				"change_stats":    fmt.Sprintf("colonyfs:///surveillance/%s/results/%s/statistics.json", job.ID, scene.SceneDate),
				"change_map_png":  fmt.Sprintf("colonyfs:///surveillance/%s/results/%s/change_map.png", job.ID, scene.SceneDate),
			},
		}
		workflow.AddFunctionSpec(changeSpec)

		// ================================================================
		// Step 4: Notify the user
		// ================================================================
		notifySpec := core.CreateEmptyFunctionSpec()
		notifySpec.NodeName = "notify_user"
		notifySpec.FuncName = "notify_surveillance_result"
		notifySpec.Conditions.ColonyName = colonyName
		notifySpec.Conditions.ExecutorType = "notification-service"
		notifySpec.Conditions.Dependencies = []string{"change_detection"}
		notifySpec.MaxExecTime = 60
		notifySpec.MaxWaitTime = 120
		notifySpec.Priority = job.Priority
		notifySpec.Label = jobTag
		notifySpec.KwArgs = map[string]interface{}{
			"job_id":          job.ID,
			"job_description": job.Description,
			"user":            job.User,
			"email":           job.Email,
			"phone":           job.Phone,
			"aoi_name":        job.AOIName,
			"detection_type":  job.DetectionType,
			"scene_date":      scene.SceneDate,
			"scene_id":        scene.SceneID,
			"notify_on_change": job.NotifyOnChange,
			"notify_always":    job.NotifyAlways,
			"include_map":      job.IncludeMapInMail,
			"webhook_url":      job.WebhookURL,
			"results": map[string]interface{}{
				"change_vectors": fmt.Sprintf("colonyfs:///surveillance/%s/results/%s/changes.geojson", job.ID, scene.SceneDate),
				"change_stats":   fmt.Sprintf("colonyfs:///surveillance/%s/results/%s/statistics.json", job.ID, scene.SceneDate),
				"change_map_png": fmt.Sprintf("colonyfs:///surveillance/%s/results/%s/change_map.png", job.ID, scene.SceneDate),
			},
			"timestamp": time.Now().Format(time.RFC3339),
		}
		workflow.AddFunctionSpec(notifySpec)

		// Submit this job's workflow
		graph, err := c.SubmitWorkflowSpec(workflow, executorPrvKey)
		if err != nil {
			fmt.Printf("  ERROR submitting workflow: %v\n\n", err)
			continue
		}

		fmt.Printf("  ProcessGraph ID: %s\n", graph.ID)
		fmt.Printf("  Processes: %d (download -> preprocess -> change_detection -> notify)\n\n",
			len(graph.ProcessIDs))
	}

	fmt.Println("=== Summary ===")
	fmt.Printf("Active surveillance jobs: %d\n", len(jobs))
	fmt.Printf("New scenes processed: %d\n", len(newScenes))
	fmt.Println("\nDAG structure (per job, per new scene):")
	fmt.Println("  Step 1 - Download:         fetch scene tiles + bands for the AOI")
	fmt.Println("  Step 2 - Preprocess:       cloud mask, co-register, reproject")
	fmt.Println("  Step 3 - Change detection: run user-selected algorithm against baseline")
	fmt.Println("  Step 4 - Notify:           email/SMS/webhook with results + map")
	fmt.Println()
	fmt.Println("Each surveillance job runs independently. Multiple users can have")
	fmt.Println("overlapping AOIs with different detection algorithms. New workflows")
	fmt.Println("are spawned automatically when new satellite data becomes available.")
}
