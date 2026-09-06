import styles from "./docs.module.css";

const MANIFEST_EXAMPLE = `version: 1
data_root: .

filter:
  include:
    - 9ZZZ

entries:
  - pdb_id: "{{ pdb_id }}"
    title: "{{ pdb_id }} room-temperature refinement"
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
        title: "{{ pdb_id }} refined model"
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

export default function UploadDocs() {
  return (
    <div className={styles.prose}>
      <p className={styles.lead}>
        <code className={styles.code}>dynamic-pdb</code> is the command line
        client for the registry. Use it to deposit datasets: entries, models,
        their files, and the metrics measured against them.
      </p>

      <h2 id="installation" className={styles.heading} style={{ marginTop: 40 }}>
        Installation
      </h2>
      <p className={styles.bodyText}>Install the client:</p>
      <pre className={styles.pre}>
        <code>curl -fsSL https://dynamicpdb.com/install.sh | bash</code>
      </pre>
      <p className={styles.bodyText}>macOS and Linux, amd64 and arm64.</p>

      <h2
        id="cli-authentication"
        className={styles.heading}
        style={{ marginTop: 40 }}
      >
        Authentication
      </h2>
      <p className={styles.bodyText}>
        Depositing requires an account. Log in with the{" "}
        <code className={styles.code}>login</code> command:
      </p>
      <pre className={styles.pre}>
        <code>dynamic-pdb login</code>
      </pre>
      <p className={styles.bodyText}>
        The command prints a URL and a one-time code; enter the code in your
        browser to authorize the client through GitHub.
      </p>
      <p className={styles.bodyText}>To log out:</p>
      <pre className={styles.pre}>
        <code>dynamic-pdb logout</code>
      </pre>

      <h2
        id="create-a-manifest"
        className={styles.heading}
        style={{ marginTop: 40 }}
      >
        Create a manifest
      </h2>
      <p className={styles.bodyText}>
        A manifest describes what to upload: the entries, their models, the
        artifacts belonging to each, and where metadata and metrics are read
        from. Generate a draft from a data folder:
      </p>
      <pre className={styles.pre}>
        <code>dynamic-pdb upload manifest init ./my-dataset</code>
      </pre>
      <p className={styles.bodyText}>
        This writes{" "}
        <code className={styles.code}>dynamic-pdb.manifest.yaml</code> to the
        current directory. Use <code className={styles.code}>--out</code> to
        write it elsewhere.
      </p>
      <pre className={styles.pre}>
        <code>{MANIFEST_EXAMPLE}</code>
      </pre>

      <h3 id="which-entries" className={styles.subheading}>
        Which entries get uploaded
      </h3>
      <p className={styles.bodyText}>
        <code className={styles.code}>{"{{ pdb_id }}"}</code> is a wildcard for
        a four-character PDB ID. The client walks{" "}
        <code className={styles.code}>data_root</code>, and every file that
        matches becomes an entry:
      </p>
      <pre className={styles.pre}>
        <code>
          {"manifest:  models/{{ pdb_id }}_model.pdb\n\n" +
            "on disk:   models/5rgd_model.pdb   →  entry 5RGD\n" +
            "           models/6zbx_model.pdb   →  entry 6ZBX"}
        </code>
      </pre>

      <h3 id="manifest-artifacts" className={styles.subheading}>
        Artifacts
      </h3>
      <p className={styles.bodyText}>
        Every artifact needs a <code className={styles.code}>source</code> and a{" "}
        <code className={styles.code}>level</code>:
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
      <p className={styles.bodyText}>
        <code className={styles.code}>level</code> is{" "}
        <code className={styles.code}>L0</code> raw source,{" "}
        <code className={styles.code}>L1</code> processed source,{" "}
        <code className={styles.code}>L2</code> structural models or{" "}
        <code className={styles.code}>L3</code> evaluations. Artifacts listed on
        the entry belong to the dataset; artifacts listed inside a model belong
        to that model.
      </p>

      <h3 id="metadata-and-metrics" className={styles.subheading}>
        Metadata and metrics
      </h3>
      <p className={styles.bodyText}>From JSON, by dotted path:</p>
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
      <p className={styles.bodyText}>From a PDB file, by REMARK field:</p>
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
      <p className={styles.bodyText}>From a CSV or TSV:</p>
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
      <p className={styles.bodyText}>From an mmCIF tag:</p>
      <pre className={styles.pre}>
        <code>
          {"extract:\n" +
            "  mmcif:\n" +
            "    field: _refine.ls_R_factor_R_free"}
        </code>
      </pre>

      <h2 id="upload-files" className={styles.heading} style={{ marginTop: 40 }}>
        Upload files
      </h2>
      <p className={styles.bodyText}>
        Pass the manifest to <code className={styles.code}>upload start</code>:
      </p>
      <pre className={styles.pre}>
        <code>dynamic-pdb upload start dynamic-pdb.manifest.yaml</code>
      </pre>
      <ul className={styles.flags}>
        <li>
          <code className={styles.code}>--concurrency &lt;n&gt;</code> — entries
          in parallel. Default 1.
        </li>
        <li>
          <code className={styles.code}>--upload-part-concurrency &lt;n&gt;</code>{" "}
          — parts per file in parallel. Default 1.
        </li>
        <li>
          <code className={styles.code}>--include &lt;pdb-id&gt;[,...]</code> —
          upload only these IDs.
        </li>
        <li>
          <code className={styles.code}>--skip &lt;pdb-id&gt;[,...]</code> —
          leave these IDs out.
        </li>
      </ul>
    </div>
  );
}
