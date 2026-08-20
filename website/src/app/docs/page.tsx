import AppFooter from "../components/AppFooter";

import styles from "./docs.module.css";

const MANIFEST_EXAMPLE = `version: 1
data_root: .

filter:
  include:
    - 9ZZZ

entries:
  - pdb_id: "{{ pdb_id }}"
    name: "{{ pdb_id }} room-temperature refinement"
    metadata:
      resolution:
        - source:
            files:
              - metadata/{{ pdb_id }}.json
          extract:
            json:
              field: entry.resolution
    artifacts:
      - id: fasta
        source:
          files:
            - sequences/{{ pdb_id }}.fasta
        level: L0
    models:
      - id: model_1
        name: "{{ pdb_id }} refined model"
        model_type: Single Conformer
        purpose: Refinement
        artifacts:
          - id: coordinates
            source:
              files:
                - models/{{ pdb_id }}_model.pdb
            level: L2
          - id: reflections
            source:
              files:
                - models/{{ pdb_id }}_model.mtz
            level: L1
        metrics:
          r_free:
            - source:
                artifact: coordinates
              extract:
                pdb:
                  field: REMARK 3 FREE R VALUE`;

/**
 * Console client reference.
 *
 * Facts come from the client, not from memory: commands from
 * cli/cmd/dynamic-pdb/main.go, flags from internal/frontend/{upload,update}.go,
 * install behaviour from install.sh, the token path from internal/paths, the
 * resume file from internal/upload/state.go, the manifest shape from
 * internal/upload/manifest/schema.go. The example is a trimmed copy of a
 * manifest that uploaded successfully. Nothing checks this page against the
 * code, so it needs updating whenever the client changes.
 */
export default function Docs() {
  return (
    <>
      <main className={styles.page}>
        <h1 className={styles.title}>Docs</h1>

        <div className={styles.prose}>
          <p className={styles.lead}>
            <code className={styles.code}>dynamic-pdb</code> is the command line
            client for the registry. Use it to deposit datasets: entries,
            models, their files, and the metrics measured against them.
          </p>

          <h2 className={styles.heading}>Installation</h2>
          <p className={styles.body}>Install the client:</p>
          <pre className={styles.pre}>
            <code>curl -fsSL https://dynamicpdb.com/install.sh | bash</code>
          </pre>
          <p className={styles.body}>macOS and Linux, amd64 and arm64.</p>

          <h2 className={styles.heading}>Authentication</h2>
          <p className={styles.body}>
            Depositing requires an account. Log in with the{" "}
            <code className={styles.code}>login</code> command:
          </p>
          <pre className={styles.pre}>
            <code>dynamic-pdb login</code>
          </pre>
          <p className={styles.body}>
            The command prints a URL and a one-time code; enter the code in your
            browser to authorize the client through GitHub.
          </p>
          <p className={styles.body}>To log out:</p>
          <pre className={styles.pre}>
            <code>dynamic-pdb logout</code>
          </pre>

          <h2 className={styles.heading}>Create a manifest</h2>
          <p className={styles.body}>
            A manifest describes what to upload: the entries, their models,
            the artifacts belonging to each, and where metadata and metrics are
            read from. Generate a draft from a data folder:
          </p>
          <pre className={styles.pre}>
            <code>dynamic-pdb upload manifest init ./my-dataset</code>
          </pre>
          <p className={styles.body}>
            This writes{" "}
            <code className={styles.code}>dynamic-pdb.manifest.yaml</code> to
            the current directory. Use <code className={styles.code}>--out</code>{" "}
            to write it elsewhere.
          </p>
          <pre className={styles.pre}>
            <code>{MANIFEST_EXAMPLE}</code>
          </pre>
          <h3 className={styles.subheading}>Which entries get uploaded</h3>
          <p className={styles.body}>
            <code className={styles.code}>{"{{ pdb_id }}"}</code> is a wildcard
            for a four-character PDB ID. The client walks{" "}
            <code className={styles.code}>data_root</code>, and every file that
            matches becomes an entry:
          </p>
          <pre className={styles.pre}>
            <code>
              {"manifest:  models/{{ pdb_id }}_model.pdb\n\n" +
                "on disk:   models/5rgd_model.pdb   \u2192  entry 5RGD\n" +
                "           models/6zbx_model.pdb   \u2192  entry 6ZBX"}
            </code>
          </pre>
          <h3 className={styles.subheading}>Artifacts</h3>
          <p className={styles.body}>
            Every artifact needs a <code className={styles.code}>source</code>{" "}
            and a <code className={styles.code}>level</code>:
          </p>
          <pre className={styles.pre}>
            <code>
              {"artifacts:\n" +
                "  - id: coordinates\n" +
                "    source:\n" +
                "      files:\n" +
                "        - models/{{ pdb_id }}_model.pdb\n" +
                "    level: L2"}
            </code>
          </pre>
          <p className={styles.body}>
            <code className={styles.code}>level</code> is{" "}
            <code className={styles.code}>L0</code> raw source,{" "}
            <code className={styles.code}>L1</code> processed source,{" "}
            <code className={styles.code}>L2</code> structural models or{" "}
            <code className={styles.code}>L3</code> evaluations. Artifacts
            listed on the entry belong to the dataset; artifacts listed inside a
            model belong to that model.
          </p>

          <h3 className={styles.subheading}>Metadata and metrics</h3>
          <p className={styles.body}>From JSON, by dotted path:</p>
          <pre className={styles.pre}>
            <code>
              {"metadata:   # title, method, resolution, organism, space_group\n" +
                "  resolution:\n" +
                "    - source:\n" +
                "        files:\n" +
                "          - metadata/{{ pdb_id }}.json\n" +
                "      extract:\n" +
                "        json:\n" +
                "          field: entry.resolution"}
            </code>
          </pre>
          <p className={styles.body}>From a PDB file, by REMARK field:</p>
          <pre className={styles.pre}>
            <code>
              {"metrics:   # r_work, r_free, clashscore, ramachandran_outliers\n" +
                "  r_free:\n" +
                "    - source:\n" +
                "        artifact: coordinates   # an artifact declared above, by id\n" +
                "      extract:\n" +
                "        pdb:\n" +
                "          field: REMARK 3 FREE R VALUE"}
            </code>
          </pre>
          <p className={styles.body}>From a CSV or TSV:</p>
          <pre className={styles.pre}>
            <code>
              {"extract:\n" +
                "  csv:              # or tsv:\n" +
                "    column: r_free\n" +
                "    where:\n" +
                "      column: pdb_id\n" +
                '      equals: "{{ pdb_id }}"'}
            </code>
          </pre>
          <p className={styles.body}>From an mmCIF tag:</p>
          <pre className={styles.pre}>
            <code>
              {"extract:\n" +
                "  mmcif:\n" +
                "    field: _refine.ls_R_factor_R_free"}
            </code>
          </pre>

          <h2 className={styles.heading}>Upload files</h2>
          <p className={styles.body}>
            Pass the manifest to{" "}
            <code className={styles.code}>upload start</code>:
          </p>
          <pre className={styles.pre}>
            <code>dynamic-pdb upload start dynamic-pdb.manifest.yaml</code>
          </pre>
          <ul className={styles.flags}>
            <li>
              <code className={styles.code}>--concurrency &lt;n&gt;</code> —
              entries in parallel. Default 1.
            </li>
            <li>
              <code className={styles.code}>
                --upload-part-concurrency &lt;n&gt;
              </code>{" "}
              — parts per file in parallel. Default 1.
            </li>
            <li>
              <code className={styles.code}>--include &lt;pdb-id&gt;[,...]</code>{" "}
              — upload only these IDs.
            </li>
            <li>
              <code className={styles.code}>--skip &lt;pdb-id&gt;[,...]</code> —
              leave these IDs out.
            </li>
          </ul>
        </div>
      </main>

      <AppFooter />
    </>
  );
}
