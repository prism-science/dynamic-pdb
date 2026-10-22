-- Old-shape data as the application wrote it before the C1 schema migrations: model-level
-- ligand/cofactor lists (some malformed), comma-joined and odd mutations, duplicate and unknown
-- metric keys, bad sha256/size/type/purpose/direction, a run without software version.
-- Apply it to a database migrated up to V20261020130902, then apply the C1 migrations.
-- backend/internal/db/schema_migrations_test.go runs the same file; its ids are fixed there.

insert into users(id, source, external_ref, created_at, updated_at)
values ('00000000-0000-0000-0000-0000000000a1', 'test', 'fixture', now(), now());
insert into artifacts(id, name, level, sha256, size_bytes, created_by)
values ('00000000-0000-0000-0000-0000000000b1', 'seq', 'L0', 'ABC', -1, '00000000-0000-0000-0000-0000000000a1');
insert into artifacts(id, name, level, sha256, created_by)
values ('00000000-0000-0000-0000-0000000000b2', 'good', 'L2', repeat('a', 64), '00000000-0000-0000-0000-0000000000a1');

insert into entries(id, created_by) values ('e_pdb', '00000000-0000-0000-0000-0000000000a1'), ('e_free', '00000000-0000-0000-0000-0000000000a1'),
    ('e_known', '00000000-0000-0000-0000-0000000000a1'), ('e_plain', '00000000-0000-0000-0000-0000000000a1'), ('e_mixed', '00000000-0000-0000-0000-0000000000a1'),
    ('e_string', '00000000-0000-0000-0000-0000000000a1');
insert into entry_revisions(id, entry_id, state, title, metadata, created_by) values
    ('00000000-0000-0000-0000-00000000e001', 'e_pdb', 'active', 'pdb', '{"external_refs":{"pdb":"1ABC"}}', '00000000-0000-0000-0000-0000000000a1'),
    ('00000000-0000-0000-0000-00000000e002', 'e_free', 'active', 'free', '{"method":"X-ray"}', '00000000-0000-0000-0000-0000000000a1'),
    ('00000000-0000-0000-0000-00000000e003', 'e_known', 'active', 'known', '{"ligands":["GOL"]}', '00000000-0000-0000-0000-0000000000a1'),
    ('00000000-0000-0000-0000-00000000e004', 'e_plain', 'active', 'plain', '{}', '00000000-0000-0000-0000-0000000000a1'),
    ('00000000-0000-0000-0000-00000000e005', 'e_free', 'archived', 'free old', '{}', '00000000-0000-0000-0000-0000000000a1'),
    ('00000000-0000-0000-0000-00000000e006', 'e_mixed', 'active', 'mixed', '{}', '00000000-0000-0000-0000-0000000000a1'),
    ('00000000-0000-0000-0000-00000000e007', 'e_string', 'active', 'string', '{}', '00000000-0000-0000-0000-0000000000a1');

insert into models(id, entry_id, created_by, created_at) values
    ('m_pdb', 'e_pdb', '00000000-0000-0000-0000-0000000000a1', '2026-01-01'),
    ('m_free_1', 'e_free', '00000000-0000-0000-0000-0000000000a1', '2026-01-01'),
    ('m_free_2', 'e_free', '00000000-0000-0000-0000-0000000000a1', '2026-01-02'),
    ('m_known', 'e_known', '00000000-0000-0000-0000-0000000000a1', '2026-01-01'),
    ('m_plain', 'e_plain', '00000000-0000-0000-0000-0000000000a1', '2026-01-01'),
    ('m_mixed', 'e_mixed', '00000000-0000-0000-0000-0000000000a1', '2026-01-01'),
    ('m_string', 'e_string', '00000000-0000-0000-0000-0000000000a1', '2026-01-01');
insert into model_revisions(id, model_id, state, title, metadata, created_by) values
    ('00000000-0000-0000-0000-00000000f001', 'm_pdb', 'active', 'm', '{"model_type":"Deposited","purpose":"Reference","ligands":["HEM"],"cofactors":["ZN"]}', '00000000-0000-0000-0000-0000000000a1'),
    ('00000000-0000-0000-0000-00000000f002', 'm_free_1', 'active', 'm', '{"model_type":"Refinement","purpose":"Model Building","ligands":["ZN","ATP"],"cofactors":[]}', '00000000-0000-0000-0000-0000000000a1'),
    ('00000000-0000-0000-0000-00000000f003', 'm_free_2', 'active', 'm', '{"model_type":"Ensemble","purpose":"Bogus","ligands":["ATP","MG","ZN"]}', '00000000-0000-0000-0000-0000000000a1'),
    ('00000000-0000-0000-0000-00000000f004', 'm_known', 'active', 'm', '{"model_type":"Multiconformer","ligands":["NAG"]}', '00000000-0000-0000-0000-0000000000a1'),
    ('00000000-0000-0000-0000-00000000f005', 'm_plain', 'active', 'm', '{}', '00000000-0000-0000-0000-0000000000a1'),
    ('00000000-0000-0000-0000-00000000f006', 'm_mixed', 'active', 'm', '{"ligands":[1,null,{"a":1},"ATP",["x"],true],"cofactors":["ZN"]}', '00000000-0000-0000-0000-0000000000a1'),
    ('00000000-0000-0000-0000-00000000f007', 'm_string', 'active', 'm', '{"ligands":"ATP","cofactors":["MG"]}', '00000000-0000-0000-0000-0000000000a1');

insert into runs(id, name, software_name, software_version, created_by) values
    ('00000000-0000-0000-0000-0000000000c1', 'refine', 'phenix', '1.21', '00000000-0000-0000-0000-0000000000a1'),
    ('00000000-0000-0000-0000-0000000000c2', 'build', 'coot', null, '00000000-0000-0000-0000-0000000000a1');
insert into model_revision_runs(model_revision_id, run_id) values ('00000000-0000-0000-0000-00000000f004', '00000000-0000-0000-0000-0000000000c1');
insert into run_artifacts(run_id, artifact_id, direction) values
    ('00000000-0000-0000-0000-0000000000c1', '00000000-0000-0000-0000-0000000000b2', 'input'),
    ('00000000-0000-0000-0000-0000000000c2', '00000000-0000-0000-0000-0000000000b2', 'sideways');

insert into metrics(id, key, value) values
    ('00000000-0000-0000-0000-00000000a001', 'r_free', 0.2),
    ('00000000-0000-0000-0000-00000000a002', 'r_free', 0.3),
    ('00000000-0000-0000-0000-00000000a003', 'r_work', 0.18),
    ('00000000-0000-0000-0000-00000000a004', 'made_up', 1);
insert into model_revision_metrics(model_revision_id, metric_id) values
    ('00000000-0000-0000-0000-00000000f002', '00000000-0000-0000-0000-00000000a001'),
    ('00000000-0000-0000-0000-00000000f002', '00000000-0000-0000-0000-00000000a002'),
    ('00000000-0000-0000-0000-00000000f002', '00000000-0000-0000-0000-00000000a003'),
    ('00000000-0000-0000-0000-00000000f003', '00000000-0000-0000-0000-00000000a004');

insert into protein_sequences(id, entry_revision_id, source_artifact_id, record_index, header, sequence)
select ('00000000-0000-0000-0000-00000000c0' || lpad(n::text, 2, '0'))::uuid, '00000000-0000-0000-0000-00000000e001',
       '00000000-0000-0000-0000-0000000000b1', n, 'h', 'A' from generate_series(1, 13) n;
insert into polymer_entities(id, protein_sequence_id, metadata)
select ('00000000-0000-0000-0000-00000000d0' || lpad(n::text, 2, '0'))::uuid,
       ('00000000-0000-0000-0000-00000000c0' || lpad(n::text, 2, '0'))::uuid, metadata
from (values
    (1, '{"mutations":"C54T, C97A"}'::jsonb),
    (2, '{"mutations":"ENGINEERED MUTATION"}'),
    (3, '{"mutations":"C54T;DELETION,A(-3)G, K7R"}'),
    (4, '{"mutations":""}'),
    (5, '{"description":"no mutations key"}'),
    (6, '{"mutations":["C54T","X"]}'),
    (7, '{"mutations":["C54T"],"uniprot_mappings":[{"accession":"P1","source":"sifts"},{"accession":"P1","source":"alignment","unp_release":"2026_01"}]}'),
    (8, '{"mutations":"R1Q","uniprot_mappings":[{"accession":"P2","source":"struct_ref"}]}'),
    (9, '{"mutations":14}'),
    (10, '{"mutations":{"a":1}}'),
    (11, '{"mutations":true}'),
    (12, '{"mutations":null,"description":"explicit null"}'),
    (13, '{"mutations":["C54T",5]}')
) as seeded(n, metadata);
