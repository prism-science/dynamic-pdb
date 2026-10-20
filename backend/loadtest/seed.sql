-- Seeds synthetic active entries, models and metrics for load testing.
-- Variables (psql -v): entries, models_per_entry, residues_per_model.

\if :{?entries}
\else
\set entries 10000
\endif
\if :{?models_per_entry}
\else
\set models_per_entry 3
\endif
\if :{?residues_per_model}
\else
\set residues_per_model 200
\endif
\set user_id '00000000-0000-4000-8000-00000000107e'

begin;

insert into users(id, source, external_ref, display_name, created_at, updated_at)
values (:'user_id', 'loadtest', 'loadtest', 'Load test', now(), now())
on conflict (id) do nothing;

insert into entries(id, created_by, created_at, state)
select format('lt_%s', lpad(n::text, 7, '0')),
       :'user_id',
       now() - make_interval(mins => n),
       'active'
from generate_series(1, :entries) as n;

insert into entry_revisions(id, entry_id, revision_number, state, entry_state, published_at,
                            title, metadata, created_by, created_at, updated_at)
select gen_random_uuid(),
       entries.id,
       1,
       'active',
       'active',
       entries.created_at,
       format('Load test entry %s', entries.id),
       jsonb_build_object(
           'external_refs', jsonb_build_object('pdb', upper(entries.id)),
           'details', 'Synthetic crystal structure seeded for load testing',
           'resolution', round(1.2 + random()::numeric * 2.5, 2),
           'method', 'X-ray crystallography',
           'space_group', 'P 21 21 21',
           'crystallography', '{"crystals": [{"id": "1", "growth": {"ph": 7.5, "temperature_kelvin": 293}, "diffractions": [{"id": "1", "temperature_kelvin": 100}]}]}'::jsonb
       ),
       :'user_id',
       entries.created_at,
       entries.created_at
from entries
where entries.created_by = :'user_id';

insert into models(id, entry_id, created_by, created_at, state)
select format('%s_m_%s', entries.id, lpad(model_number::text, 3, '0')),
       entries.id,
       :'user_id',
       entries.created_at + make_interval(secs => model_number),
       'active'
from entries
cross join generate_series(1, :models_per_entry) as model_number
where entries.created_by = :'user_id';

-- Every third model carries per-residue data, the largest field in model list responses.
insert into model_revisions(id, model_id, revision_number, state, model_state, published_at,
                            title, metadata, created_by, created_at, updated_at)
select gen_random_uuid(),
       models.id,
       1,
       'active',
       'active',
       models.created_at,
       format('Load test model %s', models.id),
       jsonb_build_object(
           'authors', jsonb_build_array('Load, T.', 'Seed, D.'),
           'details', 'Synthetic model seeded for load testing',
           'purpose', 'Refinement',
           'model_type', 'Single Conformer',
           'atom_count', 1500,
           'modeled_residues', :residues_per_model,
           'unique_protein_chains', 1,
           'ligands', jsonb_build_array('ATP')
       ) || case
           when row_number() over (order by models.id) % 3 = 0 then jsonb_build_object(
               'residue_data', (
                   select jsonb_agg(jsonb_build_object(
                       'label_asym_id', 'A',
                       'label_seq_id', residue,
                       'label_comp_id', 'ALA',
                       'rscc', round(0.7 + random()::numeric * 0.3, 3),
                       'b_iso', round(15 + random()::numeric * 40, 2),
                       'occupancy', 1.0
                   ))
                   from generate_series(1, :residues_per_model) as residue
               )
           )
           else '{}'::jsonb
       end,
       :'user_id',
       models.created_at,
       models.created_at
from models
where models.created_by = :'user_id';

create temporary table seeded_metrics on commit drop as
select gen_random_uuid() as id,
       model_revisions.id as model_revision_id,
       metric_keys.key,
       metric_keys.base_value + random() * metric_keys.spread as value,
       model_revisions.created_at
from model_revisions
cross join (values ('r_work', 0.15, 0.08),
                   ('r_free', 0.19, 0.08),
                   ('clashscore', 2.0, 10.0),
                   ('molprobity_score', 1.0, 2.0)) as metric_keys(key, base_value, spread)
where model_revisions.created_by = :'user_id';

insert into metrics(id, key, value, created_at)
select id, key, value, created_at
from seeded_metrics;

insert into model_revision_metrics(model_revision_id, metric_id)
select model_revision_id, id
from seeded_metrics;

commit;

analyze;

select (select count(*) from entries) as entries,
       (select count(*) from models) as models,
       (select count(*) from metrics) as metrics;
