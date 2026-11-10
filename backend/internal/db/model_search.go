package db

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"

	"github.com/lib/pq"

	"dynamic-pdb/backend/internal/models"
)

// ModelSearch holds the search filters of GET /v1/models. Different filters must all match;
// several values of one filter mean any of them. Entry filters read the active revision of the
// model's entry; model filters read the model revision the row is, so they never mix two models of
// one entry. A missing value (no resolution, unknown Ligands and Cofactors, no metric value) never
// satisfies a filter that asks for a value or a range; MissingMetrics is the way to find missing
// metric values.
type ModelSearch struct {
	// Methods matches the entry's experimental method, ignoring case.
	Methods []string
	// ResolutionMin and ResolutionMax bound the entry's resolution, inclusive.
	ResolutionMin *float64
	ResolutionMax *float64
	// Organisms matches a polymer entity's source organism: a scientific name, ignoring case, or
	// an NCBI taxonomy ID for a value of digits only.
	Organisms []string
	// UniProtAccessions matches a polymer entity's UniProt mapping, ignoring case.
	UniProtAccessions []string
	// LigandsOrCofactors matches a chemical component ID in the entry's Ligands or Cofactors.
	LigandsOrCofactors []string

	ModelTypes []models.StructureModelType
	Purposes   []models.ModelPurpose
	Sources    []models.ModelSearchSource
	// DataLevels matches a file of one of these levels attached to the model revision.
	DataLevels []models.ArtifactLevel
	// Compounds matches a chemical component ID in the model's coordinate file.
	Compounds []string
	// MetricBounds are inclusive bounds on metric values; each bound must hold.
	MetricBounds []MetricBound
	// MissingMetrics matches a model with no value for at least one of these metrics.
	MissingMetrics []models.MetricKey
	// Flagged true matches models with an open review flag, false models without one.
	Flagged *bool
}

// MetricBound bounds one metric's value, inclusive. A nil end is open.
type MetricBound struct {
	Key models.MetricKey
	Min *float64
	Max *float64
}

// The indexed expressions below are also written, without the table name, in the index
// migrations V20261109120000 to V20261109120800; the planner uses an index only for an expression
// written the same way. TestModelSearchIndexExpressions checks the two agree.
const (
	modelTypeExpression    = `(model_revisions.metadata ->> 'model_type')`
	modelPurposeExpression = `(model_revisions.metadata ->> 'purpose')`
	// archiveModelExpression is models.ModelMetadata.IsArchiveModel in SQL: the source filter's
	// "PDB archive" (researcher question 4). It is never null, so "not archive" is its negation.
	archiveModelExpression = `(coalesce(model_revisions.metadata ->> 'origin', '') = 'archive' ` +
		`or coalesce(model_revisions.metadata ->> 'model_type', '') = 'Deposited')`
	// entryResolutionExpression is null when the resolution is missing or not a number.
	entryResolutionExpression = `(case when jsonb_typeof(entry_revisions.metadata -> 'resolution') = 'number' ` +
		`then cast(entry_revisions.metadata ->> 'resolution' as numeric) end)`
	// The curated lists keep the case people gave, so they are lowercased like the search value.
	// Lowercasing JSON text keeps it valid (escapes such as \n stay lower case); uppercasing would not.
	entryLigandsExpression   = `cast(lower(cast(entry_revisions.metadata -> 'ligands' as text)) as jsonb)`
	entryCofactorsExpression = `cast(lower(cast(entry_revisions.metadata -> 'cofactors' as text)) as jsonb)`
	// sourceOrganismsExpression lowercases the organism list, so a name matches ignoring case.
	sourceOrganismsExpression = `cast(lower(cast(polymer_entities.metadata -> 'source_organisms' as text)) as jsonb)`
	// uniProtMappingsExpression lowercases the mappings, as accessions keep the case their source gave.
	uniProtMappingsExpression = `cast(lower(cast(polymer_entities.metadata -> 'uniprot_mappings' as text)) as jsonb)`
)

var taxonomyIDPattern = regexp.MustCompile(`^[0-9]+$`)

// readsEntryRevision reports whether a condition reads the active entry_revisions row: the entry
// and polymer entity filters. The flagged filter finds the active entry revision on its own.
func (search ModelSearch) readsEntryRevision() bool {
	return len(search.Methods) > 0 || search.ResolutionMin != nil || search.ResolutionMax != nil ||
		len(search.LigandsOrCofactors) > 0 || len(search.Organisms) > 0 || len(search.UniProtAccessions) > 0
}

// modelSearchConditions turns the search into SQL conditions on model_revisions, models and the
// left-joined active entry_revisions row, adding their arguments to args.
func modelSearchConditions(search ModelSearch, args map[string]any) ([]string, error) {
	builders := []func(ModelSearch, map[string]any) ([]string, error){
		entrySearchConditions,
		polymerEntitySearchConditions,
		modelMetadataSearchConditions,
		modelFileSearchConditions,
		metricSearchConditions,
		reviewFlagSearchConditions,
	}
	conditions := make([]string, 0)
	for _, build := range builders {
		built, err := build(search, args)
		if err != nil {
			return nil, err
		}
		conditions = append(conditions, built...)
	}
	return conditions, nil
}

func entrySearchConditions(search ModelSearch, args map[string]any) ([]string, error) {
	conditions := make([]string, 0)
	if len(search.Methods) > 0 {
		methods := make([]string, 0, len(search.Methods))
		for _, method := range search.Methods {
			methods = append(methods, strings.ToLower(strings.TrimSpace(method)))
		}
		conditions = append(conditions,
			"lower(entry_revisions.metadata ->> 'method') = any(cast(:search_methods as text[]))")
		args["search_methods"] = pq.StringArray(methods)
	}
	for _, bound := range []struct {
		name     string
		value    *float64
		operator string
	}{
		{name: "search_resolution_min", value: search.ResolutionMin, operator: ">="},
		{name: "search_resolution_max", value: search.ResolutionMax, operator: "<="},
	} {
		if bound.value == nil {
			continue
		}
		if err := checkFinite("resolution bound", *bound.value); err != nil {
			return nil, err
		}
		conditions = append(conditions, fmt.Sprintf("%s %s :%s", entryResolutionExpression, bound.operator, bound.name))
		args[bound.name] = *bound.value
	}
	if len(search.LigandsOrCofactors) > 0 {
		// One filter matches either curated list (researcher question 6).
		alternatives := make([]string, 0, 2*len(search.LigandsOrCofactors))
		for index, compound := range search.LigandsOrCofactors {
			name := fmt.Sprintf("search_ligand_or_cofactor_%d", index)
			value, err := jsonArrayOf(strings.ToLower(strings.TrimSpace(compound)))
			if err != nil {
				return nil, fmt.Errorf("encode ligand or cofactor filter: %w", err)
			}
			args[name] = value
			alternatives = append(alternatives,
				fmt.Sprintf("%s @> cast(:%s as jsonb)", entryLigandsExpression, name),
				fmt.Sprintf("%s @> cast(:%s as jsonb)", entryCofactorsExpression, name))
		}
		conditions = append(conditions, "("+strings.Join(alternatives, " or ")+")")
	}
	return conditions, nil
}

func polymerEntitySearchConditions(search ModelSearch, args map[string]any) ([]string, error) {
	conditions := make([]string, 0)
	if len(search.Organisms) > 0 {
		alternatives := make([]string, 0, len(search.Organisms))
		for index, organism := range search.Organisms {
			name := fmt.Sprintf("search_organism_%d", index)
			value, err := organismContainment(organism)
			if err != nil {
				return nil, err
			}
			args[name] = value
			alternatives = append(alternatives, fmt.Sprintf("%s @> cast(:%s as jsonb)", sourceOrganismsExpression, name))
		}
		conditions = append(conditions, entryPolymerEntityExists(strings.Join(alternatives, " or ")))
	}
	if len(search.UniProtAccessions) > 0 {
		alternatives := make([]string, 0, len(search.UniProtAccessions))
		for index, accession := range search.UniProtAccessions {
			name := fmt.Sprintf("search_uniprot_%d", index)
			value, err := json.Marshal([]map[string]string{{"accession": strings.ToLower(strings.TrimSpace(accession))}})
			if err != nil {
				return nil, fmt.Errorf("encode UniProt filter: %w", err)
			}
			args[name] = string(value)
			alternatives = append(alternatives, fmt.Sprintf("%s @> cast(:%s as jsonb)", uniProtMappingsExpression, name))
		}
		conditions = append(conditions, entryPolymerEntityExists(strings.Join(alternatives, " or ")))
	}
	return conditions, nil
}

// entryPolymerEntityExists matches when a polymer entity of the active entry revision meets condition.
func entryPolymerEntityExists(condition string) string {
	return `exists (select 1
		from entry_revision_polymer_entities search_entity_link
		join polymer_entities on polymer_entities.id = search_entity_link.polymer_entity_id
		where search_entity_link.entry_revision_id = entry_revisions.id
		  and (` + condition + `))`
}

// organismContainment is the JSON a lowercased source_organisms list contains when it has the
// organism: by NCBI taxonomy ID for digits, else by scientific name.
func organismContainment(organism string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(organism))
	var containment any = []map[string]string{{"scientific_name": value}}
	if taxonomyIDPattern.MatchString(value) {
		taxonomyID := strings.TrimLeft(value, "0")
		if taxonomyID == "" {
			return "", fmt.Errorf("organism filter %q: NCBI taxonomy IDs start at 1: %w", organism, ErrInvalidModelSearch)
		}
		containment = []map[string]json.Number{{"ncbi_taxonomy_id": json.Number(taxonomyID)}}
	}
	data, err := json.Marshal(containment)
	if err != nil {
		return "", fmt.Errorf("encode organism filter: %w", err)
	}
	return string(data), nil
}

func modelMetadataSearchConditions(search ModelSearch, args map[string]any) ([]string, error) {
	conditions := make([]string, 0)
	if len(search.ModelTypes) > 0 {
		values, err := validatedStrings(search.ModelTypes)
		if err != nil {
			return nil, err
		}
		conditions = append(conditions, modelTypeExpression+" = any(cast(:search_model_types as text[]))")
		args["search_model_types"] = pq.StringArray(values)
	}
	if len(search.Purposes) > 0 {
		values, err := validatedStrings(search.Purposes)
		if err != nil {
			return nil, err
		}
		conditions = append(conditions, modelPurposeExpression+" = any(cast(:search_purposes as text[]))")
		args["search_purposes"] = pq.StringArray(values)
	}
	if len(search.Sources) > 0 {
		condition, err := modelSourceCondition(search.Sources)
		if err != nil {
			return nil, err
		}
		if condition != "" {
			conditions = append(conditions, condition)
		}
	}
	return conditions, nil
}

// modelSourceCondition matches models from any of sources; asking for both sources is no filter.
func modelSourceCondition(sources []models.ModelSearchSource) (string, error) {
	archive, deposit := false, false
	for _, source := range sources {
		switch source {
		case models.ModelSearchSourcePDBArchive:
			archive = true
		case models.ModelSearchSourceDynamicPDBDeposit:
			deposit = true
		default:
			return "", fmt.Errorf("model source %q: %w", source, ErrInvalidModelSearch)
		}
	}
	switch {
	case archive && deposit:
		return "", nil
	case archive:
		return archiveModelExpression, nil
	default:
		return "not " + archiveModelExpression, nil
	}
}

func modelFileSearchConditions(search ModelSearch, args map[string]any) ([]string, error) {
	conditions := make([]string, 0)
	if len(search.DataLevels) > 0 {
		levels, err := validatedStrings(search.DataLevels)
		if err != nil {
			return nil, err
		}
		conditions = append(conditions, dataLevelCondition)
		args["search_data_levels"] = pq.StringArray(levels)
	}
	if len(search.Compounds) > 0 {
		compounds := make([]string, 0, len(search.Compounds))
		for _, compound := range search.Compounds {
			compounds = append(compounds, normalizedCompoundID(compound))
		}
		conditions = append(conditions, compoundCondition)
		args["search_compounds"] = pq.StringArray(compounds)
	}
	return conditions, nil
}

// dataLevelCondition counts the files attached to the model revision, its primary file included
// (researcher question 5). Entry files, such as the entry's FASTA (L0), do not count.
const dataLevelCondition = `(exists (select 1
		from artifacts search_primary_file
		where search_primary_file.id = model_revisions.primary_artifact_id
		  and search_primary_file.level = any(cast(:search_data_levels as text[])))
	or exists (select 1
		from model_revision_artifacts search_file_link
		join artifacts search_file on search_file.id = search_file_link.artifact_id
		where search_file_link.model_revision_id = model_revisions.id
		  and search_file.level = any(cast(:search_data_levels as text[]))))`

// compoundCondition matches a compound the model-compounds job read from the model's coordinate
// file. Records marked needs_review still match (researcher question 6); the response lists them.
// A file without a done set has no records, so it never matches.
const compoundCondition = `exists (select 1
		from modeled_compound_sets search_compound_set
		join modeled_compound_records search_compound on search_compound.artifact_id = search_compound_set.artifact_id
		where search_compound_set.artifact_id = model_revisions.primary_artifact_id
		  and search_compound_set.status = 'done'
		  and search_compound.ccd_id = any(cast(:search_compounds as text[])))`

// metricSearchConditions matches metric values by the metric's own key, as model responses list
// them. A revision whose duplicate values of one key are quarantined (model_revision_metrics.metric_key
// '<key>#duplicate#<metric id>') shows every one of them, so any of them may satisfy a bound and the
// metric is not missing.
func metricSearchConditions(search ModelSearch, args map[string]any) ([]string, error) {
	conditions := make([]string, 0)
	seen := make(map[models.MetricKey]bool, len(search.MetricBounds))
	for index, bound := range search.MetricBounds {
		if err := bound.Key.Validate(); err != nil {
			return nil, fmt.Errorf("metric bound: %w: %w", ErrInvalidModelSearch, err)
		}
		if seen[bound.Key] {
			return nil, fmt.Errorf("metric %s is bounded twice: %w", bound.Key, ErrInvalidModelSearch)
		}
		seen[bound.Key] = true
		if bound.Min == nil && bound.Max == nil {
			continue
		}
		prefix := fmt.Sprintf("search_metric_%d", index)
		valueConditions := make([]string, 0, 2)
		if bound.Min != nil {
			if err := checkFinite(string(bound.Key)+" minimum", *bound.Min); err != nil {
				return nil, err
			}
			valueConditions = append(valueConditions, fmt.Sprintf("search_metric_value.value >= :%s_min", prefix))
			args[prefix+"_min"] = *bound.Min
		}
		if bound.Max != nil {
			if err := checkFinite(string(bound.Key)+" maximum", *bound.Max); err != nil {
				return nil, err
			}
			valueConditions = append(valueConditions, fmt.Sprintf("search_metric_value.value <= :%s_max", prefix))
			args[prefix+"_max"] = *bound.Max
		}
		args[prefix+"_key"] = string(bound.Key)
		conditions = append(conditions, fmt.Sprintf(`exists (select 1
		from model_revision_metrics search_metric
		join metrics search_metric_value on search_metric_value.id = search_metric.metric_id
		where search_metric.model_revision_id = model_revisions.id
		  and search_metric_value.key = :%s_key
		  and %s)`, prefix, strings.Join(valueConditions, " and ")))
	}
	if len(search.MissingMetrics) > 0 {
		keys, err := validatedStrings(search.MissingMetrics)
		if err != nil {
			return nil, err
		}
		conditions = append(conditions, `exists (select 1
		from unnest(cast(:search_missing_metrics as text[])) as search_missing(metric_key)
		where not exists (select 1
			from model_revision_metrics search_present
			join metrics search_present_value on search_present_value.id = search_present.metric_id
			where search_present.model_revision_id = model_revisions.id
			  and search_present_value.key = search_missing.metric_key))`)
		args["search_missing_metrics"] = pq.StringArray(keys)
	}
	return conditions, nil
}

func reviewFlagSearchConditions(search ModelSearch, _ map[string]any) ([]string, error) {
	if search.Flagged == nil {
		return nil, nil
	}
	if *search.Flagged {
		return []string{flaggedModelCondition}, nil
	}
	return []string{"not " + flaggedModelCondition}, nil
}

// flaggedModelCondition matches a model listed by flaggedModelIDsQuery. The planner turns exists
// into a semi join and "not exists" into an anti join, both of which handle any number of flags.
// "not in" would not: once the flagged IDs outgrow work_mem, Postgres rescans the whole list for
// every model (minutes with the 64,000 entry flags the archive re-derivation once opened).
const flaggedModelCondition = `exists (select 1
	from (` + flaggedModelIDsQuery + `) as flagged_models (model_id)
	where flagged_models.model_id = models.id)`

// flaggedModelIDsQuery lists the models whose search result shows an open review flag
// (researcher question 7): a flag on the model, on its active revision, on its entry, or on a
// polymer entity of the entry's active revision. MigrationReviewsRepository.ListOpenForModelRevisions
// lists the same flags for one page of results.
//
// It starts from the open flags (schema_migration_review_open_idx) rather than testing each model:
// flagged models are few, so a capped count of "only flagged" would otherwise read every active
// model before it ends (more than 10 s on production, 10 Nov 2026).
const flaggedModelIDsQuery = `select flagged_model.object_id
	from schema_migration_review flagged_model
	where flagged_model.object_type = 'model' and flagged_model.resolved_at is null
	union all
	select flagged_revision_model.model_id
	from schema_migration_review flagged_revision
	join model_revisions flagged_revision_model
	  on cast(flagged_revision_model.id as text) = flagged_revision.object_id
	 and flagged_revision_model.state = 'active'
	where flagged_revision.object_type = 'model_revision' and flagged_revision.resolved_at is null
	union all
	select flagged_entry_model.id
	from schema_migration_review flagged_entry
	join models flagged_entry_model on flagged_entry_model.entry_id = flagged_entry.object_id
	where flagged_entry.object_type = 'entry' and flagged_entry.resolved_at is null
	union all
	select flagged_entity_model.id
	from schema_migration_review flagged_entity
	join entry_revision_polymer_entities flagged_entity_link
	  on cast(flagged_entity_link.polymer_entity_id as text) = flagged_entity.object_id
	join entry_revisions flagged_entity_revision
	  on flagged_entity_revision.id = flagged_entity_link.entry_revision_id
	 and flagged_entity_revision.state = 'active'
	join models flagged_entity_model on flagged_entity_model.entry_id = flagged_entity_revision.entry_id
	where flagged_entity.object_type = 'polymer_entity' and flagged_entity.resolved_at is null`

// ErrInvalidModelSearch marks a search the database layer refuses, for example an unknown
// vocabulary value or a bound that is not a finite number.
var ErrInvalidModelSearch = errors.New("db: invalid model search")

type validatable interface {
	~string
	Validate() error
}

func validatedStrings[T validatable](values []T) ([]string, error) {
	texts := make([]string, 0, len(values))
	for _, value := range values {
		if err := value.Validate(); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidModelSearch, err)
		}
		texts = append(texts, string(value))
	}
	return texts, nil
}

func checkFinite(what string, value float64) error {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return fmt.Errorf("%s %v is not a finite number: %w", what, value, ErrInvalidModelSearch)
	}
	return nil
}

// normalizedCompoundID is a chemical component ID as stored: trimmed and upper case.
func normalizedCompoundID(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func jsonArrayOf(value string) (string, error) {
	data, err := json.Marshal([]string{value})
	if err != nil {
		return "", fmt.Errorf("marshal json array: %w", err)
	}
	return string(data), nil
}
