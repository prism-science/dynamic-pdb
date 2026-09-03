package rcsb

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"dynamic-pdb/cli/internal/upload/extractors"
	"dynamic-pdb/cli/internal/upload/manifest"
	"dynamic-pdb/lib/rcsb"
)

type FieldExtractor struct {
	client rcsb.Client
}

var _ extractors.FieldExtractor = FieldExtractor{}

func NewFieldExtractor(client rcsb.Client) FieldExtractor {
	return FieldExtractor{client: client}
}

func (e FieldExtractor) Extract(
	ctx context.Context,
	pdbID string,
	source manifest.Source,
	extract manifest.Extract,
) (any, bool, error) {
	if rcsbSourceIsEmpty(source.RCSB) {
		return nil, false, nil
	}
	if extract.JSON == nil {
		return nil, false, nil
	}
	if e.client == nil {
		return nil, false, errors.New("RCSB client is required")
	}
	switch strings.TrimSpace(source.RCSB.Resource) {
	case "entry":
		entry, err := e.client.GetEntry(ctx, pdbID)
		if err != nil {
			if errors.Is(err, rcsb.ErrNotFound) {
				return nil, false, nil
			}
			return nil, false, err
		}
		return jsonField(entry, extract.JSON.Field)
	case "polymer_entity":
		return e.extractPolymerEntityField(ctx, pdbID, extract.JSON.Field)
	default:
		return nil, false, fmt.Errorf("unsupported RCSB field resource: %s", source.RCSB.Resource)
	}
}

func (e FieldExtractor) extractPolymerEntityField(ctx context.Context, pdbID string, field string) (any, bool, error) {
	entry, err := e.client.GetEntry(ctx, pdbID)
	if err != nil {
		if errors.Is(err, rcsb.ErrNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	entityIDs := stringSliceAtPath(entry, "rcsb_entry_container_identifiers.polymer_entity_ids")
	if len(entityIDs) == 0 {
		return nil, false, nil
	}
	values := make([]string, 0)
	for _, entityID := range entityIDs {
		entityID = strings.TrimSpace(entityID)
		if entityID == "" {
			continue
		}
		entity, err := e.client.GetPolymerEntity(ctx, pdbID, entityID)
		if err != nil {
			if errors.Is(err, rcsb.ErrNotFound) {
				continue
			}
			return nil, false, err
		}
		value, ok, err := jsonField(entity, field)
		if err != nil {
			return nil, false, err
		}
		if !ok {
			continue
		}
		values = appendStringValues(values, value)
	}
	if len(values) == 0 {
		return nil, false, nil
	}
	return strings.Join(uniqueStrings(values), "; "), true, nil
}

func rcsbSourceIsEmpty(source *manifest.RCSBSource) bool {
	return source == nil ||
		(strings.TrimSpace(source.PDBID) == "" &&
			strings.TrimSpace(source.Resource) == "" &&
			strings.TrimSpace(source.File) == "")
}

func jsonField(value any, field string) (any, bool, error) {
	field = strings.TrimSpace(field)
	if field == "" {
		return nil, false, nil
	}
	return valueAtPath(value, field)
}

func stringSliceAtPath(value any, path string) []string {
	raw, ok, err := valueAtPath(value, path)
	if err != nil || !ok {
		return nil
	}
	return appendStringValues(nil, raw)
}

func appendStringValues(values []string, value any) []string {
	switch typed := value.(type) {
	case string:
		if strings.TrimSpace(typed) != "" {
			values = append(values, strings.TrimSpace(typed))
		}
	case []any:
		for _, item := range typed {
			values = appendStringValues(values, item)
		}
	}
	return values
}

func uniqueStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func valueAtPath(value any, path string) (any, bool, error) {
	current := value
	for _, segment := range strings.Split(path, ".") {
		name, indexes, err := parseSegment(segment)
		if err != nil {
			return nil, false, err
		}
		if name != "" {
			next, ok := objectField(current, name)
			if !ok {
				return nil, false, nil
			}
			current = next
		}
		for _, index := range indexes {
			items, ok := current.([]any)
			if !ok || index < 0 || index >= len(items) {
				return nil, false, nil
			}
			current = items[index]
		}
	}
	return current, true, nil
}

func objectField(value any, name string) (any, bool) {
	object, ok := value.(map[string]any)
	if ok {
		result, ok := object[name]
		return result, ok
	}
	items, ok := value.([]any)
	if !ok {
		return nil, false
	}
	values := make([]any, 0, len(items))
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			continue
		}
		result, ok := object[name]
		if ok {
			values = append(values, result)
		}
	}
	if len(values) == 0 {
		return nil, false
	}
	return values, true
}

func parseSegment(segment string) (string, []int, error) {
	name := segment
	indexes := []int{}
	for {
		open := strings.Index(name, "[")
		if open < 0 {
			break
		}
		closeIndex := strings.Index(name[open:], "]")
		if closeIndex < 0 {
			return "", nil, fmt.Errorf("invalid JSON field segment: %s", segment)
		}
		closeIndex += open
		rawIndex := name[open+1 : closeIndex]
		index, err := strconv.Atoi(rawIndex)
		if err != nil {
			return "", nil, fmt.Errorf("invalid JSON field index %s: %w", rawIndex, err)
		}
		indexes = append(indexes, index)
		name = name[:open] + name[closeIndex+1:]
	}
	return name, indexes, nil
}
