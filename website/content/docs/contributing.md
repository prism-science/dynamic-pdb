> **The Dynamic PDB is not accepting external submissions at this time.** Deposition is currently restricted to approved GitHub organizations. To request deposition access, contact prism-core@astera.org.

Authorized depositors can contribute a new entry or add a model to an existing entry. Submissions require GitHub sign-in and an authorized account.

## Prepare your submission

Include the information needed to interpret and compare your results:

- **Coordinates:** Exactly one PDB or mmCIF coordinate file per model.
- **Supporting data:** The experimental files used for modeling or evaluation, uploaded directly or referenced by URL.
- **Processing records:** Software names, versions, and the inputs and outputs of recorded runs.
- **Quality scores:** Available metrics and information about how they were calculated.
- **Metadata:** Descriptive titles, model type, purpose, and relevant external references.

Check for an existing entry before creating a new one. When adding an alternative model, identify the processed data it uses so readers can determine whether comparisons are appropriate.

## Choose a submission method

| Method | Suitable for | Workflow |
| --- | --- | --- |
| **Website** | Individual entries or models | Complete the submission form, attach files or links, and submit for review |
| **Command-line client** | Batch submissions | Scan a folder, review the generated manifest, and upload |

The website accepts local files, HTTP file references, and imports from public Ext experiments. Review any automatically populated metadata before submitting.

The command-line client generates an editable YAML **manifest**, which describes the proposed entries, models, files, and metadata. Review this plan before uploading. Its current workflow groups files by PDB ID; files that cannot be matched to a PDB ID are excluded from the generated plan.

See [Uploading with the CLI](/docs/cli#installation) for installation and batch-upload instructions.

## Review and publication

New entries and models remain private while under review. Depositors can view their own pending and rejected submissions.

Entries and models are reviewed separately. A new entry must be approved before its models can be approved. Once approved, the active revision becomes publicly accessible. When a newer revision is approved, it replaces the previous active revision, which is archived.
