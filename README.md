# Casper: Priority Model for Heterogeneous DAG Workloads

This repository contains the use case implementations and research paper for deriving a multi-dimensional priority model for heterogeneous DAG-based workloads in distributed computing continuums (ColonyOS).

## Methodology

The priority model was derived through a bottom-up, use-case-driven methodology using an iterative human-AI collaboration process:

1. **Use case identification**: Eight diverse, realistic workload types were identified to cover the breadth of workloads encountered in computing continuums.
2. **Implementation**: Each use case was implemented as a working Go program that constructs and submits DAG workflows (ProcessGraphs) to a ColonyOS instance. A dummy executor simulates execution of all process types.
3. **Domain refinement**: Several use cases were iteratively refined based on domain expert feedback (e.g., correcting mine seismology processing steps, matching real agentic executor architecture, specifying cloud burst and HPC training patterns).
4. **Priority analysis**: The eight use cases were analyzed to extract common priority dimensions, classified by temporal availability (static, dynamic, runtime-dependent, externally-dependent) and source (process-intrinsic, DAG-structural, parent-derived, system-state, user/tenant metadata).
5. **Requirements derivation**: Six requirements were identified that any priority model must satisfy: structural safety guarantees, multi-dimensional scoring, dynamic re-evaluation, multi-tenant fairness, interpretability, and preemption support.
6. **Method evaluation**: Six alternative scheduling approaches (weighted sum, rule-based, lexicographic, constraint-based optimization, RL/GNN, learned weights) were evaluated against the six requirements.
7. **Model proposal**: A hybrid model combining lexicographic urgency classes with weighted multi-dimensional scoring was proposed, with formal mathematical notation and instantiation examples for each use case.

## Use Cases

| # | Use Case | Key Priority Pattern | Directory |
|---|----------|---------------------|-----------|
| 1 | Mine Seismology (LKAB Kiruna) | Safety-critical, runtime-dependent (sensor count), continuous stream | `usecase1_seismic/` |
| 2 | Satellite Surveillance (Sentinel-2) | Multi-user, event-triggered, urgency by detection type | `usecase2_earth_observation/` |
| 3 | Agentic AI Workflows | Dynamic DAGs (AddChild), unpredictable resource consumption | `usecase3_agentic_ai/` |
| 4 | Multi-Tenant ETL Platform | SLA tiers (Gold/Silver/Bronze), deadline escalation, fairness | `usecase4_etl_pipeline/` |
| 5 | Cloud Burst for Seismic Processing | Meta-level workflow, externally-dependent priority, cost-aware | `usecase5_automation/` |
| 6 | Cloud-to-HPC LLM Training | Cross-infrastructure, long-running, bidirectional data transfer | `usecase6_hpc_ml/` |
| 7 | Drug Discovery Parameter Sweep | Deadline-driven funnel, data-dependent priority (binding affinity) | `usecase7_parameter_sweep/` |
| 8 | Water Treatment IoT | Hard real-time, preemption on edge gateways, mixed criticality | `usecase8_iot_preemption/` |

## Repository Structure

```
casper/
├── executor/                      # Dummy executor that handles all use case process types
├── usecase1_seismic/              # Mine seismology continuous monitoring
├── usecase2_earth_observation/    # Multi-user satellite surveillance
├── usecase3_agentic_ai/           # LLM-powered agentic workflows
├── usecase4_etl_pipeline/         # Multi-tenant ETL with SLA tiers
├── usecase5_automation/           # Cloud burst infrastructure automation
├── usecase6_hpc_ml/               # Cloud-to-HPC LLM fine-tuning
├── usecase7_parameter_sweep/      # Drug discovery MD parameter sweep
├── usecase8_iot_preemption/       # Water treatment IoT with preemption
├── paper/                         # IEEE conference paper (LaTeX)
│   ├── paper.tex
│   └── Makefile
├── go.mod
└── go.sum
```

## Building

```bash
go build ./...
```

## Generating the Paper

```bash
cd paper
make
```

Requires `texlive-latex-base`, `texlive-latex-recommended`, `texlive-latex-extra`, `texlive-fonts-recommended`, and `texlive-publishers`.

## Prompts Used

The following prompts were used in the human-AI collaboration to derive the use cases, analyze priority dimensions, and generate the paper. The process was iterative: the human provided domain corrections after each AI-generated draft.

### Phase 1: Exploration and Setup

**Prompt 1** — Explore the ColonyOS SDK:
> can you take a look at /home/johan/dev/github/colonyos/colonies

**Prompt 2** — Read the initial use case descriptions:
> then take a look at /home/johan/dev/github/colonyos/casper/use_cases.txt

### Phase 2: Use Case Implementation

**Prompt 3** — Implement all use cases:
> i want to you to implement them all in different directories, you can just implement a dummy executor that executes all processes, focus on the dag generation part, make the use cases very realistic, e.g. with dummy sensors generating data, and the users interested in different region of interests etc

### Phase 3: Domain Refinement (iterative corrections)

**Prompt 4** — Correct UC1 (mine seismology):
> in use case 1, there is a constant stream of sensor data that is ingested, there is not only one shot ingestion, this is done in 3 steps, first step is to select to calc a start time in the seismogram for pressure waves, step 2 is to group seismigram togeather into an event using machine learning classifier, step 3 is to for each event calculate a epicentrum in 3d in the mine.

**Prompt 5** — Correct UC2 (satellite surveillance):
> in usecase 2, user may want to select a area of interest in spawn a job once new satellite data for that region is available, then run a change detection algorithm to detect changes, then notify the users, there may be multiple such survewilance job running at the same time set by the user or different users

**Prompt 6** — Correct UC3 (agentic AI), pointing to real codebase:
> let's continue with the usecase3, actually it is easier if you just look at /home/johan/dev/github/colonyspace/agenticexecutor to figure out an example of such dag

**Prompt 7** — Correct UC5 (cloud burst extending UC1):
> let continue with usecase5 automation, let's extend usecase1, the colonies queue has built up and the users wants to cloud burst, this is done by adding cloud VMS, install kubernetes at these (k3s), then deploy the executors, transfer data using colonyos, then start the executors, also monitoring the costs

**Prompt 8** — Correct UC6 (cloud-to-HPC LLM training):
> now, continue with usecase6, the user uses a cloud platform to manage llm training, the cloud platform can connect to a HPC, transfer data, train the model, then transfer the trained model back to the cloud

**Prompt 9** — Free hands for remaining use cases (UC4, UC7):
> for the remaining of the use cases, i give you free hands to implement them

### Phase 4: Priority Analysis

**Prompt 10** — Explain the purpose and seed the priority discussion:
> the reason to develop many different use cases, and then derive an abstraction/model how to prioritize these different types of jobs

**Prompt 11** — Provide domain-specific priority examples:
> perfect, how should the priortization abstraction look like? in usecase1 for example, the user may want to priorize DAGS with many sensors data, which this indicate larger seismic events, they may also want to proritize certain regions in the underground mines, in use case 2, users may want to priortize certain regions, e.g. wildfires should have highest prioritie, or other regions that is more important and so on

### Phase 5: Method Selection

**Prompt 12** — Ask for recommendation:
> which one do you recommend?

**Prompt 13** — Ask about ML methods:
> can you generate a paper in latex ieee conference style, including a detailed description of the use cases, description of different methods, and then argumentation for the proposed method, before you do this, is there not a machine learning method too?

**Prompt 14** — Explore alternatives:
> it is really good, what alternative method exists?

**Prompt 15** — Push for research-grade formalism:
> no, this is a for research paper, how can express this in a generic way so that it is also easy to understand and develop, what is the best method?

### Phase 6: Gap Analysis and UC8

**Prompt 16** — Identify missing use cases:
> did we miss any important use case?

**Prompt 17** — Add preemption use case and discuss remaining gaps:
> no, add preemption with IoT, update the paper and discuss the gap in the rest

## Key Observations from the Process

1. **Domain expertise is essential**: The AI-generated initial use cases were structurally correct but lacked domain realism. Human corrections on mine seismology (3-step P-wave picking), satellite surveillance (multi-user triggered jobs), and agentic AI (real AddChild pattern) were critical.

2. **Bottom-up derivation works**: Starting from concrete, diverse use cases and extracting common patterns produced a more grounded model than top-down theorizing would have.

3. **Iterative refinement**: Each use case went through 1-3 revision cycles. The human provided the "what" (domain knowledge, system architecture) while the AI provided the "how" (Go implementation, formal model, LaTeX paper).

4. **Gap identification**: After implementing 7 use cases, systematic analysis revealed that preemption on resource-constrained edge devices was a missing pattern, leading to UC8.

5. **The priority model emerged from the use cases**: The six requirements (R1-R6) were not predefined — they were discovered by analyzing what the use cases needed. The hybrid lexicographic + weighted scoring model was chosen because no single existing method satisfied all six requirements.
