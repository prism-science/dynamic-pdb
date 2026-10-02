The Dynamic PDB organizes records around an **entry**, which connects a source dataset with structural models and the processing records behind them. Multiple models can be associated with one entry, allowing alternative interpretations of the data to be compared.

## Core records

| Record | What it represents |
| --- | --- |
| **Entry** | A source dataset and its associated files, models, and metadata. |
| **Model** | A structural representation, such as a single conformation or an ensemble, with associated files and reported quality scores. |
| **File** | A stored file or a reference to a file held elsewhere. |
| **Run** | A recorded execution of a program, including its software version, inputs, and outputs. |
| **Metric** | A numeric score describing a model's quality or agreement with experimental data. |

## Data levels

Files are assigned a level according to their role in the workflow. An entry may contain only some of these levels.

| Level | Contents | Examples |
| --- | --- | --- |
| **L0: Source** | Raw experimental data and source sequence files | Diffraction images, cryo-EM movies, FASTA sequences |
| **L1: Processed** | Experimental data prepared for modeling | Reflection files, density maps |
| **L2: Modeled** | Structural models and associated modeling files | PDB or mmCIF coordinates, refinement logs |
| **L3: Evaluated** | Evaluation and validation results | Reports assessing model quality or agreement with data |

Numeric scores, such as R-work and R-free, are also stored as metrics associated with a model. A data level describes a file's role, not its quality.

## Provenance and external references

Provenance records describe how inputs, software runs, and outputs are connected. They help users trace a model back to its supporting data and processing steps.

Files may be stored by The Dynamic PDB or referenced at an external location. PDB-linked entries can incorporate metadata and files from RCSB PDB.

> **Models in the same entry do not necessarily use identical processed data.** Before comparing their scores, check the input files and evaluation methods. Matching file checksums establish that the files are identical; they do not establish that the models were evaluated in the same way.

```lineage-examples
```

The Dynamic PDB builds on PDB records by organizing alternative models around shared experimental data and connecting them to their processing histories and evaluations. Files may be stored directly or referenced in external repositories. Direct storage is in development and likely to change.

## Interpreting comparison metrics and evaluations (L3)

Metrics for comparing models of structural variation across experimental techniques are in active development. The Dynamic PDB currently reports established measures of model quality and agreement with experimental data as the best available proxies for evaluating these models.

These measures capture specific aspects of model performance. They do not fully establish how accurately a model represents molecular motion or conformational diversity. Compare scores only when the underlying data and evaluation methods are comparable.
