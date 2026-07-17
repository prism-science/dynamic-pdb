# Dynamic PDB System Design Specification

## 1. Context and Goals

Dynamic PDB is a system for registering experimental structural biology data and the derived models that try to explain that data.

The core idea is that experimental data is the stable anchor, while processed datasets, structural models, algorithm runs, artifacts, and metrics are evolving interpretations attached to that anchor. The system should make those relationships searchable, reproducible, and comparable over time.

The first version focuses on Diffuse Hub workflows:

- registering hub entries around the rawest available experimental data, such as diffraction images or PDB/MTZ reflection data
- attaching derived data entities, including processed maps, deposited models, qFit outputs, Sampleworks outputs, MD-derived models, and other structural interpretations
- tracking provenance for every entity: inputs, outputs, algorithm, parameters, logs, authors, timestamps, versions, and hashes
- comparing models against experimental data using quality and fit metrics
- supporting search by protein, sequence, resolution, data type, software run, metric values, tags, groups, experiments, and publications
- connecting metadata in the web app to files stored in federated backend storage
- supporting internal scientific workflows first, while keeping the design extensible for external collaborators and public data access

The system should reduce manual bookkeeping for scientists. When data is collected, processed, or modeled, the hub should make it easy to register the result, attach the right files, preserve run context, and expose the result through the website, API, CLI, or notebook workflows.

The main product goal is to make the repository live: as algorithms improve, existing experimental data can be reprocessed, rescored, and compared against prior results instead of becoming a static one-time deposit.

## 2. Domain Model

## 3. Core Workflows

## 4. System Architecture

## 5. Data and Storage Design

## 6. API and Interface Design

## 7. Security and Access Control

## 8. Operations, Risks, and Open Questions
