# Dynamic PDB

## Overview

Dynamic PDB is a scientific data registry for structural biology. It connects source measurements, processed datasets, structural models, and model-quality metrics into a single traceable workflow.

Unlike a traditional structure archive, Dynamic PDB allows the same source dataset to be reprocessed and interpreted by multiple algorithms over time. Researchers can see how each model was produced and how well it explains the underlying measurement data.

## Purpose

The project provides a central place to:

* register source datasets;
* store processed data and structural models;
* track input-to-output relationships;
* compare multiple structural models against the same source data;
* preserve software, parameters, and logs required to reproduce a result;
* rerun improved algorithms on existing datasets.

## Core structure

### Structure

A Structure represents one baseline source dataset.

The baseline is the least-processed data available, such as raw diffraction images or an existing MTZ file. If the baseline dataset changes, a new Structure is created.

### Model

A Model groups related processing or modeling work inside a Structure, for example:

* diffraction-data processing;
* refinement;
* qFit analysis;
* Sampleworks analysis;
* molecular-dynamics simulation;
* parameter sweep.

### Entity

An Entity is an individual data object stored in the system.

Every Entity belongs to a Structure and may also belong to a Model.

### Entity Relation

Entity Relations describe dependencies between objects:

```text
processed_from
generated_from
refined_from
evaluates
```

This creates a graph showing exactly which inputs produced each output.

## Data levels

Dynamic PDB uses the L0–L3 data maturity model.

### L0 — Raw source data

Original instrument output.

Examples:

```text
HDF5 diffraction datasets
raw diffraction images
cryo-EM particles
```

### L1 — Processed source data

Measurements extracted or reconstructed from raw data.

Examples:

```text
MTZ reflection files
CCP4 / MRC / DSN6 density maps
NXS diffuse-scattering maps
```

### L2 — Structural models

Atomic interpretations of the source data.

Examples:

```text
PDB
CIF
mmCIF
single-conformer models
multiconformer models
MD ensembles and trajectories
```

### L3 — Model-to-data evaluations

Measurements of how well a model explains source data.

Examples:

```text
Rwork
Rfree
CC
RSCC
Clashscore
Ramachandran outlier percentage
Side-chain outlier percentage
```

The proposal explicitly requires model geometry metrics and source-fit metrics to be stored for structural models.

## Typical workflow

```text
Raw diffraction images
        ↓
Reflection data and density maps
        ↓
Atomic or ensemble models
        ↓
Model-to-data evaluation metrics
```

Another supported workflow starts with existing PDB data:

```text
MTZ + existing model
        ↓
Sampleworks / qFit / refinement / MD
        ↓
New structural models
        ↓
Comparison against the original source data
```

The system must support multiple processed datasets and multiple models derived from the same baseline source data.

## Stored files

Each Entity can reference a primary file and optional supporting files.

```text
Primary files:
.h5
.mtz
.nxs
.ccp4
.mrc
.dsn6
.pdb
.cif
.mmcif
MD trajectory formats

Supporting files:
processing logs
parameter files
configuration files
validation reports
software output
```

Supporting files preserve the exact algorithm, parameters, inputs, and outputs used to create an Entity.

## Database structure

```text
structures
models
entities
entity_relations
```

```text
Structure
├── Models
├── Entities
└── Entity Relations
```

The `entities` table stores the data level, type, name, metadata, file references, and scientific properties of each object.

The `entity_relations` table forms the provenance graph connecting input data, processing results, models, and evaluations.

## Main result

Dynamic PDB provides a living view of structural biology data:

```text
source data
→ processing history
→ structural interpretations
→ measurable model quality
```

Instead of publishing one final structure and leaving it unchanged, researchers can continuously add improved models and compare them against the same source evidence.
